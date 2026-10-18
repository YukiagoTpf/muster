// Package claude is everything Muster knows about Claude Code: where its
// settings live, which env keys belong to the provider, and how model aliases
// map to env keys. Supporting another agent means another package like this.
package claude

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/YukiagoTpf/muster/internal/platform"
)

// The provider env keys Muster sets on every launch.
const (
	BaseURLKey = "ANTHROPIC_BASE_URL"
	TokenKey   = "ANTHROPIC_AUTH_TOKEN"
)

// ConfigDir is Claude Code's config directory: CLAUDE_CONFIG_DIR when set,
// otherwise ~/.claude.
func ConfigDir(getenv func(string) string) (string, error) {
	if d := getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d, nil
	}
	home, err := platform.UserHome(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// SettingsPath is the user settings file, the one the default credential and
// the shared settings live in.
func SettingsPath(getenv func(string) string) (string, error) {
	dir, err := ConfigDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

var (
	secretWord  = regexp.MustCompile(`(?i)(^|_)(TOKEN|KEY|AUTH|SECRET|BEARER|CREDENTIAL)(_|$)`)
	cloudSwitch = regexp.MustCompile(`^CLAUDE_CODE_USE_(BEDROCK|VERTEX|FOUNDRY)$`)
)

// IsProviderEnvKey reports whether an env key belongs to the provider — the
// namespace a credential owns: the whole ANTHROPIC_ namespace (base URL, keys,
// model mapping, headers) plus the auth-ish CLAUDE_CODE_ keys (OAuth token,
// API keys, cloud-provider switches). Other CLAUDE_CODE_ keys are ordinary
// shared settings: CLAUDE_CODE_MAX_OUTPUT_TOKENS contains TOKEN but is not
// auth, which is why the match is on whole words.
func IsProviderEnvKey(key string) bool {
	if strings.HasPrefix(key, "ANTHROPIC_") {
		return true
	}
	if strings.HasPrefix(key, "CLAUDE_CODE_") {
		return secretWord.MatchString(key) || cloudSwitch.MatchString(key)
	}
	return false
}

// IsSecretEnvKey reports whether a provider env key holds a secret (a key or
// token) rather than plain routing data such as a URL or a model name.
func IsSecretEnvKey(key string) bool {
	return IsProviderEnvKey(key) && secretWord.MatchString(key)
}

// SecretLookingEnv lists the env keys of a settings file whose names say they
// hold a secret (…_TOKEN, …_API_KEY, …), sorted. Shared settings must carry
// none: they reach the sync repo unencrypted.
func SecretLookingEnv(settingsFile []byte) ([]string, error) {
	var settings struct {
		Env map[string]json.RawMessage `json:"env"`
	}
	if err := json.Unmarshal(settingsFile, &settings); err != nil {
		return nil, err
	}
	var keys []string
	for k := range settings.Env {
		if secretWord.MatchString(k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

var modelKey = regexp.MustCompile(`^ANTHROPIC_DEFAULT_([A-Z0-9]+)_MODEL$`)

// ModelEnvKey is the env key that maps a model alias (opus, sonnet, …).
func ModelEnvKey(alias string) string {
	return "ANTHROPIC_DEFAULT_" + strings.ToUpper(alias) + "_MODEL"
}

// ModelAlias is one alias → model mapping carried by a provider env.
type ModelAlias struct {
	Alias string `json:"alias"`
	Model string `json:"model"`
}

// aliasRank orders the well-known aliases from the strongest model down; any
// other alias sorts after them, alphabetically.
var aliasRank = map[string]int{"opus": 0, "fable": 1, "sonnet": 2, "haiku": 3}

// ModelAliases lists the alias mappings in a provider env.
func ModelAliases(env map[string]string) []ModelAlias {
	var out []ModelAlias
	for k, v := range env {
		if m := modelKey.FindStringSubmatch(k); m != nil && v != "" {
			out = append(out, ModelAlias{Alias: strings.ToLower(m[1]), Model: v})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ri, iok := aliasRank[out[i].Alias]
		rj, jok := aliasRank[out[j].Alias]
		switch {
		case iok && jok:
			return ri < rj
		case iok != jok:
			return iok
		default:
			return out[i].Alias < out[j].Alias
		}
	})
	return out
}

// ReadProviderEnv returns the provider env keys the settings file at path
// sets — the default credential. Non-string values are not env and are
// skipped; a missing file sets nothing.
func ReadProviderEnv(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	env, err := ProviderEnv(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return env, nil
}

// ProviderEnv returns the provider env keys a settings file's content sets.
// Empty content sets nothing.
func ProviderEnv(settingsFile []byte) (map[string]string, error) {
	var settings struct {
		Env map[string]any `json:"env"`
	}
	if len(bytes.TrimSpace(settingsFile)) > 0 {
		if err := json.Unmarshal(settingsFile, &settings); err != nil {
			return nil, err
		}
	}
	env := map[string]string{}
	for k, v := range settings.Env {
		if s, ok := v.(string); ok && IsProviderEnvKey(k) {
			env[k] = s
		}
	}
	return env, nil
}
