package quota

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// asterGateBody is the shape a real /me/usage answers with (trimmed).
const asterGateBody = `{
  "today": {"requests": 1359, "billed_tokens": 182043084, "cost_usd": 79.498096},
  "week": {"requests": 6544, "cost_usd": 656.232061},
  "month": {"requests": 6544, "cost_usd": 656.232061},
  "accounts": [
    {"account_id": "acct-max", "provider": "claude", "plan": "max", "state": "available", "current": false,
     "checked_at": "2026-10-17T08:11:16.67467Z",
     "windows": [
       {"kind": "five_hour", "scope": "account", "used_ratio": 0.28, "resets_at": "2026-10-17T11:00:00Z"},
       {"kind": "weekly", "scope": "account", "used_ratio": 0.03, "resets_at": "2026-10-22T21:00:00Z"},
       {"kind": "weekly_fable", "scope": "model", "scoped_model": "Fable", "used_ratio": 0, "resets_at": "2026-10-22T21:00:00Z"}
     ]},
    {"account_id": "acct-pro", "provider": "claude", "plan": "pro", "state": "cooldown", "current": true,
     "checked_at": "2026-10-17T14:16:18.237725Z",
     "windows": [
       {"kind": "five_hour", "used_ratio": 0.22, "resets_at": "2026-10-17T18:00:00Z"},
       {"kind": "weekly", "used_ratio": null, "resets_at": "2026-10-19T17:00:00Z"}
     ]}
  ]
}`

func TestAsterGateReadsAccountsAndSpend(t *testing.T) {
	var gotAuth, gotSession, gotTZ string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/me/usage" {
			http.NotFound(w, r)
			return
		}
		gotAuth, gotSession, gotTZ = r.Header.Get("Authorization"), r.Header.Get("X-Claude-Code-Session-Id"), r.URL.Query().Get("timezone")
		w.Write([]byte(asterGateBody))
	}))
	defer srv.Close()

	r := Collect(context.Background(), srv.Client(), []Source{AsterGate("claude-pool", srv.URL+"/", "sk-1", "sess-1", "Etc/GMT-8")})[0]
	if r.Err != "" {
		t.Fatal(r.Err)
	}
	if gotAuth != "Bearer sk-1" || gotSession != "sess-1" || gotTZ != "Etc/GMT-8" {
		t.Errorf("request: auth=%q session=%q tz=%q", gotAuth, gotSession, gotTZ)
	}
	if r.Source != "astergate:claude-pool" || r.Usage.Today.CostUSD != 79.498096 || r.Usage.Today.Requests != 1359 {
		t.Errorf("report = %+v", r)
	}
	if len(r.Accounts) != 2 {
		t.Fatalf("accounts = %+v", r.Accounts)
	}
	max, pro := r.Accounts[0], r.Accounts[1]
	if max.State != "" || len(max.Windows) != 2 || max.Windows[0].Kind != "5h" || max.Windows[1].Kind != "week" {
		t.Errorf("max = %+v (model-scoped windows must be dropped, available is the default state)", max)
	}
	if pro.State != "cooldown" || !pro.Current || len(pro.Windows) != 1 || !reflect.DeepEqual(pro.Via, []string{"claude-pool"}) {
		t.Errorf("pro = %+v (a window without a ratio is not a window)", pro)
	}
}

func TestCollectIsolatesFailures(t *testing.T) {
	ok := Source{Name: "ok", Fetch: func(context.Context, *http.Client) (Report, error) {
		return Report{Accounts: []Account{{Platform: "codex"}}}, nil
	}}
	bad := Source{Name: "bad", Fetch: func(context.Context, *http.Client) (Report, error) {
		return Report{Accounts: []Account{{Platform: "x"}}}, errors.New("boom")
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "invalid key"}`))
	}))
	defer srv.Close()
	reports := Collect(context.Background(), srv.Client(), []Source{bad, ok, AsterGate("k", srv.URL, "sk", "", "")})
	if reports[0].Err != "boom" || len(reports[0].Accounts) != 0 {
		t.Errorf("failed source = %+v", reports[0])
	}
	if reports[1].Err != "" || len(reports[1].Accounts) != 1 {
		t.Errorf("healthy source = %+v", reports[1])
	}
	if !strings.Contains(reports[2].Err, "HTTP 401") || !strings.Contains(reports[2].Err, "invalid key") {
		t.Errorf("HTTP error = %q", reports[2].Err)
	}
}

func TestMergeFoldsAPoolSeenThroughTwoKeys(t *testing.T) {
	old := time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)
	reports := []Report{
		{Accounts: []Account{{Platform: "claude", ID: "a", Plan: "max", CheckedAt: old, Via: []string{"k1"}}}},
		{Accounts: []Account{
			{Platform: "claude", ID: "a", Plan: "max", CheckedAt: old.Add(time.Hour), Via: []string{"k2"}, Current: true},
			{Platform: "codex", Plan: "pro"},
		}},
	}
	got := Merge(reports)
	if len(got) != 2 || !got[0].CheckedAt.Equal(old.Add(time.Hour)) || !reflect.DeepEqual(got[0].Via, []string{"k1", "k2"}) || !got[0].Current {
		t.Fatalf("Merge = %+v", got)
	}
}

func TestTimezone(t *testing.T) {
	none := func(string) string { return "" }
	at := func(offset int) time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("x", offset)) }
	for offset, want := range map[int]string{8 * 3600: "Etc/GMT-8", -5 * 3600: "Etc/GMT+5", 0: "UTC", 5*3600 + 1800: "UTC"} {
		if got := Timezone(none, at(offset)); got != want {
			t.Errorf("offset %d: %s, want %s", offset, got, want)
		}
	}
	if Timezone(func(k string) string { return map[string]string{"TZ": "Asia/Shanghai"}[k] }, at(0)) != "Asia/Shanghai" {
		t.Error("TZ not honored")
	}
}

func TestWindowKindAndExpiry(t *testing.T) {
	for d, want := range map[time.Duration]string{5 * time.Hour: "5h", 7 * 24 * time.Hour: "week", 30 * 24 * time.Hour: "month", 48 * time.Hour: "2d", 2 * time.Hour: "2h"} {
		if got := windowKind(d); got != want {
			t.Errorf("windowKind(%v) = %s", d, got)
		}
	}
	now := time.Now()
	if !(Window{ResetsAt: now.Add(-time.Second)}).Expired(now) || (Window{}).Expired(now) {
		t.Error("Expired")
	}
	a := Account{Windows: []Window{{Used: 0.9, ResetsAt: now.Add(-time.Hour)}, {Used: 0.4, ResetsAt: now.Add(time.Hour)}}}
	if a.Pressure(now) != 0.4 {
		t.Errorf("an expired window counted toward pressure: %v", a.Pressure(now))
	}
}

func TestJWTExpiry(t *testing.T) {
	// header.payload.signature with payload {"exp":1792892730}
	tok := "eyJhbGciOiJub25lIn0.eyJleHAiOjE3OTI4OTI3MzB9.sig"
	if got := jwtExpiry(tok); got.Unix() != 1792892730 {
		t.Fatalf("jwtExpiry = %v", got)
	}
	if !jwtExpiry("not-a-jwt").IsZero() {
		t.Fatal("garbage parsed")
	}
}
