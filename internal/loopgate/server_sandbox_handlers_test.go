package loopgate

import (
	"fmt"
	"strings"
	"testing"

	"morph/internal/sandbox"
)

func TestRedactSandboxError_DoesNotExposeAbsolutePaths(t *testing.T) {
	t.Helper()
	wrapped := fmt.Errorf("detail /Users/example/leak: %w", sandbox.ErrSandboxPathOutsideRoot)
	out := redactSandboxError(wrapped)
	if strings.Contains(out, "/Users") || strings.Contains(out, "example") || strings.Contains(out, "leak") {
		t.Fatalf("redacted sandbox error leaked path detail: %q", out)
	}
	if out != sandbox.ErrSandboxPathOutsideRoot.Error() {
		t.Fatalf("expected stable sentinel message, got %q", out)
	}
}
