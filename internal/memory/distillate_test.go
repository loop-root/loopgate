package memory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"morph/internal/audit"
	"morph/internal/ledger"
)

func TestDistillFromLedger_MalformedLineFailsWithIntegrityError(t *testing.T) {
	tempDir := t.TempDir()
	ledgerPath := filepath.Join(tempDir, "ledger.jsonl")
	distillatePath := filepath.Join(tempDir, "distillates.jsonl")

	if err := audit.RecordMustPersist(ledgerPath, ledger.Event{
		TS:      "2026-03-07T00:00:00Z",
		Type:    "tool.result",
		Session: "s-test",
		Data:    map[string]interface{}{"path": "ok.txt"},
	}); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}
	ledgerFileHandle, err := os.OpenFile(ledgerPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open ledger for tamper: %v", err)
	}
	if _, err := ledgerFileHandle.WriteString("{\"ts\":\"broken-json\"\n"); err != nil {
		_ = ledgerFileHandle.Close()
		t.Fatalf("append malformed ledger line: %v", err)
	}
	if err := ledgerFileHandle.Close(); err != nil {
		t.Fatalf("close tampered ledger: %v", err)
	}

	newCursor, err := DistillFromLedger(ledgerPath, distillatePath, 0, "test")
	if err == nil {
		t.Fatal("expected malformed ledger line to fail closed")
	}
	if !errors.Is(err, ErrLedgerIntegrity) {
		t.Fatalf("expected ErrLedgerIntegrity, got %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "malformed ledger line") {
		t.Fatalf("expected malformed-ledger integrity error, got %v", err)
	}
	if newCursor != 0 {
		t.Fatalf("expected cursor to remain unchanged on integrity failure, got %d", newCursor)
	}
	if _, statErr := os.Stat(distillatePath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no distillate output on integrity failure, got stat err %v", statErr)
	}
}
