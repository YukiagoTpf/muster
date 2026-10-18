package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/YukiagoTpf/muster/internal/platform"
	"github.com/YukiagoTpf/muster/internal/setup"
)

func TestSetupListInstallUpdate(t *testing.T) {
	url := bareRemote(t)
	a, b := newSandbox(t), newSandbox(t)
	if code, _, _ := a.run(t, "", "setup", "list"); code != 1 {
		t.Fatal("the setup list needs sync set up")
	}
	if code, _, errOut := a.run(t, "pw\n", "sync", "init", url, "--passphrase-stdin"); code != 0 {
		t.Fatal(errOut)
	}
	for _, args := range [][]string{
		{"skills", "--install", "echo installing skills", "--update", "echo updating skills"},
		{"broken", "--install", "exit 3"},
		{"tool", "--install", "echo never runs", "--check", "exit 0"},
	} {
		if code, _, errOut := a.run(t, "", append([]string{"setup", "add"}, args...)...); code != 0 {
			t.Fatalf("add %v: %s", args, errOut)
		}
	}
	if code, _, _ := a.run(t, "", "setup", "add", "x"); code != 2 {
		t.Error("an entry without --install")
	}
	if code, _, errOut := a.run(t, "", "setup", "add", "skills", "--install", "y"); code != 1 || !strings.Contains(errOut, "already") {
		t.Errorf("a duplicate entry: exit %d, %s", code, errOut)
	}

	code, out, errOut := a.run(t, "", "setup", "install", "--json")
	var res struct {
		Version int
		Results []setup.Result
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("install --json printed %q: %v", out, err)
	}
	actions := map[string]string{}
	for _, r := range res.Results {
		actions[r.Name] = r.Action
	}
	if code != 1 || actions["skills"] != "installed" || actions["broken"] != "failed" || actions["tool"] != "present" || res.Results[1].ExitCode != 3 {
		t.Fatalf("install: exit %d, %+v", code, res)
	}
	if !strings.Contains(errOut, "installing skills") || strings.Contains(errOut, "never runs") {
		t.Fatalf("command output under --json goes to stderr, and a passing check skips the install: %q", errOut)
	}
	if code, out, _ := a.run(t, "", "setup", "install", "skills"); code != 0 || out != "skills: installed already\n" {
		t.Fatalf("a second install: exit %d, %q", code, out)
	}
	if code, _, errOut := a.run(t, "", "setup", "install", "nope"); code != 1 || !strings.Contains(errOut, `no "nope"`) {
		t.Fatalf("unknown entry: exit %d, %s", code, errOut)
	}

	code, out, _ = a.run(t, "", "update")
	if code != 0 || !strings.Contains(out, "updating skills") || !strings.Contains(out, "skills: updated") || !strings.Contains(out, "broken: no update command") {
		t.Fatalf("update: exit %d, %q", code, out)
	}

	if code, out, _ := a.run(t, "", "sync"); code != 0 || out != "sent: setup list ("+platform.Name()+")\nsetup list: broken not installed here — `muster setup install`\n" {
		t.Fatalf("a syncs the list: exit %d, %q", code, out)
	}
	code, out, _ = b.run(t, "pw\n", "sync", "init", url, "--passphrase-stdin")
	if code != 0 || !strings.Contains(out, "received: setup list") || !strings.Contains(out, "skills, broken, tool not installed here") {
		t.Fatalf("b joins: exit %d, %q", code, out)
	}
	if code, _, _ := a.run(t, "", "setup", "rm", "broken"); code != 0 {
		t.Fatal("rm")
	}
	code, out, _ = a.run(t, "", "setup", "list", "--json")
	var list struct {
		Platform string
		Entries  []setupEntryView
	}
	json.Unmarshal([]byte(out), &list)
	if code != 0 || list.Platform != platform.Name() || len(list.Entries) != 2 || !list.Entries[0].Installed || list.Entries[0].Update != "echo updating skills" {
		t.Fatalf("list: exit %d, %s", code, out)
	}
}
