package quota

import (
	"regexp"
	"testing"
	"time"
)

var escapes = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plainText(s string) string { return escapes.ReplaceAllString(s, "") }

func pinnedReport(current string, today float64) Report {
	future := time.Now().Add(3 * time.Hour)
	return Report{Usage: &Usage{Today: Spend{CostUSD: today}}, Accounts: []Account{
		{Platform: "claude", ID: "max-1", Plan: "max", Current: current == "max-1",
			Windows: []Window{{Kind: "5h", Used: 0.26, ResetsAt: future}, {Kind: "week", Used: 0.68, ResetsAt: future.Add(72 * time.Hour)}}},
		{Platform: "claude", ID: "pro-1", Plan: "pro", Current: current == "pro-1",
			Windows: []Window{{Kind: "5h", Used: 0.9, ResetsAt: time.Now().Add(-time.Minute)}}},
	}}
}

func TestRepinTracksTheSessionsAccount(t *testing.T) {
	now := time.Now()
	p := Repin(Pinned{Version: 1}, pinnedReport("", 1), now)
	if p.State != "unbound" || !p.Next.Equal(now.Add(10*time.Second)) {
		t.Fatalf("not pinned yet: %+v", p)
	}
	p = Repin(p, pinnedReport("max-1", 81.58), now)
	if p.State != "ok" || p.Account.ID != "max-1" || !p.SwitchedAt.IsZero() || *p.TodayUSD != 81.58 || !p.Next.Equal(now.Add(time.Minute)) {
		t.Fatalf("first pin is not a switch: %+v", p)
	}
	later := now.Add(time.Hour)
	p = Repin(p, pinnedReport("pro-1", 90), later)
	if p.Account.ID != "pro-1" || !p.SwitchedAt.Equal(later) {
		t.Fatalf("switch not noticed: %+v", p)
	}
	p = Repin(p, Report{Err: "HTTP 503: busy"}, later)
	if p.State != "error" || p.Account.ID != "pro-1" || !p.SwitchedAt.Equal(later) {
		t.Fatalf("an error must keep what is known: %+v", p)
	}
}

func TestPinnedLine(t *testing.T) {
	now := time.Now()
	ok := Repin(Pinned{Version: 1}, pinnedReport("max-1", 81.58), now)
	zh := plainText(ok.Line(Languages["zh"], now))
	want := "Max  │  5小时 [▰▰▰▱▱▱▱▱▱▱] 26% " + ResetLabel(now.Add(3*time.Hour), now) + " 重置  │  本周 [▰▰▰▰▰▰▰▱▱▱] 68% " + ResetLabel(now.Add(75*time.Hour), now) + " 重置  │  今日 $81.58"
	if zh != want {
		t.Errorf("zh:\n got %q\nwant %q", zh, want)
	}
	if en := plainText(ok.Line(Languages["en"], now)); en[:4] != "Max " || !regexp.MustCompile(`5h \[.*\] 26% resets .*today \$81\.58$`).MatchString(en) {
		t.Errorf("en = %q", en)
	}

	switched := Repin(ok, pinnedReport("pro-1", 1), now)
	if got := plainText(switched.Line(Languages["zh"], now)); got != "已换号 → Pro  │  5小时 已重置  │  今日 $1.00" {
		t.Errorf("switched = %q", got)
	}
	if got := plainText(switched.Line(Languages["zh"], now.Add(11*time.Minute))); got[:3] != "Pro" {
		t.Errorf("the switch mark must fade after 10 minutes: %q", got)
	}
	for state, want := range map[Pinned]string{
		{State: "unbound"}:                      "额度：会话尚未分配账号",
		{State: "error", Err: "HTTP 401: nope"}: "额度查询失败 (HTTP 401)",
		{State: "error", Err: "dial tcp: oops"}: "额度查询失败 (网络)",
		{}:                                      "",
	} {
		if got := plainText(state.Line(Languages["zh"], now)); got != want {
			t.Errorf("%+v: %q, want %q", state, got, want)
		}
	}
}
