package ledger

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
)

// AuditLedgerHMACCheckpointEventType is the audit JSONL type for keyed HMAC checkpoints.
// These lines extend the same append-only hash chain as other audit events; the HMAC binds
// (through_audit_sequence, through_event_hash, checkpoint_timestamp_utc) to a secret outside the log.
const AuditLedgerHMACCheckpointEventType = "audit.ledger.hmac_checkpoint"

// AuditLedgerCheckpointSchemaVersion is the only supported v1 payload shape for VerifyAuditLedgerHMACCheckpointEvent.
const AuditLedgerCheckpointSchemaVersion = 1

// BuildAuditLedgerCheckpointHMACMessageV1 returns the canonical octets signed by the checkpoint HMAC.
// throughEventHash may be empty when attesting the genesis-adjacent head.
func BuildAuditLedgerCheckpointHMACMessageV1(throughAuditSequence int64, throughEventHash, checkpointTimestampUTC string) []byte {
	return fmt.Appendf(nil, "loopgate-audit-ledger-checkpoint-v1\n%d\n%s\n%s\n",
		throughAuditSequence, throughEventHash, checkpointTimestampUTC)
}

// ComputeAuditLedgerCheckpointHMAC returns HMAC-SHA256(key, message).
func ComputeAuditLedgerCheckpointHMAC(key, message []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(message)
	return mac.Sum(nil)
}

// VerifyAuditLedgerHMACCheckpointEvent checks evt is a checkpoint and the embedded MAC matches key.
// evt must be the decoded JSON line (including top-level ts matching data.checkpoint_timestamp_utc).
func VerifyAuditLedgerHMACCheckpointEvent(evt Event, key []byte) error {
	if evt.Type != AuditLedgerHMACCheckpointEventType {
		return fmt.Errorf("event type is %q, want %q", evt.Type, AuditLedgerHMACCheckpointEventType)
	}
	if evt.Data == nil {
		return fmt.Errorf("checkpoint event missing data")
	}
	schema, err := int64FromEventData(evt.Data, "checkpoint_schema_version")
	if err != nil {
		return err
	}
	if schema != int64(AuditLedgerCheckpointSchemaVersion) {
		return fmt.Errorf("unsupported checkpoint_schema_version %d", schema)
	}
	throughSeq, err := int64FromEventData(evt.Data, "through_audit_sequence")
	if err != nil {
		return fmt.Errorf("through_audit_sequence: %w", err)
	}
	throughHash, ok := evt.Data["through_event_hash"].(string)
	if !ok {
		return fmt.Errorf("through_event_hash missing or not a string")
	}
	ts, ok := evt.Data["checkpoint_timestamp_utc"].(string)
	if !ok || ts == "" {
		return fmt.Errorf("checkpoint_timestamp_utc missing or empty")
	}
	if evt.TS != ts {
		return fmt.Errorf("checkpoint_timestamp_utc %q does not match event ts %q", ts, evt.TS)
	}
	storedHex, ok := evt.Data["checkpoint_hmac_sha256"].(string)
	if !ok || storedHex == "" {
		return fmt.Errorf("checkpoint_hmac_sha256 missing or empty")
	}
	storedMAC, err := hex.DecodeString(storedHex)
	if err != nil {
		return fmt.Errorf("checkpoint_hmac_sha256 hex: %w", err)
	}
	msg := BuildAuditLedgerCheckpointHMACMessageV1(throughSeq, throughHash, ts)
	expected := ComputeAuditLedgerCheckpointHMAC(key, msg)
	if !hmac.Equal(storedMAC, expected) {
		return fmt.Errorf("checkpoint_hmac_sha256 does not match recomputed MAC")
	}
	return nil
}

func int64FromEventData(data map[string]interface{}, key string) (int64, error) {
	raw, found := data[key]
	if !found {
		return 0, fmt.Errorf("missing %s", key)
	}
	switch typed := raw.(type) {
	case float64:
		return int64(typed), nil
	case int64:
		return typed, nil
	case int:
		return int64(typed), nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	default:
		return 0, fmt.Errorf("invalid %s type %T", key, raw)
	}
}
