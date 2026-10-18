package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/YukiagoTpf/muster/internal/claude"
	"github.com/YukiagoTpf/muster/internal/credential"
	"github.com/YukiagoTpf/muster/internal/store"
)

func aliases(c credential.Credential) string {
	var names []string
	for _, m := range c.Models() {
		names = append(names, m.Alias)
	}
	if len(names) == 0 {
		return "-"
	}
	return strings.Join(names, " ")
}

func runCredList(sys IO, args []string) int {
	var asJSON bool
	if _, ok := parseArgs(sys, "cred list", args, 0, func(f *flagSet) { f.BoolVar(&asJSON, "json", false, "machine-readable output") }); !ok {
		return 2
	}
	r, err := loadRegistry(sys)
	if err != nil {
		return fail(sys, err)
	}
	if asJSON {
		return writeJSON(sys, struct {
			Version     int               `json:"version"`
			Credentials []credential.View `json:"credentials"`
		}{1, r.Views()})
	}
	if len(r.Credentials) == 0 {
		fmt.Fprintln(sys.Out, "no credentials yet — add one with `muster cred add`")
		return 0
	}
	w := tabwriter.NewWriter(sys.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tPROVIDER\tBASE URL\tMODELS")
	for _, c := range r.Credentials {
		name := c.Name
		if _, ok := r.Secret(c.Name); !ok {
			name += " (no key)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", name, c.Provider, c.BaseURL, aliases(c))
	}
	w.Flush()
	return 0
}

func runCredAdd(sys IO, args []string) int {
	var (
		provider, baseURL      string
		models, extra          kvList
		fromSettings, keyStdin bool
		asJSON                 bool
	)
	pos, ok := parseArgs(sys, "cred add", args, 1, func(f *flagSet) {
		f.StringVar(&provider, "provider", "", "provider of the credential, e.g. astergate (required)")
		f.StringVar(&baseURL, "base-url", "", "the provider's base URL")
		f.Var(&models, "model", "model mapping alias=model (repeatable), e.g. opus=claude-opus-5-5")
		f.Var(&extra, "env", "other provider env KEY=VALUE (repeatable)")
		f.BoolVar(&fromSettings, "from-settings", false, "import base URL, key and model mapping from the agent's settings.json")
		f.BoolVar(&keyStdin, "key-stdin", false, "read the key from stdin")
		f.BoolVar(&asJSON, "json", false, "machine-readable output")
	})
	if !ok {
		return 2
	}
	if provider == "" {
		return usageError(sys, "cred add", "--provider is required")
	}
	c := credential.Credential{Name: pos[0], Provider: provider, Env: map[string]string{}}
	var key string
	if fromSettings {
		path, err := claude.SettingsPath(sys.Getenv)
		if err != nil {
			return fail(sys, err)
		}
		imported, k, err := credential.FromSettings(path)
		if err != nil {
			return fail(sys, err)
		}
		c.BaseURL, c.Env, key = imported.BaseURL, imported.Env, k
		if key == "" && !keyStdin {
			return fail(sys, fmt.Errorf("%s sets no %s; pass the key with --key-stdin", path, claude.TokenKey))
		}
	}
	if baseURL != "" {
		c.BaseURL = baseURL
	}
	if c.BaseURL == "" {
		return usageError(sys, "cred add", "--base-url is required (or import it with --from-settings)")
	}
	for _, kv := range models {
		c.Env[claude.ModelEnvKey(kv.key)] = kv.value
	}
	for _, kv := range extra {
		c.Env[kv.key] = kv.value
	}
	if keyStdin {
		if key, ok = readKey(sys); !ok {
			return 1
		}
	}
	if key == "" {
		return usageError(sys, "cred add", "no key: pass it on stdin with --key-stdin, or import it with --from-settings")
	}

	s, err := store.Open(sys.Getenv)
	if err != nil {
		return fail(sys, err)
	}
	var view credential.View
	err = credential.Update(s, func(r *credential.Registry) error {
		if err := r.Add(c, key); err != nil {
			return err
		}
		view = r.ViewOf(c)
		return nil
	})
	if err != nil {
		return fail(sys, err)
	}
	return reportCred(sys, "added", c, view, asJSON)
}

// readKey reads a key from stdin.
func readKey(sys IO) (string, bool) {
	b, err := io.ReadAll(sys.In)
	if err != nil {
		fail(sys, err)
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

func reportCred(sys IO, verb string, c credential.Credential, view credential.View, asJSON bool) int {
	if asJSON {
		return writeJSON(sys, struct {
			Version    int             `json:"version"`
			Credential credential.View `json:"credential"`
		}{1, view})
	}
	fmt.Fprintf(sys.Out, "%s %s (%s · %s · models: %s)\n", verb, c.Name, c.Provider, c.BaseURL, aliases(c))
	return 0
}

func runCredRm(sys IO, args []string) int {
	var asJSON bool
	pos, ok := parseArgs(sys, "cred rm", args, 1, func(f *flagSet) { f.BoolVar(&asJSON, "json", false, "machine-readable output") })
	if !ok {
		return 2
	}
	s, err := store.Open(sys.Getenv)
	if err != nil {
		return fail(sys, err)
	}
	err = credential.Update(s, func(r *credential.Registry) error { return r.Remove(pos[0]) })
	if errors.Is(err, credential.ErrNotFound) {
		return fail(sys, fmt.Errorf("no credential named %q (see `muster cred list`)", pos[0]))
	}
	if err != nil {
		return fail(sys, err)
	}
	if asJSON {
		return writeJSON(sys, struct {
			Version int    `json:"version"`
			Removed string `json:"removed"`
		}{1, pos[0]})
	}
	fmt.Fprintf(sys.Out, "removed %s\n", pos[0])
	return 0
}

// loadRegistry opens the store and reads the registry without taking the
// lock — for commands that only read.
func loadRegistry(sys IO) (*credential.Registry, error) {
	s, err := store.Open(sys.Getenv)
	if err != nil {
		return nil, err
	}
	return credential.Load(s)
}
