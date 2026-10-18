package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// bareRemote is an empty repo standing in for the user's private GitHub repo.
func bareRemote(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "remote.git")
	cmd := exec.Command("git", "init", "-q", "--bare", dir)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	return dir
}

func (sb *sandbox) home() string { return sb.env["HOME"] }

func (sb *sandbox) syncJSON(t *testing.T, stdin string, args ...string) (int, syncView) {
	t.Helper()
	code, out, errOut := sb.run(t, stdin, append(args, "--json")...)
	var v syncView
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("%v: exit %d, %q, %s", args, code, out, errOut)
	}
	return code, v
}

func TestSyncBetweenTwoMachines(t *testing.T) {
	url := bareRemote(t)
	a, b := newSandbox(t), newSandbox(t)
	a.writeSettings(t, astergateSettings)
	if code, _, errOut := a.run(t, "", "cred", "add", "claude-pool", "--provider", "astergate", "--from-settings"); code != 0 {
		t.Fatal(errOut)
	}
	if code, _, errOut := a.run(t, "", "sync"); code != 1 || !strings.Contains(errOut, "muster sync init") {
		t.Fatalf("sync before init: exit %d, %s", code, errOut)
	}

	code, v := a.syncJSON(t, "pw\n", "sync", "init", url, "--passphrase-stdin")
	if sent := itemList(v.Sent); code != 0 || v.Version != 1 || !strings.Contains(sent, "shared settings") || !strings.Contains(sent, "credential claude-pool") {
		t.Fatalf("a creates the repo: exit %d, %+v", code, v)
	}
	if code, _, errOut := b.run(t, "nope\n", "sync", "init", url, "--passphrase-stdin"); code != 1 || !strings.Contains(errOut, "wrong passphrase") {
		t.Fatalf("wrong passphrase: exit %d, %s", code, errOut)
	}
	code, out, errOut := b.run(t, "pw\n", "sync", "init", url, "--passphrase-stdin")
	if code != 0 || !strings.Contains(out, "received: shared settings") || !strings.Contains(out, "credential claude-pool") {
		t.Fatalf("b joins: exit %d, %q, %s", code, out, errOut)
	}
	settings, _ := os.ReadFile(filepath.Join(b.claudeDir, "settings.json"))
	if strings.Contains(string(settings), "ANTHROPIC_") || !strings.Contains(string(settings), "ENABLE_TOOL_SEARCH") {
		t.Fatalf("b's settings:\n%s", settings)
	}
	// b got the credential with its key: it can make it its default.
	if code, _, errOut := b.run(t, "", "default", "claude-pool"); code != 0 {
		t.Fatalf("b's credential has no key: %s", errOut)
	}

	script := filepath.Join(a.home(), ".claude", "statusline.sh")
	os.MkdirAll(filepath.Dir(script), 0o700)
	os.WriteFile(script, []byte("echo hi\n"), 0o755)
	if code, out, errOut := a.run(t, "", "track", script); code != 0 || !strings.Contains(out, "tracking ~/.claude/statusline.sh") {
		t.Fatalf("track: exit %d, %q, %s", code, out, errOut)
	}
	code, out, _ = a.run(t, "", "sync", "status", "--json")
	var st struct {
		Version int
		Pending []struct{ Kind, Name, Change string }
		Tracked []string
	}
	json.Unmarshal([]byte(out), &st)
	if code != 0 || len(st.Pending) != 1 || st.Pending[0].Change != "added" || st.Tracked[0] != "~/.claude/statusline.sh" {
		t.Fatalf("status: exit %d, %s", code, out)
	}
	if code, out, _ := a.run(t, "", "sync"); code != 0 || out != "sent: ~/.claude/statusline.sh\n" {
		t.Fatalf("a syncs: exit %d, %q", code, out)
	}
	if code, out, _ := b.run(t, "", "sync"); code != 0 || out != "received: ~/.claude/statusline.sh\n" {
		t.Fatalf("b syncs: exit %d, %q", code, out)
	}
	if got, _ := os.ReadFile(filepath.Join(b.home(), ".claude", "statusline.sh")); string(got) != "echo hi\n" {
		t.Fatalf("b's tracked file = %q", got)
	}
	if code, out, _ := b.run(t, "", "sync"); code != 0 || out != "already in sync\n" {
		t.Fatalf("settled: exit %d, %q", code, out)
	}

	// The same shared setting changed on both machines.
	edit := func(sb *sandbox, value string) {
		p := filepath.Join(sb.claudeDir, "settings.json")
		b, _ := os.ReadFile(p)
		os.WriteFile(p, []byte(strings.Replace(string(b), `"ENABLE_TOOL_SEARCH": "true"`, `"ENABLE_TOOL_SEARCH": "`+value+`"`, 1)), 0o600)
	}
	edit(a, "a")
	a.run(t, "", "sync")
	edit(b, "b")
	code, v = b.syncJSON(t, "", "sync")
	if code != 1 || len(v.Conflicts) != 1 || v.Conflicts[0].Kind != "settings" {
		t.Fatalf("conflict: exit %d, %+v", code, v)
	}
	if code, out, _ := b.run(t, "", "sync"); code != 1 || !strings.Contains(out, "--prefer local") {
		t.Fatalf("human conflict report: exit %d, %q", code, out)
	}
	if code, _, _ := b.run(t, "", "sync", "--prefer", "mine"); code != 2 {
		t.Fatal("--prefer takes local or remote")
	}
	if code, _, _ := b.run(t, "", "sync", "--prefer", "remote"); code != 0 {
		t.Fatal("prefer remote")
	}
	if got, _ := os.ReadFile(filepath.Join(b.claudeDir, "settings.json")); !strings.Contains(string(got), `"ENABLE_TOOL_SEARCH": "a"`) || !strings.Contains(string(got), "sk-secret-123") {
		t.Fatalf("b after taking the remote side (its default credential stays):\n%s", got)
	}

	if code, _, _ := a.run(t, "", "untrack"); code != 2 {
		t.Fatal("untrack without a path")
	}
	code, out, _ = a.run(t, "", "untrack", script, "--json")
	if code != 0 || !strings.Contains(out, `"tracked": []`) {
		t.Fatalf("untrack: exit %d, %s", code, out)
	}
}
