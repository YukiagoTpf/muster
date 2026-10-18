package quota

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GrokAuthPath is where the Grok CLI keeps its login.
func GrokAuthPath(home string) string { return filepath.Join(home, ".grok", "auth.json") }

// Grok reads a Grok subscription's credit usage through the Grok CLI's own
// login. Its access token lives about six hours and the CLI renews it as it
// runs; Muster never does, so a machine that hasn't run grok lately reports
// the login as expired.
func Grok(authPath, apiBase string) Source {
	return Source{
		Name: "grok",
		Fetch: func(ctx context.Context, c *http.Client) (Report, error) {
			b, err := os.ReadFile(authPath)
			if errors.Is(err, os.ErrNotExist) {
				return Report{}, ErrNoLogin
			}
			if err != nil {
				return Report{}, err
			}
			// One entry per issuer and client: "https://auth.x.ai::<client-id>".
			var entries map[string]struct {
				Key       string    `json:"key"`
				ExpiresAt time.Time `json:"expires_at"`
			}
			if err := json.Unmarshal(b, &entries); err != nil {
				return Report{}, fmt.Errorf("%s: %w", authPath, err)
			}
			var token string
			var expires time.Time
			for _, e := range entries {
				if e.Key != "" {
					token, expires = e.Key, e.ExpiresAt
					break
				}
			}
			if token == "" {
				return Report{}, ErrNoLogin
			}
			if !expires.IsZero() && expires.Before(time.Now()) {
				return Report{}, errExpired("grok")
			}
			// Without format=credits the endpoint answers in an older monthly
			// shape whose numbers are all zero.
			req, err := http.NewRequest(http.MethodGet, strings.TrimRight(apiBase, "/")+"/v1/billing?format=credits", nil)
			if err != nil {
				return Report{}, err
			}
			req.Header.Set("Authorization", "Bearer "+token)
			var resp grokBilling
			if err := fetchJSON(ctx, c, req, &resp); err != nil {
				return Report{}, err
			}
			return resp.report(), nil
		},
	}
}

type grokBilling struct {
	Config struct {
		CurrentPeriod struct {
			Type  string    `json:"type"`
			Start time.Time `json:"start"`
			End   time.Time `json:"end"`
		} `json:"currentPeriod"`
		CreditUsagePercent *float64 `json:"creditUsagePercent"`
		ProductUsage       []struct {
			Product      string   `json:"product"`
			UsagePercent *float64 `json:"usagePercent"`
		} `json:"productUsage"`
	} `json:"config"`
}

func (g grokBilling) report() Report {
	cfg := g.Config
	a := Account{Platform: "grok", CheckedAt: time.Now(), Windows: []Window{}}
	if cfg.CreditUsagePercent != nil {
		kind := "week"
		if p := cfg.CurrentPeriod; !p.Start.IsZero() && p.End.After(p.Start) {
			kind = windowKind(p.End.Sub(p.Start).Round(time.Hour))
		}
		a.Windows = append(a.Windows, Window{Kind: kind, Used: *cfg.CreditUsagePercent / 100, ResetsAt: cfg.CurrentPeriod.End})
	}
	var products []string
	for _, p := range cfg.ProductUsage {
		if p.UsagePercent != nil {
			products = append(products, fmt.Sprintf("%s %d%%", p.Product, int(math.Round(*p.UsagePercent))))
		}
	}
	a.Note = strings.Join(products, " · ")
	return Report{Accounts: []Account{a}}
}
