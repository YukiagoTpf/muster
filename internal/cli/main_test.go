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
	os.Exit(m.Run())
}
