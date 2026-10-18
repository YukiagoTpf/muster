package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/YukiagoTpf/muster/internal/credential"
	"github.com/YukiagoTpf/muster/internal/launch"
	"github.com/YukiagoTpf/muster/internal/quota"
)

func TestBareMusterWithoutTerminalPrintsHelp(t *testing.T) {
	sb := newSandbox(t)
	code, out, _ := sb.run(t, "")
	if code != 0 || !strings.Contains(out, "muster run <credential>") {
		t.Fatalf("exit %d: %q", code, out)
	}
}

func TestScreensOfferTheLastLaunchFirst(t *testing.T) {
	sb := newSandbox(t)
	s := openStoreFor(t, sb)
	r, _ := credential.Load(s)
	r.Add(credential.Credential{Name: "a", Provider: "astergate", BaseURL: "https://gw.a/x"}, "k")
	r.Add(credential.Credential{Name: "b", Provider: "astergate", BaseURL: "https://gw.b",
		Env: map[string]string{"ANTHROPIC_DEFAULT_OPUS_MODEL": "o", "ANTHROPIC_DEFAULT_HAIKU_MODEL": "h"}}, "k")
	last := launch.Last{Version: 1, Credential: "b", Models: map[string]string{"b": "haiku"}}

	creds := credentialScreen(r, last, nil, time.Now())
	if creds.Cursor != 1 || creds.Items[1].Tag != "last" || !strings.Contains(creds.Items[0].Detail, "gw.a") {
		t.Fatalf("credential screen = %+v", creds)
	}
	if _, ok := modelScreen(r.Credentials[0], last); ok {
		t.Fatal("a credential without model aliases offered a model choice")
	}
	models, ok := modelScreen(r.Credentials[1], last)
	if !ok || len(models.Items) != 3 || models.Items[2].Label != "default" || models.Cursor != 1 || !models.CanGoBack {
		t.Fatalf("model screen = %+v", models)
	}
	last.Models["b"] = ""
	if models, _ := modelScreen(r.Credentials[1], last); models.Cursor != 2 {
		t.Fatalf("last launch with the agent default not offered first: %+v", models)
	}
}

func TestQuotaSummaryShowsThePoolsBestAccount(t *testing.T) {
	now := time.Now()
	reports := []quota.Report{
		{Source: "astergate:pool", FetchedAt: now, Accounts: []quota.Account{
			{Platform: "claude", ID: "busy", Windows: []quota.Window{{Kind: "5h", Used: 0.9, ResetsAt: now.Add(time.Hour)}}},
			{Platform: "claude", ID: "idle", Windows: []quota.Window{
				{Kind: "5h", Used: 0.2, ResetsAt: now.Add(time.Hour)},
				{Kind: "week", Used: 0.6, ResetsAt: now.Add(-time.Minute)},
			}},
		}},
		{Source: "astergate:broken", FetchedAt: now, Err: "HTTP 401"},
	}
	plain := func(s string) string { return ansiEscape.ReplaceAllString(s, "") }
	if got := plain(quotaSummary("pool", reports, now)); got != "5h [▰▱▱▱▱] 20%  week reset" {
		t.Errorf("summary = %q", got)
	}
	if got := plain(quotaSummary("broken", reports, now)); got != "quota unavailable" {
		t.Errorf("broken = %q", got)
	}
	if quotaSummary("unknown", reports, now) != "" {
		t.Error("a credential without a report got a summary")
	}
	s := credentialScreen(&credential.Registry{Credentials: []credential.Credential{{Name: "pool", Provider: "astergate", BaseURL: "https://gw"}}}, launch.Last{}, reports, now.Add(5*time.Minute))
	if !strings.Contains(plain(s.Title), "quota 5m ago") || s.Items[0].Quota == "" {
		t.Errorf("screen = %+v", s)
	}
}

func TestRefreshIfStale(t *testing.T) {
	var spawned []string
	defer func(orig func(IO, ...string) error) { spawnBackground = orig }(spawnBackground)
	spawnBackground = func(_ IO, args ...string) error { spawned = append(spawned, strings.Join(args, " ")); return nil }
	now := time.Now()
	refreshIfStale(IO{}, []quota.Report{{FetchedAt: now.Add(-10 * time.Second)}}, now)
	if len(spawned) != 0 {
		t.Fatalf("a fresh cache was refreshed: %v", spawned)
	}
	refreshIfStale(IO{}, []quota.Report{{FetchedAt: now.Add(-2 * time.Minute)}}, now)
	refreshIfStale(IO{}, nil, now)
	if strings.Join(spawned, ",") != "__refresh-quota,__refresh-quota" {
		t.Fatalf("spawned = %v", spawned)
	}
}
