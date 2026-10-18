package syncrepo

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/YukiagoTpf/muster/internal/credential"
	"github.com/YukiagoTpf/muster/internal/store"
)

func TestMain(m *testing.M) {
	ScryptWorkFactor = 10
	os.Exit(m.Run())
}

// gitEnv is the environment git runs in for tests: the user's own git config
// never applies.
func gitEnv(t *testing.T) []string {
	t.Helper()
	empty := filepath.Join(t.TempDir(), "gitconfig")
	os.WriteFile(empty, nil, 0o600)
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
		}
	}
	return append(env, "GIT_CONFIG_GLOBAL="+empty, "GIT_CONFIG_NOSYSTEM=1")
}

// remote is an empty bare repo standing in for the user's private repo.
func remote(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "remote.git")
	cmd := exec.Command("git", "init", "-q", "--bare", dir)
	cmd.Env = gitEnv(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	return dir
}

type machine struct {
	Machine
	t *testing.T
}

func newMachine(t *testing.T, host, platform string) *machine {
	t.Helper()
	root := t.TempDir()
	s, err := store.Open(func(k string) string {
		if k == "MUSTER_HOME" {
			return filepath.Join(root, "muster")
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	return &machine{Machine{
		Store: s, Settings: filepath.Join(home, ".claude", "settings.json"), Home: home,
		Platform: platform, Host: host, Env: gitEnv(t),
	}, t}
}

func (m *machine) writeSettings(content string) {
	m.t.Helper()
	m.writeHome(".claude/settings.json", content)
}

func (m *machine) writeHome(rel, content string) {
	m.t.Helper()
	p := m.local(rel)
	os.MkdirAll(filepath.Dir(p), 0o700)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		m.t.Fatal(err)
	}
}

func (m *machine) readHome(rel string) string {
	m.t.Helper()
	b, err := os.ReadFile(m.local(rel))
	if err != nil {
		m.t.Fatal(err)
	}
	return string(b)
}

func (m *machine) settings() string { m.t.Helper(); return m.readHome(".claude/settings.json") }

func (m *machine) addCredential(name, key string) {
	m.t.Helper()
	err := m.Store.WithLock(func() error {
		r, err := credential.Load(m.Store)
		if err != nil {
			return err
		}
		if err := r.Add(credential.Credential{Name: name, Provider: "astergate", BaseURL: "https://gw.example.com"}, key); err != nil {
			return err
		}
		return r.Save(m.Store)
	})
	if err != nil {
		m.t.Fatal(err)
	}
}

func (m *machine) credentials() map[string]string {
	m.t.Helper()
	r, err := credential.Load(m.Store)
	if err != nil {
		m.t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range r.Entries() {
		out[e.Credential.Name] = e.Key
	}
	return out
}

func pass(p string) Passphrase { return func(bool) (string, error) { return p, nil } }

func (m *machine) join(url string) (*Repo, Result) {
	m.t.Helper()
	r, res, err := Join(m.Machine, url, pass("correct horse"))
	if err != nil {
		m.t.Fatalf("%s joins: %v", m.Host, err)
	}
	return r, res
}

func (m *machine) sync(prefer string) Result {
	m.t.Helper()
	r, err := Open(m.Machine)
	if err != nil {
		m.t.Fatal(err)
	}
	res, err := r.Sync(prefer)
	if err != nil {
		m.t.Fatalf("%s syncs: %v", m.Host, err)
	}
	return res
}

func listed(items []Item) string {
	var out []string
	for _, it := range items {
		out = append(out, it.String())
	}
	return strings.Join(out, ", ")
}

const settingsA = `{
  "env": {
    "ANTHROPIC_AUTH_TOKEN": "sk-default-a",
    "ANTHROPIC_BASE_URL": "https://gw.example.com",
    "DISABLE_AUTOUPDATER": "1"
  },
  "hooks": {
    "Stop": "a"
  },
  "statusLine": {
    "command": "bash ~/.claude/statusline.sh"
  }
}
`

func TestJoinCreatesTheRepoThenRestoresAnotherMachine(t *testing.T) {
	t.Parallel()
	url := remote(t)
	a := newMachine(t, "a", "windows")
	a.writeSettings(settingsA)
	a.addCredential("pool", "sk-pool")
	_, res := a.join(url)
	if listed(res.Sent) != "shared settings (windows), credential pool" || len(res.Received) != 0 {
		t.Fatalf("a seeds the repo: sent %q, received %q", listed(res.Sent), listed(res.Received))
	}

	b := newMachine(t, "b", "windows")
	if _, _, err := Join(b.Machine, url, pass("wrong")); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("wrong passphrase: %v", err)
	}
	if _, err := Open(b.Machine); !errors.Is(err, ErrNotSetUp) {
		t.Fatalf("a failed join left the machine set up: %v", err)
	}
	_, res = b.join(url)
	if listed(res.Received) != "shared settings (windows), credential pool" {
		t.Fatalf("b is restored: received %q", listed(res.Received))
	}
	got := b.settings()
	if strings.Contains(got, "ANTHROPIC_") || !strings.Contains(got, `"DISABLE_AUTOUPDATER": "1"`) || !strings.Contains(got, `"Stop": "a"`) {
		t.Fatalf("b's settings carry the shared settings and no default credential:\n%s", got)
	}
	if b.credentials()["pool"] != "sk-pool" {
		t.Fatalf("b's credentials: %v", b.credentials())
	}
	if !strings.Contains(a.settings(), "sk-default-a") {
		t.Fatal("a lost its default credential")
	}
	// Nothing in the repo is a secret in the clear.
	filepath.Walk(cloneDir(b.Machine), func(p string, fi os.FileInfo, err error) error {
		if b, _ := os.ReadFile(p); err == nil && !fi.IsDir() && (strings.Contains(string(b), "sk-pool") || strings.Contains(string(b), "sk-default-a")) {
			t.Errorf("%s holds a key in the clear", p)
		}
		return nil
	})
}

func TestSyncMergesEditsFromBothMachines(t *testing.T) {
	t.Parallel()
	url := remote(t)
	a := newMachine(t, "a", "windows")
	a.writeSettings(settingsA)
	a.join(url)
	b := newMachine(t, "b", "windows")
	b.join(url)

	a.writeSettings(strings.Replace(a.settings(), `"Stop": "a"`, `"Stop": "a2"`, 1))
	b.writeSettings(strings.Replace(b.settings(), `statusline.sh`, `statusline2.sh`, 1))
	a.addCredential("from-a", "sk-a")
	b.addCredential("from-b", "sk-b")
	if res := a.sync(""); listed(res.Sent) != "shared settings (windows), credential from-a" || len(res.Received) != 0 {
		t.Fatalf("a: sent %q received %q", listed(res.Sent), listed(res.Received))
	}
	if res := b.sync(""); listed(res.Received) != "shared settings (windows), credential from-a" || len(res.Conflicts) != 0 {
		t.Fatalf("b: received %q conflicts %q", listed(res.Received), listed(res.Conflicts))
	}
	if res := a.sync(""); listed(res.Received) != "shared settings (windows), credential from-b" {
		t.Fatalf("a again: received %q", listed(res.Received))
	}
	for _, m := range []*machine{a, b} {
		s := m.settings()
		if !strings.Contains(s, `"Stop": "a2"`) || !strings.Contains(s, "statusline2.sh") {
			t.Errorf("%s did not get both edits:\n%s", m.Host, s)
		}
		if c := m.credentials(); c["from-a"] != "sk-a" || c["from-b"] != "sk-b" {
			t.Errorf("%s credentials: %v", m.Host, c)
		}
	}
	if !strings.Contains(a.settings(), "sk-default-a") || strings.Contains(b.settings(), "ANTHROPIC_") {
		t.Error("a default credential crossed machines")
	}
	if res := b.sync(""); len(res.Sent)+len(res.Received) != 0 {
		t.Fatalf("a settled sync changed something: %+v", res)
	}

	// A removal travels like any other change.
	a.Store.WithLock(func() error {
		r, _ := credential.Load(a.Store)
		r.Remove("from-b")
		return r.Save(a.Store)
	})
	a.sync("")
	if res := b.sync(""); listed(res.Received) != "credential from-b" || b.credentials()["from-b"] != "" {
		t.Fatalf("removal: received %q, credentials %v", listed(res.Received), b.credentials())
	}
}

func TestSyncStopsOnAConflictUntilASideIsPicked(t *testing.T) {
	t.Parallel()
	url := remote(t)
	a := newMachine(t, "a", "windows")
	a.writeSettings(settingsA)
	a.join(url)
	b := newMachine(t, "b", "windows")
	b.join(url)

	a.writeSettings(strings.Replace(a.settings(), `"Stop": "a"`, `"Stop": "from-a"`, 1))
	a.sync("")
	b.writeSettings(strings.Replace(b.settings(), `"Stop": "a"`, `"Stop": "from-b"`, 1))
	before := b.settings()
	res := b.sync("")
	if listed(res.Conflicts) != "shared settings (windows)" || len(res.Received) != 0 {
		t.Fatalf("conflicts %q, received %q", listed(res.Conflicts), listed(res.Received))
	}
	if b.settings() != before {
		t.Fatal("a conflict changed this machine")
	}
	if res := b.sync(""); len(res.Conflicts) != 1 {
		t.Fatal("the conflict did not persist until a side was picked")
	}
	if res := b.sync("remote"); len(res.Conflicts) != 0 || !strings.Contains(b.settings(), `"Stop": "from-a"`) {
		t.Fatalf("prefer remote: %+v\n%s", res, b.settings())
	}

	// Picking this machine's side wins the conflict and keeps the rest of the
	// other side's changes.
	a.writeSettings(strings.Replace(strings.Replace(a.settings(), `"Stop": "from-a"`, `"Stop": "a3"`, 1), `"DISABLE_AUTOUPDATER": "1"`, `"DISABLE_AUTOUPDATER": "0"`, 1))
	a.sync("")
	b.writeSettings(strings.Replace(b.settings(), `"Stop": "from-a"`, `"Stop": "b3"`, 1))
	if res := b.sync("local"); len(res.Conflicts) != 0 {
		t.Fatalf("prefer local: %+v", res)
	}
	if s := b.settings(); !strings.Contains(s, `"Stop": "b3"`) || !strings.Contains(s, `"DISABLE_AUTOUPDATER": "0"`) {
		t.Fatalf("prefer local kept:\n%s", s)
	}
	a.sync("")
	if !strings.Contains(a.settings(), `"Stop": "b3"`) {
		t.Fatalf("the resolution did not reach a:\n%s", a.settings())
	}

	// A credential edited on one machine and removed on the other is a
	// conflict git can't split by hunk; picking a side settles it whole.
	a.addCredential("x", "sk-1")
	a.sync("")
	b.sync("")
	a.Store.WithLock(func() error {
		r, _ := credential.Load(a.Store)
		r.Remove("x")
		return r.Save(a.Store)
	})
	a.sync("")
	b.Store.WithLock(func() error {
		r, _ := credential.Load(b.Store)
		r.Remove("x")
		r.Add(credential.Credential{Name: "x", Provider: "astergate", BaseURL: "https://gw.example.com"}, "sk-2")
		return r.Save(b.Store)
	})
	if res := b.sync(""); listed(res.Conflicts) != "credential x" {
		t.Fatalf("modify/delete: %+v", res)
	}
	if b.sync("local"); b.credentials()["x"] != "sk-2" {
		t.Fatalf("prefer local kept x: %v", b.credentials())
	}
	if a.sync(""); a.credentials()["x"] != "sk-2" {
		t.Fatalf("x came back to a: %v", a.credentials())
	}
}

func TestPlatformsShareCredentialsButNotSettings(t *testing.T) {
	t.Parallel()
	url := remote(t)
	win := newMachine(t, "win", "windows")
	win.writeSettings(settingsA)
	win.addCredential("pool", "sk-pool")
	win.join(url)

	mac := newMachine(t, "mac", "macos")
	mac.writeSettings(`{"hooks":{"Stop":"mac"}}`)
	_, res := mac.join(url)
	if listed(res.Sent) != "shared settings (macos)" || listed(res.Received) != "credential pool" {
		t.Fatalf("mac joins: sent %q received %q", listed(res.Sent), listed(res.Received))
	}
	if !strings.Contains(mac.settings(), `"Stop":"mac"`) {
		t.Fatalf("mac's settings were replaced by another platform's:\n%s", mac.settings())
	}
	if res := win.sync(""); len(res.Received) != 0 || strings.Contains(win.settings(), "mac") {
		t.Fatalf("windows took mac's settings: %+v", res)
	}
}

func TestTrackedFilesFollowTheirMachine(t *testing.T) {
	t.Parallel()
	url := remote(t)
	a := newMachine(t, "a", "windows")
	a.writeSettings(`{}`)
	a.writeHome(".claude/statusline.sh", "echo a\n")
	os.Chmod(a.local(".claude/statusline.sh"), 0o755)
	ra, _ := a.join(url)
	if name, err := ra.Track(a.local(".claude/statusline.sh")); err != nil || name != "~/.claude/statusline.sh" {
		t.Fatalf("track: %q %v", name, err)
	}
	for _, bad := range []string{a.Settings, filepath.Join(a.Store.Dir, "secrets.json"), filepath.Join(t.TempDir(), "elsewhere")} {
		if _, err := ra.Track(bad); err == nil {
			t.Errorf("tracked %s", bad)
		}
	}
	st, err := ra.Status()
	if err != nil || len(st.Pending) != 1 || st.Pending[0].Change != "added" || st.Tracked[0] != "~/.claude/statusline.sh" {
		t.Fatalf("status: %+v %v", st, err)
	}
	if res := a.sync(""); listed(res.Sent) != "~/.claude/statusline.sh" {
		t.Fatalf("sent %q", listed(res.Sent))
	}

	b := newMachine(t, "b", "windows")
	b.writeHome(".claude/statusline.sh", "echo b's own\n")
	b.join(url)
	if b.readHome(".claude/statusline.sh") != "echo a\n" {
		t.Fatal("b did not get the tracked file")
	}
	if fi, _ := os.Stat(b.local(".claude/statusline.sh")); runtime.GOOS != "windows" && fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("the executable bit was lost: %v", fi.Mode())
	}
	backups, _ := filepath.Glob(filepath.Join(b.Store.Dir, "backups", "statusline-*.sh"))
	if len(backups) != 1 {
		t.Fatalf("b's own version was not backed up: %v", backups)
	}

	b.writeHome(".claude/statusline.sh", "echo b\n")
	b.sync("")
	if res := a.sync(""); listed(res.Received) != "~/.claude/statusline.sh" || a.readHome(".claude/statusline.sh") != "echo b\n" {
		t.Fatalf("a: received %q", listed(res.Received))
	}

	// A tracked file deleted on one machine comes back; untracking leaves it
	// on every machine.
	os.Remove(a.local(".claude/statusline.sh"))
	a.sync("")
	if a.readHome(".claude/statusline.sh") != "echo b\n" {
		t.Fatal("a deleted tracked file was not restored")
	}
	if _, err := ra.Untrack(a.local(".claude/statusline.sh")); err != nil {
		t.Fatal(err)
	}
	if _, err := ra.Untrack(a.local(".claude/statusline.sh")); err == nil {
		t.Error("untracked twice")
	}
	a.sync("")
	b.sync("")
	if b.readHome(".claude/statusline.sh") != "echo b\n" {
		t.Fatal("untracking removed the file from b")
	}
	if rb, _ := Open(b.Machine); func() bool { tr, _ := rb.Tracked(); return len(tr) != 0 }() {
		t.Fatal("still tracked on b")
	}
}

func TestSecretLookingSharedSettingsNeverLeave(t *testing.T) {
	t.Parallel()
	a := newMachine(t, "a", "windows")
	a.writeSettings(`{"env":{"TAVILY_API_KEY":"tvly-secret"}}`)
	if _, _, err := Join(a.Machine, remote(t), pass("p")); err == nil || !strings.Contains(err.Error(), "TAVILY_API_KEY") {
		t.Fatalf("join with a secret in the shared settings: %v", err)
	}
}

func TestJoinRefusesARepoMusterDidNotMake(t *testing.T) {
	t.Parallel()
	url := remote(t)
	work := t.TempDir()
	for _, args := range [][]string{
		{"clone", "-q", url, work},
		{"-C", work, "-c", "user.name=x", "-c", "user.email=x@x", "commit", "-q", "--allow-empty", "-m", "x"},
		{"-C", work, "push", "-q", "origin", "HEAD:refs/heads/main"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Env = gitEnv(t)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	a := newMachine(t, "a", "windows")
	if _, _, err := Join(a.Machine, url, pass("p")); err == nil || !strings.Contains(err.Error(), "not a Muster sync repo") {
		t.Fatalf("joined a repo without muster.json: %v", err)
	}
	if _, err := os.Stat(cloneDir(a.Machine)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a refused join left its clone behind")
	}
}

func TestSyncUndoesAMergeLeftHalfDone(t *testing.T) {
	t.Parallel()
	url := remote(t)
	a := newMachine(t, "a", "windows")
	a.writeSettings(settingsA)
	a.join(url)
	b := newMachine(t, "b", "windows")
	b.join(url)
	a.writeSettings(strings.Replace(a.settings(), `"Stop": "a"`, `"Stop": "from-a"`, 1))
	a.sync("")
	b.writeSettings(strings.Replace(b.settings(), `"Stop": "a"`, `"Stop": "from-b"`, 1))
	rb, _ := Open(b.Machine)
	s, _ := b.read(nil)
	rb.export(s, false, "x")
	rb.git.run("fetch", "-q", "origin")
	rb.git.run("merge", "-q", "--no-edit", rb.upstream()) // conflicts, and is left so
	if res := b.sync(""); len(res.Conflicts) != 1 || strings.Contains(b.settings(), "<<<<<<<") {
		t.Fatalf("after a half-done merge: %+v\n%s", res, b.settings())
	}
}
