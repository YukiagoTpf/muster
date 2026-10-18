// Package store is Muster's data directory. Every mutation runs inside
// WithLock and writes through WriteAtomic, so a concurrent muster process can
// never land a change between another's read and its write.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/YukiagoTpf/muster/internal/platform"
)

// lockTimeout is how long WithLock waits for another muster process.
const lockTimeout = 10 * time.Second

// Store is one Muster data directory.
type Store struct {
	Dir string
}

// Open resolves the data directory (see platform.Home) and creates it 0700.
func Open(getenv func(string) string) (*Store, error) {
	dir, err := platform.Home(getenv)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{Dir: dir}, nil
}

// Path is the absolute path of a file inside the store.
func (s *Store) Path(name string) string { return filepath.Join(s.Dir, name) }

// WithLock runs fn while holding the store's single cross-process lock. Hold
// it across the whole read → decide → write sequence. It is not reentrant:
// take it once, at the top of a command.
func (s *Store) WithLock(fn func() error) error {
	path := s.Path("lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := platform.Lock(f, lockTimeout); err != nil {
		if errors.Is(err, platform.ErrLocked) {
			return fmt.Errorf("another muster process is busy: %s stayed locked for %s", path, lockTimeout)
		}
		return err
	}
	defer platform.Unlock(f)
	return fn()
}

// Backup saves bytes about to be overwritten as backups/<kind>-<time>-<pid>.json
// (0600) and prunes that kind's pool to the newest keep files. It returns the
// backup's path.
func (s *Store) Backup(kind string, data []byte, keep int) (string, error) {
	dir := s.Path("backups")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%s-%d.json", kind, time.Now().UTC().Format("20060102T150405.000000000Z"), os.Getpid()))
	if err := WriteAtomic(path, data, 0o600); err != nil {
		return "", err
	}
	pool, err := filepath.Glob(filepath.Join(dir, kind+"-*.json"))
	if err != nil {
		return path, err
	}
	sort.Strings(pool) // the timestamp sorts oldest first
	for len(pool) > keep {
		os.Remove(pool[0])
		pool = pool[1:]
	}
	return path, nil
}

// ReadJSON decodes the file at path into v. A missing file reports
// exists == false and leaves v untouched.
func ReadJSON(path string, v any) (exists bool, err error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return true, fmt.Errorf("%s: %w", path, err)
	}
	return true, nil
}

// WriteJSON writes v as indented JSON with a trailing newline, atomically, 0600.
func WriteJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return WriteAtomic(path, append(b, '\n'), 0o600)
}

// WriteAtomic replaces the file at path with data: the bytes land in a tmp
// file beside it, are synced, and are renamed into place, so a reader sees the
// old content or the new, never a torn one. A symlinked path is written
// through — the rename lands on the file the link points at, never on the
// link — so a file linked into a dotfiles repo stays linked.
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	target, err := writeTarget(path)
	if err != nil {
		return err
	}
	var nonce [6]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.%s.tmp", target, os.Getpid(), hex.EncodeToString(nonce[:]))
	if err := writeSynced(tmp, data, perm); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := platform.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func writeSynced(path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// writeTarget is the file a write to path must replace: path itself, or for a
// symlink the file it points at (for a dangling link, the path it names).
func writeTarget(path string) (string, error) {
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return path, nil
	}
	if err != nil {
		return "", err
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		return path, nil
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real, nil
	}
	link, err := os.Readlink(path)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(link) {
		link = filepath.Join(filepath.Dir(path), link)
	}
	return link, nil
}
