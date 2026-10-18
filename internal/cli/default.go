package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/YukiagoTpf/muster/internal/claude"
	"github.com/YukiagoTpf/muster/internal/credential"
	"github.com/YukiagoTpf/muster/internal/store"
)

func runDefault(sys IO, args []string) int {
	npos := 0
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		npos = 1
	}
	var asJSON bool
	pos, ok := parseArgs(sys, "default", args, npos, func(f *flagSet) { f.BoolVar(&asJSON, "json", false, "machine-readable output") })
	if !ok {
		return 2
	}
	path, err := claude.SettingsPath(sys.Getenv)
	if err != nil {
		return fail(sys, err)
	}
	if npos == 1 {
		return setDefault(sys, path, pos[0], asJSON)
	}
	return showDefault(sys, path, asJSON)
}

// defaultState says which registered credential settings.json's provider env
// is: "match" (exactly a credential), "drift" (a credential's base URL and key
// but other keys differ), "unregistered" (a provider no credential has) or
// "none" (no provider env: the agent uses its own login).
type defaultState struct {
	State         string   `json:"state"`
	Credential    string   `json:"credential,omitempty"`
	DifferentKeys []string `json:"differentKeys,omitempty"`
	BaseURL       string   `json:"baseURL,omitempty"`
}

func classify(r *credential.Registry, env map[string]string) defaultState {
	if len(env) == 0 {
		return defaultState{State: "none"}
	}
	for _, c := range r.Credentials {
		key, _ := r.Secret(c.Name)
		if env[claude.BaseURLKey] != c.BaseURL || env[claude.TokenKey] != key {
			continue
		}
		diff := differentKeys(c.ProviderEnv(key), env)
		if len(diff) == 0 {
			return defaultState{State: "match", Credential: c.Name}
		}
		return defaultState{State: "drift", Credential: c.Name, DifferentKeys: diff}
	}
	return defaultState{State: "unregistered", BaseURL: env[claude.BaseURLKey]}
}

func differentKeys(a, b map[string]string) []string {
	var keys []string
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			keys = append(keys, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

func showDefault(sys IO, path string, asJSON bool) int {
	r, err := loadRegistry(sys)
	if err != nil {
		return fail(sys, err)
	}
	env, err := claude.ReadProviderEnv(path)
	if err != nil {
		return fail(sys, err)
	}
	st := classify(r, env)
	if asJSON {
		return writeJSON(sys, struct {
			Version  int    `json:"version"`
			Settings string `json:"settings"`
			defaultState
		}{1, path, st})
	}
	switch st.State {
	case "match":
		fmt.Fprintf(sys.Out, "default credential: %s\n", st.Credential)
	case "drift":
		fmt.Fprintf(sys.Out, "default credential: %s, but %s differs in %s — `muster default %s` resets it\n",
			st.Credential, path, strings.Join(st.DifferentKeys, " "), st.Credential)
	case "unregistered":
		fmt.Fprintf(sys.Out, "%s sets a provider no credential matches (%s) — register it with `muster cred add <name> --provider <p> --from-settings`\n", path, st.BaseURL)
	default:
		fmt.Fprintf(sys.Out, "%s sets no provider: Claude Code uses its own login\n", path)
	}
	return 0
}

// setDefault makes a credential the default: settings.json's provider env
// becomes exactly the credential's, everything else in the file is kept, and
// the previous file is backed up first.
func setDefault(sys IO, path, name string, asJSON bool) int {
	s, err := store.Open(sys.Getenv)
	if err != nil {
		return fail(sys, err)
	}
	var backup string
	err = s.WithLock(func() error {
		r, err := credential.Load(s)
		if err != nil {
			return err
		}
		c, ok := r.Get(name)
		if !ok {
			return fmt.Errorf("no credential named %q (see `muster cred list`)", name)
		}
		key, ok := r.Secret(name)
		if !ok {
			return fmt.Errorf("credential %q has no key stored; remove it and add it again", name)
		}
		old, err := os.ReadFile(path)
		perm := fs.FileMode(0o600)
		switch {
		case errors.Is(err, os.ErrNotExist):
			old = nil
		case err != nil:
			return err
		default:
			if fi, err := os.Stat(path); err == nil {
				perm = fi.Mode().Perm()
			}
		}
		next, err := claude.SetProviderEnv(old, c.ProviderEnv(key))
		if err != nil {
			return fmt.Errorf("%s: %w — fix it by hand; nothing was changed", path, err)
		}
		if old != nil {
			if backup, err = s.Backup("settings.json", old, store.BackupsKept); err != nil {
				return err
			}
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		return store.WriteAtomic(path, next, perm)
	})
	if err != nil {
		return fail(sys, err)
	}
	if asJSON {
		return writeJSON(sys, struct {
			Version    int    `json:"version"`
			Credential string `json:"credential"`
			Settings   string `json:"settings"`
			Backup     string `json:"backup,omitempty"`
		}{1, name, path, backup})
	}
	fmt.Fprintf(sys.Out, "default credential is now %s (%s)\n", name, path)
	if backup != "" {
		fmt.Fprintf(sys.Out, "previous file backed up to %s\n", backup)
	}
	return 0
}
