package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// run invokes Main with captured output and the given environment.
func run(t *testing.T, env map[string]string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	sys := IO{
		Out:    &out,
		Err:    &errb,
		Getenv: func(k string) string { return env[k] },
		Environ: func() []string {
			var kv []string
			for k, v := range env {
				kv = append(kv, k+"="+v)
			}
			return kv
		},
	}
	code = Main(args, sys)
	return code, out.String(), errb.String()
}

func TestHelpJSONListsEveryCommand(t *testing.T) {
	code, out, _ := run(t, nil, "help", "--json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var got struct {
		Version  int `json:"version"`
		Commands []struct {
			Name, Usage, Summary string
		} `json:"commands"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || len(got.Commands) != len(commands) {
		t.Fatalf("help --json = %+v", got)
	}
	for _, c := range got.Commands {
		if c.Name == "" || !strings.HasPrefix(c.Usage, "muster ") || c.Summary == "" {
			t.Errorf("incomplete command entry %+v", c)
		}
	}
}

func TestUnknownCommandExits2(t *testing.T) {
	code, _, stderr := run(t, nil, "nope")
	if code != 2 || !strings.Contains(stderr, `unknown command "nope"`) {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
}
