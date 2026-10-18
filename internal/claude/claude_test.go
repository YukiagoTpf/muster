package claude

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestIsProviderEnvKey(t *testing.T) {
	for key, want := range map[string]bool{
		"ANTHROPIC_BASE_URL":                true,
		"ANTHROPIC_AUTH_TOKEN":              true,
		"ANTHROPIC_DEFAULT_OPUS_MODEL":      true,
		"ANTHROPIC_DEFAULT_OPUS_MODEL_NAME": true,
		"ANTHROPIC_CUSTOM_HEADERS":          true,
		"CLAUDE_CODE_OAUTH_TOKEN":           true,
		"CLAUDE_CODE_API_KEY_HELPER_TTL_MS": true,
		"CLAUDE_CODE_USE_BEDROCK":           true,
		"CLAUDE_CODE_USE_FOUNDRY":           true,
		"CLAUDE_CODE_MAX_OUTPUT_TOKENS":     false,
		"CLAUDE_CODE_DISABLE_ARTIFACT":      false,
		"CLAUDE_CODE_AUTO_MODE_SERVER":      false,
		"ENABLE_TOOL_SEARCH":                false,
		"HTTPS_PROXY":                       false,
		"MY_ANTHROPIC_BASE_URL":             false,
	} {
		if got := IsProviderEnvKey(key); got != want {
			t.Errorf("IsProviderEnvKey(%s) = %v, want %v", key, got, want)
		}
	}
}

func TestIsSecretEnvKey(t *testing.T) {
	for key, want := range map[string]bool{
		"ANTHROPIC_AUTH_TOKEN":         true,
		"ANTHROPIC_API_KEY":            true,
		"CLAUDE_CODE_OAUTH_TOKEN":      true,
		"ANTHROPIC_BASE_URL":           false,
		"ANTHROPIC_DEFAULT_OPUS_MODEL": false,
		"MY_TOKEN":                     false,
	} {
		if got := IsSecretEnvKey(key); got != want {
			t.Errorf("IsSecretEnvKey(%s) = %v, want %v", key, got, want)
		}
	}
}

func TestModelAliasesOrderAndParse(t *testing.T) {
	got := ModelAliases(map[string]string{
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":     "claude-haiku-5-5",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":      "claude-opus-5-5[1M]",
		"ANTHROPIC_DEFAULT_OPUS_MODEL_NAME": "claude-opus-5-5",
		"ANTHROPIC_DEFAULT_SONNET_MODEL":    "claude-sonnet-5-5[1M]",
		"ANTHROPIC_DEFAULT_ZETA_MODEL":      "z",
		"ANTHROPIC_DEFAULT_FABLE_MODEL":     "claude-fable-5-1[1M]",
		"ANTHROPIC_DEFAULT_EMPTY_MODEL":     "",
		"ANTHROPIC_BASE_URL":                "https://gw",
	})
	want := []ModelAlias{
		{"opus", "claude-opus-5-5[1M]"},
		{"fable", "claude-fable-5-1[1M]"},
		{"sonnet", "claude-sonnet-5-5[1M]"},
		{"haiku", "claude-haiku-5-5"},
		{"zeta", "z"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ModelAliases = %v", got)
	}
	if ModelEnvKey("opus") != "ANTHROPIC_DEFAULT_OPUS_MODEL" {
		t.Fatal(ModelEnvKey("opus"))
	}
}

func TestReadProviderEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if env, err := ReadProviderEnv(path); err != nil || len(env) != 0 {
		t.Fatalf("missing file: %v %v", env, err)
	}
	os.WriteFile(path, []byte(`{"env":{"ANTHROPIC_BASE_URL":"https://gw","ANTHROPIC_AUTH_TOKEN":"sk","ENABLE_TOOL_SEARCH":"true","ANTHROPIC_X":1},"hooks":{}}`), 0o600)
	env, err := ReadProviderEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(env, map[string]string{"ANTHROPIC_BASE_URL": "https://gw", "ANTHROPIC_AUTH_TOKEN": "sk"}) {
		t.Fatalf("env = %v", env)
	}
	os.WriteFile(path, []byte(`{`), 0o600)
	if _, err := ReadProviderEnv(path); err == nil {
		t.Fatal("corrupt settings accepted")
	}
}

func TestConfigDir(t *testing.T) {
	get := func(kv map[string]string) func(string) string { return func(k string) string { return kv[k] } }
	if d, _ := ConfigDir(get(map[string]string{"CLAUDE_CONFIG_DIR": "/c", "HOME": "/h", "USERPROFILE": "/h"})); d != "/c" {
		t.Errorf("CLAUDE_CONFIG_DIR not honored: %s", d)
	}
	if d, _ := ConfigDir(get(map[string]string{"HOME": "/h", "USERPROFILE": "/h"})); d != filepath.Join("/h", ".claude") {
		t.Errorf("default = %s", d)
	}
}

func TestSecretLookingEnv(t *testing.T) {
	keys, err := SecretLookingEnv([]byte(`{"env":{"TAVILY_API_KEY":"x","GITHUB_TOKEN":"y","CLAUDE_CODE_MAX_OUTPUT_TOKENS":"9","ENABLE_TOOL_SEARCH":"true"}}`))
	if err != nil || !reflect.DeepEqual(keys, []string{"GITHUB_TOKEN", "TAVILY_API_KEY"}) {
		t.Fatalf("keys = %v, %v", keys, err)
	}
	if keys, err := SecretLookingEnv([]byte(`{"hooks":{}}`)); err != nil || keys != nil {
		t.Fatalf("no env: %v, %v", keys, err)
	}
}
