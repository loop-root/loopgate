package loopgate

import (
	"os"
	"runtime"
	"testing"
)

// TestMain relaxes the macOS-only gate for this package's tests when running on non-Darwin CI.
// The production binary (cmd/loopgate) does not use TestMain; it still requires macOS unless
// the operator explicitly exports LOOPGATE_ALLOW_NON_DARWIN in their environment.
func TestMain(m *testing.M) {
	if runtime.GOOS != "darwin" && os.Getenv("LOOPGATE_ALLOW_NON_DARWIN") == "" {
		_ = os.Setenv("LOOPGATE_ALLOW_NON_DARWIN", "1")
	}
	os.Exit(m.Run())
}
