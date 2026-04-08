package ledger

import (
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestVerifyAuditLedgerHMACCheckpointEvent_roundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	throughSeq := int64(3)
	throughHash := "abc123"
	ts := "2026-04-08T12:00:00.123456789Z"
	msg := BuildAuditLedgerCheckpointHMACMessageV1(throughSeq, throughHash, ts)
	mac := ComputeAuditLedgerCheckpointHMAC(key, msg)

	evt := Event{
		V:       SchemaVersion,
		TS:      ts,
		Type:    AuditLedgerHMACCheckpointEventType,
		Session: "",
		Data: map[string]interface{}{
			"checkpoint_schema_version": float64(AuditLedgerCheckpointSchemaVersion),
			"through_audit_sequence":    float64(throughSeq),
			"through_event_hash":        throughHash,
			"checkpoint_timestamp_utc":  ts,
			"checkpoint_hmac_sha256":    hex.EncodeToString(mac),
			"audit_sequence":            float64(4),
			"ledger_sequence":           float64(4),
			"previous_event_hash":       "prev",
			"event_hash":                "placeholder",
		},
	}
	if err := VerifyAuditLedgerHMACCheckpointEvent(evt, key); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestVerifyAuditLedgerHMACCheckpointEvent_wrongKey(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = 7
	}
	wrongKey := make([]byte, 32)
	for i := range wrongKey {
		wrongKey[i] = 8
	}
	ts := "2026-04-08T12:00:00Z"
	msg := BuildAuditLedgerCheckpointHMACMessageV1(1, "h", ts)
	mac := ComputeAuditLedgerCheckpointHMAC(key, msg)
	evt := Event{
		V:    SchemaVersion,
		TS:   ts,
		Type: AuditLedgerHMACCheckpointEventType,
		Data: map[string]interface{}{
			"checkpoint_schema_version": float64(1),
			"through_audit_sequence":    float64(1),
			"through_event_hash":        "h",
			"checkpoint_timestamp_utc":  ts,
			"checkpoint_hmac_sha256":    hex.EncodeToString(mac),
		},
	}
	if err := VerifyAuditLedgerHMACCheckpointEvent(evt, wrongKey); err == nil {
		t.Fatal("expected verification failure with wrong key")
	}
}

func TestVerifyAuditLedgerHMACCheckpointEvent_tsMismatch(t *testing.T) {
	key := make([]byte, 32)
	ts := "2026-04-08T12:00:00Z"
	msg := BuildAuditLedgerCheckpointHMACMessageV1(1, "", ts)
	mac := ComputeAuditLedgerCheckpointHMAC(key, msg)
	evt := Event{
		V:    SchemaVersion,
		TS:   "2026-04-08T12:00:01Z",
		Type: AuditLedgerHMACCheckpointEventType,
		Data: map[string]interface{}{
			"checkpoint_schema_version": float64(1),
			"through_audit_sequence":    float64(1),
			"through_event_hash":        "",
			"checkpoint_timestamp_utc":  ts,
			"checkpoint_hmac_sha256":    hex.EncodeToString(mac),
		},
	}
	if err := VerifyAuditLedgerHMACCheckpointEvent(evt, key); err == nil {
		t.Fatal("expected failure when event TS != checkpoint_timestamp_utc")
	}
}

// Ensure JSON decode of a line yields float64 for numbers (matches ParseEvent).
func TestVerifyAuditLedgerHMACCheckpointEvent_afterJSONUnmarshal(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = 3
	}
	line := `{"v":1,"ts":"2026-04-08T12:00:00Z","type":"audit.ledger.hmac_checkpoint","session":"","data":{"checkpoint_schema_version":1,"through_audit_sequence":2,"through_event_hash":"deadbeef","checkpoint_timestamp_utc":"2026-04-08T12:00:00Z","checkpoint_hmac_sha256":"` +
		hex.EncodeToString(ComputeAuditLedgerCheckpointHMAC(key, BuildAuditLedgerCheckpointHMACMessageV1(2, "deadbeef", "2026-04-08T12:00:00Z"))) + `"}}`
	var evt Event
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := VerifyAuditLedgerHMACCheckpointEvent(evt, key); err != nil {
		t.Fatalf("verify after json: %v", err)
	}
}
