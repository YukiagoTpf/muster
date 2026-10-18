// Package launch turns a credential into a running agent instance. A launch
// owns the provider env end to end: inherited provider env is stripped from
// the child's process env, and the credential arrives through a private
// --settings file that also blanks every default-credential key the
// credential does not set — the --settings layer outranks the user settings
// key by key, and a blank value counts as unset, so nothing of the default
// credential leaks into the instance.
package launch

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/YukiagoTpf/muster/internal/claude"
	"github.com/YukiagoTpf/muster/internal/platform"
	"github.com/YukiagoTpf/muster/internal/store"
)

// Plan is one launch, fully decided before anything is written or spawned.
type Plan struct {
	Credential string
	// Model is an alias or a model id; empty lets the agent choose.
	Model string
	// SettingsEnv is the env block of the private --settings file. It holds
	// the credential's key, so it is never printed.
	SettingsEnv map[string]string
	// Env is the child's process env.
	Env []string
	// Stripped lists the inherited provider env keys removed from Env.
	Stripped []string
	// Blanked lists the default-credential keys the credential does not set,
	// blanked in SettingsEnv.
	Blanked []string
	// ExtraArgs pass through to the agent.
	ExtraArgs []string
}

// Build decides a launch of credential cred, whose complete provider env is
// providerEnv, on a machine whose default credential is defaultEnv (the
// provider env in the user settings), from a parent process env parentEnv.
func Build(cred string, providerEnv, defaultEnv map[string]string, parentEnv []string, model string, extra []string) Plan {
	p := Plan{Credential: cred, Model: model, SettingsEnv: map[string]string{}, ExtraArgs: extra}
	for k := range defaultEnv {
		if _, ok := providerEnv[k]; !ok {
			p.SettingsEnv[k] = ""
			p.Blanked = append(p.Blanked, k)
		}
	}
	for k, v := range providerEnv {
		p.SettingsEnv[k] = v
	}
	for _, kv := range parentEnv {
		k, _, _ := strings.Cut(kv, "=")
		if claude.IsProviderEnvKey(k) {
			p.Stripped = append(p.Stripped, k)
			continue
		}
		p.Env = append(p.Env, kv)
	}
	sort.Strings(p.Blanked)
	sort.Strings(p.Stripped)
	return p
}

// SettingsKeys lists the env keys the private settings file sets, sorted.
func (p Plan) SettingsKeys() []string {
	keys := make([]string, 0, len(p.SettingsEnv))
	for k := range p.SettingsEnv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Args is the agent's argv for a private settings file at settingsPath.
func (p Plan) Args(settingsPath string) []string {
	args := []string{"--settings", settingsPath}
	if p.Model != "" {
		args = append(args, "--model", p.Model)
	}
	return append(args, p.ExtraArgs...)
}

// AgentBin resolves the agent binary: MUSTER_CLAUDE_BIN when set, otherwise
// `claude` on PATH.
func AgentBin(getenv func(string) string) (string, error) {
	name := "claude"
	if b := getenv("MUSTER_CLAUDE_BIN"); b != "" {
		name = b
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("cannot find the agent binary %q (install Claude Code or set MUSTER_CLAUDE_BIN): %w", name, err)
	}
	return path, nil
}

// Stdio is what the agent's terminal is wired to.
type Stdio struct {
	In       io.Reader
	Out, Err io.Writer
}

// Run executes a plan in the foreground: it writes the private settings file
// (0600, under <store>/run), runs the agent, removes the file, and returns the
// agent's exit code. Ctrl-C belongs to the agent while it runs.
func Run(s *store.Store, p Plan, bin string, stdio Stdio) (int, error) {
	dir := s.Path("run")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, err
	}
	collectStale(dir)
	path, err := writeSettings(dir, p.SettingsEnv)
	if err != nil {
		return 0, err
	}
	defer os.Remove(path)

	cmd := platform.Command(bin, p.Args(path))
	cmd.Env = p.Env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdio.In, stdio.Out, stdio.Err
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	err = cmd.Run()
	if exit := (*exec.ExitError)(nil); errors.As(err, &exit) {
		return platform.ExitCode(exit), nil
	}
	if err != nil {
		return 0, fmt.Errorf("launching %s: %w", bin, err)
	}
	return 0, nil
}

// writeSettings writes the private settings file: a fresh 0600 file named
// after this process, so a later run can tell an orphan from a live one.
func writeSettings(dir string, env map[string]string) (string, error) {
	b, err := json.MarshalIndent(map[string]any{"env": env}, "", "  ")
	if err != nil {
		return "", err
	}
	var nonce [6]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("%d-%s.json", os.Getpid(), hex.EncodeToString(nonce[:])))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	return path, f.Close()
}

var runFile = regexp.MustCompile(`^(\d+)-[0-9a-f]+\.json$`)

// collectStale removes private settings files whose owning muster process is
// gone — left behind by a crash or a kill. A live owner's file is kept however
// old it is: a session can run for days.
func collectStale(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		m := runFile.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		if pid, err := strconv.Atoi(m[1]); err == nil && !platform.ProcessAlive(pid) {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
