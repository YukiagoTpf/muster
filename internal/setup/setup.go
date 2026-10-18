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

// Runner runs setup list commands on this machine and records what got
// installed as it happens. Commands get the runner's stdin, and their output
// — after a "==> <name>: <command>" line — goes to Output.
type Runner struct {
	Store  *store.Store
	Env    []string
	Stdin  io.Reader
	Output io.Writer
	state  State
}

// Result is what an install or an update did with one entry.
type Result struct {
	Name     string `json:"name"`
	Action   string `json:"action"` // installed, present, updated, failed or skipped
	ExitCode int    `json:"exitCode,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// Text says the result in a few words.
func (r Result) Text() string {
	switch r.Action {
	case "present":
		return "installed already"
	case "failed":
		return fmt.Sprintf("%s (exit %d)", r.Reason, r.ExitCode)
	case "skipped":
		return r.Reason
	}
	return r.Action
}

// NewRunner reads what this machine has installed.
func NewRunner(s *store.Store, env []string, stdin io.Reader, output io.Writer) (*Runner, error) {
	st, err := LoadState(s)
	return &Runner{Store: s, Env: env, Stdin: stdin, Output: output, state: st}, err
}

func (r *Runner) run(line string, quiet bool) (int, error) {
	if quiet {
		return Run(line, r.Env, nil, io.Discard, io.Discard)
	}
	return Run(line, r.Env, r.Stdin, r.Output, r.Output)
}

func (r *Runner) record(e Entry) error {
	r.state.Installed[e.Name] = Installed{Install: e.Install, At: time.Now().UTC()}
	return r.Store.WithLock(func() error {
		st, err := LoadState(r.Store)
		if err != nil {
			return err
		}
		st.Installed[e.Name] = r.state.Installed[e.Name]
		return st.Save(r.Store)
	})
}

// installed reports whether the entry is on this machine: its check passes
// (which gets it recorded), or, without a check, it is recorded.
func (r *Runner) installed(e Entry) (bool, error) {
	if e.Check == "" {
		return r.state.Recorded(e), nil
	}
	code, err := r.run(e.Check, true)
	if err != nil || code != 0 {
		return false, err
	}
	if !r.state.Recorded(e) {
		return true, r.record(e)
	}
	return true, nil
}

// step runs one command of an entry and says how it went.
func (r *Runner) step(e Entry, line, done, failed string) (Result, error) {
	fmt.Fprintf(r.Output, "==> %s: %s\n", e.Name, line)
	code, err := r.run(line, false)
	if err != nil {
		return Result{}, err
	}
	if code != 0 {
		return Result{Name: e.Name, Action: "failed", ExitCode: code, Reason: failed}, nil
	}
	return Result{Name: e.Name, Action: done}, nil
}

// Install installs the entries this machine doesn't have (all of them with
// force). A failed command doesn't stop the entries after it.
func (r *Runner) Install(entries []Entry, force bool, report func(Result)) error {
	for _, e := range entries {
		if !force {
			present, err := r.installed(e)
			if err != nil {
				return err
			}
			if present {
				report(Result{Name: e.Name, Action: "present"})
				continue
			}
		}
		res, err := r.step(e, e.Install, "installed", "install failed")
		if err != nil {
			return err
		}
		if res.Action == "installed" {
			if err := r.record(e); err != nil {
				return err
			}
		}
		report(res)
	}
	return nil
}

// Update runs the update command of every entry installed here.
func (r *Runner) Update(entries []Entry, report func(Result)) error {
	for _, e := range entries {
		if e.Update == "" {
			report(Result{Name: e.Name, Action: "skipped", Reason: "no update command"})
			continue
		}
		present, err := r.installed(e)
		if err != nil {
			return err
		}
		if !present {
			report(Result{Name: e.Name, Action: "skipped", Reason: "not installed here — `muster setup install " + e.Name + "`"})
			continue
		}
		res, err := r.step(e, e.Update, "updated", "update failed")
		if err != nil {
			return err
		}
		report(res)
	}
	return nil
}

// Missing names the entries this machine has no record of installing.
func (st State) Missing(l List) []string {
	var out []string
	for _, e := range l.Entries {
		if !st.Recorded(e) {
			out = append(out, e.Name)
		}
	}
	return out
}

// Select picks entries by name, all of them when none is named.
func (l List) Select(names []string) ([]Entry, error) {
	if len(names) == 0 {
		return l.Entries, nil
	}
	var out []Entry
	for _, n := range names {
		e, ok := l.Get(n)
		if !ok {
			return nil, fmt.Errorf("the setup list has no %q (see `muster setup list`)", n)
		}
		out = append(out, e)
	}
	return out, nil
}
