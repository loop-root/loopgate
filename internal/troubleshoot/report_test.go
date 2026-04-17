package troubleshoot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

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
	if rep.NonceReplay.Capacity != defaultNonceReplayCapacity {
		t.Fatalf("expected nonce replay capacity %d, got %#v", defaultNonceReplayCapacity, rep.NonceReplay)
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

func TestBuildNonceReplayReport_WarnsOnHighUtilization(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "runtime", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(stateDir, "nonce_replay.jsonl")
	nowUTC := time.Date(2026, time.April, 17, 12, 0, 0, 0, time.UTC)

	for index := 0; index < 4; index++ {
		appendNonceReplayLogRecordForReportTest(t, logPath, nonceReplayLogRecord{
			NonceKey:         "session:nonce-" + string(rune('a'+index)),
			ControlSessionID: "session",
			SeenAt:           nowUTC.Add(-time.Duration(index) * time.Minute).Format(time.RFC3339Nano),
		})
	}

	report := buildNonceReplayReport(dir, nowUTC, 4, 80, 10, 4)
	if report.Status != "warning" {
		t.Fatalf("expected warning status, got %#v", report)
	}
	if !containsString(report.Warnings, "active_entries_high_utilization") {
		t.Fatalf("expected high utilization warning, got %#v", report.Warnings)
	}
	if report.ActiveEntries != 4 || report.UtilizationPercent != 100 {
		t.Fatalf("expected saturated active entries, got %#v", report)
	}
}

func TestBuildNonceReplayReport_WarnsOnAppendOnlyLogGrowth(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "runtime", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(stateDir, "nonce_replay.jsonl")
	nowUTC := time.Date(2026, time.April, 17, 12, 0, 0, 0, time.UTC)

	for index := 0; index < 11; index++ {
		appendNonceReplayLogRecordForReportTest(t, logPath, nonceReplayLogRecord{
			NonceKey:         "session:nonce-stable",
			ControlSessionID: "session",
			SeenAt:           nowUTC.Add(-time.Duration(index) * time.Second).Format(time.RFC3339Nano),
		})
	}

	report := buildNonceReplayReport(dir, nowUTC, 100, 80, 10, 4)
	if report.Status != "warning" {
		t.Fatalf("expected warning status, got %#v", report)
	}
	if !containsString(report.Warnings, "append_only_log_growth_visible") {
		t.Fatalf("expected append-only log growth warning, got %#v", report.Warnings)
	}
	if report.ActiveEntries != 1 || report.PersistedLineCount != 11 {
		t.Fatalf("expected one active entry backed by eleven persisted lines, got %#v", report)
	}
}

func TestBuildNonceReplayReport_UsesLegacySnapshotFallback(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "runtime", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(stateDir, "nonce_replay.json")
	nowUTC := time.Date(2026, time.April, 17, 12, 0, 0, 0, time.UTC)

	rawSnapshot, err := json.Marshal(nonceReplaySnapshotFile{
		Nonces: map[string]nonceReplayPersistedNonce{
			"legacy:nonce": {
				ControlSessionID: "legacy",
				SeenAt:           nowUTC.Add(-5 * time.Minute).Format(time.RFC3339Nano),
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	if err := os.WriteFile(legacyPath, rawSnapshot, 0o600); err != nil {
		t.Fatalf("write legacy snapshot: %v", err)
	}

	report := buildNonceReplayReport(dir, nowUTC, 100, 80, 10, 4)
	if report.StoreKind != "legacy_snapshot" {
		t.Fatalf("expected legacy snapshot store kind, got %#v", report)
	}
	if !containsString(report.Warnings, "legacy_snapshot_fallback_active") {
		t.Fatalf("expected legacy snapshot warning, got %#v", report.Warnings)
	}
	if report.ActiveEntries != 1 {
		t.Fatalf("expected one active legacy nonce entry, got %#v", report)
	}
}

func appendNonceReplayLogRecordForReportTest(t *testing.T, path string, record nonceReplayLogRecord) {
	t.Helper()

	recordBytes, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal nonce replay record: %v", err)
	}
	recordBytes = append(recordBytes, '\n')
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open nonce replay log: %v", err)
	}
	defer file.Close()
	if _, err := file.Write(recordBytes); err != nil {
		t.Fatalf("append nonce replay record: %v", err)
	}
}

func containsString(items []string, wanted string) bool {
	for _, item := range items {
		if item == wanted {
			return true
		}
	}
	return false
}
