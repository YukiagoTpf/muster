package syncrepo

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/YukiagoTpf/muster/internal/store"
)

// Passphrase asks the user for the repo's passphrase: a new one when this
// machine creates the repo, the existing one when it joins.
type Passphrase func(create bool) (string, error)

// Join sets this machine up to sync through the repo at url. An empty repo
// becomes a Muster sync repo, seeded with what this machine has. A repo other
// machines already use restores this machine: what the repo has wins — the
// files it overwrites are backed up — and what only this machine has (its
// other credentials, the shared settings of a platform new to the repo) is
// added to it.
func Join(m Machine, url string, passphrase Passphrase) (*Repo, Result, error) {
	var r *Repo
	res := Result{Sent: []Item{}, Received: []Item{}, Conflicts: []Item{}}
	err := m.Store.WithSyncLock(func() error {
		var c config
		if ok, err := store.ReadJSON(configPath(m), &c); err != nil {
			return err
		} else if ok {
			return fmt.Errorf("this machine already syncs with %s", c.Repo)
		}
		r = newRepo(m, url)
		if err := os.RemoveAll(r.dir); err != nil { // a clone left by a join that failed
			return err
		}
		if _, err := (git{env: m.Env}).run("clone", "-q", url, r.dir); err != nil {
			return err
		}
		err := r.join(passphrase, &res)
		if err != nil && !errors.Is(err, errPushFailed) {
			os.RemoveAll(r.dir)
		}
		return err
	})
	return r, res, err
}

var errPushFailed = errors.New("joined, but the push failed (`muster sync` retries it)")

func (r *Repo) join(passphrase Passphrase, res *Result) error {
	// What the remote's HEAD names doesn't matter: a sync repo is its main
	// branch, and a remote with no branch at all is a new one.
	heads, err := r.git.run("for-each-ref", "--format=%(refname)", "refs/remotes/origin")
	if err != nil {
		return err
	}
	message := fmt.Sprintf("%s joins (%s)", r.m.Host, r.m.Platform)
	switch {
	case strings.TrimSpace(heads) == "":
		if err := r.create(passphrase); err != nil {
			return err
		}
		message = fmt.Sprintf("create the sync repo from %s (%s)", r.m.Host, r.m.Platform)
	case slices.Contains(strings.Fields(heads), r.upstream()):
		if _, err := r.git.run("checkout", "-q", "-B", branch, r.upstream()); err != nil {
			return err
		}
		if err := r.unlock(passphrase); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%s is not a Muster sync repo (it has no %s branch); give an empty repo to start one", r.URL, branch)
	}

	tracked, err := r.tracked()
	if err != nil {
		return err
	}
	var s snapshot
	if err := r.m.Store.WithLock(func() (err error) { s, err = r.m.read(tracked); return err }); err != nil {
		return err
	}
	if res.Sent, err = r.export(s, true, message); err != nil {
		return err
	}
	p, err := r.planApply(s)
	if err != nil {
		return err
	}
	if _, ok, err := r.read(r.setupPath()); err != nil {
		return err
	} else if ok {
		p.items = append(p.items, Item{"setup", r.m.Platform})
	}
	err = r.m.Store.WithLock(func() error {
		now, err := r.m.read(tracked)
		if err != nil {
			return err
		}
		if !s.equal(now) {
			return errors.New("this machine's settings or credentials changed while joining; run it again")
		}
		if err := r.m.write(p); err != nil {
			return err
		}
		return store.WriteJSON(configPath(r.m), config{configVersion, r.URL, r.id.String()})
	})
	if err != nil {
		return err
	}
	res.Received = sortItems(p.items)
	if pushed, err := r.push(); err != nil {
		return fmt.Errorf("%w: %v", errPushFailed, err)
	} else if !pushed {
		return fmt.Errorf("%w: another machine pushed meanwhile", errPushFailed)
	}
	return nil
}

// create makes the empty clone a Muster sync repo with a new identity.
func (r *Repo) create(passphrase Passphrase) error {
	if _, err := r.git.run("symbolic-ref", "HEAD", "refs/heads/"+branch); err != nil {
		return err
	}
	pass, err := passphrase(true)
	if err != nil {
		return err
	}
	if r.id, err = newIdentity(); err != nil {
		return err
	}
	sealed, err := sealIdentity(r.id, pass)
	if err != nil {
		return err
	}
	marker, _ := json.Marshal(struct {
		Version int `json:"version"`
	}{1})
	for rel, data := range map[string][]byte{markerPath: append(marker, '\n'), identityPath: sealed, attributesPath: []byte(attributes)} {
		if err := store.WriteAtomic(r.abs(rel), data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// unlock opens the identity of a repo other machines created.
func (r *Repo) unlock(passphrase Passphrase) error {
	var marker struct {
		Version int `json:"version"`
	}
	if ok, err := store.ReadJSON(r.abs(markerPath), &marker); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%s is not a Muster sync repo (it has no %s); give an empty repo to start one", r.URL, markerPath)
	}
	if marker.Version != 1 {
		return fmt.Errorf("%s has sync repo version %d; this muster understands 1", r.URL, marker.Version)
	}
	sealed, ok, err := r.read(identityPath)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s has no %s", r.URL, identityPath)
	}
	pass, err := passphrase(false)
	if err != nil {
		return err
	}
	r.id, err = openIdentity(sealed, pass)
	return err
}
