package quota

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CodexAuthPath is where the Codex CLI keeps its login: $CODEX_HOME/auth.json,
// by default ~/.codex/auth.json.
func CodexAuthPath(getenv func(string) string, home string) string {
	if h := getenv("CODEX_HOME"); h != "" {
		return filepath.Join(h, "auth.json")
	}
	return filepath.Join(home, ".codex", "auth.json")
}

// Codex reads a ChatGPT subscription's Codex limits through the Codex CLI's
// own login. The access token is used as is and never refreshed: refreshing
// rotates the CLI's refresh token and would log the CLI out. A token past its
// expiry reports so, and the CLI renews it the next time it runs.
func Codex(authPath, apiBase string) Source {
	return Source{
		Name: "codex",
		Fetch: func(ctx context.Context, c *http.Client) (Report, error) {
			var auth struct {
				AuthMode string `json:"auth_mode"`
				Tokens   struct {
					AccessToken string `json:"access_token"`
					AccountID   string `json:"account_id"`
				} `json:"tokens"`
			}
			b, err := os.ReadFile(authPath)
			if errors.Is(err, os.ErrNotExist) {
				return Report{}, ErrNoLogin
			}
			if err != nil {
				return Report{}, err
			}
			if err := json.Unmarshal(b, &auth); err != nil {
				return Report{}, fmt.Errorf("%s: %w", authPath, err)
			}
			if auth.AuthMode != "chatgpt" || auth.Tokens.AccessToken == "" {
				return Report{}, errors.New("the Codex CLI is not logged in with a ChatGPT account (an API key has no subscription quota)")
			}
			if exp := jwtExpiry(auth.Tokens.AccessToken); !exp.IsZero() && exp.Before(time.Now()) {
				return Report{}, errExpired("codex")
			}
			req, err := http.NewRequest(http.MethodGet, strings.TrimRight(apiBase, "/")+"/wham/usage", nil)
			if err != nil {
				return Report{}, err
			}
			req.Header.Set("Authorization", "Bearer "+auth.Tokens.AccessToken)
			req.Header.Set("ChatGPT-Account-Id", auth.Tokens.AccountID)
			var resp codexUsage
			if err := getJSON(ctx, c, req, &resp); err != nil {
				return Report{}, err
			}
			return resp.report(auth.Tokens.AccountID), nil
		},
	}
}

type codexWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int64   `json:"limit_window_seconds"`
	ResetAt            int64   `json:"reset_at"`
}

type codexUsage struct {
	PlanType  string `json:"plan_type"`
	RateLimit struct {
		LimitReached    bool         `json:"limit_reached"`
		PrimaryWindow   *codexWindow `json:"primary_window"`
		SecondaryWindow *codexWindow `json:"secondary_window"`
	} `json:"rate_limit"`
	Credits *struct {
		HasCredits bool   `json:"has_credits"`
		Unlimited  bool   `json:"unlimited"`
		Balance    string `json:"balance"`
	} `json:"credits"`
}

func (u codexUsage) report(accountID string) Report {
	a := Account{Platform: "codex", ID: accountID, Plan: u.PlanType, CheckedAt: time.Now(), Windows: []Window{}}
	for _, w := range []*codexWindow{u.RateLimit.PrimaryWindow, u.RateLimit.SecondaryWindow} {
		if w == nil || w.LimitWindowSeconds <= 0 {
			continue
		}
		win := Window{Kind: windowKind(time.Duration(w.LimitWindowSeconds) * time.Second), Used: w.UsedPercent / 100}
		if w.ResetAt > 0 {
			win.ResetsAt = time.Unix(w.ResetAt, 0)
		}
		a.Windows = append(a.Windows, win)
	}
	if u.RateLimit.LimitReached {
		a.State = "limit reached"
	}
	if cr := u.Credits; cr != nil && cr.HasCredits && !cr.Unlimited && cr.Balance != "" {
		balance := cr.Balance
		if whole, frac, ok := strings.Cut(balance, "."); ok && strings.Trim(frac, "0") == "" {
			balance = whole
		}
		a.Note = "credits " + balance
	}
	return Report{Accounts: []Account{a}}
}
