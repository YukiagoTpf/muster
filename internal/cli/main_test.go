package cli

import (
	"os"
	"testing"

	"github.com/YukiagoTpf/muster/internal/fakeagent"
)

// TestMain doubles as a fake agent (see fakeagent), so `muster run` can be
// tested end to end with MUSTER_CLAUDE_BIN pointing at this test binary.
func TestMain(m *testing.M) {
	fakeagent.Main()
	// Background refreshes would re-execute the test binary; tests that care
	// swap in a recorder.
	spawnBackground = func(IO, ...string) error { return nil }
	os.Exit(m.Run())
}
