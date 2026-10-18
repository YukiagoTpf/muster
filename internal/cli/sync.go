package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/YukiagoTpf/muster/internal/claude"
	"github.com/YukiagoTpf/muster/internal/platform"
	"github.com/YukiagoTpf/muster/internal/store"
	"github.com/YukiagoTpf/muster/internal/syncrepo"
)

// syncMachine is this machine as the sync sees it.
func syncMachine(sys IO) (syncrepo.Machine, error) {
	s, err := store.Open(sys.Getenv)
	if err != nil {
		return syncrepo.Machine{}, err
	}
	settings, err := claude.SettingsPath(sys.Getenv)
	if err != nil {
		return syncrepo.Machine{}, err
	}
	home, err := platform.UserHome(sys.Getenv)
	if err != nil {
		return syncrepo.Machine{}, err
	}
	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}
	return syncrepo.Machine{Store: s, Settings: settings, Home: home, Platform: platform.Name(), Host: host, Env: sys.Environ()}, nil
}

func openRepo(sys IO) (*syncrepo.Repo, error) {
	m, err := syncMachine(sys)
	if err != nil {
		return nil, err
	}
	return syncrepo.Open(m)
}

// leading counts the positional arguments before the first flag, for
// commands that take any number of them.
func leading(args []string) int {
	n := 0
	for n < len(args) && !strings.HasPrefix(args[n], "-") {
		n++
	}
	return n
}

// askPassphrase reads the sync repo's passphrase: from the terminal without
// echo, or, with fromStdin, as the first line of stdin.
func askPassphrase(sys IO, fromStdin bool) syncrepo.Passphrase {
	return func(create bool) (string, error) {
		if fromStdin {
			line, err := bufio.NewReader(sys.In).ReadString('\n')
			if pass := strings.TrimRight(line, "\r\n"); pass != "" {
				return pass, nil
			}
			if err != nil {
				return "", fmt.Errorf("no passphrase on stdin: %w", err)
			}
			return "", errors.New("the passphrase is empty")
		}
		if sys.In == nil || !term.IsTerminal(int(sys.In.Fd())) {
			return "", errors.New("no terminal to ask for the passphrase on; pass --passphrase-stdin and give it on stdin")
		}
		read := func(prompt string) (string, error) {
			fmt.Fprint(sys.Err, prompt)
			b, err := term.ReadPassword(int(sys.In.Fd()))
			fmt.Fprintln(sys.Err)
			return string(b), err
		}
		prompt := "Passphrase of the sync repo: "
		if create {
			prompt = "New passphrase for the sync repo (it encrypts your credentials; every machine that joins needs it): "
		}
		pass, err := read(prompt)
		if err != nil {
			return "", err
		}
		if pass == "" {
			return "", errors.New("the passphrase is empty")
		}
		if create {
			again, err := read("Repeat it: ")
			if err != nil {
				return "", err
			}
			if again != pass {
				return "", errors.New("the two passphrases differ")
			}
		}
		return pass, nil
	}
}

func itemList(items []syncrepo.Item) string {
	var names []string
	for _, it := range items {
		names = append(names, it.String())
	}
	return strings.Join(names, ", ")
}

type syncView struct {
	Version int `json:"version"`
	syncrepo.Result
}

// reportSync prints what a sync or a join did; a conflict exits 1.
func reportSync(sys IO, r *syncrepo.Repo, m syncrepo.Machine, res syncrepo.Result, asJSON bool) int {
	code := 0
	if len(res.Conflicts) > 0 {
		code = 1
	}
	if asJSON {
		if writeJSON(sys, syncView{1, res}) != 0 {
			return 1
		}
		return code
	}
	if len(res.Sent) > 0 {
		fmt.Fprintf(sys.Out, "sent: %s\n", itemList(res.Sent))
	}
	if len(res.Received) > 0 {
		fmt.Fprintf(sys.Out, "received: %s\n", itemList(res.Received))
	}
	if len(res.Conflicts) > 0 {
		fmt.Fprintf(sys.Out, "conflict: %s changed here and on another machine since the last sync; nothing was applied here.\n", itemList(res.Conflicts))
		fmt.Fprintf(sys.Out, "  muster sync --prefer local    keep this machine's side of each conflict\n")
		fmt.Fprintf(sys.Out, "  muster sync --prefer remote   take the other machines' side\n")
		fmt.Fprintf(sys.Out, "  git -C %q diff HEAD origin/main   shows how the two differ\n", r.Dir())
		return code
	}
	if len(res.Sent)+len(res.Received) == 0 {
		fmt.Fprintln(sys.Out, "already in sync")
	}
	if hint := setupHint(r, m); hint != "" {
		fmt.Fprintln(sys.Out, hint)
	}
	return code
}

func runSyncInit(sys IO, args []string) int {
	var fromStdin, asJSON bool
	pos, ok := parseArgs(sys, "sync init", args, 1, func(f *flagSet) {
		f.BoolVar(&fromStdin, "passphrase-stdin", false, "read the passphrase from the first line of stdin")
		f.BoolVar(&asJSON, "json", false, "machine-readable output")
	})
	if !ok {
		return 2
	}
	m, err := syncMachine(sys)
	if err != nil {
		return fail(sys, err)
	}
	r, res, err := syncrepo.Join(m, pos[0], askPassphrase(sys, fromStdin))
	if err != nil {
		return fail(sys, err)
	}
	if !asJSON {
		fmt.Fprintf(sys.Out, "this machine now syncs with %s (%s)\n", pos[0], m.Platform)
	}
	return reportSync(sys, r, m, res, asJSON)
}

func runSync(sys IO, args []string) int {
	var prefer string
	var asJSON bool
	if _, ok := parseArgs(sys, "sync", args, 0, func(f *flagSet) {
		f.StringVar(&prefer, "prefer", "", "settle conflicts: local or remote")
		f.BoolVar(&asJSON, "json", false, "machine-readable output")
	}); !ok {
		return 2
	}
	if prefer != "" && prefer != "local" && prefer != "remote" {
		return usageError(sys, "sync", fmt.Sprintf("--prefer %q: want local or remote", prefer))
	}
	m, err := syncMachine(sys)
	if err != nil {
		return fail(sys, err)
	}
	r, err := syncrepo.Open(m)
	if err != nil {
		return fail(sys, err)
	}
	res, err := r.Sync(prefer)
	if err != nil {
		return fail(sys, err)
	}
	return reportSync(sys, r, m, res, asJSON)
}

func runSyncStatus(sys IO, args []string) int {
	var asJSON bool
	if _, ok := parseArgs(sys, "sync status", args, 0, func(f *flagSet) { f.BoolVar(&asJSON, "json", false, "machine-readable output") }); !ok {
		return 2
	}
	r, err := openRepo(sys)
	if err != nil {
		return fail(sys, err)
	}
	st, err := r.Status()
	if err != nil {
		return fail(sys, err)
	}
	if asJSON {
		return writeJSON(sys, struct {
			Version int `json:"version"`
			syncrepo.Status
		}{1, st})
	}
	fmt.Fprintf(sys.Out, "sync repo: %s (%s)\n", st.Repo, st.Platform)
	if len(st.Pending) == 0 {
		fmt.Fprintln(sys.Out, "nothing pending")
	} else {
		fmt.Fprintln(sys.Out, "the next `muster sync` sends:")
		for _, c := range st.Pending {
			fmt.Fprintf(sys.Out, "  %-9s %s\n", c.Change, c.Item)
		}
	}
	if st.Unpushed {
		fmt.Fprintln(sys.Out, "a sync committed changes it could not push yet; `muster sync` pushes them")
	}
	printTracked(sys, st.Tracked)
	return 0
}

func printTracked(sys IO, tracked []string) {
	if len(tracked) == 0 {
		fmt.Fprintln(sys.Out, "no tracked files (`muster track <path>` adds one)")
		return
	}
	fmt.Fprintf(sys.Out, "tracked files: %s\n", strings.Join(tracked, ", "))
}

// runTrack tracks files, or lists the tracked ones.
func runTrack(sys IO, args []string) int { return trackOrUntrack(sys, "track", args) }

func runUntrack(sys IO, args []string) int { return trackOrUntrack(sys, "untrack", args) }

func trackOrUntrack(sys IO, name string, args []string) int {
	var asJSON bool
	paths, ok := parseArgs(sys, name, args, leading(args), func(f *flagSet) { f.BoolVar(&asJSON, "json", false, "machine-readable output") })
	if !ok {
		return 2
	}
	if name == "untrack" && len(paths) == 0 {
		return usageError(sys, name, "name the file(s) to stop tracking")
	}
	r, err := openRepo(sys)
	if err != nil {
		return fail(sys, err)
	}
	for _, p := range paths {
		change := r.Track
		if name == "untrack" {
			change = r.Untrack
		}
		file, err := change(p)
		if err != nil {
			return fail(sys, err)
		}
		if !asJSON && name == "track" {
			fmt.Fprintf(sys.Out, "tracking %s — the next `muster sync` sends it\n", file)
		} else if !asJSON {
			fmt.Fprintf(sys.Out, "no longer tracking %s (the file stays on every machine)\n", file)
		}
	}
	tracked, err := r.Tracked()
	if err != nil {
		return fail(sys, err)
	}
	if asJSON {
		return writeJSON(sys, struct {
			Version int      `json:"version"`
			Tracked []string `json:"tracked"`
		}{1, tracked})
	}
	if len(paths) == 0 {
		printTracked(sys, tracked)
	}
	return 0
}
