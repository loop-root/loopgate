package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"loopgate/internal/config"
	"loopgate/internal/ledger"
)

func TestFormatVerboseTailEvent_UsesHookProjectionFields(t *testing.T) {
	formattedLine := formatVerboseTailEvent(ledger.Event{
		TS:   "2026-04-15T00:00:40Z",
		Type: "hook.pre_validate",
		Data: map[string]interface{}{
			"decision":                 "block",
			"tool_name":                "Bash",
			"command_redacted_preview": `echo "hook test"`,
			"reason":                   "bash command does not match allowed prefix",
		},
	})

	expectedFragments := []string{
		"ts=2026-04-15T00:00:40Z",
		"BLOCK",
		`Bash: echo "hook test"`,
		"bash command does not match allowed prefix",
	}
	for _, expectedFragment := range expectedFragments {
		if !strings.Contains(formattedLine, expectedFragment) {
			t.Fatalf("expected formatted line %q to contain %q", formattedLine, expectedFragment)
		}
	}
}

func TestRunTailWithIO_VerbosePrintsReadableLines(t *testing.T) {
	repoRoot := t.TempDir()
	activeLedgerPath := activeAuditPath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(activeLedgerPath), 0o755); err != nil {
		t.Fatalf("mkdir runtime state: %v", err)
	}
	if err := ledger.Append(activeLedgerPath, ledger.NewEvent("2026-04-15T00:01:24Z", "hook.pre_validate", "session-1", map[string]interface{}{
		"decision":                 "allow",
		"tool_name":                "Bash",
		"command_redacted_preview": "tail -5 {repo}/runtime/state/...",
	})); err != nil {
		t.Fatalf("append ledger event: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runTailWithIO(repoRoot, 20, true, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected success, got exit code %d stderr=%s", exitCode, stderr.String())
	}

	output := stdout.String()
	expectedFragments := []string{
		"ts=2026-04-15T00:01:24Z",
		"ALLOW",
		"Bash: tail -5 {repo}/runtime/state/...",
	}
	for _, expectedFragment := range expectedFragments {
		if !strings.Contains(output, expectedFragment) {
			t.Fatalf("expected output %q to contain %q", output, expectedFragment)
		}
	}
	if strings.Contains(output, "type=") {
		t.Fatalf("expected verbose output to avoid raw key=value fallback, got %q", output)
	}
}

func TestFormatVerifySummary_IncludesCheckpointStatus(t *testing.T) {
	repoRoot := t.TempDir()
	activeLedgerPath := activeAuditPath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(activeLedgerPath), 0o755); err != nil {
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
	if err := config.WriteRuntimeConfigYAML(repoRoot, runtimeConfig); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}
	t.Setenv("LOOPGATE_AUDIT_LEDGER_HMAC", "test-audit-hmac-key")

	if err := ledger.Append(activeLedgerPath, ledger.NewEvent("2026-04-15T00:00:01Z", "capability.requested", "session-1", map[string]interface{}{
		"audit_sequence": 1,
		"capability":     "fs_read",
	})); err != nil {
		t.Fatalf("append audit event: %v", err)
	}
	lastAuditSequence, lastEventHash, err := ledger.ReadSegmentedChainState(activeLedgerPath, "audit_sequence", loadRotationSettings(repoRoot))
	if err != nil {
		t.Fatalf("read chain state: %v", err)
	}
	checkpointMAC := ledger.ComputeAuditLedgerCheckpointHMAC(
		[]byte("test-audit-hmac-key"),
		ledger.BuildAuditLedgerCheckpointHMACMessageV1(lastAuditSequence, lastEventHash, "2026-04-15T00:00:02Z"),
	)
	if err := ledger.Append(activeLedgerPath, ledger.NewEvent("2026-04-15T00:00:02Z", ledger.AuditLedgerHMACCheckpointEventType, "", map[string]interface{}{
		"audit_sequence":            2,
		"checkpoint_schema_version": int64(ledger.AuditLedgerCheckpointSchemaVersion),
		"through_audit_sequence":    lastAuditSequence,
		"through_event_hash":        lastEventHash,
		"checkpoint_timestamp_utc":  "2026-04-15T00:00:02Z",
		"checkpoint_hmac_sha256":    fmt.Sprintf("%x", checkpointMAC),
	})); err != nil {
		t.Fatalf("append checkpoint event: %v", err)
	}

	formatted := formatVerifySummary(lastAuditSequence, lastEventHash, "verified", 1)
	expectedFragments := []string{
		"verify ok",
		"last_audit_sequence=1",
		"hmac_checkpoints=verified",
		"checkpoint_count=1",
	}
	for _, expectedFragment := range expectedFragments {
		if !strings.Contains(formatted, expectedFragment) {
			t.Fatalf("expected formatted summary %q to contain %q", formatted, expectedFragment)
		}
	}
}

func TestRunDemoResetWithIO_RequiresYes(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runDemoResetWithIO(t.TempDir(), "", false, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "-yes") {
		t.Fatalf("expected confirmation hint, got stderr=%q", stderr.String())
	}
}

func TestRunDemoResetWithIO_RemovesLocalDemoState(t *testing.T) {
	repoRoot := t.TempDir()
	activeLedgerPath := activeAuditPath(repoRoot)
	diagnosticDir := filepath.Join(repoRoot, "runtime", "logs")
	if err := os.MkdirAll(filepath.Dir(activeLedgerPath), 0o755); err != nil {
		t.Fatalf("mkdir runtime state: %v", err)
	}
	if err := os.MkdirAll(diagnosticDir, 0o755); err != nil {
		t.Fatalf("mkdir diagnostic dir: %v", err)
	}
	if err := os.WriteFile(activeLedgerPath, []byte("test\n"), 0o600); err != nil {
		t.Fatalf("write ledger: %v", err)
	}
	if err := os.WriteFile(filepath.Join(diagnosticDir, "audit.log"), []byte("demo\n"), 0o600); err != nil {
		t.Fatalf("write audit log: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runDemoResetWithIO(repoRoot, filepath.Join(repoRoot, "runtime", "state", "loopgate.sock"), true, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected success, got exit code %d stderr=%s", exitCode, stderr.String())
	}
	if _, err := os.Stat(activeLedgerPath); !os.IsNotExist(err) {
		t.Fatalf("expected active ledger removed, stat err=%v", err)
	}
	if _, err := os.Stat(diagnosticDir); !os.IsNotExist(err) {
		t.Fatalf("expected diagnostic dir removed, stat err=%v", err)
	}
	if !strings.Contains(stdout.String(), "demo reset complete") {
		t.Fatalf("expected success output, got %q", stdout.String())
	}
}

func TestRunDemoResetWithIO_RefusesWhenLoopgateIsRunning(t *testing.T) {
	repoRoot := t.TempDir()
	socketPath := filepath.Join(repoRoot, "runtime", "state", "loopgate.sock")
	previousEnsureLoopgateStopped := ensureLoopgateStoppedForDemoReset
	ensureLoopgateStoppedForDemoReset = func(actualSocketPath string) error {
		if actualSocketPath != socketPath {
			t.Fatalf("expected demo-reset to probe socket %q, got %q", socketPath, actualSocketPath)
		}
		return fmt.Errorf("refusing demo reset while Loopgate is running at %s", actualSocketPath)
	}
	defer func() {
		ensureLoopgateStoppedForDemoReset = previousEnsureLoopgateStopped
	}()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runDemoResetWithIO(repoRoot, socketPath, true, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("expected refusal exit code 1, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "refusing demo reset while Loopgate is running") {
		t.Fatalf("expected running refusal, got stderr=%q", stderr.String())
	}
}
