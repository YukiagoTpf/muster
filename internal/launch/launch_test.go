package launch

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/YukiagoTpf/muster/internal/fakeagent"
	"github.com/YukiagoTpf/muster/internal/store"
)

// TestMain doubles as a fake agent (see fakeagent).
func TestMain(m *testing.M) {
	fakeagent.Main()
	os.Exit(m.Run())
}

func TestBuildOwnsTheProviderEnv(t *testing.T) {
	provider := map[string]string{
		"ANTHROPIC_BASE_URL":           "https://codex.example",
		"ANTHROPIC_AUTH_TOKEN":         "sk-codex",
		"ANTHROPIC_DEFAULT_OPUS_MODEL": "gpt-x",
	}
	defaults := map[string]string{
		"ANTHROPIC_BASE_URL":             "https://claude.example",
		"ANTHROPIC_AUTH_TOKEN":           "sk-default",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   "claude-opus-5-5[1M]",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "claude-sonnet-5-5[1M]",
		"ANTHROPIC_CUSTOM_HEADERS":       "x-team: a",
	}
	parent := []string{"PATH=/bin", "ANTHROPIC_API_KEY=sk-inherited", "CLAUDE_CODE_OAUTH_TOKEN=oauth", "CLAUDE_CODE_MAX_OUTPUT_TOKENS=9", "KEEP_ME=1"}
	p := Build("codex-pool", provider, defaults, parent, "opus", []string{"--resume"})

	want := map[string]string{
		"ANTHROPIC_BASE_URL":             "https://codex.example",
		"ANTHROPIC_AUTH_TOKEN":           "sk-codex",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   "gpt-x",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "",
		"ANTHROPIC_CUSTOM_HEADERS":       "",
	}
	if !reflect.DeepEqual(p.SettingsEnv, want) {
		t.Errorf("settings env = %v", p.SettingsEnv)
	}
	if !reflect.DeepEqual(p.Blanked, []string{"ANTHROPIC_CUSTOM_HEADERS", "ANTHROPIC_DEFAULT_SONNET_MODEL"}) {
		t.Errorf("blanked = %v", p.Blanked)
	}
	if !reflect.DeepEqual(p.Stripped, []string{"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN"}) {
		t.Errorf("stripped = %v", p.Stripped)
	}
	if !reflect.DeepEqual(p.Env, []string{"PATH=/bin", "CLAUDE_CODE_MAX_OUTPUT_TOKENS=9", "KEEP_ME=1"}) {
		t.Errorf("child env = %v", p.Env)
	}
	if got := p.Args("/run/s.json"); !reflect.DeepEqual(got, []string{"--settings", "/run/s.json", "--model", "opus", "--resume"}) {
		t.Errorf("args = %v", got)
	}
	if got := (Plan{}).Args("/s"); !reflect.DeepEqual(got, []string{"--settings", "/s"}) {
		t.Errorf("no model: args = %v", got)
	}
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(func(k string) string {
		if k == "MUSTER_HOME" {
			return dir
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRunPassesPrivateSettingsAndExitCode(t *testing.T) {
	s := openStore(t)
	out := filepath.Join(t.TempDir(), "agent.json")
	p := Build("c", map[string]string{"ANTHROPIC_BASE_URL": "https://gw", "ANTHROPIC_AUTH_TOKEN": "sk-1"},
		map[string]string{"ANTHROPIC_DEFAULT_OPUS_MODEL": "m"},
		append(os.Environ(), "MUSTER_FAKE_AGENT_OUT="+out, "MUSTER_FAKE_AGENT_EXIT=7", "ANTHROPIC_API_KEY=leak", "KEEP_ME=1"),
		"opus", nil)

	// A stale file from a dead muster is collected; nothing else is touched.
	os.MkdirAll(s.Path("run"), 0o700)
	stale := filepath.Join(s.Path("run"), "999999999-abc123.json")
	os.WriteFile(stale, []byte("{}"), 0o600)

	var stdout bytes.Buffer
	code, err := Run(s, p, os.Args[0], Stdio{Out: &stdout, Err: &stdout})
	if err != nil {
		t.Fatal(err)
	}
	if code != 7 {
		t.Errorf("exit code = %d, want the agent's 7", code)
	}
	rec, err := fakeagent.Read(out)
	if err != nil {
		t.Fatalf("agent did not run: %v", err)
	}

	var settings struct{ Env map[string]string }
	json.Unmarshal(rec.Settings, &settings)
	if settings.Env["ANTHROPIC_AUTH_TOKEN"] != "sk-1" || settings.Env["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "" {
		t.Errorf("agent saw settings %s", rec.Settings)
	}
	if runtime.GOOS != "windows" && rec.SettingsMode != "-rw-------" {
		t.Errorf("settings file mode = %s", rec.SettingsMode)
	}
	if _, ok := rec.Env["ANTHROPIC_API_KEY"]; ok {
		t.Error("inherited ANTHROPIC_API_KEY reached the agent")
	}
	if rec.Env["KEEP_ME"] != "1" {
		t.Error("ordinary env was dropped")
	}
	if rec.Args[len(rec.Args)-2] != "--model" || rec.Args[len(rec.Args)-1] != "opus" {
		t.Errorf("args = %v", rec.Args)
	}
	left, _ := os.ReadDir(s.Path("run"))
	if len(left) != 0 {
		t.Errorf("run dir not cleaned: %v", left)
	}
}

func TestRememberAndLoadLast(t *testing.T) {
	s := openStore(t)
	if l := LoadLast(s); l.Credential != "" {
		t.Fatalf("fresh state = %+v", l)
	}
	Remember(s, "a", "opus")
	Remember(s, "b", "")
	l := LoadLast(s)
	if l.Credential != "b" || l.Models["a"] != "opus" || l.Models["b"] != "" {
		t.Fatalf("state = %+v", l)
	}
}

func TestAgentBin(t *testing.T) {
	if _, err := AgentBin(func(string) string { return filepath.Join(t.TempDir(), "nope") }); err == nil {
		t.Fatal("missing binary accepted")
	}
	if got, err := AgentBin(func(k string) string {
		if k == "MUSTER_CLAUDE_BIN" {
			return os.Args[0]
		}
		return ""
	}); err != nil || got == "" {
		t.Fatalf("MUSTER_CLAUDE_BIN not honored: %v %v", got, err)
	}
}
