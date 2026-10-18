package syncrepo

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/YukiagoTpf/muster/internal/claude"
	"github.com/YukiagoTpf/muster/internal/store"
)

// Doc is one platform's shared settings as an editor sees them: this
// machine's settings.json without its provider env, or the sync repo's copy
// of another platform's.
type Doc struct {
	Platform string `json:"platform"`
	// Local is true for this machine's own settings.json; a save goes there.
	// Otherwise a save goes to the sync clone and the next sync sends it.
	Local   bool   `json:"local"`
	Exists  bool   `json:"exists"`
	Path    string `json:"path"`
	Content string `json:"content"`
	// Hash identifies the file as read; a save carries it back and fails
	// with ErrChanged if the file changed since.
	Hash string `json:"hash"`
	// SecretEnv lists env keys that look like secrets: shared settings carry
	// them to the sync repo unencrypted, so a sync refuses them.
	SecretEnv []string `json:"secretEnv"`
}

// ErrChanged reports a save over a file that changed since it was read.
var ErrChanged = errors.New("the file changed since it was read")

func hashOf(b []byte, exists bool) string {
	if !exists {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func docOf(platform, path string, local bool, file []byte, exists bool) (Doc, error) {
	d := Doc{Platform: platform, Local: local, Exists: exists, Path: path, Hash: hashOf(file, exists), Content: "{}\n", SecretEnv: []string{}}
	if !exists {
		return d, nil
	}
	shared, err := claude.SharedSettings(file)
	if err != nil {
		return d, fmt.Errorf("%s: %w", path, err)
	}
	if d.SecretEnv, err = claude.SecretLookingEnv(shared); err != nil {
		return d, err
	}
	if d.SecretEnv == nil {
		d.SecretEnv = []string{}
	}
	d.Content = string(shared)
	return d, nil
}

func readMaybe(p string) ([]byte, bool, error) {
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return b, err == nil, err
}

// checkShared refuses content that is not shared settings: not a JSON object,
// or carrying provider env, which belongs to a credential.
func checkShared(content []byte) ([]byte, error) {
	shared, err := claude.SharedSettings(content)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Env map[string]json.RawMessage `json:"env"`
	}
	json.Unmarshal(content, &doc)
	var provider []string
	for k := range doc.Env {
		if claude.IsProviderEnvKey(k) {
			provider = append(provider, k)
		}
	}
	if len(provider) > 0 {
		sort.Strings(provider)
		return nil, fmt.Errorf("env %s is provider env: it belongs to a credential, not to the shared settings", strings.Join(provider, ", "))
	}
	return shared, nil
}

// SharedDoc reads this machine's shared settings.
func (m Machine) SharedDoc() (Doc, error) {
	b, ok, err := readMaybe(m.Settings)
	if err != nil {
		return Doc{}, err
	}
	return docOf(m.Platform, m.Settings, true, b, ok)
}

// SaveSharedDoc replaces this machine's shared settings: settings.json gets
// content with its own provider env put back, backed up first, unless the file
// changed since it was read as hash.
func (m Machine) SaveSharedDoc(content []byte, hash string) (Doc, error) {
	shared, err := checkShared(content)
	if err != nil {
		return Doc{}, err
	}
	var d Doc
	err = m.Store.WithLock(func() error {
		old, ok, err := readMaybe(m.Settings)
		if err != nil {
			return err
		}
		if hashOf(old, ok) != hash {
			return ErrChanged
		}
		provider, err := claude.ProviderEnv(old)
		if err != nil {
			return fmt.Errorf("%s: %w", m.Settings, err)
		}
		next, err := claude.SetProviderEnv(shared, provider)
		if err != nil {
			return err
		}
		perm := fs.FileMode(0o600)
		if ok {
			if fi, err := os.Stat(m.Settings); err == nil {
				perm = fi.Mode().Perm()
			}
			if !bytes.Equal(old, next) {
				if _, err := m.Store.Backup("settings.json", old, store.BackupsKept); err != nil {
					return err
				}
			}
		}
		if err := os.MkdirAll(filepath.Dir(m.Settings), 0o700); err != nil {
			return err
		}
		if err := store.WriteAtomic(m.Settings, next, perm); err != nil {
			return err
		}
		d, err = docOf(m.Platform, m.Settings, true, next, true)
		return err
	})
	return d, err
}

// Platforms the repo can hold shared settings for.
var Platforms = []string{"windows", "macos", "linux"}

func (r *Repo) platformSettings(platform string) (string, error) {
	for _, p := range Platforms {
		if p == platform {
			return p + "/settings.json", nil
		}
	}
	return "", fmt.Errorf("unknown platform %q (windows, macos, linux)", platform)
}

// SharedDoc reads a platform's shared settings: this machine's own for its
// platform, the sync clone's copy for another.
func (r *Repo) SharedDoc(platform string) (Doc, error) {
	if platform == r.m.Platform {
		return r.m.SharedDoc()
	}
	rel, err := r.platformSettings(platform)
	if err != nil {
		return Doc{}, err
	}
	b, ok, err := r.read(rel)
	if err != nil {
		return Doc{}, err
	}
	return docOf(platform, r.abs(rel), false, b, ok)
}

// SaveSharedDoc replaces a platform's shared settings. Another platform's
// copy is written into the sync clone, and the next sync sends it; since it
// goes to the repo as is, it may carry no secret-looking env.
func (r *Repo) SaveSharedDoc(platform string, content []byte, hash string) (Doc, error) {
	if platform == r.m.Platform {
		return r.m.SaveSharedDoc(content, hash)
	}
	rel, err := r.platformSettings(platform)
	if err != nil {
		return Doc{}, err
	}
	shared, err := checkShared(content)
	if err != nil {
		return Doc{}, err
	}
	if secret, err := claude.SecretLookingEnv(shared); err != nil {
		return Doc{}, err
	} else if len(secret) > 0 {
		return Doc{}, fmt.Errorf("env %s looks like a secret; shared settings reach the sync repo unencrypted", strings.Join(secret, ", "))
	}
	var d Doc
	err = r.m.Store.WithSyncLock(func() error {
		old, ok, err := r.read(rel)
		if err != nil {
			return err
		}
		if hashOf(old, ok) != hash {
			return ErrChanged
		}
		p := r.abs(rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return err
		}
		if err := store.WriteAtomic(p, shared, 0o600); err != nil {
			return err
		}
		d, err = docOf(platform, p, false, shared, true)
		return err
	})
	return d, err
}

// Sides is both versions of a conflicting item after a sync stopped on it:
// this machine's, and the one the other machines pushed. A credential shows
// without its key, saying only whether the keys differ.
type Sides struct {
	Item
	Here        string `json:"here"`
	HereExists  bool   `json:"hereExists"`
	There       string `json:"there"`
	ThereExists bool   `json:"thereExists"`
}

func (r *Repo) pathOf(it Item) (string, error) {
	switch it.Kind {
	case "settings":
		return it.Name + "/settings.json", nil
	case "setup":
		return it.Name + "/setup.json", nil
	case "credential":
		return credentialPath(it.Name), nil
	case "file":
		return r.filesDir() + "/" + strings.TrimPrefix(it.Name, "~/"), nil
	}
	return "", fmt.Errorf("unknown item kind %q", it.Kind)
}

// Sides reads both versions of an item: HEAD (what this machine committed)
// and what the remote has.
func (r *Repo) Sides(it Item) (Sides, error) {
	s := Sides{Item: it}
	p, err := r.pathOf(it)
	if err != nil {
		return s, err
	}
	show := func(rev string) ([]byte, bool, error) {
		out, err := r.git.run("show", rev+":"+p)
		var ge *gitError
		if errors.As(err, &ge) {
			return nil, false, nil // not in that revision
		}
		return []byte(out), err == nil, err
	}
	here, hereOK, err := show("HEAD")
	if err != nil {
		return s, err
	}
	there, thereOK, err := show(r.upstream())
	if err != nil {
		return s, err
	}
	s.HereExists, s.ThereExists = hereOK, thereOK
	if it.Kind != "credential" {
		s.Here, s.There = string(here), string(there)
		return s, nil
	}
	var keys [2]string
	for i, side := range []struct {
		b   []byte
		ok  bool
		out *string
	}{{here, hereOK, &s.Here}, {there, thereOK, &s.There}} {
		if !side.ok {
			continue
		}
		e, err := openEntry(side.b, r.id)
		if err != nil {
			return s, err
		}
		keys[i] = e.Key
		b, _ := json.MarshalIndent(e.Credential, "", "  ")
		*side.out = string(b) + "\n"
	}
	if hereOK && thereOK && keys[0] != keys[1] {
		s.Here += "(the key differs)\n"
		s.There += "(the key differs)\n"
	}
	return s, nil
}
