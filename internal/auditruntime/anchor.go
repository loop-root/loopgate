package auditruntime

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"loopgate/internal/ledger"
)

const auditLedgerAnchorSchemaVersion = 1

type auditLedgerAnchor struct {
	V                     int              `json:"v"`
	UpdatedAt             string           `json:"updated_at"`
	AuditSequence         uint64           `json:"audit_sequence"`
	LastEventHash         string           `json:"last_event_hash"`
	EventsSinceCheckpoint int              `json:"events_since_checkpoint"`
	ActiveSizeBytes       int64            `json:"active_size_bytes"`
	ActiveFileState       ledger.FileState `json:"active_file_state"`
	HMACSHA256            string           `json:"hmac_sha256"`
}

func loadAuditLedgerAnchor(path string, key []byte) (auditLedgerAnchor, bool, error) {
	anchorBytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return auditLedgerAnchor{}, false, nil
		}
		return auditLedgerAnchor{}, false, fmt.Errorf("read audit ledger anchor: %w", err)
	}
	var anchor auditLedgerAnchor
	if err := json.Unmarshal(anchorBytes, &anchor); err != nil {
		return auditLedgerAnchor{}, true, fmt.Errorf("%w: malformed audit ledger anchor: %v", ledger.ErrLedgerIntegrity, err)
	}
	if err := verifyAuditLedgerAnchor(anchor, key); err != nil {
		return auditLedgerAnchor{}, true, err
	}
	return anchor, true, nil
}

func storeAuditLedgerAnchor(path string, key []byte, now time.Time, auditSequence uint64, lastEventHash string, eventsSinceCheckpoint int, activeFileState ledger.FileState) error {
	if path == "" {
		return nil
	}
	anchor := auditLedgerAnchor{
		V:                     auditLedgerAnchorSchemaVersion,
		UpdatedAt:             now.UTC().Format(time.RFC3339Nano),
		AuditSequence:         auditSequence,
		LastEventHash:         lastEventHash,
		EventsSinceCheckpoint: eventsSinceCheckpoint,
		ActiveSizeBytes:       activeFileState.Size,
		ActiveFileState:       activeFileState,
	}
	anchor.HMACSHA256 = computeAuditLedgerAnchorHMACHex(anchor, key)

	anchorBytes, err := json.MarshalIndent(anchor, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal audit ledger anchor: %w", err)
	}
	anchorBytes = append(anchorBytes, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create audit ledger anchor dir: %w", err)
	}
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, anchorBytes, 0o600); err != nil {
		return fmt.Errorf("write audit ledger anchor temp: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("replace audit ledger anchor: %w", err)
	}
	if err := syncParentDirectory(path); err != nil {
		return fmt.Errorf("sync audit ledger anchor dir: %w", err)
	}
	return nil
}

func verifyAuditLedgerAnchor(anchor auditLedgerAnchor, key []byte) error {
	if anchor.V != auditLedgerAnchorSchemaVersion {
		return fmt.Errorf("%w: unsupported audit ledger anchor version %d", ledger.ErrLedgerIntegrity, anchor.V)
	}
	if anchor.AuditSequence > 0 && anchor.LastEventHash == "" {
		return fmt.Errorf("%w: audit ledger anchor missing last_event_hash", ledger.ErrLedgerIntegrity)
	}
	if anchor.EventsSinceCheckpoint < 0 {
		return fmt.Errorf("%w: audit ledger anchor has negative events_since_checkpoint", ledger.ErrLedgerIntegrity)
	}
	if anchor.ActiveFileState.Size != anchor.ActiveSizeBytes {
		return fmt.Errorf("%w: audit ledger anchor active file size mismatch", ledger.ErrLedgerIntegrity)
	}
	storedMAC, err := hex.DecodeString(anchor.HMACSHA256)
	if err != nil {
		return fmt.Errorf("%w: audit ledger anchor hmac hex: %v", ledger.ErrLedgerIntegrity, err)
	}
	expectedMAC, err := hex.DecodeString(computeAuditLedgerAnchorHMACHex(anchor, key))
	if err != nil {
		return fmt.Errorf("decode expected audit ledger anchor hmac: %w", err)
	}
	if !hmac.Equal(storedMAC, expectedMAC) {
		return fmt.Errorf("%w: audit ledger anchor HMAC mismatch", ledger.ErrLedgerIntegrity)
	}
	return nil
}

func computeAuditLedgerAnchorHMACHex(anchor auditLedgerAnchor, key []byte) string {
	message := fmt.Appendf(nil, "loopgate-audit-ledger-anchor-v1\n%d\n%s\n%d\n%d\n%d\n%d\n%d\n%d\n%s\n",
		anchor.AuditSequence,
		anchor.LastEventHash,
		anchor.EventsSinceCheckpoint,
		anchor.ActiveFileState.Size,
		anchor.ActiveFileState.Device,
		anchor.ActiveFileState.Inode,
		anchor.ActiveFileState.ChangeTimeSeconds,
		anchor.ActiveFileState.ChangeTimeNanos,
		anchor.UpdatedAt,
	)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(message)
	return hex.EncodeToString(mac.Sum(nil))
}

func syncParentDirectory(path string) error {
	parentHandle, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer parentHandle.Close()
	return parentHandle.Sync()
}
