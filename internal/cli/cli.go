// Package cli is Muster's command surface. Every command is declared once in
// the commands table: human help and `help --json` both generate from it, so
// they cannot drift.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Version is the Muster release this binary was built as.
const Version = "0.1.0-dev"

// IO is the process environment a command runs against. Tests swap every
// field; nothing in a command reads os.Stdin, os.Stdout or os.Getenv directly.
type IO struct {
	In      *os.File
	Out     io.Writer
	Err     io.Writer
	Getenv  func(string) string
	Environ func() []string
}

// StdIO is the real process environment.
func StdIO() IO {
	return IO{In: os.Stdin, Out: os.Stdout, Err: os.Stderr, Getenv: os.Getenv, Environ: os.Environ}
}

type command struct {
	name    string
	usage   string
	summary string
	run     func(sys IO, args []string) int
}

// commands is the public surface; plumbing is muster's own background work,
// dispatched like a command but never listed in help.
var commands, plumbing []command

func init() {
	commands = []command{
		{"run", "muster run <credential> [-m <model>] [--dry-run [--json]] [-- <agent args>...]", "Launch Claude Code with a credential in this terminal", runRun},
		{"quota", "muster quota [--cached] [--json]", "Show every subscription account's quota and what each credential spent", runQuota},
		{"statusline", "muster statusline [--lang en|zh]", "Print the quota line for a Claude Code statusline (reads the statusline JSON on stdin)", runStatusline},
		{"default", "muster default [<credential>] [--json]", "Show, or set, the credential plain `claude` and IDE plugins use (the provider part of settings.json)", runDefault},
		{"cred list", "muster cred list [--json]", "List credentials (never shows keys)", runCredList},
		{"cred add", "muster cred add <name> --provider <p> (--from-settings | --base-url <url>) [--model alias=model]... [--env K=V]... [--key-stdin] [--json]", "Register a credential; the key comes from stdin or settings.json, never argv", runCredAdd},
		{"cred rm", "muster cred rm <name> [--json]", "Remove a credential and its key", runCredRm},
		{"help", "muster help [--json]", "Show commands (--json: machine-readable schema)", runHelp},
		{"version", "muster version", "Print the Muster version", runVersion},
	}
	plumbing = []command{
		{"__refresh-quota", "muster __refresh-quota", "Refresh the quota cache (spawned in the background)", runRefreshQuota},
		{"__refresh-statusline", "muster __refresh-statusline <session>", "Refresh one session's statusline state (spawned in the background)", runRefreshStatusline},
	}
}

// Main runs one muster invocation and returns its exit code.
func Main(args []string, sys IO) int {
	if len(args) == 0 {
		return runPick(sys)
	}
	for _, c := range append(commands, plumbing...) {
		words := strings.Fields(c.name)
		if len(args) >= len(words) && strings.Join(args[:len(words)], " ") == c.name {
			return c.run(sys, args[len(words):])
		}
	}
	fmt.Fprintf(sys.Err, "muster: unknown command %q (see `muster help`)\n", strings.Join(args[:min(2, len(args))], " "))
	return 2
}

func runHelp(sys IO, args []string) int {
	if len(args) == 1 && args[0] == "--json" {
		type entry struct {
			Name    string `json:"name"`
			Usage   string `json:"usage"`
			Summary string `json:"summary"`
		}
		out := struct {
			Version  int     `json:"version"`
			Commands []entry `json:"commands"`
		}{Version: 1}
		for _, c := range commands {
			out.Commands = append(out.Commands, entry{c.name, c.usage, c.summary})
		}
		return writeJSON(sys, out)
	}
	var b strings.Builder
	b.WriteString("muster — launch coding agents per credential, see every quota at a glance\n\nCommands:\n")
	for _, c := range commands {
		fmt.Fprintf(&b, "  %s\n      %s\n", c.usage, c.summary)
	}
	fmt.Fprint(sys.Out, b.String())
	return 0
}

func runVersion(sys IO, _ []string) int {
	fmt.Fprintln(sys.Out, Version)
	return 0
}

func writeJSON(sys IO, v any) int {
	enc := json.NewEncoder(sys.Out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(sys.Err, "muster: %v\n", err)
		return 1
	}
	return 0
}
