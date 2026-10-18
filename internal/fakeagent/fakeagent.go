// Package fakeagent stands in for Claude Code in tests. A test binary whose
// TestMain calls Main becomes a fake agent when MUSTER_FAKE_AGENT_OUT is set:
// it records its args, the --settings file it was given and its provider env
// in that file, then exits with MUSTER_FAKE_AGENT_EXIT.
package fakeagent

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
)

// Record is what one fake-agent run saw.
type Record struct {
	Args         []string          `json:"args"`
	Settings     json.RawMessage   `json:"settings"`
	SettingsMode string            `json:"settingsMode"`
	Env          map[string]string `json:"env"`
}

// Main runs the fake agent and exits when MUSTER_FAKE_AGENT_OUT is set, and
// returns otherwise.
func Main() {
	out := os.Getenv("MUSTER_FAKE_AGENT_OUT")
	if out == "" {
		return
	}
	rec := Record{Args: os.Args[1:], Env: map[string]string{}}
	for i, a := range rec.Args {
		if a == "--settings" && i+1 < len(rec.Args) {
			rec.Settings, _ = os.ReadFile(rec.Args[i+1])
			if fi, err := os.Stat(rec.Args[i+1]); err == nil {
				rec.SettingsMode = fi.Mode().Perm().String()
			}
		}
	}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "ANTHROPIC_") || strings.HasPrefix(k, "CLAUDE_CODE_") || k == "KEEP_ME" {
			rec.Env[k] = v
		}
	}
	b, _ := json.Marshal(rec)
	os.WriteFile(out, b, 0o600)
	code, _ := strconv.Atoi(os.Getenv("MUSTER_FAKE_AGENT_EXIT"))
	os.Exit(code)
}

// Read loads the record a fake-agent run left at path.
func Read(path string) (Record, error) {
	var rec Record
	b, err := os.ReadFile(path)
	if err != nil {
		return rec, err
	}
	return rec, json.Unmarshal(b, &rec)
}
