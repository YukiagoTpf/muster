package store

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "home")
	s, err := Open(func(k string) string {
		if k == "MUSTER_HOME" {
			return dir
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestOpenCreatesPrivateDir(t *testing.T) {
	s := open(t)
	fi, err := os.Stat(s.Dir)
	if err != nil || !fi.IsDir() {
		t.Fatalf("store dir not created: %v", err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o700 {
		t.Errorf("store dir mode = %v, want 0700", fi.Mode().Perm())
	}
}

func TestWithLockSerializesTransactions(t *testing.T) {
	s := open(t)
	var mu sync.Mutex
	var events []string
	log := func(e string) { mu.Lock(); events = append(events, e); mu.Unlock() }
	held := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		s.WithLock(func() error {
			log("a-start")
			close(held)
			time.Sleep(150 * time.Millisecond)
			log("a-end")
			return nil
		})
	}()
	go func() {
		defer wg.Done()
		<-held
		s.WithLock(func() error { log("b"); return nil })
	}()
	wg.Wait()
	if len(events) != 3 || events[0] != "a-start" || events[1] != "a-end" || events[2] != "b" {
		t.Fatalf("transactions interleaved: %v", events)
	}
}

func TestWithLockReturnsFnError(t *testing.T) {
	s := open(t)
	want := os.ErrInvalid
	if err := s.WithLock(func() error { return want }); err != want {
		t.Fatalf("got %v", err)
	}
	if err := s.WithLock(func() error { return nil }); err != nil {
		t.Fatalf("lock not released after an error: %v", err)
	}
}

func TestWriteAtomicReplacesWithoutLeftovers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.json")
	os.WriteFile(path, []byte("old"), 0o600)
	if err := WriteAtomic(path, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "new" {
		t.Fatalf("content = %q", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("leftover files: %v", entries)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
		}
	}
}

func TestWriteAtomicWritesThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles", "settings.json")
	os.MkdirAll(filepath.Dir(real), 0o700)
	os.WriteFile(real, []byte("old"), 0o600)
	link := filepath.Join(dir, "settings.json")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
	if err := WriteAtomic(link, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced by a regular file")
	}
	if got, _ := os.ReadFile(real); string(got) != "new" {
		t.Fatalf("link target = %q", got)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.json")
	var v struct{ A int }
	if ok, err := ReadJSON(path, &v); ok || err != nil {
		t.Fatalf("missing file: exists=%v err=%v", ok, err)
	}
	if err := WriteJSON(path, struct{ A int }{7}); err != nil {
		t.Fatal(err)
	}
	if ok, err := ReadJSON(path, &v); !ok || err != nil || v.A != 7 {
		t.Fatalf("round trip: exists=%v err=%v v=%+v", ok, err, v)
	}
	os.WriteFile(path, []byte("{"), 0o600)
	if _, err := ReadJSON(path, &v); err == nil {
		t.Fatal("corrupt JSON was accepted")
	}
}
