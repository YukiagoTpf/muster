package credential

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/YukiagoTpf/muster/internal/claude"
	"github.com/YukiagoTpf/muster/internal/store"
)

// View is a credential as every surface shows it (`cred list --json`, the
// web page). It never carries the key — only whether one is stored.
type View struct {
	Name     string              `json:"name"`
	Provider string              `json:"provider"`
	BaseURL  string              `json:"baseURL"`
	Env      map[string]string   `json:"env"`
	Models   []claude.ModelAlias `json:"models"`
	HasKey   bool                `json:"hasKey"`
}

// ViewOf is the view of one registered credential.
func (r *Registry) ViewOf(c Credential) View {
	_, hasKey := r.Secret(c.Name)
	env := c.Env
	if env == nil {
		env = map[string]string{}
	}
	models := c.Models()
	if models == nil {
		models = []claude.ModelAlias{}
	}
	return View{c.Name, c.Provider, c.BaseURL, env, models, hasKey}
}

// Views lists every credential's view.
func (r *Registry) Views() []View {
	out := []View{}
	for _, c := range r.Credentials {
		out = append(out, r.ViewOf(c))
	}
	return out
}

// Update runs fn on the registry under the store lock and saves what it left.
func Update(s *store.Store, fn func(*Registry) error) error {
	return s.WithLock(func() error {
		r, err := Load(s)
		if err != nil {
			return err
		}
		if err := fn(r); err != nil {
			return err
		}
		return r.Save(s)
	})
}

// FromSettings is the credential the agent's settings file holds as its
// provider env — base URL, key and every non-secret provider key — for
// registering what a machine already uses.
func FromSettings(settingsPath string) (Credential, string, error) {
	env, err := claude.ReadProviderEnv(settingsPath)
	if err != nil {
		return Credential{}, "", err
	}
	c := Credential{BaseURL: env[claude.BaseURLKey], Env: map[string]string{}}
	if c.BaseURL == "" {
		return c, "", fmt.Errorf("%s sets no %s to import", settingsPath, claude.BaseURLKey)
	}
	for k, v := range env {
		if k != claude.BaseURLKey && k != claude.TokenKey && !claude.IsSecretEnvKey(k) {
			c.Env[k] = v
		}
	}
	return c, env[claude.TokenKey], nil
}

// DefaultState says which registered credential the settings file's provider
// env is: "match" (exactly a credential), "drift" (a credential's base URL and
// key, but other keys differ), "unregistered" (a provider no credential has)
// or "none" (no provider env: the agent uses its own login).
type DefaultState struct {
	State         string   `json:"state"`
	Credential    string   `json:"credential,omitempty"`
	DifferentKeys []string `json:"differentKeys,omitempty"`
	BaseURL       string   `json:"baseURL,omitempty"`
}

// ClassifyDefault works out the default credential from a provider env.
func (r *Registry) ClassifyDefault(env map[string]string) DefaultState {
	if len(env) == 0 {
		return DefaultState{State: "none"}
	}
	for _, c := range r.Credentials {
		key, _ := r.Secret(c.Name)
		if env[claude.BaseURLKey] != c.BaseURL || env[claude.TokenKey] != key {
			continue
		}
		diff := differentKeys(c.ProviderEnv(key), env)
		if len(diff) == 0 {
			return DefaultState{State: "match", Credential: c.Name}
		}
		return DefaultState{State: "drift", Credential: c.Name, DifferentKeys: diff}
	}
	return DefaultState{State: "unregistered", BaseURL: env[claude.BaseURLKey]}
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

// MakeDefault makes a credential the default: the settings file's provider
// env becomes exactly the credential's, everything else in the file is kept,
// and the previous file is backed up first. It returns the backup's path
// ("" when there was no file).
func MakeDefault(s *store.Store, settingsPath, name string) (string, error) {
	var backup string
	err := s.WithLock(func() error {
		r, err := Load(s)
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
		old, err := os.ReadFile(settingsPath)
		perm := fs.FileMode(0o600)
		switch {
		case errors.Is(err, os.ErrNotExist):
			old = nil
		case err != nil:
			return err
		default:
			if fi, err := os.Stat(settingsPath); err == nil {
				perm = fi.Mode().Perm()
			}
		}
		next, err := claude.SetProviderEnv(old, c.ProviderEnv(key))
		if err != nil {
			return fmt.Errorf("%s: %w — fix it by hand; nothing was changed", settingsPath, err)
		}
		if old != nil {
			if backup, err = s.Backup("settings.json", old, store.BackupsKept); err != nil {
				return err
			}
		}
		if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
			return err
		}
		return store.WriteAtomic(settingsPath, next, perm)
	})
	return backup, err
}
