package troubleshoot

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"loopgate/internal/config"
	"loopgate/internal/ledger"
)

func testRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func TestBuildReport_LoopgateWorkspace(t *testing.T) {
	root := testRepoRoot(t)
	rc, err := LoadEffectiveRuntimeConfig(root)
	if err != nil {
		t.Fatalf("load effective runtime: %v", err)
	}
	rc.Logging.AuditLedger.HMACCheckpoint.Enabled = false
	rep, err := BuildReport(root, rc)
	if err != nil {
		t.Fatalf("build report: %v", err)
	}
	if got := filepath.Base(rep.LedgerActive.ActiveFile); got != "loopgate_events.jsonl" {
		t.Fatalf("active file basename: %q", got)
	}
	if rep.LedgerActive.LineCount < 0 {
		t.Fatal("line count")
	}
	if rep.Diagnostics.Enabled != rc.Logging.Diagnostic.Enabled {
		t.Fatalf("diagnostic enabled mismatch")
	}
}

func TestBuildReport_ActiveSummaryErrorOnMalformedLine(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "runtime", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(stateDir, "loopgate_events.jsonl")
	if err := os.WriteFile(active, []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rc := config.DefaultRuntimeConfig()
	rep, err := BuildReport(dir, rc)
	if err != nil {
		t.Fatalf("unexpected build error: %v", err)
	}
	if rep.LedgerActive.SummaryError == "" {
		t.Fatal("expected summary_error for malformed ledger line")
	}
}

func TestBuildReport_IncludesHMACCheckpointStatus(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "runtime", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	activeAuditPath := filepath.Join(stateDir, "loopgate_events.jsonl")

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
	lastAuditSequence, lastEventHash, err := ledger.ReadSegmentedChainState(activeAuditPath, "audit_sequence", AuditRotationSettings(dir, runtimeConfig))
	if err != nil {
		t.Fatalf("read chain after first event: %v", err)
	}
	appendAuditCheckpointForCheckpointTest(t, activeAuditPath, "2026-04-15T00:00:02Z", 2, lastAuditSequence, lastEventHash, []byte("test-audit-hmac-key"))

	report, err := BuildReport(dir, runtimeConfig)
	if err != nil {
		t.Fatalf("build report: %v", err)
	}
	if !report.LedgerVerify.HMACCheckpoints.OK {
		t.Fatalf("expected verified checkpoint report, got %#v", report.LedgerVerify.HMACCheckpoints)
	}
	if report.LedgerVerify.HMACCheckpoints.Status != "verified" {
		t.Fatalf("expected verified checkpoint status, got %#v", report.LedgerVerify.HMACCheckpoints)
	}
	if report.LedgerVerify.HMACCheckpoints.CheckpointCount != 1 {
		t.Fatalf("expected one checkpoint, got %#v", report.LedgerVerify.HMACCheckpoints)
	}
}
