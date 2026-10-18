package cli

import (
	"fmt"
	"net/url"
	"os"

	"github.com/YukiagoTpf/muster/internal/credential"
	"github.com/YukiagoTpf/muster/internal/launch"
	"github.com/YukiagoTpf/muster/internal/picker"
	"github.com/YukiagoTpf/muster/internal/store"
)

// runPick is a bare `muster` in a terminal: pick a credential, then a model,
// and launch. The last launch is offered first on both screens.
func runPick(sys IO) int {
	out, _ := sys.Out.(*os.File)
	if !picker.Interactive(sys.In, out) {
		return runHelp(sys, nil)
	}
	s, err := store.Open(sys.Getenv)
	if err != nil {
		return fail(sys, err)
	}
	r, err := credential.Load(s)
	if err != nil {
		return fail(sys, err)
	}
	if len(r.Credentials) == 0 {
		fmt.Fprintln(sys.Out, "no credentials yet — add one with `muster cred add` (see `muster help`)")
		return 0
	}
	last := launch.LoadLast(s)

	term, err := picker.Open(sys.In, out)
	if err != nil {
		return fail(sys, err)
	}
	cred, model, ok, err := pickCredentialAndModel(term, r, last)
	term.Close()
	if err != nil {
		return fail(sys, err)
	}
	if !ok {
		return 0
	}
	shown := model
	if shown == "" {
		shown = "agent default"
	}
	fmt.Fprintf(sys.Out, "muster: launching %s · %s\n", cred, shown)
	s, plan, err := planLaunch(sys, cred, model, nil)
	if err != nil {
		return fail(sys, err)
	}
	return startLaunch(sys, s, plan)
}

func pickCredentialAndModel(term *picker.Terminal, r *credential.Registry, last launch.Last) (cred, model string, ok bool, err error) {
	creds := credentialScreen(r, last)
	for {
		var o picker.Outcome
		creds, o, err = term.Pick(creds)
		if err != nil || o != picker.Chosen {
			return "", "", false, err
		}
		c := r.Credentials[creds.Cursor]
		models, hasChoice := modelScreen(c, last)
		if !hasChoice {
			return c.Name, "", true, nil
		}
		models, o, err = term.Pick(models)
		switch {
		case err != nil:
			return "", "", false, err
		case o == picker.Chosen:
			if models.Cursor < len(c.Models()) {
				model = c.Models()[models.Cursor].Alias
			}
			return c.Name, model, true, nil
		case o == picker.Quit:
			return "", "", false, nil
		}
		// Back: choose the credential again.
	}
}

func credentialScreen(r *credential.Registry, last launch.Last) picker.Screen {
	s := picker.Screen{Title: "Launch Claude Code with"}
	for i, c := range r.Credentials {
		detail := c.Provider + " · " + host(c.BaseURL) + " · " + aliases(c)
		item := picker.Item{Label: c.Name, Detail: detail}
		if c.Name == last.Credential {
			item.Tag, s.Cursor = "last", i
		}
		s.Items = append(s.Items, item)
	}
	return s
}

// modelScreen lists a credential's model aliases plus the agent's own
// default; a credential that maps no alias has no choice to offer.
func modelScreen(c credential.Credential, last launch.Last) (picker.Screen, bool) {
	models := c.Models()
	if len(models) == 0 {
		return picker.Screen{}, false
	}
	s := picker.Screen{Title: c.Name + " · model", CanGoBack: true}
	lastModel, launched := last.Models[c.Name]
	for i, m := range models {
		item := picker.Item{Label: m.Alias, Detail: m.Model}
		if launched && m.Alias == lastModel {
			item.Tag, s.Cursor = "last", i
		}
		s.Items = append(s.Items, item)
	}
	def := picker.Item{Label: "default", Detail: "the agent's own default"}
	if launched && lastModel == "" {
		def.Tag, s.Cursor = "last", len(models)
	}
	s.Items = append(s.Items, def)
	return s, true
}

func host(baseURL string) string {
	if u, err := url.Parse(baseURL); err == nil && u.Host != "" {
		return u.Host
	}
	return baseURL
}
