package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func (sb *sandbox) settings(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(sb.claudeDir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func readDefault(t *testing.T, sb *sandbox) map[string]any {
	t.Helper()
	code, out, errOut := sb.run(t, "", "default", "--json")
	if code != 0 {
		t.Fatalf("default --json: %s", errOut)
	}
	var st map[string]any
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestDefaultSwitchesOnlyTheProviderPart(t *testing.T) {
	sb := withCodexPool(t)
	before := sb.settings(t)
	code, out, errOut := sb.run(t, "", "default", "codex-pool")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	after := sb.settings(t)
	var s struct {
		Env   map[string]string
		Hooks map[string]any
	}
	if err := json.Unmarshal([]byte(after), &s); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"ANTHROPIC_BASE_URL":           "https://codex.example",
		"ANTHROPIC_AUTH_TOKEN":         "sk-codex",
		"ANTHROPIC_DEFAULT_OPUS_MODEL": "gpt-x",
		"ENABLE_TOOL_SEARCH":           "true", // shared settings stay
	}
	if len(s.Env) != len(want) {
		t.Fatalf("env = %v", s.Env)
	}
	for k, v := range want {
		if s.Env[k] != v {
			t.Errorf("env[%s] = %q, want %q", k, s.Env[k], v)
		}
	}
	if s.Hooks == nil {
		t.Error("hooks were dropped")
	}
	backups, _ := filepath.Glob(filepath.Join(sb.env["MUSTER_HOME"], "backups", "settings-*.json"))
	if len(backups) != 1 || !strings.Contains(out, backups[0]) {
		t.Fatalf("backups = %v, out = %q", backups, out)
	}
	if b, _ := os.ReadFile(backups[0]); string(b) != before {
		t.Error("backup is not the previous file")
	}
	if st := readDefault(t, sb); st["state"] != "match" || st["credential"] != "codex-pool" {
		t.Errorf("after switching: %v", st)
	}
}

func TestDefaultShowsWhichCredentialSettingsUse(t *testing.T) {
	sb := newSandbox(t)
	if st := readDefault(t, sb); st["state"] != "none" {
		t.Errorf("no settings: %v", st)
	}
	sb.writeSettings(t, astergateSettings)
	if st := readDefault(t, sb); st["state"] != "unregistered" || st["baseURL"] != "https://gw.example.com" {
		t.Errorf("unregistered: %v", st)
	}
	sb.run(t, "", "cred", "add", "claude-pool", "--provider", "astergate", "--from-settings")
	if st := readDefault(t, sb); st["state"] != "match" || st["credential"] != "claude-pool" {
		t.Errorf("match: %v", st)
	}
	sb.writeSettings(t, strings.Replace(astergateSettings, `"ENABLE_TOOL_SEARCH"`, `"ANTHROPIC_DEFAULT_SONNET_MODEL": "x", "ENABLE_TOOL_SEARCH"`, 1))
	st := readDefault(t, sb)
	if st["state"] != "drift" || st["credential"] != "claude-pool" {
		t.Errorf("drift: %v", st)
	}
	if keys, _ := st["differentKeys"].([]any); len(keys) != 1 || keys[0] != "ANTHROPIC_DEFAULT_SONNET_MODEL" {
		t.Errorf("drift keys: %v", st["differentKeys"])
	}
	if strings.Contains(sb.mustRun(t, "default"), "sk-secret-123") {
		t.Fatal("default printed the key")
	}
}

func TestDefaultRefusesSettingsItCannotRead(t *testing.T) {
	sb := withCodexPool(t)
	sb.writeSettings(t, `{"env": [}`)
	code, _, errOut := sb.run(t, "", "default", "codex-pool")
	if code == 0 || !strings.Contains(errOut, "nothing was changed") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if sb.settings(t) != `{"env": [}` {
		t.Fatal("unreadable settings were rewritten")
	}
}

func (sb *sandbox) mustRun(t *testing.T, args ...string) string {
	t.Helper()
	code, out, errOut := sb.run(t, "", args...)
	if code != 0 {
		t.Fatalf("muster %v: exit %d: %s", args, code, errOut)
	}
	return out
}
