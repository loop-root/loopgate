package troubleshoot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"loopgate/internal/config"
	"loopgate/internal/ledger"
	"loopgate/internal/secrets"
)

type auditIntegrityTestSecretStore struct {
	getSecret func(context.Context, secrets.SecretRef) ([]byte, secrets.SecretMetadata, error)
}

func (store auditIntegrityTestSecretStore) Put(context.Context, secrets.SecretRef, []byte) (secrets.SecretMetadata, error) {
	return secrets.SecretMetadata{}, fmt.Errorf("unexpected Put call in audit integrity test")
}

func (store auditIntegrityTestSecretStore) Get(ctx context.Context, validatedRef secrets.SecretRef) ([]byte, secrets.SecretMetadata, error) {
	return store.getSecret(ctx, validatedRef)
}

func (store auditIntegrityTestSecretStore) Delete(context.Context, secrets.SecretRef) error {
	return fmt.Errorf("unexpected Delete call in audit integrity test")
}

func (store auditIntegrityTestSecretStore) Metadata(context.Context, secrets.SecretRef) (secrets.SecretMetadata, error) {
	return secrets.SecretMetadata{}, fmt.Errorf("unexpected Metadata call in audit integrity test")
}

func TestVerifyAuditLedgerCheckpoints_Verified(t *testing.T) {
	repoRoot := t.TempDir()
	activeAuditPath := ActiveAuditPath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(activeAuditPath), 0o755); err != nil {
		t.Fatalf("mkdir runtime state: %v", err)
	}

	runtimeConfig := config.DefaultRuntimeConfig()
	runtimeConfig.Logging.AuditLedger.HMACCheckpoint.Enabled = true
	runtimeConfig.Logging.AuditLedger.HMACCheckpoint.IntervalEvents = 2
	runtimeConfig.Logging.AuditLedger.HMACCheckpoint.SecretRef = &config.AuditLedgerHMACSecretRef{
		ID:          "audit_ledger_hmac",
		Backend:     "env",
		AccountName: "LOOPGATE_AUDIT_LEDGER_HMAC",
		Scope:       "test",
	}
	t.Setenv("LOOPGATE_AUDIT_LEDGER_HMAC", "test-audit-hmac-key")

	appendAuditEventForCheckpointTest(t, activeAuditPath, "2026-04-15T00:00:01Z", "capability.requested", 1, map[string]interface{}{"capability": "fs_read"})
	lastAuditSequence, lastEventHash, err := ledger.ReadSegmentedChainState(activeAuditPath, "audit_sequence", AuditRotationSettings(repoRoot, runtimeConfig))
	if err != nil {
		t.Fatalf("read chain after first event: %v", err)
	}
	appendAuditCheckpointForCheckpointTest(t, activeAuditPath, "2026-04-15T00:00:02Z", 2, lastAuditSequence, lastEventHash, []byte("test-audit-hmac-key"))

	report, err := VerifyAuditLedgerCheckpoints(repoRoot, runtimeConfig)
	if err != nil {
		t.Fatalf("verify audit ledger checkpoints: %v", err)
	}
	if !report.OK || report.Status != "verified" {
		t.Fatalf("expected verified checkpoint report, got %#v", report)
	}
	if report.CheckpointCount != 1 {
		t.Fatalf("expected one checkpoint, got %#v", report)
	}
	if report.LastCheckpointThroughAuditSequence != 1 {
		t.Fatalf("expected through audit sequence 1, got %#v", report)
	}
}

func TestVerifyAuditLedgerCheckpoints_DefaultKeyBootstrapPendingWhenLedgerMissing(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfig := config.DefaultRuntimeConfig()
	runtimeConfig.Logging.AuditLedger.HMACCheckpoint = config.DefaultAuditLedgerHMACCheckpoint()
	originalNewSecretStoreForRef := newSecretStoreForRef
	newSecretStoreForRef = func(validatedRef secrets.SecretRef) (secrets.SecretStore, error) {
		return auditIntegrityTestSecretStore{
			getSecret: func(context.Context, secrets.SecretRef) ([]byte, secrets.SecretMetadata, error) {
				return nil, secrets.SecretMetadata{}, fmt.Errorf("%w: keychain item for secret ref %q", secrets.ErrSecretNotFound, validatedRef.ID)
			},
		}, nil
	}
	t.Cleanup(func() {
		newSecretStoreForRef = originalNewSecretStoreForRef
	})

	report, err := VerifyAuditLedgerCheckpoints(repoRoot, runtimeConfig)
	if err != nil {
		t.Fatalf("verify audit ledger checkpoints: %v", err)
	}
	if !report.OK {
		t.Fatalf("expected bootstrap-pending report to remain operator-readable, got %#v", report)
	}
	if report.Status != "bootstrap_pending" {
		t.Fatalf("expected bootstrap_pending status, got %#v", report)
	}
	if report.Error == "" {
		t.Fatalf("expected bootstrap-pending guidance, got %#v", report)
	}
}

func appendAuditEventForCheckpointTest(t *testing.T, activeAuditPath string, timestamp string, eventType string, auditSequence int64, data map[string]interface{}) {
	t.Helper()
	copied := map[string]interface{}{}
	for key, value := range data {
		copied[key] = value
	}
	copied["audit_sequence"] = auditSequence
	if err := ledger.Append(activeAuditPath, ledger.NewEvent(timestamp, eventType, "session-1", copied)); err != nil {
		t.Fatalf("append audit event: %v", err)
	}
}

func appendAuditCheckpointForCheckpointTest(t *testing.T, activeAuditPath string, timestamp string, auditSequence int64, throughAuditSequence int64, throughEventHash string, key []byte) {
	t.Helper()
	checkpointData := map[string]interface{}{
		"audit_sequence":            auditSequence,
		"checkpoint_schema_version": int64(ledger.AuditLedgerCheckpointSchemaVersion),
		"through_audit_sequence":    throughAuditSequence,
		"through_event_hash":        throughEventHash,
		"checkpoint_timestamp_utc":  timestamp,
		"checkpoint_hmac_sha256":    "",
	}
	checkpointMAC := ledger.ComputeAuditLedgerCheckpointHMAC(
		key,
		ledger.BuildAuditLedgerCheckpointHMACMessageV1(throughAuditSequence, throughEventHash, timestamp),
	)
	checkpointData["checkpoint_hmac_sha256"] = fmt.Sprintf("%x", checkpointMAC)
	if err := ledger.Append(activeAuditPath, ledger.NewEvent(timestamp, ledger.AuditLedgerHMACCheckpointEventType, "", checkpointData)); err != nil {
		t.Fatalf("append audit checkpoint: %v", err)
	}
}
