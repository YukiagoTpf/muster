package quota

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AsterGate reads a gateway key's view: the subscription accounts in its pool
// (5-hour and weekly windows, account-wide only — a model-scoped window is
// not the account's limit) and what the key itself spent. The query is
// read-only and spends no quota. sessionID, when set, asks the gateway which
// account that Claude Code session is pinned to.
func AsterGate(credential, baseURL, key, sessionID, timezone string) Source {
	return Source{
		Name: "astergate:" + credential,
		Fetch: func(ctx context.Context, c *http.Client) (Report, error) {
			u := strings.TrimRight(baseURL, "/") + "/me/usage"
			if timezone != "" {
				u += "?timezone=" + url.QueryEscape(timezone)
			}
			req, err := http.NewRequest(http.MethodGet, u, nil)
			if err != nil {
				return Report{}, err
			}
			req.Header.Set("Authorization", "Bearer "+key)
			if sessionID != "" {
				req.Header.Set("X-Claude-Code-Session-Id", sessionID)
			}
			var resp asterGateUsage
			if err := getJSON(ctx, c, req, &resp); err != nil {
				return Report{}, err
			}
			return resp.report(credential), nil
		},
	}
}

type asterGateSpend struct {
	Requests int     `json:"requests"`
	CostUSD  float64 `json:"cost_usd"`
}

type asterGateUsage struct {
	Today    asterGateSpend `json:"today"`
	Week     asterGateSpend `json:"week"`
	Month    asterGateSpend `json:"month"`
	Accounts []struct {
		AccountID string    `json:"account_id"`
		Provider  string    `json:"provider"`
		Plan      string    `json:"plan"`
		State     string    `json:"state"`
		CheckedAt time.Time `json:"checked_at"`
		Current   bool      `json:"current"`
		Windows   []struct {
			Kind      string    `json:"kind"`
			Scope     string    `json:"scope"`
			UsedRatio *float64  `json:"used_ratio"`
			ResetsAt  time.Time `json:"resets_at"`
		} `json:"windows"`
	} `json:"accounts"`
}

var asterGateKinds = map[string]string{"five_hour": "5h", "weekly": "week"}

func (u asterGateUsage) report(credential string) Report {
	r := Report{Usage: &Usage{
		Today: Spend(u.Today), Week: Spend(u.Week), Month: Spend(u.Month),
	}}
	for _, a := range u.Accounts {
		acct := Account{
			Platform: a.Provider, ID: a.AccountID, Plan: a.Plan, CheckedAt: a.CheckedAt,
			Current: a.Current, Via: []string{credential}, Windows: []Window{},
		}
		if acct.Platform == "" {
			acct.Platform = "claude"
		}
		if a.State != "" && a.State != "available" {
			acct.State = a.State
		}
		for _, w := range a.Windows {
			kind, known := asterGateKinds[w.Kind]
			if !known || w.Scope == "model" || w.UsedRatio == nil {
				continue
			}
			acct.Windows = append(acct.Windows, Window{Kind: kind, Used: *w.UsedRatio, ResetsAt: w.ResetsAt})
		}
		r.Accounts = append(r.Accounts, acct)
	}
	return r
}

// Timezone is the IANA zone the gateway should cut "today" in: TZ when set,
// otherwise the fixed-offset Etc/GMT zone of the local offset (Etc/GMT-8 is
// UTC+8 — the sign is inverted by IANA convention), or UTC for a
// non-whole-hour offset.
func Timezone(getenv func(string) string, now time.Time) string {
	if tz := getenv("TZ"); tz != "" {
		return tz
	}
	_, offset := now.Zone()
	switch {
	case offset == 0:
		return "UTC"
	case offset%3600 != 0:
		return "UTC"
	case offset > 0:
		return fmt.Sprintf("Etc/GMT-%d", offset/3600)
	default:
		return fmt.Sprintf("Etc/GMT+%d", -offset/3600)
	}
}
