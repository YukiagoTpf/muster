package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// pinningGateway pins sessions to whichever account `current` names ("" = none).
func pinningGateway(t *testing.T, key string, current *atomic.Value, sessions *[]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		*sessions = append(*sessions, r.Header.Get("X-Claude-Code-Session-Id"))
		cur := current.Load().(string)
		w.Write([]byte(`{"today":{"cost_usd":12.5},"accounts":[
		  {"account_id":"max-1","provider":"claude","plan":"max","current":` + boolJSON(cur == "max-1") + `,
		   "windows":[{"kind":"five_hour","used_ratio":0.3,"resets_at":"2099-01-01T00:00:00Z"}]},
		  {"account_id":"pro-1","provider":"claude","plan":"pro","current":` + boolJSON(cur == "pro-1") + `,
		   "windows":[{"kind":"five_hour","used_ratio":0.6,"resets_at":"2099-01-01T00:00:00Z"}]}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func boolJSON(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestStatuslineFollowsTheSessionsAccount(t *testing.T) {
	sb := newSandbox(t)
	var current atomic.Value
	current.Store("")
	var sessions []string
	srv := pinningGateway(t, "sk-gw", &current, &sessions)
	if code, _, errOut := sb.run(t, "sk-gw", "cred", "add", "pool", "--provider", "astergate", "--base-url", srv.URL, "--key-stdin"); code != 0 {
		t.Fatal(errOut)
	}
	// The session carries the credential's provider env, as a launch sets it.
	sb.env["ANTHROPIC_BASE_URL"] = srv.URL + "/"
	sb.env["ANTHROPIC_AUTH_TOKEN"] = "sk-gw"

	var spawned []string
	defer func(orig func(IO, ...string) error) { spawnBackground = orig }(spawnBackground)
	spawnBackground = func(_ IO, args ...string) error { spawned = append(spawned, strings.Join(args, " ")); return nil }

	render := func() string {
		t.Helper()
		code, out, _ := sb.run(t, `{"session_id":"sess-1","model":{"id":"x"}}`, "statusline", "--lang", "zh")
		if code != 0 {
			t.Fatalf("statusline exit %d", code)
		}
		return ansiEscape.ReplaceAllString(strings.TrimSpace(out), "")
	}
	refresh := func() {
		t.Helper()
		if code, _, errOut := sb.run(t, "", "__refresh-statusline", "sess-1"); code != 0 {
			t.Fatalf("refresh: %s", errOut)
		}
	}

	if got := render(); got != "" || strings.Join(spawned, ",") != "__refresh-statusline sess-1" {
		t.Fatalf("first render: %q, spawned %v (nothing known yet: print nothing, refresh in the background)", got, spawned)
	}
	if render(); len(spawned) != 1 {
		t.Fatalf("a refresh already under way was started again: %v", spawned)
	}

	refresh()
	if got := render(); got != "额度：会话尚未分配账号" {
		t.Fatalf("unpinned: %q", got)
	}
	if strings.Join(sessions, ",") != "sess-1" {
		t.Fatalf("the gateway was not asked about this session: %v", sessions)
	}

	current.Store("max-1")
	refresh()
	if got := render(); !strings.HasPrefix(got, "Max  │  5小时 [▰▰▰▱▱▱▱▱▱▱] 30%") || !strings.HasSuffix(got, "今日 $12.50") {
		t.Fatalf("pinned: %q", got)
	}

	current.Store("pro-1")
	refresh()
	if got := render(); !strings.HasPrefix(got, "已换号 → Pro  │  5小时 [▰▰▰▰▰▰▱▱▱▱] 60%") {
		t.Fatalf("switched: %q", got)
	}
}

func TestStatuslineStaysSilentOffItsTurf(t *testing.T) {
	sb := newSandbox(t)
	var spawned []string
	defer func(orig func(IO, ...string) error) { spawnBackground = orig }(spawnBackground)
	spawnBackground = func(_ IO, args ...string) error { spawned = append(spawned, strings.Join(args, " ")); return nil }

	sb.env["ANTHROPIC_BASE_URL"] = "https://unregistered.example"
	sb.env["ANTHROPIC_AUTH_TOKEN"] = "sk-x"
	for _, stdin := range []string{`{"session_id":"s"}`, `not json`, `{"session_id":"../../etc"}`, ``} {
		if code, out, _ := sb.run(t, stdin, "statusline"); code != 0 || out != "" {
			t.Errorf("stdin %q: exit %d, out %q", stdin, code, out)
		}
	}
	if len(spawned) != 0 {
		t.Fatalf("spawned for a session that isn't on a registered gateway: %v", spawned)
	}
	if code, _, _ := sb.run(t, "", "statusline", "--lang", "fr"); code != 2 {
		t.Error("an unknown language is a usage error")
	}
}
