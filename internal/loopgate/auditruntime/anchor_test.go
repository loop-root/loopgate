package auditruntime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"loopgate/internal/config"
	"loopgate/internal/ledger"
)

func TestAuditLedgerAnchorRoundTrip(t *testing.T) {
	anchorPath := filepath.Join(t.TempDir(), "audit_ledger_anchor.json")
	key := []byte("test-anchor-key")

	fileState := ledger.FileState{Size: 1024, Device: 1, Inode: 2, ChangeTimeSeconds: 3, ChangeTimeNanos: 4}
	if err := storeAuditLedgerAnchor(anchorPath, key, time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC), 7, "event-hash", 3, fileState); err != nil {
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

	fileState := ledger.FileState{Size: 1024, Device: 1, Inode: 2, ChangeTimeSeconds: 3, ChangeTimeNanos: 4}
	if err := storeAuditLedgerAnchor(anchorPath, key, time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC), 7, "event-hash", 3, fileState); err != nil {
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

func TestRuntimeLoadUsesAnchorFastPathWhenActiveFileStateMatches(t *testing.T) {
	tmpDir := t.TempDir()
	activePath := filepath.Join(tmpDir, "loopgate_events.jsonl")
	anchorPath := filepath.Join(tmpDir, "audit_ledger_anchor.json")
	key := []byte("test-anchor-key")

	if err := os.WriteFile(activePath, []byte("not a valid ledger line\n"), 0o600); err != nil {
		t.Fatalf("write active ledger: %v", err)
	}
	fileState, err := ledger.ReadFileState(activePath)
	if err != nil {
		t.Fatalf("read file state: %v", err)
	}
	if err := storeAuditLedgerAnchor(anchorPath, key, time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC), 9, "anchored-head", 4, fileState); err != nil {
		t.Fatalf("store anchor: %v", err)
	}

	runtime := New(Options{
		Path:       activePath,
		AnchorPath: anchorPath,
		HMACCheckpointConfig: func() config.AuditLedgerHMACCheckpoint {
			return config.AuditLedgerHMACCheckpoint{
				Enabled:        true,
				IntervalEvents: 10,
				SecretRef:      &config.AuditLedgerHMACSecretRef{ID: "test", Backend: "env", AccountName: "TEST", Scope: "test"},
			}
		},
		LoadCheckpointSecret: func(_ context.Context) ([]byte, error) {
			return append([]byte(nil), key...), nil
		},
	})

	if err := runtime.Load(context.Background(), ledger.RotationSettings{}); err != nil {
		t.Fatalf("load runtime from matching anchor: %v", err)
	}
	state := runtime.Snapshot()
	if state.Sequence != 9 {
		t.Fatalf("expected anchored sequence 9, got %d", state.Sequence)
	}
	if state.LastHash != "anchored-head" {
		t.Fatalf("expected anchored hash, got %q", state.LastHash)
	}
	if state.EventsSinceCheckpoint != 4 {
		t.Fatalf("expected anchored checkpoint cadence 4, got %d", state.EventsSinceCheckpoint)
	}
}
