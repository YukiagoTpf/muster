package syncrepo

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/YukiagoTpf/muster/internal/claude"
	"github.com/YukiagoTpf/muster/internal/credential"
	"github.com/YukiagoTpf/muster/internal/store"
)

// op is one change an export makes to the clone.
type op struct {
	path   string
	data   []byte
	exec   bool
	remove bool
}

// entries decrypts every credential in the clone, by name.
func (r *Repo) entries() (map[string]credential.Entry, error) {
	paths, err := filepath.Glob(filepath.Join(r.abs(credentialsDir), "*.age"))
	if err != nil {
		return nil, err
	}
	out := map[string]credential.Entry{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		e, err := openEntry(b, r.id)
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", credentialsDir, filepath.Base(p), err)
		}
		out[e.Credential.Name] = e
	}
	return out, nil
}

// exportOps works out how the clone has to change to hold what this machine
// has. Joining, the repo wins: only what the repo lacks is added.
func (r *Repo) exportOps(s snapshot, joining bool) ([]op, error) {
	var ops []op
	if s.settings != nil {
		shared, err := claude.SharedSettings(s.settings)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", r.m.Settings, err)
		}
		secret, err := claude.SecretLookingEnv(shared)
		if err != nil {
			return nil, err
		}
		if len(secret) > 0 {
			return nil, fmt.Errorf("%s sets env %s, which looks like a secret; shared settings reach the sync repo unencrypted, so move it out of settings.json first",
				r.m.Settings, strings.Join(secret, ", "))
		}
		cur, ok, err := r.read(r.settingsPath())
		if err != nil {
			return nil, err
		}
		if !(joining && ok) && !bytes.Equal(cur, shared) {
			ops = append(ops, op{path: r.settingsPath(), data: shared})
		}
	}

	inRepo, err := r.entries()
	if err != nil {
		return nil, err
	}
	here := map[string]bool{}
	for _, e := range s.entries {
		here[e.Credential.Name] = true
		cur, ok := inRepo[e.Credential.Name]
		if ok && (joining || bytes.Equal(entryBytes(cur), entryBytes(e))) {
			continue
		}
		sealed, err := sealEntry(e, r.id)
		if err != nil {
			return nil, err
		}
		ops = append(ops, op{path: credentialPath(e.Credential.Name), data: sealed})
	}
	if !joining {
		for name := range inRepo {
			if !here[name] {
				ops = append(ops, op{path: credentialPath(name), remove: true})
			}
		}
		// A tracked file missing here is not a deletion: the next apply
		// restores it. Only `muster untrack` stops tracking a file.
		for rel, f := range s.files {
			cur, ok, err := readFile(r.abs(r.filesDir() + "/" + rel))
			if err != nil {
				return nil, err
			}
			if !ok || !cur.equal(f) {
				ops = append(ops, op{path: r.filesDir() + "/" + rel, data: f.data, exec: f.exec})
			}
		}
	}
	// Removals go first: on a case-insensitive file system a credential
	// renamed only in case is the same file.
	sort.SliceStable(ops, func(i, j int) bool {
		if ops[i].remove != ops[j].remove {
			return ops[i].remove
		}
		return ops[i].path < ops[j].path
	})
	return ops, nil
}

// export writes this machine's state into the clone and commits it; it
// returns what changed.
func (r *Repo) export(s snapshot, joining bool, message string) ([]Item, error) {
	ops, err := r.exportOps(s, joining)
	if err != nil {
		return nil, err
	}
	paths := []string{}
	for _, o := range ops {
		paths = append(paths, o.path)
		p := r.abs(o.path)
		if o.remove {
			if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			continue
		}
		perm := os.FileMode(0o600)
		if o.exec {
			perm = 0o700
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return nil, err
		}
		if err := store.WriteAtomic(p, o.data, perm); err != nil {
			return nil, err
		}
	}
	pending, err := r.uncommitted()
	if err != nil {
		return nil, err
	}
	if len(pending) > 0 {
		if _, err := r.git.run("add", "-A"); err != nil {
			return nil, err
		}
		if _, err := r.git.run("commit", "-q", "-m", message); err != nil {
			return nil, err
		}
	}
	return r.items(append(paths, pending...)), nil
}

// uncommitted lists the clone's paths that differ from HEAD (an export's
// writes, and what track, untrack and the setup list's edits left there).
func (r *Repo) uncommitted() ([]string, error) {
	out, err := r.git.run("status", "--porcelain", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	var paths []string
	entries := names(out)
	for i := 0; i < len(entries); i++ {
		e := entries[i]
		if len(e) < 4 {
			continue
		}
		paths = append(paths, e[3:])
		if e[0] == 'R' || e[0] == 'C' {
			i++ // the rename's source follows
		}
	}
	return paths, nil
}

// plan is what applying the clone to this machine changes.
type plan struct {
	settings []byte               // nil: settings.json stays as it is
	registry *credential.Registry // nil: the registry stays as it is
	files    map[string]file
	items    []Item
}

// planApply works out what this machine has to change to hold what the clone
// has. It reads what the snapshot doesn't cover (files tracked only since).
func (r *Repo) planApply(s snapshot) (plan, error) {
	p := plan{files: map[string]file{}, items: []Item{}}
	shared, ok, err := r.read(r.settingsPath())
	if err != nil {
		return p, err
	}
	// settings.json is rewritten only when its shared part changed, so a file
	// whose content the repo already has keeps its formatting.
	var mine []byte
	if s.settings != nil {
		if mine, err = claude.SharedSettings(s.settings); err != nil {
			return p, fmt.Errorf("%s: %w", r.m.Settings, err)
		}
	}
	if ok && (s.settings == nil || !bytes.Equal(mine, shared)) {
		provider, err := claude.ProviderEnv(s.settings)
		if err != nil {
			return p, fmt.Errorf("%s: %w", r.m.Settings, err)
		}
		if p.settings, err = claude.SetProviderEnv(shared, provider); err != nil {
			return p, fmt.Errorf("the repo's %s: %w", r.settingsPath(), err)
		}
		p.items = append(p.items, Item{"settings", r.m.Platform})
	}

	inRepo, err := r.entries()
	if err != nil {
		return p, err
	}
	here := map[string]credential.Entry{}
	for _, e := range s.entries {
		here[e.Credential.Name] = e
	}
	var changed []string
	for name, e := range inRepo {
		if h, ok := here[name]; !ok || !bytes.Equal(entryBytes(h), entryBytes(e)) {
			changed = append(changed, name)
		}
	}
	for name := range here {
		if _, ok := inRepo[name]; !ok {
			changed = append(changed, name)
		}
	}
	if len(changed) > 0 {
		var list []credential.Entry
		for _, e := range inRepo {
			list = append(list, e)
		}
		if p.registry, err = credential.FromEntries(list); err != nil {
			return p, fmt.Errorf("the repo's credentials: %w", err)
		}
		sort.Strings(changed)
		for _, name := range changed {
			p.items = append(p.items, Item{"credential", name})
		}
	}

	tracked, err := r.tracked()
	if err != nil {
		return p, err
	}
	for _, rel := range tracked {
		f, _, err := readFile(r.abs(r.filesDir() + "/" + rel))
		if err != nil {
			return p, err
		}
		cur, ok := s.files[rel]
		if !ok {
			if cur, ok, err = readFile(r.m.local(rel)); err != nil {
				return p, err
			}
		}
		if !ok || !cur.equal(f) {
			p.files[rel] = f
			p.items = append(p.items, Item{"file", "~/" + rel})
		}
	}
	return p, nil
}

// apply puts the clone onto this machine, unless the machine changed since
// the snapshot the decision was made from (then it reports false and writes
// nothing).
func (r *Repo) apply(s snapshot, tracked []string, p plan) (bool, error) {
	same := false
	err := r.m.Store.WithLock(func() error {
		now, err := r.m.read(tracked)
		if err != nil {
			return err
		}
		if same = s.equal(now); !same {
			return nil
		}
		return r.m.write(p)
	})
	return same, err
}

// Result is what a sync did.
type Result struct {
	Sent      []Item `json:"sent"`      // changed here, now in the repo
	Received  []Item `json:"received"`  // changed elsewhere, now here
	Conflicts []Item `json:"conflicts"` // changed here and elsewhere: nothing was applied
}

func (res *Result) add(sent, received []Item) {
	res.Sent = sortItems(append(res.Sent, sent...))
	res.Received = sortItems(append(res.Received, received...))
}

// attempts bounds how often a sync starts over because this machine or the
// remote changed under it.
const attempts = 3

// Sync exchanges changes with the other machines: it commits what changed
// here, merges what the others pushed, applies the merge here and pushes it.
// When both sides changed the same thing differently it stops before
// applying anything and reports the conflicts, unless prefer ("local" or
// "remote") says which side wins them.
func (r *Repo) Sync(prefer string) (Result, error) {
	res := Result{Sent: []Item{}, Received: []Item{}, Conflicts: []Item{}}
	err := r.m.Store.WithSyncLock(func() error {
		for range attempts {
			tracked, err := r.tracked()
			if err != nil {
				return err
			}
			var s snapshot
			if err := r.m.Store.WithLock(func() (err error) { s, err = r.m.read(tracked); return err }); err != nil {
				return err
			}
			sent, err := r.export(s, false, fmt.Sprintf("sync from %s (%s)", r.m.Host, r.m.Platform))
			if err != nil {
				return err
			}
			if _, err := r.git.run("fetch", "-q", "origin"); err != nil {
				return err
			}
			before, err := r.head()
			if err != nil {
				return err
			}
			conflicts, err := r.merge(prefer)
			if err != nil {
				return err
			}
			if len(conflicts) > 0 {
				res.add(sent, nil)
				res.Conflicts = r.items(conflicts)
				return nil
			}
			p, err := r.planApply(s)
			if err != nil {
				r.git.run("reset", "-q", "--hard", before)
				return err
			}
			same, err := r.apply(s, tracked, p)
			if err != nil {
				return err
			}
			res.add(sent, nil)
			if !same {
				continue // changed here meanwhile: export that too
			}
			res.add(nil, p.items)
			pushed, err := r.push()
			if err != nil {
				return fmt.Errorf("applied here, but the push failed (the next sync retries it): %w", err)
			}
			if pushed {
				return nil
			}
		}
		return errors.New("this machine or the repo kept changing during the sync; run it again")
	})
	return res, err
}

func (r *Repo) head() (string, error) {
	out, err := r.git.run("rev-parse", "-q", "--verify", "HEAD")
	var ge *gitError
	if errors.As(err, &ge) && ge.code == 1 {
		return "", nil // no commit yet
	}
	return strings.TrimSpace(out), err
}

// merge merges what the remote has into HEAD. Conflicting paths come back
// with the merge undone, unless prefer resolves them: by hunk where git can,
// by whole file where it can't (one side deleted the file).
func (r *Repo) merge(prefer string) ([]string, error) {
	if ok, err := r.git.test("rev-parse", "-q", "--verify", r.upstream()); err != nil || !ok {
		return nil, err // nothing pushed yet
	}
	if ok, err := r.git.test("merge-base", "--is-ancestor", r.upstream(), "HEAD"); err != nil || ok {
		return nil, err // nothing new
	}
	var strategy, side, stage string
	switch prefer {
	case "local":
		strategy, side, stage = "ours", "--ours", " 2\t"
	case "remote":
		strategy, side, stage = "theirs", "--theirs", " 3\t"
	}
	args := []string{"merge", "-q", "--no-edit", "--ff"}
	if strategy != "" {
		args = append(args, "-X", strategy)
	}
	_, mergeErr := r.git.run(append(args, r.upstream())...)
	if mergeErr == nil {
		return nil, nil
	}
	out, err := r.git.run("diff", "--name-only", "-z", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	conflicts := names(out)
	if len(conflicts) == 0 {
		return nil, mergeErr
	}
	if strategy == "" {
		_, err := r.git.run("merge", "--abort")
		return conflicts, err
	}
	for _, p := range conflicts {
		stages, err := r.git.run("ls-files", "-u", "-z", "--", p)
		if err != nil {
			return nil, err
		}
		if strings.Contains(stages, stage) {
			_, err = r.git.run("checkout", side, "--", p)
			if err == nil {
				_, err = r.git.run("add", "--", p)
			}
		} else {
			_, err = r.git.run("rm", "-q", "--", p)
		}
		if err != nil {
			return nil, err
		}
	}
	_, err = r.git.run("commit", "-q", "--no-edit")
	return nil, err
}

// push pushes HEAD unless the remote has it already. It reports false when
// the remote moved meanwhile (another machine pushed first): the caller
// merges again.
func (r *Repo) push() (bool, error) {
	if ok, err := r.git.test("rev-parse", "-q", "--verify", r.upstream()); err != nil {
		return false, err
	} else if ok {
		if ok, err := r.git.test("merge-base", "--is-ancestor", "HEAD", r.upstream()); err != nil || ok {
			return ok, err // the remote has HEAD
		}
	}
	_, pushErr := r.git.run("push", "-q", "origin", "HEAD:refs/heads/"+branch)
	if pushErr == nil {
		return true, nil
	}
	if _, err := r.git.run("fetch", "-q", "origin"); err != nil {
		return false, pushErr
	}
	if ok, err := r.git.test("merge-base", "--is-ancestor", r.upstream(), "HEAD"); err == nil && !ok {
		return false, nil
	}
	return false, pushErr
}

// Change is a pending change: here, not yet in the repo.
type Change struct {
	Item
	Change string `json:"change"` // "added", "modified" or "removed"
}

// Status is where this machine stands, worked out without the network.
type Status struct {
	Repo     string   `json:"repo"`
	Platform string   `json:"platform"`
	Pending  []Change `json:"pending"`  // the next sync sends these
	Unpushed bool     `json:"unpushed"` // a sync committed but could not push
	Tracked  []string `json:"tracked"`  // as ~/paths
}

// Status reports what the next sync would send, without the network.
func (r *Repo) Status() (Status, error) {
	st := Status{Repo: r.URL, Platform: r.m.Platform, Pending: []Change{}, Tracked: []string{}}
	err := r.m.Store.WithSyncLock(func() error {
		tracked, err := r.tracked()
		if err != nil {
			return err
		}
		for _, rel := range tracked {
			st.Tracked = append(st.Tracked, "~/"+rel)
		}
		var s snapshot
		if err := r.m.Store.WithLock(func() (err error) { s, err = r.m.read(tracked); return err }); err != nil {
			return err
		}
		ops, err := r.exportOps(s, false)
		if err != nil {
			return err
		}
		seen := map[Item]bool{}
		note := func(p, change string) {
			if it, ok := r.item(p); ok && !seen[it] {
				seen[it] = true
				st.Pending = append(st.Pending, Change{it, change})
			}
		}
		for _, o := range ops {
			change := "modified"
			if o.remove {
				change = "removed"
			} else if _, ok, _ := r.read(o.path); !ok {
				change = "added"
			}
			note(o.path, change)
		}
		out, err := r.git.run("status", "--porcelain", "-z", "--untracked-files=all")
		if err != nil {
			return err
		}
		for _, e := range names(out) {
			if len(e) < 4 {
				continue
			}
			change := map[byte]string{'?': "added", 'A': "added", 'D': "removed"}[strings.TrimSpace(e[:2])[0]]
			if change == "" {
				change = "modified"
			}
			note(e[3:], change)
		}
		if ok, err := r.git.test("rev-parse", "-q", "--verify", r.upstream()); err != nil {
			return err
		} else if ok {
			ahead, err := r.git.test("merge-base", "--is-ancestor", "HEAD", r.upstream())
			st.Unpushed = !ahead
			return err
		}
		st.Unpushed = true
		return nil
	})
	return st, err
}

// prune removes the empty directories left above a removed file, up to root.
func prune(root, p string) {
	for dir := filepath.Dir(p); dir != root && strings.HasPrefix(dir, root); dir = filepath.Dir(dir) {
		if os.Remove(dir) != nil {
			return
		}
	}
}
