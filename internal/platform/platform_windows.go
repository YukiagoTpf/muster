package platform

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// Command is the command that runs bin with args. CreateProcess runs .exe and
// .com directly; a .cmd or .bat (an npm-installed shim) has to go through
// cmd.exe, with one fully quoted command line so spaces and shell
// metacharacters in the path or the args can neither split nor inject.
func Command(bin string, args []string) *exec.Cmd {
	if ext := strings.ToLower(filepath.Ext(bin)); ext != ".cmd" && ext != ".bat" {
		return exec.Command(bin, args...)
	}
	comspec := os.Getenv("ComSpec")
	if comspec == "" {
		comspec = filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	}
	cmd := exec.Command(comspec)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: quoteCmdArg(comspec) + " /d /s /c " + cmdLine(bin, args)}
	return cmd
}

// cmdLine is the argument of `cmd /d /s /c`: every part quoted on its own,
// and the whole wrapped in one outer pair of quotes, which /s strips.
func cmdLine(bin string, args []string) string {
	parts := []string{quoteCmdArg(bin)}
	for _, a := range args {
		parts = append(parts, quoteCmdArg(a))
	}
	return `"` + strings.Join(parts, " ") + `"`
}

var (
	cmdSpecial          = regexp.MustCompile(`[\s"&|<>()^]`)
	backslashesQuote    = regexp.MustCompile(`(\\*)"`)
	trailingBackslashes = regexp.MustCompile(`(\\+)$`)
)

// quoteCmdArg quotes one argument for cmd.exe: wrapped in double quotes when
// it holds whitespace or a metacharacter, with embedded quotes and trailing
// backslashes escaped the C-runtime way.
func quoteCmdArg(arg string) string {
	if arg == "" {
		return `""`
	}
	if !cmdSpecial.MatchString(arg) {
		return arg
	}
	out := backslashesQuote.ReplaceAllString(arg, `$1$1\"`)
	out = trailingBackslashes.ReplaceAllString(out, `$1$1`)
	return `"` + out + `"`
}

// ExitCode is a finished child's exit code.
func ExitCode(e *exec.ExitError) int { return e.ExitCode() }

func tryLock(f *os.File) (bool, error) {
	var ol windows.Overlapped
	err := windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ol)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return err == nil, err
}

func unlock(f *os.File) error {
	var ol windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &ol)
}

func transientRenameError(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}

// stillActive is the exit code GetExitCodeProcess reports for a running process.
const stillActive = 259

func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		// Access denied means the process exists but belongs to someone else.
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

// EnableVT makes a console interpret ANSI escape sequences and returns a
// restore func. Output that is not a console (a pseudo-terminal, a pipe) is
// left alone.
func EnableVT(out *os.File) func() {
	h := windows.Handle(out.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return func() {}
	}
	if err := windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING); err != nil {
		return func() {}
	}
	return func() { windows.SetConsoleMode(h, mode) }
}

// StartDetached starts cmd with no console and no stdio, in its own process
// group so a Ctrl-C in this console doesn't reach it, and lets it outlive
// this process.
func StartDetached(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
