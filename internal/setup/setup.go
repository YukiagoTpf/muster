// Package setup is the setup list: per platform, what a machine installs
// beyond Claude Code itself (a set of skills, a framework, a plugin
// marketplace), each entry an install command with an optional update
// command and an optional check command. Muster runs the commands and never
// knows what they install. The list lives in the sync repo; what this machine
// has installed lives in its data directory.
package setup

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/YukiagoTpf/muster/internal/platform"
	"github.com/YukiagoTpf/muster/internal/store"
)

// Entry is one thing the setup list installs.
type Entry struct {
	Name    string `json:"name"`
	Install string `json:"install"`
	Update  string `json:"update,omitempty"`
	// Check exits 0 when the thing is installed already.
	Check string `json:"check,omitempty"`
}

// List is a platform's setup list.
type List struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// Load reads the setup list at path; a missing one is empty.
func Load(path string) (List, error) {
	l := List{Version: 1, Entries: []Entry{}}
	if _, err := store.ReadJSON(path, &l); err != nil {
		return l, err
	}
	if l.Version != 1 {
		return l, fmt.Errorf("%s has version %d; this muster understands 1", path, l.Version)
	}
	return l, nil
}

// Save writes the setup list to path.
func (l List) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return store.WriteJSON(path, l)
}

// Get finds an entry by name.
func (l List) Get(name string) (Entry, bool) {
	for _, e := range l.Entries {
		if e.Name == name {
			return e, true
		}
	}
	return Entry{}, false
}

// Add appends an entry; names are unique.
func (l *List) Add(e Entry) error {
	if !nameRe.MatchString(e.Name) {
		return fmt.Errorf("invalid name %q: use letters, digits, '.', '_' or '-' (at most 64, starting with a letter or digit)", e.Name)
	}
	if strings.TrimSpace(e.Install) == "" {
		return errors.New("the install command is empty")
	}
	if _, ok := l.Get(e.Name); ok {
		return fmt.Errorf("the setup list has %q already; remove it first to change it", e.Name)
	}
	l.Entries = append(l.Entries, e)
	return nil
}

// Remove drops an entry.
func (l *List) Remove(name string) error {
	for i, e := range l.Entries {
		if e.Name == name {
			l.Entries = append(l.Entries[:i], l.Entries[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("the setup list has no %q", name)
}

// State is what this machine has installed from the setup list,
// <MUSTER_HOME>/setup-state.json.
type State struct {
	Version   int                  `json:"version"`
	Installed map[string]Installed `json:"installed"`
}

// Installed records one install on this machine.
type Installed struct {
	Install string    `json:"install"` // the install command that ran
	At      time.Time `json:"at"`
}

func statePath(s *store.Store) string { return s.Path("setup-state.json") }

// LoadState reads what this machine has installed.
func LoadState(s *store.Store) (State, error) {
	st := State{Version: 1}
	if _, err := store.ReadJSON(statePath(s), &st); err != nil {
		return st, err
	}
	if st.Installed == nil {
		st.Installed = map[string]Installed{}
	}
	return st, nil
}

// Save writes the state back. The caller holds the store lock.
func (st State) Save(s *store.Store) error { return store.WriteJSON(statePath(s), st) }

// Recorded reports whether this machine installed the entry with its current
// install command: a changed command is a new install.
func (st State) Recorded(e Entry) bool {
	in, ok := st.Installed[e.Name]
	return ok && in.Install == e.Install
}

// Run runs a command line in the platform's shell and returns its exit code.
func Run(line string, env []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	cmd := platform.Shell(line)
	cmd.Env, cmd.Stdin, cmd.Stdout, cmd.Stderr = env, stdin, stdout, stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return platform.ExitCode(exit), nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}
