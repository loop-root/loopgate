package ledger

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppendRuntime_CachesAreIsolated(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "isolated.jsonl")

	runtimeOne := NewAppendRuntime()
	runtimeTwo := NewAppendRuntime()

	if err := runtimeOne.Append(path, Event{
		TS:      "2025-01-01T00:00:00Z",
		Type:    "test.event",
		Session: "test-session",
		Data:    map[string]interface{}{"index": 0},
	}); err != nil {
		t.Fatalf("append with runtime one: %v", err)
	}

	fileHandle, err := os.Open(path)
	if err != nil {
		t.Fatalf("open isolated ledger: %v", err)
	}
	defer fileHandle.Close()

	fileInfo, err := fileHandle.Stat()
	if err != nil {
		t.Fatalf("stat isolated ledger: %v", err)
	}
	fileState, err := ledgerFileStateFromFileInfo(fileInfo)
	if err != nil {
		t.Fatalf("load isolated ledger file state: %v", err)
	}

	normalizedPath := normalizeLedgerPath(path)
	if _, found := runtimeOne.loadCachedChainState(normalizedPath, "ledger_sequence", fileState); !found {
		t.Fatal("expected runtime one to own cached chain state after append")
	}
	if _, found := runtimeTwo.loadCachedChainState(normalizedPath, "ledger_sequence", fileState); found {
		t.Fatal("expected runtime two cache to remain empty for the same ledger path")
	}
}
