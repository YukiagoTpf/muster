package credential

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/YukiagoTpf/muster/internal/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(func(k string) string {
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

func valid() Credential {
	return Credential{
		Name: "claude-pool", Provider: "astergate", BaseURL: "https://gw.example.com",
		Env: map[string]string{"ANTHROPIC_DEFAULT_OPUS_MODEL": "claude-opus-5-5[1M]"},
	}
}

func TestValidate(t *testing.T) {
	if err := Validate(valid()); err != nil {
		t.Fatalf("valid credential rejected: %v", err)
	}
	for name, mutate := range map[string]func(*Credential){
		"bad name":         func(c *Credential) { c.Name = "-x" },
		"space in name":    func(c *Credential) { c.Name = "a b" },
		"bad provider":     func(c *Credential) { c.Provider = "Aster Gate" },
		"no scheme":        func(c *Credential) { c.BaseURL = "gw.example.com" },
		"ftp":              func(c *Credential) { c.BaseURL = "ftp://gw" },
		"base url in env":  func(c *Credential) { c.Env["ANTHROPIC_BASE_URL"] = "https://x" },
		"token in env":     func(c *Credential) { c.Env["ANTHROPIC_AUTH_TOKEN"] = "sk" },
		"secret in env":    func(c *Credential) { c.Env["ANTHROPIC_API_KEY"] = "sk" },
		"non-provider env": func(c *Credential) { c.Env["ENABLE_TOOL_SEARCH"] = "true" },
	} {
		c := valid()
		c.Env = map[string]string{"ANTHROPIC_DEFAULT_OPUS_MODEL": "m"}
		mutate(&c)
		if err := Validate(c); err == nil {
			t.Errorf("%s: accepted %+v", name, c)
		}
	}
}

func TestAddRemoveSaveLoad(t *testing.T) {
	s := openStore(t)
	r, err := Load(s)
	if err != nil || len(r.Credentials) != 0 {
		t.Fatalf("empty store: %v %v", r, err)
	}
	if err := r.Add(valid(), "sk-1"); err != nil {
		t.Fatal(err)
	}
	other := valid()
	other.Name = "Claude-Pool"
	if err := r.Add(other, "sk-2"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("case-insensitive duplicate accepted: %v", err)
	}
	if err := r.Add(Credential{Name: "b", Provider: "astergate", BaseURL: "https://gw"}, " "); err == nil {
		t.Fatal("empty key accepted")
	}
	if err := r.Save(s); err != nil {
		t.Fatal(err)
	}

	r2, err := Load(s)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := r2.Get("claude-pool")
	if !ok || c.Env["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "claude-opus-5-5[1M]" {
		t.Fatalf("round trip lost the credential: %+v", c)
	}
	if _, ok := r2.Get("CLAUDE-POOL"); ok {
		t.Fatal("lookup is not exact")
	}
	if k, ok := r2.Secret("claude-pool"); !ok || k != "sk-1" {
		t.Fatalf("secret = %q %v", k, ok)
	}
	reg, _ := os.ReadFile(s.Path("credentials.json"))
	if strings.Contains(string(reg), "sk-1") {
		t.Fatal("credentials.json contains the key")
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(s.Path("secrets.json")); fi.Mode().Perm() != 0o600 {
			t.Errorf("secrets.json mode = %v", fi.Mode().Perm())
		}
	}

	if err := r2.Remove("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("remove unknown: %v", err)
	}
	if err := r2.Remove("claude-pool"); err != nil {
		t.Fatal(err)
	}
	r2.Save(s)
	sec, _ := os.ReadFile(s.Path("secrets.json"))
	if strings.Contains(string(sec), "sk-1") {
		t.Fatal("removed credential's key is still stored")
	}
}

func TestLoadRejectsNewerVersion(t *testing.T) {
	s := openStore(t)
	os.WriteFile(filepath.Join(s.Dir, "credentials.json"), []byte(`{"version":2,"credentials":[]}`), 0o600)
	if _, err := Load(s); err == nil || !strings.Contains(err.Error(), "version 2") {
		t.Fatalf("newer file accepted: %v", err)
	}
}

func TestProviderEnv(t *testing.T) {
	env := valid().ProviderEnv("sk")
	if env["ANTHROPIC_BASE_URL"] != "https://gw.example.com" || env["ANTHROPIC_AUTH_TOKEN"] != "sk" || env["ANTHROPIC_DEFAULT_OPUS_MODEL"] == "" {
		t.Fatalf("ProviderEnv = %v", env)
	}
}
