package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/YukiagoTpf/muster/internal/setup"
	"github.com/YukiagoTpf/muster/internal/syncrepo"
)

// openSetup opens the sync repo, whose clone holds this platform's setup list.
func openSetup(sys IO) (*syncrepo.Repo, syncrepo.Machine, setup.List, error) {
	m, err := syncMachine(sys)
	if err != nil {
		return nil, m, setup.List{}, err
	}
	r, err := syncrepo.Open(m)
	if err != nil {
		return nil, m, setup.List{}, err
	}
	l, err := setup.Load(r.SetupList())
	return r, m, l, err
}

type setupEntryView struct {
	setup.Entry
	Installed bool `json:"installed"`
}

func runSetupList(sys IO, args []string) int {
	var asJSON bool
	if _, ok := parseArgs(sys, "setup list", args, 0, func(f *flagSet) { f.BoolVar(&asJSON, "json", false, "machine-readable output") }); !ok {
		return 2
	}
	r, m, l, err := openSetup(sys)
	if err != nil {
		return fail(sys, err)
	}
	st, err := setup.LoadState(m.Store)
	if err != nil {
		return fail(sys, err)
	}
	views := []setupEntryView{}
	for _, e := range l.Entries {
		views = append(views, setupEntryView{e, st.Recorded(e)})
	}
	if asJSON {
		return writeJSON(sys, struct {
			Version  int              `json:"version"`
			Platform string           `json:"platform"`
			Entries  []setupEntryView `json:"entries"`
		}{1, r.Platform(), views})
	}
	if len(views) == 0 {
		fmt.Fprintf(sys.Out, "the setup list (%s) is empty — `muster setup add <name> --install <cmd>` adds an entry\n", r.Platform())
		return 0
	}
	fmt.Fprintf(sys.Out, "setup list (%s):\n", r.Platform())
	tw := tabwriter.NewWriter(sys.Out, 0, 0, 2, ' ', 0)
	for _, v := range views {
		here := "not installed"
		if v.Installed {
			here = "installed"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", v.Name, here, v.Install)
		if v.Update != "" {
			fmt.Fprintf(tw, "  \t\tupdate: %s\n", v.Update)
		}
		if v.Check != "" {
			fmt.Fprintf(tw, "  \t\tcheck: %s\n", v.Check)
		}
	}
	tw.Flush()
	return 0
}

func runSetupAdd(sys IO, args []string) int {
	var e setup.Entry
	var asJSON bool
	pos, ok := parseArgs(sys, "setup add", args, 1, func(f *flagSet) {
		f.StringVar(&e.Install, "install", "", "the command that installs it")
		f.StringVar(&e.Update, "update", "", "the command that updates it (`muster update` runs it)")
		f.StringVar(&e.Check, "check", "", "a command that exits 0 when it is installed already")
		f.BoolVar(&asJSON, "json", false, "machine-readable output")
	})
	if !ok {
		return 2
	}
	if e.Install == "" {
		return usageError(sys, "setup add", "--install is required")
	}
	e.Name = pos[0]
	r, _, _, err := openSetup(sys)
	if err != nil {
		return fail(sys, err)
	}
	err = r.Edit(func() error {
		l, err := setup.Load(r.SetupList())
		if err != nil {
			return err
		}
		if err := l.Add(e); err != nil {
			return err
		}
		return l.Save(r.SetupList())
	})
	if err != nil {
		return fail(sys, err)
	}
	if asJSON {
		return writeJSON(sys, struct {
			Version int         `json:"version"`
			Entry   setup.Entry `json:"entry"`
		}{1, e})
	}
	fmt.Fprintf(sys.Out, "added %s to the setup list (%s) — `muster setup install %s` installs it here, the next `muster sync` sends it\n", e.Name, r.Platform(), e.Name)
	return 0
}

func runSetupRm(sys IO, args []string) int {
	var asJSON bool
	pos, ok := parseArgs(sys, "setup rm", args, 1, func(f *flagSet) { f.BoolVar(&asJSON, "json", false, "machine-readable output") })
	if !ok {
		return 2
	}
	r, _, _, err := openSetup(sys)
	if err != nil {
		return fail(sys, err)
	}
	err = r.Edit(func() error {
		l, err := setup.Load(r.SetupList())
		if err != nil {
			return err
		}
		if err := l.Remove(pos[0]); err != nil {
			return err
		}
		return l.Save(r.SetupList())
	})
	if err != nil {
		return fail(sys, err)
	}
	if asJSON {
		return writeJSON(sys, struct {
			Version int    `json:"version"`
			Removed string `json:"removed"`
		}{1, pos[0]})
	}
	fmt.Fprintf(sys.Out, "removed %s from the setup list (it stays installed here)\n", pos[0])
	return 0
}

// setupResult is what install or update did with one entry.
type setupResult struct {
	Name     string `json:"name"`
	Action   string `json:"action"` // installed, present, updated, failed or skipped
	ExitCode int    `json:"exitCode,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// setupRun runs setup list commands for install and update: the commands'
// output goes to the terminal (to stderr under --json), and what got
// installed is recorded as it happens.
type setupRun struct {
	sys     IO
	m       syncrepo.Machine
	asJSON  bool
	state   setup.State
	results []setupResult
}

func (sr *setupRun) out() io.Writer {
	if sr.asJSON {
		return sr.sys.Err
	}
	return sr.sys.Out
}

func (sr *setupRun) run(line string, quiet bool) (int, error) {
	out, errOut := sr.out(), sr.sys.Err
	if quiet {
		out, errOut = io.Discard, io.Discard
	}
	var in io.Reader
	if sr.sys.In != nil && !quiet {
		in = sr.sys.In
	}
	return setup.Run(line, sr.sys.Environ(), in, out, errOut)
}

func (sr *setupRun) record(e setup.Entry) error {
	sr.state.Installed[e.Name] = setup.Installed{Install: e.Install, At: time.Now().UTC()}
	return sr.m.Store.WithLock(func() error {
		st, err := setup.LoadState(sr.m.Store)
		if err != nil {
			return err
		}
		st.Installed[e.Name] = sr.state.Installed[e.Name]
		return st.Save(sr.m.Store)
	})
}

// installed reports whether the entry is on this machine: recorded, or its
// check passes (which gets it recorded).
func (sr *setupRun) installed(e setup.Entry) (bool, error) {
	if e.Check != "" {
		code, err := sr.run(e.Check, true)
		if err != nil || code != 0 {
			return false, err
		}
		if !sr.state.Recorded(e) {
			return true, sr.record(e)
		}
		return true, nil
	}
	return sr.state.Recorded(e), nil
}

func (sr *setupRun) note(res setupResult, human string) {
	sr.results = append(sr.results, res)
	if !sr.asJSON {
		fmt.Fprintf(sr.sys.Out, "%s: %s\n", res.Name, human)
	}
}

// finish prints the results and exits 1 if any command failed.
func (sr *setupRun) finish() int {
	code := 0
	for _, r := range sr.results {
		if r.Action == "failed" {
			code = 1
		}
	}
	if sr.asJSON {
		if writeJSON(sr.sys, struct {
			Version int           `json:"version"`
			Results []setupResult `json:"results"`
		}{1, sr.results}) != 0 {
			return 1
		}
	}
	return code
}

// startSetupRun parses `<name>... [flags]`, opens the setup list and picks
// the named entries (all of them when none is named).
func startSetupRun(sys IO, name string, args []string, define func(*flagSet)) (*setupRun, []setup.Entry, int) {
	sr := &setupRun{sys: sys}
	names, ok := parseArgs(sys, name, args, leading(args), func(f *flagSet) {
		f.BoolVar(&sr.asJSON, "json", false, "machine-readable output")
		define(f)
	})
	if !ok {
		return nil, nil, 2
	}
	_, m, l, err := openSetup(sys)
	if err != nil {
		return nil, nil, fail(sys, err)
	}
	sr.m = m
	if sr.state, err = setup.LoadState(m.Store); err != nil {
		return nil, nil, fail(sys, err)
	}
	entries := l.Entries
	if len(names) > 0 {
		entries = nil
		for _, n := range names {
			e, ok := l.Get(n)
			if !ok {
				return nil, nil, fail(sys, fmt.Errorf("the setup list has no %q (see `muster setup list`)", n))
			}
			entries = append(entries, e)
		}
	}
	sr.results = []setupResult{}
	return sr, entries, -1
}

func runSetupInstall(sys IO, args []string) int {
	var force bool
	sr, entries, code := startSetupRun(sys, "setup install", args, func(f *flagSet) {
		f.BoolVar(&force, "force", false, "run the install command even if it is installed already")
	})
	if code >= 0 {
		return code
	}
	for _, e := range entries {
		if !force {
			present, err := sr.installed(e)
			if err != nil {
				return fail(sys, err)
			}
			if present {
				sr.note(setupResult{Name: e.Name, Action: "present"}, "installed already")
				continue
			}
		}
		fmt.Fprintf(sr.out(), "==> %s: %s\n", e.Name, e.Install)
		exit, err := sr.run(e.Install, false)
		if err != nil {
			return fail(sys, err)
		}
		if exit != 0 {
			sr.note(setupResult{Name: e.Name, Action: "failed", ExitCode: exit}, fmt.Sprintf("install failed (exit %d)", exit))
			continue
		}
		if err := sr.record(e); err != nil {
			return fail(sys, err)
		}
		sr.note(setupResult{Name: e.Name, Action: "installed"}, "installed")
	}
	return sr.finish()
}

func runUpdate(sys IO, args []string) int {
	sr, entries, code := startSetupRun(sys, "update", args, func(*flagSet) {})
	if code >= 0 {
		return code
	}
	for _, e := range entries {
		if e.Update == "" {
			sr.note(setupResult{Name: e.Name, Action: "skipped", Reason: "no update command"}, "no update command")
			continue
		}
		present, err := sr.installed(e)
		if err != nil {
			return fail(sys, err)
		}
		if !present {
			reason := "not installed here — `muster setup install " + e.Name + "`"
			sr.note(setupResult{Name: e.Name, Action: "skipped", Reason: reason}, reason)
			continue
		}
		fmt.Fprintf(sr.out(), "==> %s: %s\n", e.Name, e.Update)
		exit, err := sr.run(e.Update, false)
		if err != nil {
			return fail(sys, err)
		}
		if exit != 0 {
			sr.note(setupResult{Name: e.Name, Action: "failed", ExitCode: exit}, fmt.Sprintf("update failed (exit %d)", exit))
			continue
		}
		sr.note(setupResult{Name: e.Name, Action: "updated"}, "updated")
	}
	return sr.finish()
}

// setupHint names the setup list's entries this machine hasn't installed, for
// the end of a sync.
func setupHint(r *syncrepo.Repo, m syncrepo.Machine) string {
	l, err := setup.Load(r.SetupList())
	if err != nil {
		return ""
	}
	st, err := setup.LoadState(m.Store)
	if err != nil {
		return ""
	}
	var missing []string
	for _, e := range l.Entries {
		if !st.Recorded(e) {
			missing = append(missing, e.Name)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	return fmt.Sprintf("setup list: %s not installed here — `muster setup install`", strings.Join(missing, ", "))
}
