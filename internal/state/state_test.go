package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSave_CreatesPrivateStateDirectory(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "runtime", "state", "runtime_state.json")

	if err := Save(statePath, New()); err != nil {
		t.Fatalf("save state: %v", err)
	}

	stateDir := filepath.Dir(statePath)
	stateDirInfo, err := os.Stat(stateDir)
	if err != nil {
		t.Fatalf("stat state dir: %v", err)
	}
	if stateDirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("expected private state dir permissions, got %o", stateDirInfo.Mode().Perm())
	}
}
