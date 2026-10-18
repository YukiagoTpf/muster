package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/YukiagoTpf/muster/internal/fakeagent"
	"github.com/YukiagoTpf/muster/internal/launch"
)

// withCodexPool is a sandbox whose default credential is a Claude pool (in
// settings.json) and which has a second credential, codex-pool, registered.
func withCodexPool(t *testing.T) *sandbox {
	t.Helper()
	sb := newSandbox(t)
	sb.writeSettings(t, astergateSettings)
	code, _, errOut := sb.run(t, "sk-codex", "cred", "add", "codex-pool", "--provider", "astergate",
		"--base-url", "https://codex.example", "--model", "opus=gpt-x", "--key-stdin")
	if code != 0 {
		t.Fatalf("cred add: %s", errOut)
	}
	return sb
}

func TestRunLaunchesWithOnlyTheCredentialsProviderEnv(t *testing.T) {
	sb := withCodexPool(t)
	out := filepath.Join(t.TempDir(), "agent.json")
	sb.env["MUSTER_CLAUDE_BIN"] = os.Args[0]
	sb.env["MUSTER_FAKE_AGENT_OUT"] = out
	sb.env["MUSTER_FAKE_AGENT_EXIT"] = "5"
	sb.env["ANTHROPIC_API_KEY"] = "sk-inherited"
	sb.env["KEEP_ME"] = "1"

	code, _, errOut := sb.run(t, "", "run", "codex-pool", "-m", "opus", "--", "--resume", "abc")
	if code != 5 {
		t.Fatalf("exit %d (want the agent's 5): %s", code, errOut)
	}
	rec, err := fakeagent.Read(out)
	if err != nil {
		t.Fatalf("agent did not run: %v", err)
	}
	var settings struct{ Env map[string]string }
	if err := json.Unmarshal(rec.Settings, &settings); err != nil {
		t.Fatalf("settings file: %v (%s)", err, rec.Settings)
	}
	want := map[string]string{
		"ANTHROPIC_BASE_URL":            "https://codex.example",
		"ANTHROPIC_AUTH_TOKEN":          "sk-codex",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":  "gpt-x",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL": "", // the default credential's mapping must not leak in
	}
	if !reflect.DeepEqual(settings.Env, want) {
		t.Errorf("agent's --settings env = %v", settings.Env)
	}
	if _, ok := rec.Env["ANTHROPIC_API_KEY"]; ok {
		t.Error("the shell's ANTHROPIC_API_KEY reached the agent")
	}
	if rec.Env["KEEP_ME"] != "1" {
		t.Error("ordinary env was dropped")
	}
	tail := rec.Args[len(rec.Args)-4:]
	if !reflect.DeepEqual(tail, []string{"--model", "opus", "--resume", "abc"}) {
		t.Errorf("args = %v", rec.Args)
	}
	if last := launch.LoadLast(openStoreFor(t, sb)); last.Credential != "codex-pool" || last.Models["codex-pool"] != "opus" {
		t.Errorf("launch not remembered: %+v", last)
	}
}

func TestRunDryRunShowsKeyNamesOnly(t *testing.T) {
	sb := withCodexPool(t)
	sb.env["ANTHROPIC_API_KEY"] = "sk-inherited"
	code, out, errOut := sb.run(t, "", "run", "codex-pool", "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, secret := range []string{"sk-codex", "sk-secret-123", "sk-inherited"} {
		if strings.Contains(out, secret) {
			t.Fatalf("dry run printed %s: %s", secret, out)
		}
	}
	var plan struct {
		Version         int
		Command         []string
		SettingsEnvKeys []string
		BlankedKeys     []string
		StrippedEnvKeys []string
	}
	json.Unmarshal([]byte(out), &plan)
	if plan.Version != 1 || plan.Command[0] != "claude" || plan.Command[2] != settingsPlaceholder {
		t.Errorf("plan = %+v", plan)
	}
	if !reflect.DeepEqual(plan.BlankedKeys, []string{"ANTHROPIC_DEFAULT_HAIKU_MODEL"}) || !reflect.DeepEqual(plan.StrippedEnvKeys, []string{"ANTHROPIC_API_KEY"}) {
		t.Errorf("plan = %+v", plan)
	}
	if _, human, _ := sb.run(t, "", "run", "codex-pool", "--dry-run"); !strings.Contains(human, "blanks: ANTHROPIC_DEFAULT_HAIKU_MODEL") {
		t.Errorf("human dry run = %q", human)
	}
}

func TestRunUnknownCredential(t *testing.T) {
	sb := newSandbox(t)
	if code, _, errOut := sb.run(t, "", "run", "ghost"); code != 1 || !strings.Contains(errOut, `no credential named "ghost"`) {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}
