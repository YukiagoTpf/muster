// Package syncrepo keeps the user's machines in step through a private git
// repo of their own. The repo holds, per platform, the shared settings, the
// tracked files and the setup list, and, for every platform, the credentials,
// encrypted. Each machine keeps a clone in its data directory; the clone's
// HEAD is what the machine last synced, so git's merge base is the common
// ancestor of a three-way merge between this machine and the others.
package syncrepo

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"filippo.io/age"

	"github.com/YukiagoTpf/muster/internal/claude"
	"github.com/YukiagoTpf/muster/internal/platform"
	"github.com/YukiagoTpf/muster/internal/store"
)

// The repo's layout, as slash paths from its root.
const (
	markerPath     = "muster.json"  // {"version": 1}: this is a Muster sync repo
	identityPath   = "identity.age" // the repo's identity, passphrase-encrypted
	attributesPath = ".gitattributes"
	credentialsDir = "credentials" // <name>.age, one per credential
)

// attributes keep git from converting line endings and from merging the
// encrypted files as text.
const attributes = "* -text\n*.age binary\n"

const configVersion = 1

// config is a machine's sync config, <MUSTER_HOME>/sync.json. It holds the
// repo's identity, so like secrets.json it is readable only by the user.
type config struct {
	Version  int    `json:"version"`
	Repo     string `json:"repo"`
	Identity string `json:"identity"`
}

// Machine is this machine's side of the sync: where its agent settings and
// tracked files live, and how it names itself.
type Machine struct {
	Store    *store.Store
	Settings string // the agent's settings file
	Home     string // tracked files are named relative to it
	Platform string
	Host     string   // names the machine in the repo's history
	Env      []string // the environment git runs in
}

// Here is this machine, from its environment.
func Here(getenv func(string) string, environ func() []string) (Machine, error) {
	s, err := store.Open(getenv)
	if err != nil {
		return Machine{}, err
	}
	settings, err := claude.SettingsPath(getenv)
	if err != nil {
		return Machine{}, err
	}
	home, err := platform.UserHome(getenv)
	if err != nil {
		return Machine{}, err
	}
	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}
	return Machine{Store: s, Settings: settings, Home: home, Platform: platform.Name(), Host: host, Env: environ()}, nil
}

// Repo is a machine's clone of its sync repo.
type Repo struct {
	URL string
	m   Machine
	dir string
	git git
	id  *age.HybridIdentity
}

// branch is the sync repo's one branch.
const branch = "main"

// ErrNotSetUp reports a machine that has not joined a sync repo.
var ErrNotSetUp = errors.New("sync is not set up: run `muster sync init <repo>` first")

func configPath(m Machine) string { return m.Store.Path("sync.json") }

func cloneDir(m Machine) string { return m.Store.Path("sync") }

// Open opens the sync repo this machine joined.
func Open(m Machine) (*Repo, error) {
	var c config
	ok, err := store.ReadJSON(configPath(m), &c)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotSetUp
	}
	if c.Version != configVersion {
		return nil, fmt.Errorf("%s has version %d; this muster understands %d", configPath(m), c.Version, configVersion)
	}
	id, err := parseIdentity(c.Identity)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", configPath(m), err)
	}
	r := newRepo(m, c.Repo)
	r.id = id
	return r, nil
}

func newRepo(m Machine, url string) *Repo {
	dir := cloneDir(m)
	return &Repo{URL: url, m: m, dir: dir, git: git{dir, m.Env}}
}

func (r *Repo) upstream() string { return "refs/remotes/origin/" + branch }

// Dir is the clone.
func (r *Repo) Dir() string { return r.dir }

// Platform is the platform whose part of the repo this machine syncs.
func (r *Repo) Platform() string { return r.m.Platform }

func (r *Repo) settingsPath() string { return r.m.Platform + "/settings.json" }

func (r *Repo) setupPath() string { return r.m.Platform + "/setup.json" }

func (r *Repo) filesDir() string { return r.m.Platform + "/files" }

func credentialPath(name string) string { return credentialsDir + "/" + name + ".age" }

// SetupList is the path of this platform's setup list in the clone.
func (r *Repo) SetupList() string { return r.abs(r.setupPath()) }

func (r *Repo) abs(rel string) string { return filepath.Join(r.dir, filepath.FromSlash(rel)) }

// read reads a file of the clone; a missing one reports ok == false.
func (r *Repo) read(rel string) ([]byte, bool, error) {
	b, err := os.ReadFile(r.abs(rel))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return b, err == nil, err
}

// tracked lists the tracked files of this platform, as slash paths relative
// to the home directory: whatever lies under the platform's files/.
func (r *Repo) tracked() ([]string, error) {
	root := r.abs(r.filesDir())
	var rels []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) && p == root {
			return filepath.SkipDir
		}
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, p)
		rels = append(rels, filepath.ToSlash(rel))
		return err
	})
	sort.Strings(rels)
	return rels, err
}

// Item is one thing the repo syncs, as the user knows it.
type Item struct {
	Kind string `json:"kind"` // "settings", "credential", "file" or "setup"
	Name string `json:"name"` // the platform, the credential's name, or the file as ~/path
}

func (it Item) String() string {
	switch it.Kind {
	case "settings":
		return "shared settings (" + it.Name + ")"
	case "credential":
		return "credential " + it.Name
	case "setup":
		return "setup list (" + it.Name + ")"
	}
	return it.Name
}

// item names the thing a repo path holds; false for the repo's bookkeeping
// and for another platform's part.
func (r *Repo) item(p string) (Item, bool) {
	switch {
	case p == r.settingsPath():
		return Item{"settings", r.m.Platform}, true
	case p == r.setupPath():
		return Item{"setup", r.m.Platform}, true
	case strings.HasPrefix(p, r.filesDir()+"/"):
		return Item{"file", "~/" + strings.TrimPrefix(p, r.filesDir()+"/")}, true
	case path.Dir(p) == credentialsDir && path.Ext(p) == ".age":
		return Item{"credential", strings.TrimSuffix(path.Base(p), ".age")}, true
	}
	return Item{}, false
}

// items names the things a list of repo paths holds, each once, sorted.
func (r *Repo) items(paths []string) []Item {
	var out []Item
	for _, p := range paths {
		if it, ok := r.item(p); ok {
			out = append(out, it)
		}
	}
	return sortItems(out)
}

var kindRank = map[string]int{"settings": 0, "setup": 1, "credential": 2, "file": 3}

// sortItems sorts items the way they are shown — shared settings, setup
// list, credentials, files — and drops duplicates.
func sortItems(items []Item) []Item {
	sort.Slice(items, func(i, j int) bool {
		if a, b := kindRank[items[i].Kind], kindRank[items[j].Kind]; a != b {
			return a < b
		}
		return items[i].Name < items[j].Name
	})
	out := []Item{}
	for i, it := range items {
		if i == 0 || it != items[i-1] {
			out = append(out, it)
		}
	}
	return out
}
