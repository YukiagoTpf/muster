package claude

import (
	"strings"
	"testing"
)

const userSettings = `{
    "attribution": {"commit": "<none> & co"},
    "env": {
        "ANTHROPIC_AUTH_TOKEN": "sk-old",
        "ANTHROPIC_BASE_URL": "https://old",
        "ENABLE_TOOL_SEARCH": "true",
        "ANTHROPIC_DEFAULT_OPUS_MODEL": "old-opus",
        "DISABLE_AUTOUPDATER": "1"
    },
    "hooks": {"PostToolUse": [{"hooks": [{"type": "command", "command": "jq -r '.x' >> ~/t.jsonl; exit 0"}]}]},
    "statusLine": {"type": "command", "command": "bash ~/.claude/statusline.sh"}
}`

func TestSetProviderEnvReplacesOnlyTheProviderKeys(t *testing.T) {
	got, err := SetProviderEnv([]byte(userSettings), map[string]string{
		"ANTHROPIC_BASE_URL":   "https://new",
		"ANTHROPIC_AUTH_TOKEN": "sk-new",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "attribution": {
    "commit": "<none> & co"
  },
  "env": {
    "ANTHROPIC_AUTH_TOKEN": "sk-new",
    "ANTHROPIC_BASE_URL": "https://new",
    "ENABLE_TOOL_SEARCH": "true",
    "DISABLE_AUTOUPDATER": "1"
  },
  "hooks": {
    "PostToolUse": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "jq -r '.x' >> ~/t.jsonl; exit 0"
          }
        ]
      }
    ]
  },
  "statusLine": {
    "type": "command",
    "command": "bash ~/.claude/statusline.sh"
  }
}
`
	if string(got) != want {
		t.Fatalf("got:\n%s", got)
	}
}

func TestSetProviderEnvWithoutProviderOrEnv(t *testing.T) {
	env := map[string]string{"ANTHROPIC_BASE_URL": "https://gw"}
	got, _ := SetProviderEnv([]byte(`{"env":{"A":"1"},"model":"opus"}`), env)
	if !strings.Contains(string(got), `"env": {
    "ANTHROPIC_BASE_URL": "https://gw",
    "A": "1"
  },`) {
		t.Errorf("no provider keys yet: %s", got)
	}
	got, _ = SetProviderEnv([]byte(`{"model":"opus"}`), env)
	if !strings.HasSuffix(string(got), `"env": {
    "ANTHROPIC_BASE_URL": "https://gw"
  }
}
`) {
		t.Errorf("no env yet: %s", got)
	}
	if got, _ := SetProviderEnv(nil, env); !strings.Contains(string(got), `"ANTHROPIC_BASE_URL": "https://gw"`) {
		t.Errorf("empty file: %s", got)
	}
}

func TestSetProviderEnvRefusesWhatItCannotRead(t *testing.T) {
	for _, bad := range []string{`[]`, `{"env": []}`, `{"env": {}`, `{} {}`, `nope`} {
		if _, err := SetProviderEnv([]byte(bad), map[string]string{"ANTHROPIC_BASE_URL": "x"}); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}
