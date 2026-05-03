package auditruntime

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"loopgate/internal/ledger"
)

func TestAuditLedgerAnchorRoundTrip(t *testing.T) {
	anchorPath := filepath.Join(t.TempDir(), "audit_ledger_anchor.json")
	key := []byte("test-anchor-key")

	if err := storeAuditLedgerAnchor(anchorPath, key, time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC), 7, "event-hash", 3, 1024); err != nil {
		t.Fatalf("store anchor: %v", err)
	}

	anchor, found, err := loadAuditLedgerAnchor(anchorPath, key)
	if err != nil {
		t.Fatalf("load anchor: %v", err)
	}
	if !found {
		t.Fatal("expected anchor to be found")
	}
	if anchor.AuditSequence != 7 {
		t.Fatalf("expected audit sequence 7, got %d", anchor.AuditSequence)
	}
	if anchor.LastEventHash != "event-hash" {
		t.Fatalf("expected last event hash to round-trip, got %q", anchor.LastEventHash)
	}
	if anchor.EventsSinceCheckpoint != 3 {
		t.Fatalf("expected checkpoint cadence counter 3, got %d", anchor.EventsSinceCheckpoint)
	}
	if anchor.ActiveSizeBytes != 1024 {
		t.Fatalf("expected active size 1024, got %d", anchor.ActiveSizeBytes)
	}
}

func TestAuditLedgerAnchorRejectsTamperedPayload(t *testing.T) {
	anchorPath := filepath.Join(t.TempDir(), "audit_ledger_anchor.json")
	key := []byte("test-anchor-key")

	if err := storeAuditLedgerAnchor(anchorPath, key, time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC), 7, "event-hash", 3, 1024); err != nil {
		t.Fatalf("store anchor: %v", err)
	}

	anchorBytes, err := os.ReadFile(anchorPath)
	if err != nil {
		t.Fatalf("read anchor: %v", err)
	}
	var tampered map[string]interface{}
	if err := json.Unmarshal(anchorBytes, &tampered); err != nil {
		t.Fatalf("decode anchor: %v", err)
	}
	tampered["audit_sequence"] = float64(8)
	tamperedBytes, err := json.Marshal(tampered)
	if err != nil {
		t.Fatalf("encode tampered anchor: %v", err)
	}
	if err := os.WriteFile(anchorPath, append(tamperedBytes, '\n'), 0o600); err != nil {
		t.Fatalf("write tampered anchor: %v", err)
	}

	_, _, err = loadAuditLedgerAnchor(anchorPath, key)
	if !errors.Is(err, ledger.ErrLedgerIntegrity) {
		t.Fatalf("expected ledger integrity error, got %v", err)
	}
}
