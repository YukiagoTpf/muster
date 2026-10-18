package syncrepo

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/YukiagoTpf/muster/internal/credential"
	"github.com/YukiagoTpf/muster/internal/store"
)

// file is a file's content and whether it is executable — all of a file the
// repo keeps.
type file struct {
	data []byte
	exec bool
}

func (f file) equal(o file) bool { return f.exec == o.exec && bytes.Equal(f.data, o.data) }

// readFile reads a regular file (through a symlink); a missing one reports
// ok == false.
func readFile(p string) (file, bool, error) {
	fi, err := os.Stat(p)
	if errors.Is(err, os.ErrNotExist) {
		return file{}, false, nil
	}
	if err != nil {
		return file{}, false, err
	}
	if !fi.Mode().IsRegular() {
		return file{}, false, fmt.Errorf("%s is not a regular file", p)
	}
	b, err := os.ReadFile(p)
	return file{b, fi.Mode().Perm()&0o111 != 0}, err == nil, err
}

// snapshot is what this machine has of everything it syncs, at one moment.
type snapshot struct {
	settings []byte // the agent's settings file; nil when there is none
	entries  []credential.Entry
	files    map[string]file // tracked file → its content here; absent when missing here
}

func (s snapshot) equal(o snapshot) bool {
	if (s.settings == nil) != (o.settings == nil) || !bytes.Equal(s.settings, o.settings) {
		return false
	}
	if len(s.entries) != len(o.entries) || len(s.files) != len(o.files) {
		return false
	}
	for i := range s.entries {
		if !bytes.Equal(entryBytes(s.entries[i]), entryBytes(o.entries[i])) {
			return false
		}
	}
	for rel, f := range s.files {
		if g, ok := o.files[rel]; !ok || !f.equal(g) {
			return false
		}
	}
	return true
}

// local is where a tracked file lives on this machine.
func (m Machine) local(rel string) string { return filepath.Join(m.Home, filepath.FromSlash(rel)) }

// read takes a snapshot of this machine. The caller holds the store lock.
func (m Machine) read(tracked []string) (snapshot, error) {
	var s snapshot
	b, err := os.ReadFile(m.Settings)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return s, err
	default:
		s.settings = b
	}
	reg, err := credential.Load(m.Store)
	if err != nil {
		return s, err
	}
	s.entries = reg.Entries()
	s.files = map[string]file{}
	for _, rel := range tracked {
		f, ok, err := readFile(m.local(rel))
		if err != nil {
			return s, err
		}
		if ok {
			s.files[rel] = f
		}
	}
	return s, nil
}

// write puts what a sync decided onto this machine, backing up every file it
// overwrites. The caller holds the store lock.
func (m Machine) write(p plan) error {
	if p.settings != nil {
		if err := m.replace(m.Settings, file{data: p.settings}, 0o600); err != nil {
			return err
		}
	}
	if p.registry != nil {
		if err := p.registry.Save(m.Store); err != nil {
			return err
		}
	}
	for rel, f := range p.files {
		if err := m.replace(m.local(rel), f, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// replace writes f over the file at p, keeping its mode but for the
// executable bits, which follow f; a new file gets perm (plus x for an
// executable).
func (m Machine) replace(p string, f file, perm fs.FileMode) error {
	old, err := os.ReadFile(p)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return err
	default:
		if fi, err := os.Stat(p); err == nil {
			perm = fi.Mode().Perm() &^ 0o111
		}
		if _, err := m.Store.Backup(filepath.Base(p), old, store.BackupsKept); err != nil {
			return err
		}
	}
	if f.exec {
		perm |= (perm & 0o444) >> 2
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return store.WriteAtomic(p, f.data, perm)
}
