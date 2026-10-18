package setup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YukiagoTpf/muster/internal/store"
)

func TestListAddRemoveSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.json")
	l, err := Load(path)
	if err != nil || len(l.Entries) != 0 {
		t.Fatalf("missing list: %+v %v", l, err)
	}
	if err := l.Add(Entry{Name: "skills", Install: "npx skills add x -g -y", Update: "npx skills update -g -y"}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Entry{{Name: "skills", Install: "x"}, {Name: "-x", Install: "x"}, {Name: "y", Install: " "}} {
		if err := l.Add(bad); err == nil {
			t.Errorf("added %+v", bad)
		}
	}
	if err := l.Save(path); err != nil {
		t.Fatal(err)
	}
	back, _ := Load(path)
	if e, ok := back.Get("skills"); !ok || e.Update != "npx skills update -g -y" {
		t.Fatalf("round trip: %+v", back)
	}
	if back.Remove("skills") != nil || back.Remove("skills") == nil || len(back.Entries) != 0 {
		t.Fatal("remove")
	}
}

func TestStateRecordsTheInstallCommand(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.Open(func(k string) string {
		if k == "MUSTER_HOME" {
			return dir
		}
		return ""
	})
	st, err := LoadState(s)
	if err != nil {
		t.Fatal(err)
	}
	e := Entry{Name: "gsd", Install: "v1"}
	st.Installed["gsd"] = Installed{Install: "v1"}
	if err := st.Save(s); err != nil {
		t.Fatal(err)
	}
	back, _ := LoadState(s)
	if !back.Recorded(e) {
		t.Fatal("not recorded")
	}
	e.Install = "v2"
	if back.Recorded(e) {
		t.Fatal("a changed install command counts as installed")
	}
}

func TestRunCarriesOutputAndExitCode(t *testing.T) {
	var out bytes.Buffer
	code, err := Run("echo hello&& exit 4", os.Environ(), nil, &out, &out)
	if err != nil || code != 4 || !strings.Contains(out.String(), "hello") {
		t.Fatalf("code %d, err %v, out %q", code, err, out.String())
	}
}
