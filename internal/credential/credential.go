// Package credential is the credential registry. credentials.json says what
// each credential is and never holds a secret; secrets.json (0600) holds each
// credential's key. A credential owns exactly the provider part of the agent's
// env: its base URL, its key, and the rest of the provider env it carries
// (model mapping and the like).
package credential

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/YukiagoTpf/muster/internal/claude"
	"github.com/YukiagoTpf/muster/internal/store"
)

// fileVersion is the shape version of credentials.json and secrets.json.
const fileVersion = 1

// Credential is one registered credential.
type Credential struct {
	Name     string            `json:"name"`
	Provider string            `json:"provider"`
	BaseURL  string            `json:"baseURL"`
	Env      map[string]string `json:"env,omitempty"`
}

// ProviderEnv is the complete provider env a launch with this credential
// sets: the carried env, the base URL and the key.
func (c Credential) ProviderEnv(secret string) map[string]string {
	env := map[string]string{claude.BaseURLKey: c.BaseURL, claude.TokenKey: secret}
	for k, v := range c.Env {
		env[k] = v
	}
	return env
}

// Models lists the model aliases this credential maps.
func (c Credential) Models() []claude.ModelAlias { return claude.ModelAliases(c.Env) }

var (
	nameRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	providerRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

// Validate checks a credential before it is stored.
func Validate(c Credential) error {
	if !nameRe.MatchString(c.Name) {
		return fmt.Errorf("invalid credential name %q: use letters, digits, '.', '_' or '-' (at most 64, starting with a letter or digit)", c.Name)
	}
	if !providerRe.MatchString(c.Provider) {
		return fmt.Errorf("invalid provider %q: use lowercase letters, digits and '-'", c.Provider)
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("invalid base URL %q: want http(s)://host[/path]", c.BaseURL)
	}
	for k := range c.Env {
		switch {
		case k == claude.BaseURLKey || k == claude.TokenKey:
			return fmt.Errorf("env key %s is set from the credential itself; don't carry it in env", k)
		case !claude.IsProviderEnvKey(k):
			return fmt.Errorf("env key %s is not provider env: a credential carries only provider keys, everything else belongs in the shared settings", k)
		case claude.IsSecretEnvKey(k):
			return fmt.Errorf("env key %s looks like a secret: credential env is stored unencrypted, and the key is the only secret a credential has", k)
		}
	}
	return nil
}

// Registry is the loaded credential registry.
type Registry struct {
	Credentials []Credential
	secrets     map[string]string
}

type registryFile struct {
	Version     int          `json:"version"`
	Credentials []Credential `json:"credentials"`
}

type secretsFile struct {
	Version int               `json:"version"`
	Secrets map[string]string `json:"secrets"`
}

// Load reads the registry from the store. Missing files are an empty registry.
func Load(s *store.Store) (*Registry, error) {
	var reg registryFile
	if ok, err := store.ReadJSON(s.Path("credentials.json"), &reg); err != nil {
		return nil, err
	} else if ok && reg.Version != fileVersion {
		return nil, fmt.Errorf("%s has version %d; this muster understands %d", s.Path("credentials.json"), reg.Version, fileVersion)
	}
	var sec secretsFile
	if ok, err := store.ReadJSON(s.Path("secrets.json"), &sec); err != nil {
		return nil, err
	} else if ok && sec.Version != fileVersion {
		return nil, fmt.Errorf("%s has version %d; this muster understands %d", s.Path("secrets.json"), sec.Version, fileVersion)
	}
	if sec.Secrets == nil {
		sec.Secrets = map[string]string{}
	}
	return &Registry{Credentials: reg.Credentials, secrets: sec.Secrets}, nil
}

// Save writes the registry back. The caller holds the store lock. Secrets are
// written first, so a crash in between leaves at worst a key nothing refers to
// (ignored), never a credential whose key was dropped.
func (r *Registry) Save(s *store.Store) error {
	kept := map[string]string{}
	for _, c := range r.Credentials {
		if k, ok := r.secrets[c.Name]; ok {
			kept[c.Name] = k
		}
	}
	if err := store.WriteJSON(s.Path("secrets.json"), secretsFile{fileVersion, kept}); err != nil {
		return err
	}
	creds := r.Credentials
	if creds == nil {
		creds = []Credential{}
	}
	return store.WriteJSON(s.Path("credentials.json"), registryFile{fileVersion, creds})
}

// Get finds a credential by its exact name.
func (r *Registry) Get(name string) (Credential, bool) {
	for _, c := range r.Credentials {
		if c.Name == name {
			return c, true
		}
	}
	return Credential{}, false
}

// Secret is the key stored for a credential.
func (r *Registry) Secret(name string) (string, bool) {
	k, ok := r.secrets[name]
	return k, ok && k != ""
}

// ErrNotFound reports a credential name that is not registered.
var ErrNotFound = errors.New("no such credential")

// Add registers a new credential with its key. Names are unique ignoring case,
// so two credentials can never differ only by case.
func (r *Registry) Add(c Credential, secret string) error {
	if err := Validate(c); err != nil {
		return err
	}
	if strings.TrimSpace(secret) == "" {
		return errors.New("the key is empty")
	}
	for _, existing := range r.Credentials {
		if strings.EqualFold(existing.Name, c.Name) {
			return fmt.Errorf("credential %q already exists", existing.Name)
		}
	}
	r.Credentials = append(r.Credentials, c)
	sort.Slice(r.Credentials, func(i, j int) bool {
		return strings.ToLower(r.Credentials[i].Name) < strings.ToLower(r.Credentials[j].Name)
	})
	r.secrets[c.Name] = secret
	return nil
}

// Remove drops a credential and its key.
func (r *Registry) Remove(name string) error {
	for i, c := range r.Credentials {
		if c.Name == name {
			r.Credentials = append(r.Credentials[:i], r.Credentials[i+1:]...)
			delete(r.secrets, name)
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrNotFound, name)
}
