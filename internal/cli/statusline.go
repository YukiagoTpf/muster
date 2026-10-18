package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/YukiagoTpf/muster/internal/claude"
	"github.com/YukiagoTpf/muster/internal/credential"
	"github.com/YukiagoTpf/muster/internal/quota"
	"github.com/YukiagoTpf/muster/internal/store"
)

// A statusline render must take milliseconds — Claude Code cancels a slow
// one — so `muster statusline` never touches the network: it renders the
// session's cached state and, when that is due, starts a detached
// `muster __refresh-statusline <session>` that queries the gateway.

var sessionIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// sessionCredential finds the gateway credential this Claude Code session
// runs on: the one whose base URL and key are the provider env the session
// carries (a launch or the default credential put them there).
func sessionCredential(sys IO, r *credential.Registry) (credential.Credential, string, bool) {
	base := strings.TrimRight(sys.Getenv(claude.BaseURLKey), "/")
	token := sys.Getenv(claude.TokenKey)
	if base == "" || token == "" {
		return credential.Credential{}, "", false
	}
	for _, c := range r.Credentials {
		if key, ok := r.Secret(c.Name); ok && key == token && strings.TrimRight(c.BaseURL, "/") == base && c.Provider == "astergate" {
			return c, key, true
		}
	}
	return credential.Credential{}, "", false
}

func pinnedPath(s *store.Store, session string) string {
	return filepath.Join(s.Path("statusline"), session+".json")
}

func loadPinned(s *store.Store, session string) quota.Pinned {
	var p quota.Pinned
	if _, err := store.ReadJSON(pinnedPath(s, session), &p); err != nil || p.Version != 1 {
		return quota.Pinned{Version: 1}
	}
	return p
}

func savePinned(s *store.Store, session string, p quota.Pinned) error {
	if err := os.MkdirAll(s.Path("statusline"), 0o700); err != nil {
		return err
	}
	return store.WriteJSON(pinnedPath(s, session), p)
}

func runStatusline(sys IO, args []string) int {
	lang := "en"
	if _, ok := parseArgs(sys, "statusline", args, 0, func(f *flagSet) {
		f.StringVar(&lang, "lang", "en", "label language: en or zh")
	}); !ok {
		return 2
	}
	labels, ok := quota.Languages[lang]
	if !ok {
		return usageError(sys, "statusline", fmt.Sprintf("unknown --lang %q (en, zh)", lang))
	}
	// From here on a statusline prints its line or nothing, and exits 0.
	var in struct {
		SessionID string `json:"session_id"`
	}
	if sys.In == nil || json.NewDecoder(io.LimitReader(sys.In, 1<<20)).Decode(&in) != nil || !sessionIDRe.MatchString(in.SessionID) {
		return 0
	}
	s, err := store.Open(sys.Getenv)
	if err != nil {
		return 0
	}
	r, err := credential.Load(s)
	if err != nil {
		return 0
	}
	if _, _, ok := sessionCredential(sys, r); !ok {
		return 0
	}
	now := time.Now()
	p := loadPinned(s, in.SessionID)
	if !now.Before(p.Next) {
		p.Next = now.Add(time.Minute) // one refresh at a time
		if s.WithLock(func() error { return savePinned(s, in.SessionID, p) }) == nil {
			spawnBackground(sys, "__refresh-statusline", in.SessionID)
		}
	}
	if line := p.Line(labels, now); line != "" {
		fmt.Fprintln(sys.Out, line)
	}
	return 0
}

// pinnedKept is how long a session's statusline state outlives its last refresh.
const pinnedKept = 7 * 24 * time.Hour

// runRefreshStatusline queries the gateway for one session and stores what it
// learned, then drops the state of sessions not seen for a week.
func runRefreshStatusline(sys IO, args []string) int {
	if len(args) != 1 || !sessionIDRe.MatchString(args[0]) {
		return 2
	}
	session := args[0]
	s, err := store.Open(sys.Getenv)
	if err != nil {
		return 1
	}
	r, err := credential.Load(s)
	if err != nil {
		return 1
	}
	c, key, ok := sessionCredential(sys, r)
	if !ok {
		return 1
	}
	now := time.Now()
	report := quota.Collect(context.Background(), quota.HTTPClient,
		[]quota.Source{quota.AsterGate(c.Name, c.BaseURL, key, session, quota.Timezone(sys.Getenv, now))})[0]
	err = s.WithLock(func() error {
		return savePinned(s, session, quota.Repin(loadPinned(s, session), report, now))
	})
	if err != nil {
		return 1
	}
	if entries, err := os.ReadDir(s.Path("statusline")); err == nil {
		for _, e := range entries {
			if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > pinnedKept {
				os.Remove(filepath.Join(s.Path("statusline"), e.Name()))
			}
		}
	}
	return 0
}
