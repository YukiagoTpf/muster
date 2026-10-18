package cli

import (
	"fmt"
	"strings"

	"github.com/YukiagoTpf/muster/internal/claude"
	"github.com/YukiagoTpf/muster/internal/credential"
	"github.com/YukiagoTpf/muster/internal/store"
)

func runDefault(sys IO, args []string) int {
	npos := 0
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		npos = 1
	}
	var asJSON bool
	pos, ok := parseArgs(sys, "default", args, npos, func(f *flagSet) { f.BoolVar(&asJSON, "json", false, "machine-readable output") })
	if !ok {
		return 2
	}
	path, err := claude.SettingsPath(sys.Getenv)
	if err != nil {
		return fail(sys, err)
	}
	if npos == 1 {
		return setDefault(sys, path, pos[0], asJSON)
	}
	return showDefault(sys, path, asJSON)
}

func showDefault(sys IO, path string, asJSON bool) int {
	r, err := loadRegistry(sys)
	if err != nil {
		return fail(sys, err)
	}
	env, err := claude.ReadProviderEnv(path)
	if err != nil {
		return fail(sys, err)
	}
	st := r.ClassifyDefault(env)
	if asJSON {
		return writeJSON(sys, struct {
			Version  int    `json:"version"`
			Settings string `json:"settings"`
			credential.DefaultState
		}{1, path, st})
	}
	switch st.State {
	case "match":
		fmt.Fprintf(sys.Out, "default credential: %s\n", st.Credential)
	case "drift":
		fmt.Fprintf(sys.Out, "default credential: %s, but %s differs in %s — `muster default %s` resets it\n",
			st.Credential, path, strings.Join(st.DifferentKeys, " "), st.Credential)
	case "unregistered":
		fmt.Fprintf(sys.Out, "%s sets a provider no credential matches (%s) — register it with `muster cred add <name> --provider <p> --from-settings`\n", path, st.BaseURL)
	default:
		fmt.Fprintf(sys.Out, "%s sets no provider: Claude Code uses its own login\n", path)
	}
	return 0
}

// setDefault makes a credential the default (see credential.MakeDefault).
func setDefault(sys IO, path, name string, asJSON bool) int {
	s, err := store.Open(sys.Getenv)
	if err != nil {
		return fail(sys, err)
	}
	backup, err := credential.MakeDefault(s, path, name)
	if err != nil {
		return fail(sys, err)
	}
	if asJSON {
		return writeJSON(sys, struct {
			Version    int    `json:"version"`
			Credential string `json:"credential"`
			Settings   string `json:"settings"`
			Backup     string `json:"backup,omitempty"`
		}{1, name, path, backup})
	}
	fmt.Fprintf(sys.Out, "default credential is now %s (%s)\n", name, path)
	if backup != "" {
		fmt.Fprintf(sys.Out, "previous file backed up to %s\n", backup)
	}
	return 0
}
