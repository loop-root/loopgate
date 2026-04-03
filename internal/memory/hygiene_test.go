package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectUnsupportedRawMemoryArtifacts_AbsentPathReturnsNone(t *testing.T) {
	repoRoot := t.TempDir()

	rawMemoryArtifacts, err := InspectUnsupportedRawMemoryArtifacts(repoRoot)
	if err != nil {
		t.Fatalf("inspect unsupported raw memory artifacts: %v", err)
	}
	if len(rawMemoryArtifacts) != 0 {
		t.Fatalf("expected no raw memory artifacts, got %#v", rawMemoryArtifacts)
	}
}

func TestInspectUnsupportedRawMemoryArtifacts_DirectoryCountsFiles(t *testing.T) {
	repoRoot := t.TempDir()
	rawMemoryDir := filepath.Join(repoRoot, ".morph", "memory", "global")
	if err := os.MkdirAll(rawMemoryDir, 0o700); err != nil {
		t.Fatalf("mkdir raw memory dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".morph", "memory", "keys.json"), []byte("{\"keys\":[]}\n"), 0o600); err != nil {
		t.Fatalf("write keys file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rawMemoryDir, "user.profile.yaml"), []byte("name: Ada\n"), 0o600); err != nil {
		t.Fatalf("write profile file: %v", err)
	}

	rawMemoryArtifacts, err := InspectUnsupportedRawMemoryArtifacts(repoRoot)
	if err != nil {
		t.Fatalf("inspect unsupported raw memory artifacts: %v", err)
	}
	if len(rawMemoryArtifacts) != 1 {
		t.Fatalf("expected one raw memory artifact, got %#v", rawMemoryArtifacts)
	}
	if rawMemoryArtifacts[0].Kind != "directory" {
		t.Fatalf("expected directory artifact, got %#v", rawMemoryArtifacts[0])
	}
	if rawMemoryArtifacts[0].FileCount != 2 {
		t.Fatalf("expected file count 2, got %#v", rawMemoryArtifacts[0])
	}
	if !strings.Contains(FormatUnsupportedRawMemoryArtifactWarning(rawMemoryArtifacts[0]), ".morph/memory") {
		t.Fatalf("expected warning to mention raw memory path, got %q", FormatUnsupportedRawMemoryArtifactWarning(rawMemoryArtifacts[0]))
	}
}
