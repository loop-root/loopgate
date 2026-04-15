package loopgate

import (
	"os"
	"path/filepath"
	"testing"

	"loopgate/internal/config"
	"loopgate/internal/ledger"
)

func TestLogEvent_AppendsConfiguredAuditHMACCheckpoint(t *testing.T) {
	repoRoot := t.TempDir()
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))

	runtimeConfig := config.DefaultRuntimeConfig()
	runtimeConfig.Logging.AuditLedger.HMACCheckpoint.Enabled = true
	runtimeConfig.Logging.AuditLedger.HMACCheckpoint.IntervalEvents = 2
	runtimeConfig.Logging.AuditLedger.HMACCheckpoint.SecretRef = &config.AuditLedgerHMACSecretRef{
		ID:          "audit_ledger_hmac",
		Backend:     "env",
		AccountName: "LOOPGATE_AUDIT_LEDGER_HMAC",
		Scope:       "test",
	}
	if err := config.WriteRuntimeConfigYAML(repoRoot, runtimeConfig); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}
	t.Setenv("LOOPGATE_AUDIT_LEDGER_HMAC", "test-audit-hmac-key")

	server, err := NewServer(repoRoot, newShortLoopgateSocketPath(t))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	if err := server.logEvent("capability.requested", "", map[string]interface{}{"capability": "fs_read"}); err != nil {
		t.Fatalf("log first event: %v", err)
	}
	if err := server.logEvent("capability.executed", "", map[string]interface{}{"capability": "fs_read", "status": "allow"}); err != nil {
		t.Fatalf("log second event: %v", err)
	}

	lastAuditSequence, lastEventHash, err := ledger.ReadSegmentedChainState(server.auditPath, "audit_sequence", server.auditLedgerRotationSettings())
	if err != nil {
		t.Fatalf("read segmented chain state: %v", err)
	}
	if lastAuditSequence != 3 {
		t.Fatalf("expected 3 total audit events including checkpoint, got %d", lastAuditSequence)
	}
	if lastEventHash == "" {
		t.Fatal("expected non-empty last event hash")
	}

	orderedPaths, err := ledger.OrderedSegmentedPaths(server.auditPath, server.auditLedgerRotationSettings())
	if err != nil {
		t.Fatalf("ordered segmented paths: %v", err)
	}
	inspection, err := ledger.InspectAuditHMACCheckpoints(orderedPaths, []byte("test-audit-hmac-key"))
	if err != nil {
		t.Fatalf("inspect audit checkpoints: %v", err)
	}
	if inspection.CheckpointCount != 1 {
		t.Fatalf("expected one checkpoint, got %#v", inspection)
	}
	if inspection.LastCheckpointThroughAuditSequence != 2 {
		t.Fatalf("expected checkpoint through audit sequence 2, got %#v", inspection)
	}
	if inspection.OrdinaryEventsSinceLastCheckpoint != 0 {
		t.Fatalf("expected zero ordinary events after checkpoint, got %#v", inspection)
	}
}

func TestNewServer_RestoresAuditCheckpointCadenceFromLedger(t *testing.T) {
	repoRoot := t.TempDir()
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))

	runtimeConfig := config.DefaultRuntimeConfig()
	runtimeConfig.Logging.AuditLedger.HMACCheckpoint.Enabled = true
	runtimeConfig.Logging.AuditLedger.HMACCheckpoint.IntervalEvents = 2
	runtimeConfig.Logging.AuditLedger.HMACCheckpoint.SecretRef = &config.AuditLedgerHMACSecretRef{
		ID:          "audit_ledger_hmac",
		Backend:     "env",
		AccountName: "LOOPGATE_AUDIT_LEDGER_HMAC",
		Scope:       "test",
	}
	if err := config.WriteRuntimeConfigYAML(repoRoot, runtimeConfig); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}
	t.Setenv("LOOPGATE_AUDIT_LEDGER_HMAC", "test-audit-hmac-key")

	serverOne, err := NewServer(repoRoot, newShortLoopgateSocketPath(t))
	if err != nil {
		t.Fatalf("new first server: %v", err)
	}
	if err := serverOne.logEvent("capability.requested", "", map[string]interface{}{"capability": "fs_read"}); err != nil {
		t.Fatalf("log first event: %v", err)
	}
	if err := serverOne.logEvent("capability.executed", "", map[string]interface{}{"capability": "fs_read", "status": "allow"}); err != nil {
		t.Fatalf("log second event: %v", err)
	}
	if err := serverOne.logEvent("capability.requested", "", map[string]interface{}{"capability": "fs_list"}); err != nil {
		t.Fatalf("log third event: %v", err)
	}

	serverTwo, err := NewServer(repoRoot, newShortLoopgateSocketPath(t))
	if err != nil {
		t.Fatalf("new second server: %v", err)
	}
	if serverTwo.auditEventsSinceCheckpoint != 1 {
		t.Fatalf("expected one ordinary event since last checkpoint after reload, got %d", serverTwo.auditEventsSinceCheckpoint)
	}
	if err := serverTwo.logEvent("capability.executed", "", map[string]interface{}{"capability": "fs_list", "status": "allow"}); err != nil {
		t.Fatalf("log post-reload event: %v", err)
	}

	orderedPaths, err := ledger.OrderedSegmentedPaths(serverTwo.auditPath, serverTwo.auditLedgerRotationSettings())
	if err != nil {
		t.Fatalf("ordered segmented paths: %v", err)
	}
	inspection, err := ledger.InspectAuditHMACCheckpoints(orderedPaths, []byte("test-audit-hmac-key"))
	if err != nil {
		t.Fatalf("inspect audit checkpoints: %v", err)
	}
	if inspection.CheckpointCount != 2 {
		t.Fatalf("expected two checkpoints after reload, got %#v", inspection)
	}
	if inspection.LastCheckpointThroughAuditSequence != 5 {
		t.Fatalf("expected last checkpoint through audit sequence 5, got %#v", inspection)
	}

	activeAuditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read active audit: %v", err)
	}
	if len(activeAuditBytes) == 0 {
		t.Fatal("expected active audit bytes")
	}
}
