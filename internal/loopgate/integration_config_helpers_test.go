package loopgate

import (
	"path/filepath"
	"runtime"
	"testing"
)

func testRepoRoot(t *testing.T) string {
	t.Helper()
	_, currentFilePath, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current test file path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFilePath), "..", ".."))
}
