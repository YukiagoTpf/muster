package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/YukiagoTpf/muster/internal/claude"
	"github.com/YukiagoTpf/muster/internal/credential"
	"github.com/YukiagoTpf/muster/internal/quota"
	"github.com/YukiagoTpf/muster/internal/setup"
	"github.com/YukiagoTpf/muster/internal/store"
	"github.com/YukiagoTpf/muster/internal/syncrepo"
)

// startTime stamps the embedded files.
var startTime = time.Now()

// Version is the Muster release the page reports; cli sets it.
var Version = ""

func (s *Server) routes() {
	m := s.mux
	m.HandleFunc("GET /api/state", s.state)
	m.HandleFunc("GET /api/quota", s.quota)
	m.HandleFunc("POST /api/quota/refresh", s.refreshQuota)
	m.HandleFunc("GET /api/credentials", s.credentials)
	m.HandleFunc("POST /api/credentials", s.addCredential)
	m.HandleFunc("POST /api/credentials/import", s.importCredential)
	m.HandleFunc("PUT /api/credentials/{name}", s.editCredential)
	m.HandleFunc("DELETE /api/credentials/{name}", s.removeCredential)
	m.HandleFunc("POST /api/default", s.setDefault)
	m.HandleFunc("GET /api/sync", s.syncStatus)
	m.HandleFunc("POST /api/sync", s.sync)
	m.HandleFunc("GET /api/sync/sides", s.sides)
	m.HandleFunc("POST /api/tracked", s.track)
	m.HandleFunc("DELETE /api/tracked", s.untrack)
	m.HandleFunc("GET /api/setup", s.setupList)
	m.HandleFunc("POST /api/setup", s.setupAdd)
	m.HandleFunc("DELETE /api/setup/{name}", s.setupRemove)
	m.HandleFunc("POST /api/setup/install", s.setupRun(true))
	m.HandleFunc("POST /api/setup/update", s.setupRun(false))
	m.HandleFunc("GET /api/settings", s.settings)
	m.HandleFunc("PUT /api/settings", s.saveSettings)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

type apiError struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(apiError{msg})
}

// fail reports a failed call. The packages' errors are written for people,
// so the message goes to the page as is.
func fail(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, credential.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, syncrepo.ErrChanged):
		status = http.StatusConflict
	case errors.Is(err, syncrepo.ErrNotSetUp):
		status = http.StatusPreconditionFailed
	}
	writeError(w, status, err.Error())
}

func readBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad request body: "+err.Error())
		return false
	}
	return true
}

func (s *Server) store() (*store.Store, error) { return store.Open(s.env.Getenv) }

func (s *Server) machine() (syncrepo.Machine, error) {
	return syncrepo.Here(s.env.Getenv, s.env.Environ)
}

func (s *Server) repo() (*syncrepo.Repo, syncrepo.Machine, error) {
	m, err := s.machine()
	if err != nil {
		return nil, m, err
	}
	r, err := syncrepo.Open(m)
	return r, m, err
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	m, err := s.machine()
	if err != nil {
		fail(w, err)
		return
	}
	out := struct {
		Version  int    `json:"version"`
		Muster   string `json:"muster"`
		Platform string `json:"platform"`
		Settings string `json:"settings"`
		Synced   bool   `json:"synced"`
		Repo     string `json:"repo,omitempty"`
	}{Version: 1, Muster: Version, Platform: m.Platform, Settings: m.Settings}
	if repo, err := syncrepo.Open(m); err == nil {
		out.Synced, out.Repo = true, repo.URL
	}
	writeJSON(w, out)
}

// staleAfter is how old cached quota may be before the page refreshes it.
const staleAfter = time.Minute

type quotaView struct {
	quota.Overview
	Stale bool `json:"stale"`
}

func (s *Server) quota(w http.ResponseWriter, r *http.Request) {
	st, err := s.store()
	if err != nil {
		fail(w, err)
		return
	}
	reports := quota.LoadCache(st)
	o := quota.OverviewOf(reports)
	writeJSON(w, quotaView{o, len(reports) == 0 || time.Since(o.FetchedAt) > staleAfter})
}

// refreshing lets one quota refresh run at a time; a second request waits
// for it rather than asking every source again.
var refreshing sync.Mutex

func (s *Server) refreshQuota(w http.ResponseWriter, r *http.Request) {
	st, err := s.store()
	if err != nil {
		fail(w, err)
		return
	}
	refreshing.Lock()
	defer refreshing.Unlock()
	reports := quota.LoadCache(st)
	if len(reports) == 0 || time.Since(quota.OverviewOf(reports).FetchedAt) > 5*time.Second {
		reg, err := credential.Load(st)
		if err != nil {
			fail(w, err)
			return
		}
		if reports, err = quota.Refresh(st, reg, s.env.Getenv); err != nil {
			fail(w, err)
			return
		}
	}
	writeJSON(w, quotaView{quota.OverviewOf(reports), false})
}

func (s *Server) credentials(w http.ResponseWriter, r *http.Request) {
	st, err := s.store()
	if err != nil {
		fail(w, err)
		return
	}
	reg, err := credential.Load(st)
	if err != nil {
		fail(w, err)
		return
	}
	path, err := claude.SettingsPath(s.env.Getenv)
	if err != nil {
		fail(w, err)
		return
	}
	env, err := claude.ReadProviderEnv(path)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, struct {
		Version     int                     `json:"version"`
		Credentials []credential.View       `json:"credentials"`
		Default     credential.DefaultState `json:"default"`
		Settings    string                  `json:"settings"`
	}{1, reg.Views(), reg.ClassifyDefault(env), path})
}

// credentialBody is a credential as the page sends it. Key is empty on an
// edit that keeps the stored key.
type credentialBody struct {
	Name     string            `json:"name"`
	Provider string            `json:"provider"`
	BaseURL  string            `json:"baseURL"`
	Env      map[string]string `json:"env"`
	Key      string            `json:"key"`
}

func (s *Server) changeCredentials(w http.ResponseWriter, fn func(*credential.Registry) (credential.View, error)) {
	st, err := s.store()
	if err != nil {
		fail(w, err)
		return
	}
	var view credential.View
	err = credential.Update(st, func(reg *credential.Registry) (err error) {
		view, err = fn(reg)
		return err
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, struct {
		Credential credential.View `json:"credential"`
	}{view})
}

func (s *Server) addCredential(w http.ResponseWriter, r *http.Request) {
	var b credentialBody
	if !readBody(w, r, &b) {
		return
	}
	s.changeCredentials(w, func(reg *credential.Registry) (credential.View, error) {
		c := credential.Credential{Name: b.Name, Provider: b.Provider, BaseURL: b.BaseURL, Env: b.Env}
		if err := reg.Add(c, strings.TrimSpace(b.Key)); err != nil {
			return credential.View{}, err
		}
		return reg.ViewOf(c), nil
	})
}

func (s *Server) importCredential(w http.ResponseWriter, r *http.Request) {
	var b credentialBody
	if !readBody(w, r, &b) {
		return
	}
	path, err := claude.SettingsPath(s.env.Getenv)
	if err != nil {
		fail(w, err)
		return
	}
	c, key, err := credential.FromSettings(path)
	if err != nil {
		fail(w, err)
		return
	}
	c.Name, c.Provider = b.Name, b.Provider
	s.changeCredentials(w, func(reg *credential.Registry) (credential.View, error) {
		if err := reg.Add(c, key); err != nil {
			return credential.View{}, err
		}
		return reg.ViewOf(c), nil
	})
}

func (s *Server) editCredential(w http.ResponseWriter, r *http.Request) {
	var b credentialBody
	if !readBody(w, r, &b) {
		return
	}
	s.changeCredentials(w, func(reg *credential.Registry) (credential.View, error) {
		c := credential.Credential{Name: r.PathValue("name"), Provider: b.Provider, BaseURL: b.BaseURL, Env: b.Env}
		if err := reg.Edit(c, strings.TrimSpace(b.Key)); err != nil {
			return credential.View{}, err
		}
		return reg.ViewOf(c), nil
	})
}

func (s *Server) removeCredential(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	st, err := s.store()
	if err != nil {
		fail(w, err)
		return
	}
	if err := credential.Update(st, func(reg *credential.Registry) error { return reg.Remove(name) }); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, struct {
		Removed string `json:"removed"`
	}{name})
}

func (s *Server) setDefault(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Credential string `json:"credential"`
	}
	if !readBody(w, r, &b) {
		return
	}
	st, err := s.store()
	if err != nil {
		fail(w, err)
		return
	}
	path, err := claude.SettingsPath(s.env.Getenv)
	if err != nil {
		fail(w, err)
		return
	}
	backup, err := credential.MakeDefault(st, path, b.Credential)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, struct {
		Credential string `json:"credential"`
		Backup     string `json:"backup,omitempty"`
	}{b.Credential, backup})
}

func (s *Server) syncStatus(w http.ResponseWriter, r *http.Request) {
	repo, m, err := s.repo()
	if errors.Is(err, syncrepo.ErrNotSetUp) {
		writeJSON(w, struct {
			SetUp    bool   `json:"setUp"`
			Platform string `json:"platform"`
		}{false, m.Platform})
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	st, err := repo.Status()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, struct {
		SetUp bool `json:"setUp"`
		syncrepo.Status
	}{true, st})
}

func (s *Server) sync(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Prefer string `json:"prefer"`
	}
	if !readBody(w, r, &b) {
		return
	}
	if b.Prefer != "" && b.Prefer != "local" && b.Prefer != "remote" {
		writeError(w, http.StatusBadRequest, "prefer: want local or remote")
		return
	}
	repo, m, err := s.repo()
	if err != nil {
		fail(w, err)
		return
	}
	res, err := repo.Sync(b.Prefer)
	if err != nil {
		fail(w, err)
		return
	}
	missing := []string{}
	if l, err := setup.Load(repo.SetupList()); err == nil {
		if st, err := setup.LoadState(m.Store); err == nil && st.Missing(l) != nil {
			missing = st.Missing(l)
		}
	}
	writeJSON(w, struct {
		syncrepo.Result
		NotInstalled []string `json:"notInstalled"`
	}{res, missing})
}

func (s *Server) sides(w http.ResponseWriter, r *http.Request) {
	repo, _, err := s.repo()
	if err != nil {
		fail(w, err)
		return
	}
	sides, err := repo.Sides(syncrepo.Item{Kind: r.URL.Query().Get("kind"), Name: r.URL.Query().Get("name")})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, sides)
}

func (s *Server) changeTracked(w http.ResponseWriter, r *http.Request, track bool) {
	var b struct {
		Path string `json:"path"`
	}
	if !readBody(w, r, &b) {
		return
	}
	repo, _, err := s.repo()
	if err != nil {
		fail(w, err)
		return
	}
	change := repo.Untrack
	if track {
		change = repo.Track
	}
	if _, err := change(b.Path); err != nil {
		fail(w, err)
		return
	}
	tracked, err := repo.Tracked()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, struct {
		Tracked []string `json:"tracked"`
	}{tracked})
}

func (s *Server) track(w http.ResponseWriter, r *http.Request)   { s.changeTracked(w, r, true) }
func (s *Server) untrack(w http.ResponseWriter, r *http.Request) { s.changeTracked(w, r, false) }

type setupEntryView struct {
	setup.Entry
	Installed bool `json:"installed"`
}

func (s *Server) setupList(w http.ResponseWriter, r *http.Request) {
	repo, m, err := s.repo()
	if err != nil {
		fail(w, err)
		return
	}
	l, err := setup.Load(repo.SetupList())
	if err != nil {
		fail(w, err)
		return
	}
	st, err := setup.LoadState(m.Store)
	if err != nil {
		fail(w, err)
		return
	}
	views := []setupEntryView{}
	for _, e := range l.Entries {
		views = append(views, setupEntryView{e, st.Recorded(e)})
	}
	writeJSON(w, struct {
		Platform string           `json:"platform"`
		Entries  []setupEntryView `json:"entries"`
	}{repo.Platform(), views})
}

func (s *Server) editSetup(w http.ResponseWriter, fn func(*setup.List) error) {
	repo, _, err := s.repo()
	if err != nil {
		fail(w, err)
		return
	}
	err = repo.Edit(func() error {
		l, err := setup.Load(repo.SetupList())
		if err != nil {
			return err
		}
		if err := fn(&l); err != nil {
			return err
		}
		return l.Save(repo.SetupList())
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, struct {
		OK bool `json:"ok"`
	}{true})
}

func (s *Server) setupAdd(w http.ResponseWriter, r *http.Request) {
	var e setup.Entry
	if !readBody(w, r, &e) {
		return
	}
	s.editSetup(w, func(l *setup.List) error { return l.Add(e) })
}

func (s *Server) setupRemove(w http.ResponseWriter, r *http.Request) {
	s.editSetup(w, func(l *setup.List) error { return l.Remove(r.PathValue("name")) })
}

// streamWriter sends a command's output to the page as it comes, one JSON
// line per write.
type streamWriter struct {
	mu  *sync.Mutex
	enc *json.Encoder
	fl  http.Flusher
}

type streamEvent struct {
	Type   string        `json:"type"` // output, result or done
	Text   string        `json:"text,omitempty"`
	Result *setup.Result `json:"result,omitempty"`
	Error  string        `json:"error,omitempty"`
}

func (sw streamWriter) send(e streamEvent) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.enc.Encode(e)
	sw.fl.Flush()
}

func (sw streamWriter) Write(p []byte) (int, error) {
	sw.send(streamEvent{Type: "output", Text: string(p)})
	return len(p), nil
}

// setupRun installs or updates setup list entries, streaming the commands'
// output and each result as NDJSON.
func (s *Server) setupRun(install bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Names []string `json:"names"`
			Force bool     `json:"force"`
		}
		if !readBody(w, r, &b) {
			return
		}
		repo, m, err := s.repo()
		if err != nil {
			fail(w, err)
			return
		}
		l, err := setup.Load(repo.SetupList())
		if err != nil {
			fail(w, err)
			return
		}
		entries, err := l.Select(b.Names)
		if err != nil {
			fail(w, err)
			return
		}
		fl, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "streaming unsupported")
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
		sw := streamWriter{&sync.Mutex{}, json.NewEncoder(w), fl}
		runner, err := setup.NewRunner(m.Store, s.env.Environ(), nil, sw)
		if err == nil {
			report := func(res setup.Result) { sw.send(streamEvent{Type: "result", Result: &res}) }
			if install {
				err = runner.Install(entries, b.Force, report)
			} else {
				err = runner.Update(entries, report)
			}
		}
		done := streamEvent{Type: "done"}
		if err != nil {
			done.Error = err.Error()
		}
		sw.send(done)
	}
}

type settingsView struct {
	Doc       syncrepo.Doc   `json:"doc"`
	Platforms []platformView `json:"platforms"`
}

type platformView struct {
	Platform string `json:"platform"`
	Local    bool   `json:"local"`
}

// settings reads one platform's shared settings: this machine's by default;
// another platform's when the machine syncs.
func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	m, err := s.machine()
	if err != nil {
		fail(w, err)
		return
	}
	platform := r.URL.Query().Get("platform")
	views := []platformView{{m.Platform, true}}
	repo, err := syncrepo.Open(m)
	if err == nil {
		for _, p := range syncrepo.Platforms {
			if p != m.Platform {
				views = append(views, platformView{p, false})
			}
		}
	} else if !errors.Is(err, syncrepo.ErrNotSetUp) {
		fail(w, err)
		return
	}
	var doc syncrepo.Doc
	switch {
	case platform == "" || platform == m.Platform:
		doc, err = m.SharedDoc()
	case repo == nil:
		err = errors.New("editing another platform's shared settings needs sync set up (`muster sync init <repo>`)")
	default:
		doc, err = repo.SharedDoc(platform)
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, settingsView{doc, views})
}

func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Platform string `json:"platform"`
		Content  string `json:"content"`
		Hash     string `json:"hash"`
	}
	if !readBody(w, r, &b) {
		return
	}
	m, err := s.machine()
	if err != nil {
		fail(w, err)
		return
	}
	var doc syncrepo.Doc
	if b.Platform == "" || b.Platform == m.Platform {
		doc, err = m.SaveSharedDoc([]byte(b.Content), b.Hash)
	} else {
		var repo *syncrepo.Repo
		if repo, err = syncrepo.Open(m); err == nil {
			doc, err = repo.SaveSharedDoc(b.Platform, []byte(b.Content), b.Hash)
		}
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, struct {
		Doc syncrepo.Doc `json:"doc"`
	}{doc})
}
