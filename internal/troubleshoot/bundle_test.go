package troubleshoot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTailFileLines_KeepsLastN(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "log.txt")
	content := strings.Join([]string{"a", "b", "c", "d"}, "\n") + "\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := tailFileLines(p, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "c") || !strings.Contains(out, "d") {
		t.Fatalf("expected last two lines, got %q", out)
	}
	if strings.Contains(out, "a") {
		t.Fatalf("should not include first line, got %q", out)
	}
}
