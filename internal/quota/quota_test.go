package quota

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// jwt is an unsigned JWT whose exp claim is exp.
func jwt(exp time.Time) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":` + strconv.FormatInt(exp.Unix(), 10) + `}`))
	return "eyJhbGciOiJub25lIn0." + payload + ".sig"
}

func TestCodexReadsTheCLILoginWithoutRefreshing(t *testing.T) {
	tok := jwt(time.Now().Add(time.Hour))
	var gotAuth, gotAccount string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backend-api/wham/usage" {
			t.Errorf("Codex asked %s (only the read-only usage endpoint is allowed)", r.URL.Path)
		}
		gotAuth, gotAccount = r.Header.Get("Authorization"), r.Header.Get("ChatGPT-Account-Id")
		w.Write([]byte(`{"plan_type":"pro","rate_limit":{"limit_reached":false,
		  "primary_window":{"used_percent":12.5,"limit_window_seconds":604800,"reset_at":1792896075},
		  "secondary_window":{"used_percent":40,"limit_window_seconds":18000,"reset_at":1792800000}},
		  "credits":{"has_credits":true,"unlimited":false,"balance":"62500.0000000000"}}`))
	}))
	defer srv.Close()
	auth := filepath.Join(t.TempDir(), "auth.json")
	original := `{"auth_mode":"chatgpt","tokens":{"access_token":"` + tok + `","refresh_token":"rt","account_id":"acct-9"},"last_refresh":"x"}`
	writeFile(t, auth, original)

	r := Collect(context.Background(), srv.Client(), []Source{Codex(auth, srv.URL+"/backend-api")})[0]
	if r.Err != "" {
		t.Fatal(r.Err)
	}
	if gotAuth != "Bearer "+tok || gotAccount != "acct-9" {
		t.Errorf("auth = %q, account = %q", gotAuth, gotAccount)
	}
	a := r.Accounts[0]
	if a.Platform != "codex" || a.Plan != "pro" || a.Note != "credits 62500" || len(a.Windows) != 2 {
		t.Fatalf("account = %+v", a)
	}
	if a.Windows[0].Kind != "week" || a.Windows[0].Used != 0.125 || a.Windows[0].ResetsAt.Unix() != 1792896075 || a.Windows[1].Kind != "5h" {
		t.Errorf("windows = %+v", a.Windows)
	}
	if b, _ := os.ReadFile(auth); string(b) != original {
		t.Fatal("the Codex login file was modified")
	}
}

func TestCodexRefusals(t *testing.T) {
	dir := t.TempDir()
	missing := Collect(context.Background(), http.DefaultClient, []Source{Codex(filepath.Join(dir, "none.json"), "http://127.0.0.1:1")})[0]
	if missing.Err != ErrNoLogin.Error() {
		t.Errorf("missing login: %q", missing.Err)
	}
	expired := filepath.Join(dir, "expired.json")
	writeFile(t, expired, `{"auth_mode":"chatgpt","tokens":{"access_token":"`+jwt(time.Now().Add(-time.Minute))+`","account_id":"a"}}`)
	if r := Collect(context.Background(), http.DefaultClient, []Source{Codex(expired, "http://127.0.0.1:1")})[0]; !strings.Contains(r.Err, "open codex once") {
		t.Errorf("expired login: %q", r.Err)
	}
	apiKey := filepath.Join(dir, "apikey.json")
	writeFile(t, apiKey, `{"auth_mode":"apikey","OPENAI_API_KEY":"sk"}`)
	if r := Collect(context.Background(), http.DefaultClient, []Source{Codex(apiKey, "http://127.0.0.1:1")})[0]; !strings.Contains(r.Err, "ChatGPT account") {
		t.Errorf("api key login: %q", r.Err)
	}
}

func TestGrokReadsCreditsWithoutRefreshing(t *testing.T) {
	var gotAuth, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotQuery = r.Header.Get("Authorization"), r.URL.RawQuery
		w.Write([]byte(`{"config":{"currentPeriod":{"type":"USAGE_PERIOD_TYPE_WEEKLY","start":"2026-10-11T14:17:40.119067+00:00","end":"2026-10-18T14:17:40.119067+00:00"},
		  "creditUsagePercent":12,"productUsage":[{"product":"GrokChat","usagePercent":12},{"product":"GrokBuild"}]}}`))
	}))
	defer srv.Close()
	auth := filepath.Join(t.TempDir(), "auth.json")
	original := `{"https://auth.x.ai::client":{"key":"gk","auth_mode":"oidc","refresh_token":"rt","expires_at":"` + time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano) + `"}}`
	writeFile(t, auth, original)

	r := Collect(context.Background(), srv.Client(), []Source{Grok(auth, srv.URL)})[0]
	if r.Err != "" {
		t.Fatal(r.Err)
	}
	if gotAuth != "Bearer gk" || gotQuery != "format=credits" {
		t.Errorf("auth = %q, query = %q", gotAuth, gotQuery)
	}
	a := r.Accounts[0]
	if a.Platform != "grok" || len(a.Windows) != 1 || a.Windows[0].Kind != "week" || a.Windows[0].Used != 0.12 || a.Note != "GrokChat 12%" {
		t.Fatalf("account = %+v", a)
	}
	if b, _ := os.ReadFile(auth); string(b) != original {
		t.Fatal("the Grok login file was modified")
	}
}

func TestGrokReportsAnExpiredLogin(t *testing.T) {
	auth := filepath.Join(t.TempDir(), "auth.json")
	writeFile(t, auth, `{"https://auth.x.ai::c":{"key":"gk","expires_at":"2020-01-01T00:00:00Z"}}`)
	if r := Collect(context.Background(), http.DefaultClient, []Source{Grok(auth, "http://127.0.0.1:1")})[0]; !strings.Contains(r.Err, "open grok once") {
		t.Fatalf("expired: %q", r.Err)
	}
}

func TestCursorReadsTheEditorLoginThroughWAL(t *testing.T) {
	tok := jwt(time.Now().Add(time.Hour))
	statePath := filepath.Join(t.TempDir(), "state.vscdb")
	// The editor's side: a WAL database it keeps open. The token is written
	// and not checkpointed, so it exists only in the -wal file.
	editor, err := sql.Open("sqlite", "file:"+filepath.ToSlash(statePath))
	if err != nil {
		t.Fatal(err)
	}
	defer editor.Close()
	editor.SetMaxOpenConns(1)
	for _, q := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA wal_autocheckpoint=0`,
		`CREATE TABLE ItemTable (key TEXT UNIQUE ON CONFLICT REPLACE, value BLOB)`,
	} {
		if _, err := editor.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := editor.Exec(`INSERT INTO ItemTable VALUES ('cursorAuth/accessToken', ?)`, tok); err != nil {
		t.Fatal(err)
	}
	mainBefore, _ := os.ReadFile(statePath)

	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer "+tok || r.Header.Get("Connect-Protocol-Version") != "1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/aiserver.v1.DashboardService/GetCurrentPeriodUsage":
			w.Write([]byte(`{"billingCycleStart":"1792175544000","billingCycleEnd":"1794853944000","planUsage":{"totalSpend":164,"includedSpend":164,"remaining":39836,"limit":40000,"totalPercentUsed":0.0529}}`))
		case "/aiserver.v1.DashboardService/GetPlanInfo":
			w.Write([]byte(`{"planInfo":{"planName":"Ultra","price":"$200/mo"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	r := Collect(context.Background(), srv.Client(), []Source{Cursor(statePath, srv.URL)})[0]
	if r.Err != "" {
		t.Fatal(r.Err)
	}
	a := r.Accounts[0]
	if a.Platform != "cursor" || a.Plan != "Ultra" || len(a.Windows) != 1 || a.Windows[0].Kind != "month" || a.Windows[0].Used != 0.0041 {
		t.Fatalf("account = %+v (used must come from totalSpend / limit)", a)
	}
	if a.Note != "$1.64 of $400.00 included" || a.Windows[0].ResetsAt.UnixMilli() != 1794853944000 {
		t.Errorf("note = %q, resets = %v", a.Note, a.Windows[0].ResetsAt)
	}
	if strings.Join(calls, ",") != "POST /aiserver.v1.DashboardService/GetCurrentPeriodUsage,POST /aiserver.v1.DashboardService/GetPlanInfo" {
		t.Errorf("calls = %v", calls)
	}
	if mainAfter, _ := os.ReadFile(statePath); !reflect.DeepEqual(mainBefore, mainAfter) {
		t.Fatal("reading the login modified the editor's database")
	}
}

func TestCursorWithoutALogin(t *testing.T) {
	dir := t.TempDir()
	if r := Collect(context.Background(), http.DefaultClient, []Source{Cursor(filepath.Join(dir, "none.vscdb"), "http://127.0.0.1:1")})[0]; r.Err != ErrNoLogin.Error() {
		t.Fatalf("missing db: %q", r.Err)
	}
	statePath := filepath.Join(dir, "state.vscdb")
	db, _ := sql.Open("sqlite", "file:"+filepath.ToSlash(statePath))
	db.Exec(`CREATE TABLE ItemTable (key TEXT, value BLOB)`)
	db.Close()
	if r := Collect(context.Background(), http.DefaultClient, []Source{Cursor(statePath, "http://127.0.0.1:1")})[0]; r.Err != ErrNoLogin.Error() {
		t.Fatalf("logged out: %q", r.Err)
	}
}
