package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// flagSet is the standard flag set; the alias keeps command definitions short.
type flagSet = flag.FlagSet

// parseArgs parses `<positional...> [flags]` for a command: exactly npos
// leading positional arguments, then flags. It reports usage errors on
// sys.Err itself.
func parseArgs(sys IO, name string, args []string, npos int, define func(*flagSet)) ([]string, bool) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	define(fs)
	var pos []string
	for len(pos) < npos && len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		pos, args = append(pos, args[0]), args[1:]
	}
	if len(pos) < npos {
		usageError(sys, name, fmt.Sprintf("expected %d argument(s)", npos))
		return nil, false
	}
	if err := fs.Parse(args); err != nil {
		usageError(sys, name, err.Error())
		return nil, false
	}
	if fs.NArg() > 0 {
		usageError(sys, name, fmt.Sprintf("unexpected argument %q", fs.Arg(0)))
		return nil, false
	}
	return pos, true
}

// usageError prints a usage problem with the command's usage line; exit 2.
func usageError(sys IO, name, msg string) int {
	fmt.Fprintf(sys.Err, "muster %s: %s\n", name, msg)
	for _, c := range commands {
		if c.name == name {
			fmt.Fprintf(sys.Err, "usage: %s\n", c.usage)
		}
	}
	return 2
}

// fail reports a command failure; exit 1.
func fail(sys IO, err error) int {
	fmt.Fprintf(sys.Err, "muster: %v\n", err)
	return 1
}

type kv struct{ key, value string }

// kvList is a repeatable KEY=VALUE flag.
type kvList []kv

func (l *kvList) String() string {
	var parts []string
	for _, p := range *l {
		parts = append(parts, p.key+"="+p.value)
	}
	return strings.Join(parts, ",")
}

func (l *kvList) Set(s string) error {
	k, v, ok := strings.Cut(s, "=")
	if !ok || k == "" || v == "" {
		return fmt.Errorf("want KEY=VALUE, got %q", s)
	}
	*l = append(*l, kv{k, v})
	return nil
}
