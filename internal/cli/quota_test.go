package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YukiagoTpf/muster/internal/quota"
)

// fakeGateway answers /me/usage like a gateway whose pool has one account.
func fakeGateway(t *testing.T, key string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"today":{"requests":3,"cost_usd":1.5},"week":{"cost_usd":2},"month":{"cost_usd":4},
		  "accounts":[{"account_id":"acct-1","provider":"claude","plan":"max","checked_at":"2026-10-17T08:00:00Z",
		    "windows":[{"kind":"five_hour","scope":"account","used_ratio":0.25,"resets_at":"2099-01-01T00:00:00Z"}]}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestQuotaReadsEveryGatewayCredentialOnce(t *testing.T) {
	sb := newSandbox(t)
	srv := fakeGateway(t, "sk-gw")
	for _, name := range []string{"pool-a", "pool-b"} {
		if code, _, errOut := sb.run(t, "sk-gw", "cred", "add", name, "--provider", "astergate", "--base-url", srv.URL, "--key-stdin"); code != 0 {
			t.Fatal(errOut)
		}
	}
	code, out, errOut := sb.run(t, "", "quota", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var o quota.Overview
	if err := json.Unmarshal([]byte(out), &o); err != nil {
		t.Fatal(err)
	}
	if len(o.Accounts) != 1 || strings.Join(o.Accounts[0].Via, ",") != "pool-a,pool-b" || o.Accounts[0].Windows[0].Used != 0.25 {
		t.Fatalf("accounts = %+v (one pool account reached through two keys)", o.Accounts)
	}
	if o.Usage["pool-a"].Today.CostUSD != 1.5 || len(o.Errors) != 0 {
		t.Fatalf("usage/errors = %+v %+v", o.Usage, o.Errors)
	}
	if strings.Contains(out, "sk-gw") {
		t.Fatal("quota printed the key")
	}

	// --cached shows the stored numbers without touching the network.
	srv.Close()
	_, cached, _ := sb.run(t, "", "quota", "--cached", "--json")
	if !strings.Contains(cached, `"acct-1"`) {
		t.Fatalf("cache = %s", cached)
	}
	_, human, _ := sb.run(t, "", "quota", "--cached")
	if !strings.Contains(human, "AsterGate · pool-a") || !strings.Contains(human, "max") || !strings.Contains(human, "25%") {
		t.Fatalf("human overview = %q", human)
	}
	if strings.Count(human, "acct-1") != 1 {
		t.Fatalf("the shared account is listed twice: %q", human)
	}
}

func TestQuotaReportsAFailingSourceAndGoesOn(t *testing.T) {
	sb := newSandbox(t)
	srv := fakeGateway(t, "sk-right")
	sb.run(t, "sk-wrong", "cred", "add", "broken", "--provider", "astergate", "--base-url", srv.URL, "--key-stdin")
	sb.run(t, "sk-right", "cred", "add", "fine", "--provider", "astergate", "--base-url", srv.URL, "--key-stdin")
	code, out, _ := sb.run(t, "", "quota", "--json")
	var o quota.Overview
	json.Unmarshal([]byte(out), &o)
	if code != 0 || len(o.Errors) != 1 || o.Errors[0].Source != "astergate:broken" || len(o.Accounts) != 1 {
		t.Fatalf("exit %d, overview %+v", code, o)
	}
}

func TestColorOutStripsEscapesOffTerminal(t *testing.T) {
	var b strings.Builder
	sys := colorOut(IO{Out: &b, Getenv: func(string) string { return "" }})
	sys.Out.Write([]byte("\x1b[1;32m25%\x1b[0m done"))
	if b.String() != "25% done" {
		t.Fatalf("got %q", b.String())
	}
}

func TestQuotaReadsTheCodexLoginOnlyWhereItExists(t *testing.T) {
	sb := newSandbox(t)
	_, out, _ := sb.run(t, "", "quota", "--json")
	if !strings.Contains(out, `"accounts": []`) || !strings.Contains(out, `"errors": []`) {
		t.Fatalf("no logins anywhere should mean no sources and no errors: %s", out)
	}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":50,"limit_window_seconds":18000,"reset_at":4102444800}}}`))
	}))
	defer api.Close()
	sb.env["MUSTER_CODEX_API"] = api.URL
	auth := filepath.Join(sb.env["HOME"], ".codex", "auth.json")
	os.MkdirAll(filepath.Dir(auth), 0o700)
	os.WriteFile(auth, []byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"t","account_id":"acct-c"}}`), 0o600)
	_, out, _ = sb.run(t, "", "quota", "--json")
	var o quota.Overview
	json.Unmarshal([]byte(out), &o)
	if len(o.Accounts) != 1 || o.Accounts[0].Platform != "codex" || o.Accounts[0].Plan != "plus" || o.Accounts[0].Windows[0].Kind != "5h" {
		t.Fatalf("overview = %+v", o)
	}
}
