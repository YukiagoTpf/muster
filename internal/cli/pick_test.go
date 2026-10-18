package cli

import (
	"strings"
	"testing"

	"github.com/YukiagoTpf/muster/internal/credential"
	"github.com/YukiagoTpf/muster/internal/launch"
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

	creds := credentialScreen(r, last)
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
