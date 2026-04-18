package integration_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"loopgate/internal/ledger"
	"loopgate/internal/loopgate"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"loopgate/internal/troubleshoot"
)

func TestAuditCrashRecoveryFailsClosedOnTruncatedActiveLedgerTail(t *testing.T) {
	harness := newLoopgateHarness(t, integrationPolicyYAML(true))
	status := harness.waitForStatus(t)

	client := harness.newClient("integration-actor", "integration-audit-crash-recovery", capabilityNames(status.Capabilities))
	t.Cleanup(client.CloseIdleConnections)

	executeResponse, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-audit-crash-recovery",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		t.Fatalf("execute capability before simulated crash: %v", err)
	}
	if executeResponse.Status != controlapipkg.ResponseStatusSuccess {
		t.Fatalf("expected successful capability execution before simulated crash, got %#v", executeResponse)
	}

	harness.stop(t)

	auditFile, err := os.OpenFile(harness.auditPath(), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open audit file for truncated tail append: %v", err)
	}
	if _, err := auditFile.WriteString("{\"v\":1,\"ts\":\"2026-04-17T12:00:00Z\""); err != nil {
		_ = auditFile.Close()
		t.Fatalf("append truncated audit tail: %v", err)
	}
	if err := auditFile.Close(); err != nil {
		t.Fatalf("close truncated audit tail file: %v", err)
	}

	runtimeConfig, err := troubleshoot.LoadEffectiveRuntimeConfig(harness.repoRoot)
	if err != nil {
		t.Fatalf("load effective runtime config: %v", err)
	}
	report, err := troubleshoot.BuildReport(harness.repoRoot, runtimeConfig)
	if err != nil {
		t.Fatalf("build crash-recovery report: %v", err)
	}
	if report.LedgerVerify.OK {
		t.Fatalf("expected ledger verification failure after truncated audit tail, got %#v", report.LedgerVerify)
	}
	if !strings.Contains(report.LedgerVerify.Error, ledger.ErrLedgerIntegrity.Error()) {
		t.Fatalf("expected ledger integrity error in report, got %q", report.LedgerVerify.Error)
	}
	if report.LedgerActive.SummaryError == "" {
		t.Fatalf("expected malformed active ledger summary error after truncated tail, got %#v", report.LedgerActive)
	}

	restartSocketPath := filepath.Join(t.TempDir(), "loopgate-restart.sock")
	_, err = loopgate.NewServer(harness.repoRoot, restartSocketPath)
	if !errors.Is(err, ledger.ErrLedgerIntegrity) {
		t.Fatalf("expected restart to fail closed with ErrLedgerIntegrity, got %v", err)
	}
}
