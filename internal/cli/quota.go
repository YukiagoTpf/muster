package cli

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/YukiagoTpf/muster/internal/credential"
	"github.com/YukiagoTpf/muster/internal/platform"
	"github.com/YukiagoTpf/muster/internal/quota"
	"github.com/YukiagoTpf/muster/internal/store"
)

func runQuota(sys IO, args []string) int {
	var asJSON, cached bool
	if _, ok := parseArgs(sys, "quota", args, 0, func(f *flagSet) {
		f.BoolVar(&asJSON, "json", false, "machine-readable output")
		f.BoolVar(&cached, "cached", false, "show the last fetched numbers, don't fetch")
	}); !ok {
		return 2
	}
	s, err := store.Open(sys.Getenv)
	if err != nil {
		return fail(sys, err)
	}
	r, err := credential.Load(s)
	if err != nil {
		return fail(sys, err)
	}
	var reports []quota.Report
	if cached {
		reports = quota.LoadCache(s)
	} else if reports, err = quota.Refresh(s, r, sys.Getenv); err != nil {
		return fail(sys, err)
	}
	if asJSON {
		return writeJSON(sys, quota.OverviewOf(reports))
	}
	printOverview(colorOut(sys), reports, time.Now())
	return 0
}

// sourceTitle is how a source is introduced in the human overview.
func sourceTitle(source string) string {
	switch kind, name, _ := strings.Cut(source, ":"); kind {
	case "astergate":
		return "AsterGate · " + name
	default:
		return strings.ToUpper(kind[:1]) + kind[1:] + " · local login"
	}
}

func printOverview(sys IO, reports []quota.Report, now time.Time) {
	if len(reports) == 0 {
		fmt.Fprintln(sys.Out, "no quota sources: add a gateway credential (`muster cred add`) or log in to a supported client on this machine")
		return
	}
	shown := map[string]bool{}
	for i, r := range reports {
		if i > 0 {
			fmt.Fprintln(sys.Out)
		}
		fmt.Fprintf(sys.Out, "%s  %s%s%s\n", sourceTitle(r.Source), quota.Dim, quota.Age(r.FetchedAt, now), quota.Reset)
		if r.Err != "" {
			fmt.Fprintf(sys.Out, "  %serror:%s %s\n", "\x1b[1;31m", quota.Reset, r.Err)
			continue
		}
		accounts := append([]quota.Account(nil), r.Accounts...)
		sort.SliceStable(accounts, func(i, j int) bool { return accounts[i].Pressure(now) < accounts[j].Pressure(now) })
		for _, a := range accounts {
			key := a.Platform + "\x00" + a.ID
			if a.ID != "" && shown[key] {
				continue // listed under an earlier source already
			}
			shown[key] = true
			fmt.Fprintln(sys.Out, "  "+accountLine(a, now))
		}
		if len(r.Accounts) == 0 {
			fmt.Fprintf(sys.Out, "  %sno subscription accounts reported%s\n", quota.Dim, quota.Reset)
		}
		if u := r.Usage; u != nil {
			fmt.Fprintf(sys.Out, "  %s%s spent%s today $%.2f (%d req) · week $%.2f · month $%.2f\n",
				quota.Dim, quota.CredentialOf(r.Source), quota.Reset, u.Today.CostUSD, u.Today.Requests, u.Week.CostUSD, u.Month.CostUSD)
		}
	}
}

// accountLine is one account: plan, a short id, and every window.
func accountLine(a quota.Account, now time.Time) string {
	plan := a.Plan
	if plan == "" {
		plan = "-"
	}
	id := a.ID
	if len(id) > 8 {
		id = id[:8]
	}
	line := fmt.Sprintf("\x1b[1;35m%-6s\x1b[0m %s%-8s%s", plan, quota.Dim, id, quota.Reset)
	if a.State != "" {
		line += "  \x1b[1;31m" + a.State + quota.Reset
	}
	for _, w := range a.Windows {
		line += "   " + windowText(w, now)
	}
	if len(a.Windows) == 0 {
		line += "   " + quota.Dim + "no windows observed yet" + quota.Reset
	}
	if a.Note != "" {
		line += "   " + quota.Dim + a.Note + quota.Reset
	}
	return line
}

// windowText is one window: kind, bar, percentage and reset time. A window
// past its reset time shows that instead of a stale percentage.
func windowText(w quota.Window, now time.Time) string {
	if w.Expired(now) {
		return fmt.Sprintf("%s%-5s reset since%s", quota.Dim, w.Kind, quota.Reset)
	}
	text := fmt.Sprintf("%s%-5s%s %s %s", quota.Dim, w.Kind, quota.Reset, quota.Bar(w.Used, 10), quota.Percent(w.Used))
	if r := quota.ResetLabel(w.ResetsAt, now); r != "" {
		text += fmt.Sprintf(" %s↻%s%s", quota.Dim, r, quota.Reset)
	}
	return text
}

// staleAfter is how old the quota cache may get before a view that shows it
// starts a background refresh.
const staleAfter = time.Minute

// spawnBackground starts muster again, detached, with args. Tests replace it:
// spawning the test binary would run the tests.
var spawnBackground = func(sys IO, args ...string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(self, args...)
	cmd.Env = sys.Environ()
	return platform.StartDetached(cmd)
}

// refreshIfStale starts a background quota refresh when the cache is older
// than staleAfter (or missing) and there is something to read.
func refreshIfStale(sys IO, reports []quota.Report, now time.Time) {
	fresh := len(reports) > 0
	for _, r := range reports {
		if now.Sub(r.FetchedAt) > staleAfter {
			fresh = false
		}
	}
	if !fresh {
		spawnBackground(sys, "__refresh-quota")
	}
}

// runRefreshQuota is the background refresh: fetch everything, cache it,
// print nothing.
func runRefreshQuota(sys IO, _ []string) int {
	s, err := store.Open(sys.Getenv)
	if err != nil {
		return 1
	}
	r, err := credential.Load(s)
	if err != nil {
		return 1
	}
	if _, err := quota.Refresh(s, r, sys.Getenv); err != nil {
		return 1
	}
	return 0
}

// quotaSummary is the picker's quota cell for a gateway credential: the
// windows of the account in its pool with the most headroom — the one a new
// session can count on.
func quotaSummary(credential string, reports []quota.Report, now time.Time) string {
	for _, r := range reports {
		if r.Source != "astergate:"+credential {
			continue
		}
		if r.Err != "" {
			return "\x1b[1;31mquota unavailable" + quota.Reset
		}
		var best *quota.Account
		for i := range r.Accounts {
			if best == nil || r.Accounts[i].Pressure(now) < best.Pressure(now) {
				best = &r.Accounts[i]
			}
		}
		if best == nil {
			return ""
		}
		var parts []string
		for _, w := range best.Windows {
			if w.Expired(now) {
				parts = append(parts, quota.Dim+w.Kind+" reset"+quota.Reset)
				continue
			}
			parts = append(parts, quota.Dim+w.Kind+quota.Reset+" "+quota.Bar(w.Used, 5)+" "+quota.Percent(w.Used))
		}
		return strings.Join(parts, "  ")
	}
	return ""
}
