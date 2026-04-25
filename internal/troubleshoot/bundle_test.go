package troubleshoot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"loopgate/internal/config"
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

func TestWriteOperatorBundle_RejectsAbsoluteDiagnosticDirectory(t *testing.T) {
	repoRoot := t.TempDir()
	outDir := filepath.Join(t.TempDir(), "bundle")
	runtimeConfig := config.DefaultRuntimeConfig()
	runtimeConfig.Logging.Diagnostic.Enabled = true
	runtimeConfig.Logging.Diagnostic.Directory = "/etc"

	err := WriteOperatorBundle(repoRoot, runtimeConfig, outDir, 10)
	if err == nil {
		t.Fatal("expected absolute diagnostic directory to be rejected")
	}
	if !strings.Contains(err.Error(), "must be repo-relative") {
		t.Fatalf("expected repo-relative diagnostic directory error, got %v", err)
	}
}

func TestWriteOperatorBundle_RejectsEscapingDiagnosticDirectory(t *testing.T) {
	repoRoot := t.TempDir()
	outDir := filepath.Join(t.TempDir(), "bundle")
	runtimeConfig := config.DefaultRuntimeConfig()
	runtimeConfig.Logging.Diagnostic.Enabled = true
	runtimeConfig.Logging.Diagnostic.Directory = "../outside"

	err := WriteOperatorBundle(repoRoot, runtimeConfig, outDir, 10)
	if err == nil {
		t.Fatal("expected escaping diagnostic directory to be rejected")
	}
	if !strings.Contains(err.Error(), "escapes repository root") {
		t.Fatalf("expected escaping diagnostic directory error, got %v", err)
	}
}
