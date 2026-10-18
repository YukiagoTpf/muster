package web

import (
	"bufio"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YukiagoTpf/muster/internal/claude"
	"github.com/YukiagoTpf/muster/internal/syncrepo"
)

func TestMain(m *testing.M) {
	syncrepo.ScryptWorkFactor = 10
	os.Exit(m.Run())
}

type site struct {
	t      *testing.T
	srv    *Server
	ts     *httptest.Server
	env    map[string]string
	origin string
}

// newSite serves a sandboxed machine: every data, config and home directory
// is a temp dir, and the real environment's provider keys are gone.
func newSite(t *testing.T) *site {
	t.Helper()
	root := t.TempDir()
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, _ := strings.Cut(kv, "="); k != "" && !claude.IsProviderEnvKey(k) {
			env[k] = v
		}
	}
	home := filepath.Join(root, "home")
	env["MUSTER_HOME"] = filepath.Join(root, "muster")
	env["CLAUDE_CONFIG_DIR"] = filepath.Join(home, ".claude")
	env["HOME"], env["USERPROFILE"] = home, home
	env["APPDATA"] = filepath.Join(home, "AppData", "Roaming")
	env["XDG_CONFIG_HOME"] = filepath.Join(home, ".config")
	delete(env, "CODEX_HOME")
	ts := httptest.NewUnstartedServer(nil)
	port := ts.Listener.Addr().(*net.TCPAddr).Port
	srv, err := New(Env{
		Getenv: func(k string) string { return env[k] },
		Environ: func() []string {
			var out []string
			for k, v := range env {
				out = append(out, k+"="+v)
			}
			return out
		},
	}, port)
	if err != nil {
		t.Fatal(err)
	}
	ts.Config.Handler = srv
	ts.Start()
	t.Cleanup(ts.Close)
	return &site{t, srv, ts, env, ts.URL}
}

func (s *site) writeSettings(content string) {
	s.t.Helper()
	p := filepath.Join(s.env["CLAUDE_CONFIG_DIR"], "settings.json")
	os.MkdirAll(filepath.Dir(p), 0o700)
	os.WriteFile(p, []byte(content), 0o600)
}

func (s *site) settingsFile() string {
	s.t.Helper()
	b, _ := os.ReadFile(filepath.Join(s.env["CLAUDE_CONFIG_DIR"], "settings.json"))
	return string(b)
}

// call makes an API request with the session's token; out, when given,
// receives the decoded body.
func (s *site) call(method, path, body string, out any) int {
	s.t.Helper()
	req, _ := http.NewRequest(method, s.ts.URL+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+s.srv.token)
	if method != http.MethodGet {
		req.Header.Set("Origin", s.origin)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func TestTheAPINeedsTokenHostAndOrigin(t *testing.T) {
	s := newSite(t)
	do := func(method, path, token, host, origin string) int {
		req, _ := http.NewRequest(method, s.ts.URL+path, strings.NewReader("{}"))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if host != "" {
			req.Host = host
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	tok := s.srv.token
	for _, c := range []struct {
		name                       string
		method, token, host, orign string
		want                       int
	}{
		{"no token", "GET", "", "", "", 401},
		{"wrong token", "GET", "nope", "", "", 401},
		{"token", "GET", tok, "", "", 200},
		{"a rebinding host", "GET", tok, "evil.example:80", "", 421},
		{"mutation without origin", "POST", tok, "", "", 403},
		{"mutation from another site", "POST", tok, "", "https://evil.example", 403},
	} {
		if got := do(c.method, "/api/default", c.token, c.host, c.orign); got != c.want && !(c.want == 200 && got == 405) {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
	// The page itself needs no token: it holds no data.
	if got := do("GET", "/", "", "", ""); got == 401 || got == 403 {
		t.Errorf("the page: %d", got)
	}
	if !strings.Contains(s.srv.URL(), "/#token="+tok) {
		t.Errorf("URL = %s", s.srv.URL())
	}
}

func TestCredentialsThroughTheAPI(t *testing.T) {
	s := newSite(t)
	s.writeSettings(`{"env":{"ANTHROPIC_BASE_URL":"https://gw.example.com","ANTHROPIC_AUTH_TOKEN":"sk-default","ANTHROPIC_DEFAULT_OPUS_MODEL":"o"},"hooks":{}}`)
	var list struct {
		Credentials []struct {
			Name   string
			HasKey bool
			Models []struct{ Alias, Model string }
		}
		Default struct{ State, Credential string }
	}
	if code := s.call("GET", "/api/credentials", "", &list); code != 200 || list.Default.State != "unregistered" {
		t.Fatalf("list: %d %+v", code, list)
	}
	if code := s.call("POST", "/api/credentials/import", `{"name":"pool","provider":"astergate"}`, nil); code != 200 {
		t.Fatalf("import: %d", code)
	}
	if code := s.call("POST", "/api/credentials", `{"name":"other","provider":"astergate","baseURL":"https://o.example","key":"sk-o","env":{"ANTHROPIC_DEFAULT_HAIKU_MODEL":"h"}}`, nil); code != 200 {
		t.Fatalf("add: %d", code)
	}
	var e apiError
	if code := s.call("POST", "/api/credentials", `{"name":"bad","provider":"astergate","baseURL":"https://o.example","key":"k","env":{"ANTHROPIC_API_KEY":"x"}}`, &e); code != 400 || !strings.Contains(e.Error, "secret") {
		t.Fatalf("a secret in env: %d %+v", code, e)
	}
	if code := s.call("PUT", "/api/credentials/other", `{"provider":"astergate","baseURL":"https://o2.example","env":{}}`, nil); code != 200 {
		t.Fatalf("edit: %d", code)
	}
	if code := s.call("PUT", "/api/credentials/nope", `{"provider":"astergate","baseURL":"https://o2.example"}`, nil); code != 404 {
		t.Fatalf("edit unknown: %d", code)
	}
	s.call("GET", "/api/credentials", "", &list)
	if list.Default.State != "match" || list.Default.Credential != "pool" || len(list.Credentials) != 2 || list.Credentials[0].Name != "other" || !list.Credentials[0].HasKey || len(list.Credentials[0].Models) != 0 {
		t.Fatalf("after the changes: %+v", list)
	}
	if code := s.call("POST", "/api/default", `{"credential":"other"}`, nil); code != 200 || !strings.Contains(s.settingsFile(), "sk-o") {
		t.Fatalf("default: %d\n%s", code, s.settingsFile())
	}
	if code := s.call("DELETE", "/api/credentials/pool", "", nil); code != 200 {
		t.Fatalf("remove: %d", code)
	}
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), "sk-") {
		t.Fatal("a key reached the page")
	}
}

func TestSettingsEditorSavesWithCompareAndSwap(t *testing.T) {
	s := newSite(t)
	s.writeSettings(`{"env":{"ANTHROPIC_AUTH_TOKEN":"sk-default","DISABLE_AUTOUPDATER":"1"},"hooks":{}}`)
	var v settingsView
	if code := s.call("GET", "/api/settings", "", &v); code != 200 || !v.Doc.Local || strings.Contains(v.Doc.Content, "sk-default") || len(v.Platforms) != 1 {
		t.Fatalf("get: %d %+v", code, v)
	}
	body := func(content, hash string) string {
		b, _ := json.Marshal(map[string]string{"content": content, "hash": hash})
		return string(b)
	}
	edited := strings.Replace(v.Doc.Content, `"DISABLE_AUTOUPDATER": "1"`, `"DISABLE_AUTOUPDATER": "0"`, 1)
	if code := s.call("PUT", "/api/settings", body(edited, "stale"), nil); code != 409 {
		t.Fatalf("stale: %d", code)
	}
	if code := s.call("PUT", "/api/settings", body(edited, v.Doc.Hash), nil); code != 200 {
		t.Fatalf("save: %d", code)
	}
	if f := s.settingsFile(); !strings.Contains(f, `"DISABLE_AUTOUPDATER": "0"`) || !strings.Contains(f, "sk-default") {
		t.Fatalf("saved:\n%s", f)
	}
	if code := s.call("GET", "/api/settings?platform=macos", "", nil); code != 400 {
		t.Fatalf("another platform without sync: %d", code)
	}
}

func TestSetupRunsStreamTheirOutput(t *testing.T) {
	s := newSite(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	m, _ := s.srv.machine()
	if _, _, err := syncrepo.Join(m, remote, func(bool) (string, error) { return "pw", nil }); err != nil {
		t.Fatal(err)
	}
	if code := s.call("POST", "/api/setup", `{"name":"hello","install":"echo hello from setup"}`, nil); code != 200 {
		t.Fatalf("add: %d", code)
	}
	req, _ := http.NewRequest("POST", s.ts.URL+"/api/setup/install", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+s.srv.token)
	req.Header.Set("Origin", s.origin)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var output, results []string
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		var e streamEvent
		json.Unmarshal(sc.Bytes(), &e)
		switch e.Type {
		case "output":
			output = append(output, e.Text)
		case "result":
			results = append(results, e.Result.Name+":"+e.Result.Action)
		case "done":
			if e.Error != "" {
				t.Fatal(e.Error)
			}
		}
	}
	if !strings.Contains(strings.Join(output, ""), "hello from setup") || strings.Join(results, ",") != "hello:installed" {
		t.Fatalf("output %q, results %v", output, results)
	}
	var list struct{ Entries []setupEntryView }
	if s.call("GET", "/api/setup", "", &list); len(list.Entries) != 1 || !list.Entries[0].Installed {
		t.Fatalf("after install: %+v", list)
	}
	var st struct {
		SetUp   bool
		Pending []struct{ Kind string }
	}
	if s.call("GET", "/api/sync", "", &st); !st.SetUp || len(st.Pending) != 1 || st.Pending[0].Kind != "setup" {
		t.Fatalf("sync status: %+v", st)
	}
}
