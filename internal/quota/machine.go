package quota

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/YukiagoTpf/muster/internal/credential"
	"github.com/YukiagoTpf/muster/internal/platform"
	"github.com/YukiagoTpf/muster/internal/store"
)

// HTTPClient is the client every source uses. Proxies come from the
// environment (HTTPS_PROXY and friends).
var HTTPClient = &http.Client{Timeout: 15 * time.Second}

// Sources lists every source this machine can read: each gateway credential,
// then each local login of another client.
func Sources(r *credential.Registry, getenv func(string) string) []Source {
	var sources []Source
	tz := Timezone(getenv, time.Now())
	for _, c := range r.Credentials {
		key, ok := r.Secret(c.Name)
		if ok && c.Provider == "astergate" {
			sources = append(sources, AsterGate(c.Name, c.BaseURL, key, "", tz))
		}
	}
	home, err := platform.UserHome(getenv)
	if err != nil {
		return sources
	}
	// A local login is a source only where that client is logged in.
	if path := CodexAuthPath(getenv, home); exists(path) {
		sources = append(sources, Codex(path, apiBase(getenv, "MUSTER_CODEX_API", "https://chatgpt.com/backend-api")))
	}
	if dir, err := platform.AppConfigDir(getenv); err == nil {
		if path := CursorStatePath(dir); exists(path) {
			sources = append(sources, Cursor(path, apiBase(getenv, "MUSTER_CURSOR_API", "https://api2.cursor.sh")))
		}
	}
	if path := GrokAuthPath(home); exists(path) {
		sources = append(sources, Grok(path, apiBase(getenv, "MUSTER_GROK_API", "https://cli-chat-proxy.grok.com")))
	}
	return sources
}

// apiBase is a local-login source's API base: the real one, or the env
// override tests point at a fake server.
func apiBase(getenv func(string) string, override, real string) string {
	if u := getenv(override); u != "" {
		return u
	}
	return real
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Refresh reads every source and caches the result.
func Refresh(s *store.Store, r *credential.Registry, getenv func(string) string) ([]Report, error) {
	reports := Collect(context.Background(), HTTPClient, Sources(r, getenv))
	err := s.WithLock(func() error { return SaveCache(s, reports) })
	return reports, err
}

// SourceError is a source that failed.
type SourceError struct {
	Source string `json:"source"`
	Error  string `json:"error"`
}

// Overview is the quota as every surface shows it (`quota --json`, the web
// page): every subscription account once, what each credential spent, which
// sources failed, and when the oldest of the numbers was fetched.
type Overview struct {
	Version   int              `json:"version"`
	Accounts  []Account        `json:"accounts"`
	Usage     map[string]Usage `json:"usage"`
	Errors    []SourceError    `json:"errors"`
	FetchedAt time.Time        `json:"fetchedAt,omitzero"`
}

// OverviewOf folds reports into the overview.
func OverviewOf(reports []Report) Overview {
	o := Overview{Version: 1, Accounts: Merge(reports), Usage: map[string]Usage{}, Errors: []SourceError{}}
	if o.Accounts == nil {
		o.Accounts = []Account{}
	}
	for _, r := range reports {
		if r.Usage != nil {
			o.Usage[CredentialOf(r.Source)] = *r.Usage
		}
		if r.Err != "" {
			o.Errors = append(o.Errors, SourceError{r.Source, r.Err})
		}
		if o.FetchedAt.IsZero() || r.FetchedAt.Before(o.FetchedAt) {
			o.FetchedAt = r.FetchedAt
		}
	}
	return o
}

// CredentialOf is the credential name of a gateway source.
func CredentialOf(source string) string {
	_, name, _ := strings.Cut(source, ":")
	return name
}
