package quota

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Pinned is what the statusline knows about one Claude Code session on a
// gateway: the account the session is pinned to, refreshed in the background
// and read on every render.
type Pinned struct {
	Version int `json:"version"`
	// Next is when the next background refresh is due.
	Next time.Time `json:"next"`
	// State is "ok", "unbound" (the gateway has not pinned the session to an
	// account yet) or "error".
	State   string   `json:"state"`
	Err     string   `json:"error,omitempty"`
	Account *Account `json:"account,omitempty"`
	// SwitchedAt is when the session last moved to another account.
	SwitchedAt time.Time `json:"switchedAt,omitzero"`
	TodayUSD   *float64  `json:"todayUSD,omitempty"`
}

// switchShown is how long a move to another account stays flagged.
const switchShown = 10 * time.Minute

// Repin folds a fresh gateway report into the previous state of a session.
func Repin(prev Pinned, r Report, now time.Time) Pinned {
	next := Pinned{Version: 1, Next: now.Add(time.Minute), SwitchedAt: prev.SwitchedAt}
	if r.Err != "" {
		next.State, next.Err, next.Account = "error", r.Err, prev.Account
		return next
	}
	if r.Usage != nil {
		cost := r.Usage.Today.CostUSD
		next.TodayUSD = &cost
	}
	for i := range r.Accounts {
		if r.Accounts[i].Current {
			a := r.Accounts[i]
			next.State, next.Account = "ok", &a
			if prev.Account != nil && prev.Account.ID != a.ID {
				next.SwitchedAt = now
			}
			return next
		}
	}
	// Not pinned yet: keep the last account so a later pin can still be told
	// apart from a switch, and look again soon.
	next.State, next.Account, next.Next = "unbound", prev.Account, now.Add(10*time.Second)
	return next
}

// Labels are the words of the statusline in one language.
type Labels struct {
	Windows map[string]string
	// Resets formats a reset time; ResetSince marks a window already reset.
	Resets, ResetSince   string
	Today, Switched      string
	Unbound, Unavailable string
	NetworkErr           string
}

// Languages the statusline speaks.
var Languages = map[string]Labels{
	"en": {
		Windows: map[string]string{"5h": "5h", "week": "week", "month": "month"},
		Resets:  "resets %s", ResetSince: "reset", Today: "today", Switched: "switched",
		Unbound: "quota: session not pinned to an account yet", Unavailable: "quota unavailable (%s)", NetworkErr: "network",
	},
	"zh": {
		Windows: map[string]string{"5h": "5小时", "week": "本周", "month": "本月"},
		Resets:  "%s 重置", ResetSince: "已重置", Today: "今日", Switched: "已换号",
		Unbound: "额度：会话尚未分配账号", Unavailable: "额度查询失败 (%s)", NetworkErr: "网络",
	},
}

var httpStatus = regexp.MustCompile(`^HTTP \d+`)

const (
	plan  = "\x1b[1;35m"
	blue  = "\x1b[1;94m"
	alert = "\x1b[1;33m"
)

// Line renders the statusline's quota line; empty when there is nothing to
// say yet.
func (p Pinned) Line(l Labels, now time.Time) string {
	sep := Dim + "  │  " + Reset
	switch p.State {
	case "unbound":
		return Dim + l.Unbound + Reset
	case "error":
		reason := l.NetworkErr
		if m := httpStatus.FindString(p.Err); m != "" {
			reason = m
		}
		return Dim + fmt.Sprintf(l.Unavailable, reason) + Reset
	case "ok":
		if p.Account == nil {
			return ""
		}
	default:
		return ""
	}
	var parts []string
	head := ""
	if p.Account.Plan != "" {
		head = plan + strings.ToUpper(p.Account.Plan[:1]) + p.Account.Plan[1:] + Reset
	}
	if !p.SwitchedAt.IsZero() && now.Sub(p.SwitchedAt) < switchShown {
		head = strings.TrimSpace(alert + l.Switched + Reset + " " + Dim + "→" + Reset + " " + head)
	}
	if head != "" {
		parts = append(parts, head)
	}
	for _, w := range p.Account.Windows {
		name := l.Windows[w.Kind]
		if name == "" {
			name = w.Kind
		}
		if w.Expired(now) {
			parts = append(parts, Dim+name+" "+l.ResetSince+Reset)
			continue
		}
		part := Dim + name + Reset + " " + Bar(w.Used, 10) + " " + Percent(w.Used)
		if r := ResetLabel(w.ResetsAt, now); r != "" {
			part += " " + Dim + fmt.Sprintf(l.Resets, r) + Reset
		}
		parts = append(parts, part)
	}
	if p.TodayUSD != nil {
		parts = append(parts, Dim+l.Today+Reset+" "+blue+fmt.Sprintf("$%.2f", *p.TodayUSD)+Reset)
	}
	return strings.Join(parts, sep)
}
