package platform

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestMain doubles as a helper process: with MUSTER_PLATFORM_HELPER=hold-lock
// the test binary locks the file named by MUSTER_LOCK_FILE, reports it, and
// waits to be killed.
func TestMain(m *testing.M) {
	if os.Getenv("MUSTER_PLATFORM_HELPER") == "hold-lock" {
		f, err := os.OpenFile(os.Getenv("MUSTER_LOCK_FILE"), os.O_CREATE|os.O_RDWR, 0o600)
		if err == nil {
			err = Lock(f, time.Second)
		}
		if err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
		fmt.Println("locked")
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func env(kv map[string]string) func(string) string {
	return func(k string) string { return kv[k] }
}

func TestHome(t *testing.T) {
	if got, _ := Home(env(map[string]string{"MUSTER_HOME": "/m", "APPDATA": "/a"})); got != "/m" {
		t.Errorf("MUSTER_HOME not honored: %s", got)
	}
	if runtime.GOOS == "windows" {
		if got, _ := Home(env(map[string]string{"APPDATA": `C:\A`})); got != `C:\A\muster` {
			t.Errorf("windows default = %s", got)
		}
		return
	}
	if got, _ := Home(env(map[string]string{"XDG_CONFIG_HOME": "/x", "HOME": "/h"})); got != "/x/muster" {
		t.Errorf("XDG_CONFIG_HOME not honored: %s", got)
	}
	if got, _ := Home(env(map[string]string{"HOME": "/h"})); got != "/h/.config/muster" {
		t.Errorf("unix default = %s", got)
	}
}

func openLock(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestLockExcludesAnotherHandle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	a, b := openLock(t, path), openLock(t, path)
	if err := Lock(a, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := Lock(b, 100*time.Millisecond); !errors.Is(err, ErrLocked) {
		t.Fatalf("second handle got %v while the first held the lock", err)
	}
	if err := Unlock(a); err != nil {
		t.Fatal(err)
	}
	if err := Lock(b, time.Second); err != nil {
		t.Fatalf("lock not available after unlock: %v", err)
	}
	Unlock(b)
}

func TestLockReleasedWhenHolderIsKilled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "MUSTER_PLATFORM_HELPER=hold-lock", "MUSTER_LOCK_FILE="+path)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if line, _ := bufio.NewReader(out).ReadString('\n'); line != "locked\n" {
		cmd.Process.Kill()
		t.Fatalf("helper said %q", line)
	}
	f := openLock(t, path)
	if err := Lock(f, 100*time.Millisecond); !errors.Is(err, ErrLocked) {
		t.Fatalf("lock not held by the live helper: %v", err)
	}
	cmd.Process.Kill()
	cmd.Wait()
	if err := Lock(f, 2*time.Second); err != nil {
		t.Fatalf("lock still held after the holder was killed: %v", err)
	}
	Unlock(f)
}

func TestProcessAlive(t *testing.T) {
	if !ProcessAlive(os.Getpid()) {
		t.Error("own process reported dead")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if ProcessAlive(cmd.Process.Pid) {
		t.Error("exited process reported alive")
	}
	if ProcessAlive(0) || ProcessAlive(-1) {
		t.Error("invalid pid reported alive")
	}
}

func TestRenameReplaces(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	os.WriteFile(a, []byte("new"), 0o600)
	os.WriteFile(b, []byte("old"), 0o600)
	if err := Rename(a, b); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(b); string(got) != "new" {
		t.Fatalf("b = %q", got)
	}
}
