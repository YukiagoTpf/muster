package platform

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuoteCmdArg(t *testing.T) {
	for in, want := range map[string]string{
		"":                 `""`,
		"plain":            "plain",
		`C:\Program Files`: `"C:\Program Files"`,
		`a"b`:              `"a\"b"`,
		`dir with space\`:  `"dir with space\\"`,
		"a&b":              `"a&b"`,
		`x\"y`:             `"x\\\"y"`,
	} {
		if got := quoteCmdArg(in); got != want {
			t.Errorf("quoteCmdArg(%q) = %s, want %s", in, got, want)
		}
	}
}

// TestCommandRunsCmdShim runs a .cmd in a directory with a space through
// Command and checks the arguments arrive intact.
func TestCommandRunsCmdShim(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "with space")
	os.MkdirAll(dir, 0o700)
	shim := filepath.Join(dir, "agent.cmd")
	os.WriteFile(shim, []byte("@echo off\r\necho [%1] [%2] [%3]\r\nexit /b 3\r\n"), 0o700)
	out, err := Command(shim, []string{"--settings", `C:\a b\s.json`, "x&y"}).Output()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || ExitCode(exit) != 3 {
		t.Fatalf("exit = %v, out = %s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != `[--settings] ["C:\a b\s.json"] ["x&y"]` {
		t.Fatalf("shim saw %s", got)
	}
}
