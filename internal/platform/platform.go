// Package platform holds every operating-system difference Muster has: where
// its data lives, file locking, atomic renames, process liveness, process
// spawning and console modes. Nothing outside this package branches on the OS.
package platform

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Home is Muster's data directory: MUSTER_HOME when set, otherwise
// %APPDATA%\muster on Windows and $XDG_CONFIG_HOME/muster (default
// ~/.config/muster) elsewhere.
func Home(getenv func(string) string) (string, error) {
	if h := getenv("MUSTER_HOME"); h != "" {
		return h, nil
	}
	if runtime.GOOS == "windows" {
		if a := getenv("APPDATA"); a != "" {
			return filepath.Join(a, "muster"), nil
		}
		return "", errors.New("APPDATA is not set; set MUSTER_HOME")
	}
	if x := getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "muster"), nil
	}
	home, err := UserHome(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "muster"), nil
}

// AppConfigDir is where desktop applications keep their per-user config:
// %APPDATA% on Windows, ~/Library/Application Support on macOS, and
// $XDG_CONFIG_HOME (default ~/.config) elsewhere.
func AppConfigDir(getenv func(string) string) (string, error) {
	switch runtime.GOOS {
	case "windows":
		if a := getenv("APPDATA"); a != "" {
			return a, nil
		}
		return "", errors.New("APPDATA is not set")
	case "darwin":
		home, err := UserHome(getenv)
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support"), nil
	default:
		if x := getenv("XDG_CONFIG_HOME"); x != "" {
			return x, nil
		}
		home, err := UserHome(getenv)
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".config"), nil
	}
}

// Name is this platform as the sync repo names it: windows, macos or linux.
// Machines on one platform share a copy of the shared settings; two platforms
// never share one.
func Name() string {
	if runtime.GOOS == "darwin" {
		return "macos"
	}
	return runtime.GOOS
}

// UserHome is the user's home directory (USERPROFILE on Windows, HOME
// elsewhere).
func UserHome(getenv func(string) string) (string, error) {
	key := "HOME"
	if runtime.GOOS == "windows" {
		key = "USERPROFILE"
	}
	if h := getenv(key); h != "" {
		return h, nil
	}
	return "", errors.New(key + " is not set")
}

// ErrLocked reports that another process held the lock for the whole wait.
var ErrLocked = errors.New("locked by another process")

// Lock takes an exclusive lock on f, waiting up to timeout. It is an OS lock
// (flock on Unix, LockFileEx on Windows): the kernel releases it when the
// holding process exits, however it exits, so a dead holder never blocks
// anyone. Two handles in one process contend like two processes.
func Lock(f *os.File, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		ok, err := tryLock(f)
		if err != nil || ok {
			return err
		}
		if time.Now().After(deadline) {
			return ErrLocked
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// Unlock releases a lock taken by Lock.
func Unlock(f *os.File) error { return unlock(f) }

// Rename atomically replaces newpath with oldpath. On Windows a scanner or
// indexer can hold either file for a few milliseconds; that transient denial
// is retried rather than surfaced.
func Rename(oldpath, newpath string) error {
	var err error
	for i := 0; i < 6; i++ {
		if err = os.Rename(oldpath, newpath); err == nil || !transientRenameError(err) {
			return err
		}
		time.Sleep(time.Duration(50*(i+1)) * time.Millisecond)
	}
	return err
}

// ProcessAlive reports whether a process with this pid is running.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return processAlive(pid)
}
