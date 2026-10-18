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

// credView is a credential as `cred list --json` and `cred add --json` show
// it. It never carries the key — only whether one is stored.
type credView struct {
	Name     string              `json:"name"`
	Provider string              `json:"provider"`
	BaseURL  string              `json:"baseURL"`
	Env      map[string]string   `json:"env"`
	Models   []claude.ModelAlias `json:"models"`
	HasKey   bool                `json:"hasKey"`
}

func viewOf(r *credential.Registry, c credential.Credential) credView {
	_, hasKey := r.Secret(c.Name)
	env := c.Env
	if env == nil {
		env = map[string]string{}
	}
	models := c.Models()
	if models == nil {
		models = []claude.ModelAlias{}
	}
	return credView{c.Name, c.Provider, c.BaseURL, env, models, hasKey}
}

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
		views := []credView{}
		for _, c := range r.Credentials {
			views = append(views, viewOf(r, c))
		}
		return writeJSON(sys, struct {
			Version     int        `json:"version"`
			Credentials []credView `json:"credentials"`
		}{1, views})
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
		env, err := claude.ReadProviderEnv(path)
		if err != nil {
			return fail(sys, err)
		}
		if c.BaseURL = env[claude.BaseURLKey]; c.BaseURL == "" {
			return fail(sys, fmt.Errorf("%s sets no %s to import", path, claude.BaseURLKey))
		}
		key = env[claude.TokenKey]
		for k, v := range env {
			if k != claude.BaseURLKey && k != claude.TokenKey && !claude.IsSecretEnvKey(k) {
				c.Env[k] = v
			}
		}
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
		b, err := io.ReadAll(sys.In)
		if err != nil {
			return fail(sys, err)
		}
		key = strings.TrimSpace(string(b))
	}
	if key == "" {
		return usageError(sys, "cred add", "no key: pass it on stdin with --key-stdin, or import it with --from-settings")
	}

	s, err := store.Open(sys.Getenv)
	if err != nil {
		return fail(sys, err)
	}
	var view credView
	err = s.WithLock(func() error {
		r, err := credential.Load(s)
		if err != nil {
			return err
		}
		if err := r.Add(c, key); err != nil {
			return err
		}
		view = viewOf(r, c)
		return r.Save(s)
	})
	if err != nil {
		return fail(sys, err)
	}
	if asJSON {
		return writeJSON(sys, struct {
			Version    int      `json:"version"`
			Credential credView `json:"credential"`
		}{1, view})
	}
	fmt.Fprintf(sys.Out, "added %s (%s · %s · models: %s)\n", c.Name, c.Provider, c.BaseURL, aliases(c))
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
	err = s.WithLock(func() error {
		r, err := credential.Load(s)
		if err != nil {
			return err
		}
		if err := r.Remove(pos[0]); err != nil {
			return err
		}
		return r.Save(s)
	})
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
