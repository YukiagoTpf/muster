//go:build !windows

package platform

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"syscall"
)

func tryLock(f *os.File) (bool, error) {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	return err == nil, err
}

func unlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}

func transientRenameError(error) bool { return false }

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// Command is the command that runs bin with args.
func Command(bin string, args []string) *exec.Cmd { return exec.Command(bin, args...) }

// Shell is the command that runs a command line the user wrote, in the
// platform's shell: sh on Unix.
func Shell(line string) *exec.Cmd { return exec.Command("sh", "-c", line) }

// ExitCode is a finished child's exit code; a child killed by a signal
// reports 128 + the signal number, as a shell does.
func ExitCode(e *exec.ExitError) int {
	if ws, ok := e.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return e.ExitCode()
}

// EnableVT makes out interpret ANSI escape sequences and returns a restore
// func. Unix terminals always do.
func EnableVT(*os.File) func() { return func() {} }

// StartDetached starts cmd in its own session, with no terminal and no
// stdio, and lets it outlive this process.
func StartDetached(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// OpenURL opens a URL in the default browser: open on macOS, xdg-open
// elsewhere.
func OpenURL(url string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	cmd := exec.Command(name, url)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
