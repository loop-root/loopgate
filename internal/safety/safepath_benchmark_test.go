package safety

import (
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkExplainSafePathExistingFile(b *testing.B) {
	repo := b.TempDir()
	targetPath := filepath.Join(repo, "body", "real.txt")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		b.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(targetPath, []byte("hello"), 0o644); err != nil {
		b.Fatalf("write: %v", err)
	}

	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		if _, err := ExplainSafePath(repo, []string{"."}, []string{"runtime/state"}, "body/real.txt"); err != nil {
			b.Fatalf("explain safe path: %v", err)
		}
	}
}
