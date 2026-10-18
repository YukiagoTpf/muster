package syncrepo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/YukiagoTpf/muster/internal/store"
)

// homeRel names a file under the home directory as a slash path relative to
// it — how the repo names a tracked file on every machine of the platform. A
// leading ~ is the home directory, as the page writes it.
func (r *Repo) homeRel(p string) (string, error) {
	if rest, ok := strings.CutPrefix(p, "~"); ok && (rest == "" || rest[0] == '/' || rest[0] == '\\') {
		p = filepath.Join(r.m.Home, rest)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(r.m.Home, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is not under your home directory (%s); only files there can be tracked", abs, r.m.Home)
	}
	return filepath.ToSlash(rel), nil
}

// Track starts syncing a file the user wrote (a statusline script, a hook):
// it goes to the clone now and to the repo with the next sync. It returns the
// file as a ~/path.
func (r *Repo) Track(p string) (string, error) {
	var name string
	err := r.m.Store.WithSyncLock(func() error {
		rel, err := r.homeRel(p)
		if err != nil {
			return err
		}
		abs := r.m.local(rel)
		if same, _ := sameFile(abs, r.m.Settings); same {
			return errors.New("settings.json syncs as the shared settings already (without the default credential); it can't be tracked as a file")
		}
		if within(abs, r.m.Store.Dir) {
			return errors.New("muster's own data never syncs as a file")
		}
		f, ok, err := readFile(abs)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%s does not exist", abs)
		}
		dst := r.abs(r.filesDir() + "/" + rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		perm := os.FileMode(0o600)
		if f.exec {
			perm = 0o700
		}
		name = "~/" + rel
		return store.WriteAtomic(dst, f.data, perm)
	})
	return name, err
}

// Untrack stops syncing a file; the file itself stays on every machine.
func (r *Repo) Untrack(p string) (string, error) {
	var name string
	err := r.m.Store.WithSyncLock(func() error {
		rel, err := r.homeRel(p)
		if err != nil {
			return err
		}
		name = "~/" + rel
		dst := r.abs(r.filesDir() + "/" + rel)
		if err := os.Remove(dst); errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s is not tracked", name)
		} else if err != nil {
			return err
		}
		prune(r.abs(r.filesDir()), dst)
		return nil
	})
	return name, err
}

// Tracked lists the tracked files as ~/paths.
func (r *Repo) Tracked() ([]string, error) {
	rels, err := r.tracked()
	out := []string{}
	for _, rel := range rels {
		out = append(out, "~/"+rel)
	}
	return out, err
}

// Edit runs fn while no other muster process uses the clone — for edits of
// the clone's files, such as the setup list.
func (r *Repo) Edit(fn func() error) error { return r.m.Store.WithSyncLock(fn) }

func sameFile(a, b string) (bool, error) {
	fa, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(fa, fb), nil
}

func within(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
