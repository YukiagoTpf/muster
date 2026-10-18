package quota

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // the "sqlite" database/sql driver
)

// CursorStatePath is the database where the Cursor editor keeps its login.
func CursorStatePath(appConfigDir string) string {
	return filepath.Join(appConfigDir, "Cursor", "User", "globalStorage", "state.vscdb")
}

// Cursor reads a Cursor subscription's included usage through the editor's
// own login. The login lives in a SQLite database the editor keeps open in
// WAL mode, so it is read through SQLite itself, strictly read-only; the token
// is never refreshed.
func Cursor(statePath, apiBase string) Source {
	return Source{
		Name: "cursor",
		Fetch: func(ctx context.Context, c *http.Client) (Report, error) {
			token, err := cursorToken(ctx, statePath)
			if err != nil {
				return Report{}, err
			}
			if exp := jwtExpiry(token); !exp.IsZero() && exp.Before(time.Now()) {
				return Report{}, errExpired("cursor")
			}
			call := func(method string, v any) error {
				req, err := http.NewRequest(http.MethodPost, strings.TrimRight(apiBase, "/")+"/aiserver.v1.DashboardService/"+method, strings.NewReader("{}"))
				if err != nil {
					return err
				}
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Connect-Protocol-Version", "1")
				return fetchJSON(ctx, c, req, v)
			}
			var usage cursorUsage
			if err := call("GetCurrentPeriodUsage", &usage); err != nil {
				return Report{}, err
			}
			var plan struct {
				PlanInfo struct {
					PlanName string `json:"planName"`
				} `json:"planInfo"`
			}
			if err := call("GetPlanInfo", &plan); err != nil {
				return Report{}, err
			}
			return usage.report(plan.PlanInfo.PlanName), nil
		},
	}
}

// cursorToken reads the editor's access token from its state database.
func cursorToken(ctx context.Context, path string) (string, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return "", ErrNoLogin
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro&"+url.Values{"_pragma": {"busy_timeout(2000)"}}.Encode())
	if err != nil {
		return "", err
	}
	defer db.Close()
	var raw []byte
	err = db.QueryRowContext(ctx, `SELECT value FROM ItemTable WHERE key = 'cursorAuth/accessToken'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNoLogin
	}
	if err != nil {
		return "", fmt.Errorf("reading the Cursor login: %w", err)
	}
	token := string(raw)
	if strings.HasPrefix(token, `"`) {
		json.Unmarshal(raw, &token)
	}
	if token == "" {
		return "", ErrNoLogin
	}
	return token, nil
}

type cursorUsage struct {
	BillingCycleStart string `json:"billingCycleStart"`
	BillingCycleEnd   string `json:"billingCycleEnd"`
	PlanUsage         *struct {
		TotalSpend float64 `json:"totalSpend"`
		Limit      float64 `json:"limit"`
	} `json:"planUsage"`
}

func millis(s string) time.Time {
	ms, err := strconv.ParseInt(s, 10, 64)
	if err != nil || ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

func (u cursorUsage) report(plan string) Report {
	a := Account{Platform: "cursor", Plan: plan, CheckedAt: time.Now(), Windows: []Window{}}
	start, end := millis(u.BillingCycleStart), millis(u.BillingCycleEnd)
	if p := u.PlanUsage; p != nil && p.Limit > 0 {
		kind := "month"
		if !start.IsZero() && end.After(start) {
			kind = windowKind(end.Sub(start).Round(24 * time.Hour))
		}
		// totalSpend and limit are in cents. The API's own percentage
		// fields don't agree with them, so the ratio is computed here.
		a.Windows = append(a.Windows, Window{Kind: kind, Used: p.TotalSpend / p.Limit, ResetsAt: end})
		a.Note = fmt.Sprintf("$%.2f of $%.2f included", p.TotalSpend/100, p.Limit/100)
	}
	return Report{Accounts: []Account{a}}
}
