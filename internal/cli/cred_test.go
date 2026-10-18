package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YukiagoTpf/muster/internal/claude"
	"github.com/YukiagoTpf/muster/internal/credential"
	"github.com/YukiagoTpf/muster/internal/store"
)

// sandbox is an isolated MUSTER_HOME and CLAUDE_CONFIG_DIR.
type sandbox struct {
	env       map[string]string
	claudeDir string
}

func newSandbox(t *testing.T) *sandbox {
	t.Helper()
	root := t.TempDir()
	sb := &sandbox{claudeDir: filepath.Join(root, "claude")}
	os.MkdirAll(sb.claudeDir, 0o700)
	// The real environment minus every provider key (this machine's own
	// gateway key must never reach a test), so a spawned agent can run.
	sb.env = map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, _ := strings.Cut(kv, "="); k != "" && !claude.IsProviderEnvKey(k) {
			sb.env[k] = v
		}
	}
	sb.env["MUSTER_HOME"] = filepath.Join(root, "muster")
	sb.env["CLAUDE_CONFIG_DIR"] = sb.claudeDir
	// Every home and config root points into the sandbox too, so no test can
	// read this machine's own logins (Codex, Cursor, Grok) or call their APIs.
	home := filepath.Join(root, "home")
	for _, k := range []string{"HOME", "USERPROFILE"} {
		sb.env[k] = home
	}
	sb.env["APPDATA"] = filepath.Join(home, "AppData", "Roaming")
	sb.env["XDG_CONFIG_HOME"] = filepath.Join(home, ".config")
	delete(sb.env, "CODEX_HOME")
	return sb
}

func (sb *sandbox) writeSettings(t *testing.T, settings string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(sb.claudeDir, "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
}

// run invokes muster in the sandbox; stdin, when given, is fed to the command.
func (sb *sandbox) run(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	in := filepath.Join(t.TempDir(), "stdin")
	os.WriteFile(in, []byte(stdin), 0o600)
	f, err := os.Open(in)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	return runWith(t, sb.env, f, args...)
}

const astergateSettings = `{
  "env": {
    "ANTHROPIC_AUTH_TOKEN": "sk-secret-123",
    "ANTHROPIC_BASE_URL": "https://gw.example.com",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "claude-opus-5-5[1M]",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "claude-haiku-5-5",
    "ENABLE_TOOL_SEARCH": "true"
  },
  "hooks": {}
}`

func TestCredAddFromSettingsListRemove(t *testing.T) {
	sb := newSandbox(t)
	sb.writeSettings(t, astergateSettings)

	code, out, errOut := sb.run(t, "", "cred", "add", "claude-pool", "--provider", "astergate", "--from-settings")
	if code != 0 {
		t.Fatalf("add: exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "added claude-pool") || !strings.Contains(out, "opus haiku") {
		t.Errorf("add output = %q", out)
	}

	code, out, _ = sb.run(t, "", "cred", "list", "--json")
	if code != 0 {
		t.Fatalf("list: exit %d", code)
	}
	var list struct {
		Version     int               `json:"version"`
		Credentials []credential.View `json:"credentials"`
	}
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatal(err)
	}
	if list.Version != 1 || len(list.Credentials) != 1 {
		t.Fatalf("list = %+v", list)
	}
	c := list.Credentials[0]
	if c.BaseURL != "https://gw.example.com" || !c.HasKey || len(c.Models) != 2 || c.Models[0].Alias != "opus" {
		t.Errorf("imported credential = %+v", c)
	}
	if _, ok := c.Env["ENABLE_TOOL_SEARCH"]; ok {
		t.Error("shared setting ENABLE_TOOL_SEARCH was imported into the credential")
	}
	if strings.Contains(out, "sk-secret-123") {
		t.Fatal("cred list --json printed the key")
	}

	_, human, _ := sb.run(t, "", "cred", "list")
	if !strings.Contains(human, "claude-pool") || strings.Contains(human, "sk-secret-123") {
		t.Errorf("human list = %q", human)
	}

	if code, _, _ := sb.run(t, "", "cred", "rm", "claude-pool"); code != 0 {
		t.Fatalf("rm: exit %d", code)
	}
	_, out, _ = sb.run(t, "", "cred", "list", "--json")
	if !strings.Contains(out, `"credentials": []`) {
		t.Errorf("after rm: %s", out)
	}
}

func TestCredAddFromFlagsWithKeyOnStdin(t *testing.T) {
	sb := newSandbox(t)
	code, _, errOut := sb.run(t, "sk-from-stdin\n", "cred", "add", "codex-pool", "--provider", "astergate",
		"--base-url", "https://gw.example.com", "--model", "opus=gpt-x", "--model", "haiku=gpt-x-mini", "--key-stdin", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	s := openStoreFor(t, sb)
	b, _ := os.ReadFile(s.Path("secrets.json"))
	if !strings.Contains(string(b), "sk-from-stdin") || strings.Contains(string(b), "sk-from-stdin\\n") {
		t.Fatalf("stored key = %s", b)
	}
}

func TestCredAddRefusals(t *testing.T) {
	sb := newSandbox(t)
	for name, args := range map[string][]string{
		"no provider":         {"cred", "add", "x", "--base-url", "https://gw", "--key-stdin"},
		"no base url":         {"cred", "add", "x", "--provider", "astergate", "--key-stdin"},
		"no key":              {"cred", "add", "x", "--provider", "astergate", "--base-url", "https://gw"},
		"no name":             {"cred", "add", "--provider", "astergate"},
		"secret env via flag": {"cred", "add", "x", "--provider", "astergate", "--base-url", "https://gw", "--env", "ANTHROPIC_API_KEY=sk", "--key-stdin"},
	} {
		if code, _, _ := sb.run(t, "sk", args...); code == 0 {
			t.Errorf("%s: accepted", name)
		}
	}
	sb.writeSettings(t, `{"env":{"ANTHROPIC_BASE_URL":"https://gw"}}`)
	if code, _, errOut := sb.run(t, "", "cred", "add", "x", "--provider", "astergate", "--from-settings"); code == 0 || !strings.Contains(errOut, "--key-stdin") {
		t.Errorf("import without a token: exit %d, %q", code, errOut)
	}
}

func TestCredRmUnknown(t *testing.T) {
	sb := newSandbox(t)
	if code, _, errOut := sb.run(t, "", "cred", "rm", "ghost"); code != 1 || !strings.Contains(errOut, `no credential named "ghost"`) {
		t.Fatalf("exit %d: %q", code, errOut)
	}
}

func openStoreFor(t *testing.T, sb *sandbox) *store.Store {
	t.Helper()
	s, err := store.Open(func(k string) string { return sb.env[k] })
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCredEdit(t *testing.T) {
	sb := newSandbox(t)
	if code, _, errOut := sb.run(t, "sk-1", "cred", "add", "pool", "--provider", "astergate", "--base-url", "https://a.example",
		"--model", "opus=o1", "--model", "haiku=h1", "--key-stdin"); code != 0 {
		t.Fatal(errOut)
	}
	code, out, errOut := sb.run(t, "", "cred", "edit", "pool", "--base-url", "https://b.example", "--model", "opus=o2", "--unset-model", "haiku", "--json")
	if code != 0 {
		t.Fatalf("edit: %s", errOut)
	}
	var got struct{ Credential credential.View }
	json.Unmarshal([]byte(out), &got)
	if c := got.Credential; c.BaseURL != "https://b.example" || len(c.Models) != 1 || c.Models[0].Model != "o2" || !c.HasKey {
		t.Fatalf("edited = %+v", c)
	}
	if code, _, _ := sb.run(t, "sk-2", "cred", "edit", "pool", "--key-stdin"); code != 0 {
		t.Fatal("replace the key")
	}
	if secrets := readFile(t, filepath.Join(sb.env["MUSTER_HOME"], "secrets.json")); !strings.Contains(secrets, "sk-2") {
		t.Fatal("the key was not replaced")
	}
	if code, _, errOut := sb.run(t, "", "cred", "edit", "pool", "--env", "ANTHROPIC_API_KEY=x"); code != 1 || !strings.Contains(errOut, "secret") {
		t.Fatalf("a secret in env: exit %d, %s", code, errOut)
	}
	if code, _, _ := sb.run(t, "", "cred", "edit", "nope", "--base-url", "https://c.example"); code != 1 {
		t.Fatal("editing an unknown credential")
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
