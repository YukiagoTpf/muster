//go:build !windows

package platform

import (
	"errors"
	"os"
	"os/exec"
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
