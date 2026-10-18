// Package web is `muster web`: a loopback HTTP server for the web page, which
// is a third surface over the same packages the CLI uses. The page is a
// single-page app embedded in the binary; the API is JSON.
//
// Every API call — reads included — needs the session's token, which reaches
// the page through the #fragment of the URL muster opens (a fragment never
// reaches a server, so the token stays out of logs and out of reach of other
// local processes that can only guess the port). The Host header must name the
// loopback address (no DNS rebinding), and a mutation must come with a
// loopback Origin (no cross-site requests).
package web

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"io/fs"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// Env is the process environment the server's work runs against.
type Env struct {
	Getenv  func(string) string
	Environ func() []string
}

// Server is one `muster web` session.
type Server struct {
	env   Env
	token string
	port  int
	mux   *http.ServeMux
}

// New makes a server for a listener on port, with a fresh token.
func New(env Env, port int) (*Server, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	s := &Server{env: env, token: hex.EncodeToString(b[:]), port: port, mux: http.NewServeMux()}
	s.routes()
	return s, nil
}

// Listen opens a loopback listener: on port, or on a free one for 0.
func Listen(port int) (net.Listener, error) {
	return net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
}

// URL is the address the page opens at, token included.
func (s *Server) URL() string {
	return "http://127.0.0.1:" + strconv.Itoa(s.port) + "/#token=" + s.token
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.loopbackHost(r.Host) {
		http.Error(w, "unexpected Host", http.StatusMisdirectedRequest)
		return
	}
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Frame-Options", "DENY")
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		s.static(w, r)
		return
	}
	h.Set("Cache-Control", "no-store")
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
		writeError(w, http.StatusUnauthorized, "this page's session ended: open the address `muster web` printed")
		return
	}
	if r.Method != http.MethodGet && !s.loopbackOrigin(r.Header.Get("Origin")) {
		writeError(w, http.StatusForbidden, "cross-origin request refused")
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) loopbackHost(host string) bool {
	p := ":" + strconv.Itoa(s.port)
	return host == "127.0.0.1"+p || host == "localhost"+p
}

func (s *Server) loopbackOrigin(origin string) bool {
	return origin != "" && s.loopbackHost(strings.TrimPrefix(origin, "http://")) && strings.HasPrefix(origin, "http://")
}

// static serves the page: a file of the build, or index.html for any other
// path (the page routes itself).
func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	root, _ := fs.Sub(dist, "dist")
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" || name == "." {
		name = "index.html"
	}
	b, err := fs.ReadFile(root, name)
	if err != nil {
		name = "index.html"
		if b, err = fs.ReadFile(root, name); err != nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("This muster was built without its web page. Build it with `npm --prefix web ci && npm --prefix web run build`, then build muster again.\n"))
			return
		}
	}
	h := w.Header()
	h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'")
	if strings.HasPrefix(name, "assets/") {
		h.Set("Cache-Control", "public, max-age=31536000, immutable") // content-hashed names
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, name, startTime, bytes.NewReader(b))
}
