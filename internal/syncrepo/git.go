package syncrepo

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// gitConfig is set on every git call, so the user's own git config can't
// change what a sync does: files go to the agent byte for byte (no line-ending
// conversion), commits are never signed (a signing prompt would hang a sync),
// and every commit is muster's.
var gitConfig = []string{
	"-c", "core.autocrlf=false",
	"-c", "core.quotepath=false",
	"-c", "commit.gpgsign=false",
	"-c", "user.name=muster",
	"-c", "user.email=muster@localhost",
}

// git runs the git CLI in one directory. Muster drives the user's own git, so
// it authenticates to the remote however the user's git already does (SSH
// keys, a credential helper); it never prompts on a terminal.
type git struct {
	dir string
	env []string
}

type gitError struct {
	args   []string
	code   int
	stderr string
}

func (e *gitError) Error() string {
	msg := e.stderr
	if msg == "" {
		msg = fmt.Sprintf("exit status %d", e.code)
	}
	return fmt.Sprintf("git %s: %s", e.args[0], msg)
}

// run runs git and returns its stdout.
func (g git) run(args ...string) (string, error) {
	full := append([]string{}, gitConfig...)
	if g.dir != "" {
		full = append([]string{"-C", g.dir}, full...)
	}
	cmd := exec.Command("git", append(full, args...)...)
	cmd.Env = append(append([]string{}, g.env...), "GIT_TERMINAL_PROMPT=0")
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	if errors.Is(err, exec.ErrNotFound) {
		return "", errors.New("git is not installed: sync needs git on PATH")
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return out.String(), &gitError{args, exit.ExitCode(), strings.TrimSpace(errOut.String())}
	}
	return out.String(), err
}

// test runs a git command whose exit status is the answer: 0 is true, 1 is
// false, anything else an error.
func (g git) test(args ...string) (bool, error) {
	_, err := g.run(args...)
	var ge *gitError
	if errors.As(err, &ge) && ge.code == 1 {
		return false, nil
	}
	return err == nil, err
}

// names splits NUL-separated path output (-z).
func names(out string) []string {
	var paths []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}
