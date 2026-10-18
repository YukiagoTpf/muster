package cli

import (
	"fmt"
	"strings"

	"github.com/YukiagoTpf/muster/internal/claude"
	"github.com/YukiagoTpf/muster/internal/credential"
	"github.com/YukiagoTpf/muster/internal/launch"
	"github.com/YukiagoTpf/muster/internal/store"
)

// settingsPlaceholder stands for the private settings file in --dry-run
// output: the real file exists only while the agent runs.
const settingsPlaceholder = "<private settings file>"

func runRun(sys IO, args []string) int {
	var passthrough []string
	for i, a := range args {
		if a == "--" {
			args, passthrough = args[:i], args[i+1:]
			break
		}
	}
	var (
		model          string
		dryRun, asJSON bool
	)
	pos, ok := parseArgs(sys, "run", args, 1, func(f *flagSet) {
		f.StringVar(&model, "model", "", "model alias (opus, sonnet, …) or model id; default: the agent's")
		f.StringVar(&model, "m", "", "shorthand for --model")
		f.BoolVar(&dryRun, "dry-run", false, "show what the launch would do (key names only), launch nothing")
		f.BoolVar(&asJSON, "json", false, "with --dry-run: machine-readable output")
	})
	if !ok {
		return 2
	}
	s, plan, err := planLaunch(sys, pos[0], model, passthrough)
	if err != nil {
		return fail(sys, err)
	}
	if dryRun {
		return printPlan(sys, plan, asJSON)
	}
	return startLaunch(sys, s, plan)
}

// planLaunch decides a launch of credential name.
func planLaunch(sys IO, name, model string, passthrough []string) (*store.Store, launch.Plan, error) {
	s, err := store.Open(sys.Getenv)
	if err != nil {
		return nil, launch.Plan{}, err
	}
	r, err := credential.Load(s)
	if err != nil {
		return nil, launch.Plan{}, err
	}
	c, ok := r.Get(name)
	if !ok {
		return nil, launch.Plan{}, fmt.Errorf("no credential named %q (see `muster cred list`)", name)
	}
	key, ok := r.Secret(name)
	if !ok {
		return nil, launch.Plan{}, fmt.Errorf("credential %q has no key stored; remove it and add it again", name)
	}
	settings, err := claude.SettingsPath(sys.Getenv)
	if err != nil {
		return nil, launch.Plan{}, err
	}
	defaults, err := claude.ReadProviderEnv(settings)
	if err != nil {
		return nil, launch.Plan{}, err
	}
	return s, launch.Build(name, c.ProviderEnv(key), defaults, sys.Environ(), model, passthrough), nil
}

// startLaunch runs a decided launch in the foreground and remembers it for
// the picker.
func startLaunch(sys IO, s *store.Store, plan launch.Plan) int {
	bin, err := launch.AgentBin(sys.Getenv)
	if err != nil {
		return fail(sys, err)
	}
	if err := launch.Remember(s, plan.Credential, plan.Model); err != nil {
		return fail(sys, err)
	}
	code, err := launch.Run(s, plan, bin, launch.Stdio{In: sys.In, Out: sys.Out, Err: sys.Err})
	if err != nil {
		return fail(sys, err)
	}
	return code
}

func printPlan(sys IO, plan launch.Plan, asJSON bool) int {
	command := append([]string{"claude"}, plan.Args(settingsPlaceholder)...)
	if asJSON {
		return writeJSON(sys, struct {
			Version         int      `json:"version"`
			Credential      string   `json:"credential"`
			Model           string   `json:"model"`
			Command         []string `json:"command"`
			SettingsEnvKeys []string `json:"settingsEnvKeys"`
			BlankedKeys     []string `json:"blankedKeys"`
			StrippedEnvKeys []string `json:"strippedEnvKeys"`
		}{1, plan.Credential, plan.Model, command, plan.SettingsKeys(), nonNil(plan.Blanked), nonNil(plan.Stripped)})
	}
	fmt.Fprintf(sys.Out, "would run: %s\n", strings.Join(command, " "))
	fmt.Fprintf(sys.Out, "  sets:   %s\n", strings.Join(plan.SettingsKeys(), " "))
	if len(plan.Blanked) > 0 {
		fmt.Fprintf(sys.Out, "  blanks: %s  (default credential keys this credential does not set)\n", strings.Join(plan.Blanked, " "))
	}
	if len(plan.Stripped) > 0 {
		fmt.Fprintf(sys.Out, "  strips: %s  (inherited from this shell)\n", strings.Join(plan.Stripped, " "))
	}
	return 0
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
