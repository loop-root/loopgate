package integration_test

import (
	"os"
	"runtime"
	"testing"
)

// TestMain mirrors internal/loopgate: allow non-Darwin hosts to run integration tests in CI.
func TestMain(m *testing.M) {
	if runtime.GOOS != "darwin" && os.Getenv("LOOPGATE_ALLOW_NON_DARWIN") == "" {
		_ = os.Setenv("LOOPGATE_ALLOW_NON_DARWIN", "1")
	}
	os.Exit(m.Run())
}
