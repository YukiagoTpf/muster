package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

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

// setupRun starts an install or update: `<name>... [flags]`, the named
// entries (all of them when none is named), and a runner whose commands
// print to the terminal — to stderr under --json, where stdout carries the
// results.
func setupRun(sys IO, name string, args []string, define func(*flagSet)) (*setup.Runner, []setup.Entry, bool, int) {
	var asJSON bool
	names, ok := parseArgs(sys, name, args, leading(args), func(f *flagSet) {
		f.BoolVar(&asJSON, "json", false, "machine-readable output")
		define(f)
	})
	if !ok {
		return nil, nil, false, 2
	}
	_, m, l, err := openSetup(sys)
	if err != nil {
		return nil, nil, false, fail(sys, err)
	}
	entries, err := l.Select(names)
	if err != nil {
		return nil, nil, false, fail(sys, err)
	}
	out := sys.Out
	if asJSON {
		out = sys.Err
	}
	var in io.Reader
	if sys.In != nil {
		in = sys.In
	}
	r, err := setup.NewRunner(m.Store, sys.Environ(), in, out)
	if err != nil {
		return nil, nil, false, fail(sys, err)
	}
	return r, entries, asJSON, -1
}

// setupResults collects results, printing each as it comes unless --json.
type setupResults struct {
	sys     IO
	asJSON  bool
	results []setup.Result
}

func (sr *setupResults) report(res setup.Result) {
	sr.results = append(sr.results, res)
	if !sr.asJSON {
		fmt.Fprintf(sr.sys.Out, "%s: %s\n", res.Name, res.Text())
	}
}

// finish prints the results under --json and exits 1 if a command failed.
func (sr *setupResults) finish(err error) int {
	if err != nil {
		return fail(sr.sys, err)
	}
	code := 0
	for _, r := range sr.results {
		if r.Action == "failed" {
			code = 1
		}
	}
	if sr.asJSON {
		if writeJSON(sr.sys, struct {
			Version int            `json:"version"`
			Results []setup.Result `json:"results"`
		}{1, sr.results}) != 0 {
			return 1
		}
	}
	return code
}

func runSetupInstall(sys IO, args []string) int {
	var force bool
	r, entries, asJSON, code := setupRun(sys, "setup install", args, func(f *flagSet) {
		f.BoolVar(&force, "force", false, "run the install command even if it is installed already")
	})
	if code >= 0 {
		return code
	}
	sr := &setupResults{sys: sys, asJSON: asJSON, results: []setup.Result{}}
	return sr.finish(r.Install(entries, force, sr.report))
}

func runUpdate(sys IO, args []string) int {
	r, entries, asJSON, code := setupRun(sys, "update", args, func(*flagSet) {})
	if code >= 0 {
		return code
	}
	sr := &setupResults{sys: sys, asJSON: asJSON, results: []setup.Result{}}
	return sr.finish(r.Update(entries, sr.report))
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
	missing := st.Missing(l)
	if len(missing) == 0 {
		return ""
	}
	return fmt.Sprintf("setup list: %s not installed here — `muster setup install`", strings.Join(missing, ", "))
}
