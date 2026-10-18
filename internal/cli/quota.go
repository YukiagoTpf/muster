package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/YukiagoTpf/muster/internal/credential"
	"github.com/YukiagoTpf/muster/internal/platform"
	"github.com/YukiagoTpf/muster/internal/quota"
	"github.com/YukiagoTpf/muster/internal/store"
)

// httpClient is the client every quota source uses. Proxies come from the
// environment (HTTPS_PROXY and friends).
var httpClient = &http.Client{Timeout: 15 * time.Second}

// quotaSources lists every source this machine can read: each gateway
// credential, then each local login of another client.
func quotaSources(sys IO, r *credential.Registry) []quota.Source {
	var sources []quota.Source
	tz := quota.Timezone(sys.Getenv, time.Now())
	for _, c := range r.Credentials {
		key, ok := r.Secret(c.Name)
		if ok && c.Provider == "astergate" {
			sources = append(sources, quota.AsterGate(c.Name, c.BaseURL, key, "", tz))
		}
	}
	home, err := platform.UserHome(sys.Getenv)
	if err != nil {
		return sources
	}
	// A local login is a source only where that client is logged in.
	if path := quota.CodexAuthPath(sys.Getenv, home); exists(path) {
		sources = append(sources, quota.Codex(path, apiBase(sys, "MUSTER_CODEX_API", "https://chatgpt.com/backend-api")))
	}
	if path := quota.GrokAuthPath(home); exists(path) {
		sources = append(sources, quota.Grok(path, apiBase(sys, "MUSTER_GROK_API", "https://cli-chat-proxy.grok.com")))
	}
	return sources
}

// apiBase is a local-login source's API base: the real one, or the env
// override tests point at a fake server.
func apiBase(sys IO, override, real string) string {
	if u := sys.Getenv(override); u != "" {
		return u
	}
	return real
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// refreshQuota reads every source and caches the result.
func refreshQuota(sys IO, s *store.Store, r *credential.Registry) ([]quota.Report, error) {
	reports := quota.Collect(context.Background(), httpClient, quotaSources(sys, r))
	err := s.WithLock(func() error { return quota.SaveCache(s, reports) })
	return reports, err
}

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
	} else if reports, err = refreshQuota(sys, s, r); err != nil {
		return fail(sys, err)
	}
	if asJSON {
		return writeJSON(sys, overviewOf(reports))
	}
	printOverview(colorOut(sys), reports, time.Now())
	return 0
}

type sourceError struct {
	Source string `json:"source"`
	Error  string `json:"error"`
}

type overview struct {
	Version  int                    `json:"version"`
	Accounts []quota.Account        `json:"accounts"`
	Usage    map[string]quota.Usage `json:"usage"`
	Errors   []sourceError          `json:"errors"`
}

// overviewOf is the --json shape: every subscription account once, what each
// credential spent, and which sources failed.
func overviewOf(reports []quota.Report) overview {
	o := overview{Version: 1, Accounts: quota.Merge(reports), Usage: map[string]quota.Usage{}, Errors: []sourceError{}}
	if o.Accounts == nil {
		o.Accounts = []quota.Account{}
	}
	for _, r := range reports {
		if r.Usage != nil {
			o.Usage[credentialOf(r.Source)] = *r.Usage
		}
		if r.Err != "" {
			o.Errors = append(o.Errors, sourceError{r.Source, r.Err})
		}
	}
	return o
}

// credentialOf is the credential name of a gateway source.
func credentialOf(source string) string {
	_, name, _ := strings.Cut(source, ":")
	return name
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
				quota.Dim, credentialOf(r.Source), quota.Reset, u.Today.CostUSD, u.Today.Requests, u.Week.CostUSD, u.Month.CostUSD)
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
