// Package quota reads how much quota each subscription account has left.
// Every source — a gateway key, a local login of another client — is read on
// its own and reports on its own: a failed source carries its error and never
// fails the others. Sources only read: none spends quota, and none refreshes a
// token it borrowed from another client's login.
package quota

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Window is one quota window of a subscription account.
type Window struct {
	// Kind names the window by its length: "5h", "week", "month", or e.g.
	// "2d" for any other.
	Kind string `json:"kind"`
	// Used is the fraction used, 0..1.
	Used     float64   `json:"used"`
	ResetsAt time.Time `json:"resetsAt,omitzero"`
}

// Expired reports whether the window has passed its reset time since it was
// read; its Used is then stale (the real value is lower).
func (w Window) Expired(now time.Time) bool {
	return !w.ResetsAt.IsZero() && !w.ResetsAt.After(now)
}

// Account is one subscription account's quota, as one source saw it.
type Account struct {
	// Platform is whose subscription it is: claude, codex, cursor, grok.
	Platform string `json:"platform"`
	// ID identifies the account within its platform, when the source tells.
	ID   string `json:"id,omitempty"`
	Plan string `json:"plan,omitempty"`
	// State is a non-default availability the source reports, e.g. "cooldown".
	State   string   `json:"state,omitempty"`
	Windows []Window `json:"windows"`
	// CheckedAt is when the numbers were observed.
	CheckedAt time.Time `json:"checkedAt,omitzero"`
	// Current marks the account the asking session is pinned to.
	Current bool `json:"current,omitempty"`
	// Via lists the credentials the account is reachable through (gateway pools).
	Via []string `json:"via,omitempty"`
	// Note is extra context such as a credit balance.
	Note string `json:"note,omitempty"`
}

// Pressure is how close the account is to a limit: its fullest live window.
func (a Account) Pressure(now time.Time) float64 {
	p := 0.0
	for _, w := range a.Windows {
		if !w.Expired(now) {
			p = max(p, w.Used)
		}
	}
	return p
}

// Spend is what a credential spent over a period.
type Spend struct {
	Requests int     `json:"requests"`
	CostUSD  float64 `json:"costUSD"`
}

// Usage is what a credential itself has spent.
type Usage struct {
	Today Spend `json:"today"`
	Week  Spend `json:"week"`
	Month Spend `json:"month"`
}

// Report is one source's answer.
type Report struct {
	// Source is "astergate:<credential>", "codex", "cursor" or "grok".
	Source    string    `json:"source"`
	Accounts  []Account `json:"accounts"`
	Usage     *Usage    `json:"usage,omitempty"`
	Err       string    `json:"error,omitempty"`
	FetchedAt time.Time `json:"fetchedAt"`
}

// Source is one place quota can be read from.
type Source struct {
	Name  string
	Fetch func(ctx context.Context, c *http.Client) (Report, error)
}

// fetchTimeout bounds each source.
const fetchTimeout = 10 * time.Second

// Collect reads every source concurrently and returns their reports in source
// order. A source that fails or times out reports its error.
func Collect(ctx context.Context, c *http.Client, sources []Source) []Report {
	reports := make([]Report, len(sources))
	var wg sync.WaitGroup
	for i, s := range sources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
			defer cancel()
			r, err := s.Fetch(ctx, c)
			r.Source, r.FetchedAt = s.Name, time.Now()
			if err != nil {
				r = Report{Source: s.Name, FetchedAt: r.FetchedAt, Err: err.Error()}
			}
			if r.Accounts == nil {
				r.Accounts = []Account{}
			}
			reports[i] = r
		}()
	}
	wg.Wait()
	return reports
}

// Merge folds the reports into one account list: a gateway account reachable
// through several credentials appears once, with the freshest numbers and
// every credential in Via. Order follows the reports.
func Merge(reports []Report) []Account {
	var out []Account
	index := map[string]int{}
	for _, r := range reports {
		for _, a := range r.Accounts {
			if a.ID == "" {
				out = append(out, a)
				continue
			}
			key := a.Platform + "\x00" + a.ID
			i, seen := index[key]
			if !seen {
				index[key] = len(out)
				out = append(out, a)
				continue
			}
			via := union(out[i].Via, a.Via)
			current := out[i].Current || a.Current
			if a.CheckedAt.After(out[i].CheckedAt) {
				out[i] = a
			}
			out[i].Via, out[i].Current = via, current
		}
	}
	return out
}

func union(a, b []string) []string {
	set := map[string]bool{}
	var out []string
	for _, s := range append(append([]string{}, a...), b...) {
		if !set[s] {
			set[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// windowKind names a window by its length.
func windowKind(d time.Duration) string {
	switch {
	case d == 5*time.Hour:
		return "5h"
	case d == 7*24*time.Hour:
		return "week"
	case d >= 28*24*time.Hour && d <= 31*24*time.Hour:
		return "month"
	case d%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	default:
		return fmt.Sprintf("%dh", d/time.Hour)
	}
}

// getJSON performs one request and decodes a 200 response into v. Any other
// status is an error carrying the start of the body.
func getJSON(ctx context.Context, c *http.Client, req *http.Request, v any) error {
	req = req.WithContext(ctx)
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		snippet := strings.Join(strings.Fields(string(body)), " ")
		if len(snippet) > 120 {
			snippet = snippet[:120] + "…"
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("unexpected response: %w", err)
	}
	return nil
}

// errExpired is a borrowed token past its expiry: the client that owns the
// login refreshes it the next time it runs, and Muster never does.
func errExpired(client string) error {
	return fmt.Errorf("the %s login has expired — open %s once to refresh it", client, client)
}

// jwtExpiry is the exp claim of a JWT, zero when absent or unreadable.
func jwtExpiry(token string) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(b, &claims) != nil || claims.Exp == 0 {
		return time.Time{}
	}
	return time.Unix(claims.Exp, 0)
}

// ErrNoLogin means a local-login source found no login on this machine.
var ErrNoLogin = errors.New("not logged in on this machine")
