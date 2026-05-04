package loopgate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewServerWithOptions_SeedsAuditExportStateWhenEnabled(t *testing.T) {
	repoRoot := t.TempDir()

	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir runtime config dir: %v", err)
	}
	rawRuntimeConfig := `version: "1"
logging:
  audit_export:
    enabled: true
    destination_kind: "admin_node"
    destination_label: "corp-admin"
    endpoint_url: "http://127.0.0.1:18080/v1/admin/audit/ingest"
    authorization:
      secret_ref:
        id: "audit_export_admin_bearer"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_TOKEN"
        scope: "test"`
	if err := os.WriteFile(runtimeConfigPath, []byte(rawRuntimeConfig), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	socketFile, err := os.CreateTemp("", "loopgate-*.sock")
	if err != nil {
		t.Fatalf("create temp socket file: %v", err)
	}
	socketPath := socketFile.Name()
	_ = socketFile.Close()
	_ = os.Remove(socketPath)
	t.Cleanup(func() { _ = os.Remove(socketPath) })

	server, err := NewServerWithOptions(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("NewServerWithOptions: %v", err)
	}
	defer server.CloseDiagnosticLogs()

	exportStatePath := filepath.Join(repoRoot, "runtime", "state", "audit_export_state.json")
	exportStateBytes, err := os.ReadFile(exportStatePath)
	if err != nil {
		t.Fatalf("read audit export state: %v", err)
	}
	exportStateText := string(exportStateBytes)
	if !strings.Contains(exportStateText, `"destination_kind": "admin_node"`) {
		t.Fatalf("expected admin_node destination kind in export state, got %s", exportStateText)
	}
	if !strings.Contains(exportStateText, `"destination_label": "corp-admin"`) {
		t.Fatalf("expected corp-admin destination label in export state, got %s", exportStateText)
	}
}

func TestPrepareNextAuditExportBatch_ReadsAcrossSegmentsAndActiveLedger(t *testing.T) {
	repoRoot := t.TempDir()

	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir runtime config dir: %v", err)
	}
	rawRuntimeConfig := `version: "1"
logging:
  audit_ledger:
    max_event_bytes: 8192
    rotate_at_bytes: 550
    segment_dir: "runtime/state/loopgate_event_segments"
    manifest_path: "runtime/state/loopgate_event_segments/manifest.jsonl"
    verify_closed_segments_on_startup: true
  audit_export:
    enabled: true
    destination_kind: "admin_node"
    destination_label: "corp-admin"
    endpoint_url: "http://127.0.0.1:18080/v1/admin/audit/ingest"
    authorization:
      secret_ref:
        id: "audit_export_admin_bearer"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_TOKEN"
        scope: "test"
    max_batch_events: 50
    max_batch_bytes: 1048576
    min_flush_interval_seconds: 5`
	if err := os.WriteFile(runtimeConfigPath, []byte(rawRuntimeConfig), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	socketFile, err := os.CreateTemp("", "loopgate-*.sock")
	if err != nil {
		t.Fatalf("create temp socket file: %v", err)
	}
	socketPath := socketFile.Name()
	_ = socketFile.Close()
	_ = os.Remove(socketPath)
	t.Cleanup(func() { _ = os.Remove(socketPath) })

	server, err := NewServerWithOptions(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("NewServerWithOptions: %v", err)
	}
	defer server.CloseDiagnosticLogs()

	for eventIndex := 0; eventIndex < 5; eventIndex++ {
		if err := server.logEvent("test.audit", "session-a", map[string]interface{}{
			"payload": strings.Repeat(string(rune('a'+eventIndex)), 140),
		}); err != nil {
			t.Fatalf("log event %d: %v", eventIndex, err)
		}
	}

	exportBatch, err := server.prepareNextAuditExportBatch()
	if err != nil {
		t.Fatalf("prepare next audit export batch: %v", err)
	}
	if exportBatch.EventCount != 5 {
		t.Fatalf("expected 5 events in export batch, got %#v", exportBatch)
	}
	if exportBatch.FromAuditSequence != 1 {
		t.Fatalf("expected export batch to start at audit sequence 1, got %#v", exportBatch)
	}
	if exportBatch.ThroughAuditSequence != 5 {
		t.Fatalf("expected export batch through audit sequence 5, got %#v", exportBatch)
	}
	if strings.TrimSpace(exportBatch.ThroughEventHash) == "" {
		t.Fatalf("expected non-empty through event hash, got %#v", exportBatch)
	}

	manifestPath := filepath.Join(repoRoot, "runtime", "state", "loopgate_event_segments", "manifest.jsonl")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("expected rotated manifest to exist, stat err=%v", err)
	}
}

func TestPrepareNextAuditExportBatch_AdvancesAfterMarkedSuccess(t *testing.T) {
	repoRoot := t.TempDir()

	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir runtime config dir: %v", err)
	}
	rawRuntimeConfig := `version: "1"
logging:
  audit_export:
    enabled: true
    destination_kind: "admin_node"
    destination_label: "corp-admin"
    endpoint_url: "http://127.0.0.1:18080/v1/admin/audit/ingest"
    authorization:
      secret_ref:
        id: "audit_export_admin_bearer"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_TOKEN"
        scope: "test"
    max_batch_events: 2
    max_batch_bytes: 1048576
    min_flush_interval_seconds: 5`
	if err := os.WriteFile(runtimeConfigPath, []byte(rawRuntimeConfig), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	socketFile, err := os.CreateTemp("", "loopgate-*.sock")
	if err != nil {
		t.Fatalf("create temp socket file: %v", err)
	}
	socketPath := socketFile.Name()
	_ = socketFile.Close()
	_ = os.Remove(socketPath)
	t.Cleanup(func() { _ = os.Remove(socketPath) })

	server, err := NewServerWithOptions(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("NewServerWithOptions: %v", err)
	}
	defer server.CloseDiagnosticLogs()

	for eventIndex := 0; eventIndex < 3; eventIndex++ {
		if err := server.logEvent("test.audit", "session-a", map[string]interface{}{"step": eventIndex}); err != nil {
			t.Fatalf("log event %d: %v", eventIndex, err)
		}
	}

	firstBatch, err := server.prepareNextAuditExportBatch()
	if err != nil {
		t.Fatalf("prepare first audit export batch: %v", err)
	}
	if firstBatch.EventCount != 2 {
		t.Fatalf("expected first batch size 2, got %#v", firstBatch)
	}
	if err := server.markAuditExportSuccess(firstBatch.ThroughAuditSequence, firstBatch.ThroughEventHash); err != nil {
		t.Fatalf("mark audit export success: %v", err)
	}

	secondBatch, err := server.prepareNextAuditExportBatch()
	if err != nil {
		t.Fatalf("prepare second audit export batch: %v", err)
	}
	if secondBatch.EventCount != 1 {
		t.Fatalf("expected second batch size 1, got %#v", secondBatch)
	}
	if secondBatch.FromAuditSequence != 3 {
		t.Fatalf("expected second batch to start from audit sequence 3, got %#v", secondBatch)
	}
	if secondBatch.ThroughAuditSequence != 3 {
		t.Fatalf("expected second batch through audit sequence 3, got %#v", secondBatch)
	}
}

func TestBuildAdminNodeAuditIngestRequest_IncludesSourceAndBatchMetadata(t *testing.T) {
	repoRoot := t.TempDir()

	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir runtime config dir: %v", err)
	}
	rawRuntimeConfig := `version: "1"
tenancy:
  deployment_tenant_id: "tenant-acme"
  deployment_user_id: "user-ada"
logging:
  audit_export:
    enabled: true
    destination_kind: "admin_node"
    destination_label: "corp-admin"
    endpoint_url: "http://127.0.0.1:18080/v1/admin/audit/ingest"
    authorization:
      secret_ref:
        id: "audit_export_admin_bearer"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_TOKEN"
        scope: "test"
    max_batch_events: 50
    max_batch_bytes: 1048576
    min_flush_interval_seconds: 5`
	if err := os.WriteFile(runtimeConfigPath, []byte(rawRuntimeConfig), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	socketFile, err := os.CreateTemp("", "loopgate-*.sock")
	if err != nil {
		t.Fatalf("create temp socket file: %v", err)
	}
	socketPath := socketFile.Name()
	_ = socketFile.Close()
	_ = os.Remove(socketPath)
	t.Cleanup(func() { _ = os.Remove(socketPath) })

	server, err := NewServerWithOptions(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("NewServerWithOptions: %v", err)
	}
	defer server.CloseDiagnosticLogs()

	for eventIndex := 0; eventIndex < 2; eventIndex++ {
		if err := server.logEvent("test.audit", "session-a", map[string]interface{}{"step": eventIndex}); err != nil {
			t.Fatalf("log event %d: %v", eventIndex, err)
		}
	}

	exportBatch, err := server.prepareNextAuditExportBatch()
	if err != nil {
		t.Fatalf("prepare next audit export batch: %v", err)
	}
	ingestRequest, err := server.buildAdminNodeAuditIngestRequest(exportBatch)
	if err != nil {
		t.Fatalf("build admin-node audit ingest request: %v", err)
	}

	if ingestRequest.SchemaVersion != adminNodeAuditIngestSchemaVersion {
		t.Fatalf("unexpected ingest schema version: %#v", ingestRequest)
	}
	if ingestRequest.Source.TransportProfile != "local_http_over_uds" {
		t.Fatalf("unexpected transport profile: %#v", ingestRequest.Source)
	}
	if ingestRequest.Source.DestinationKind != "admin_node" {
		t.Fatalf("unexpected destination kind: %#v", ingestRequest.Source)
	}
	if ingestRequest.Source.DestinationLabel != "corp-admin" {
		t.Fatalf("unexpected destination label: %#v", ingestRequest.Source)
	}
	if ingestRequest.Source.DeploymentTenantID != "tenant-acme" {
		t.Fatalf("unexpected deployment tenant id: %#v", ingestRequest.Source)
	}
	if ingestRequest.Source.DeploymentUserID != "user-ada" {
		t.Fatalf("unexpected deployment user id: %#v", ingestRequest.Source)
	}
	if strings.TrimSpace(ingestRequest.Source.SourceHostname) == "" {
		t.Fatalf("expected source hostname in ingest request: %#v", ingestRequest.Source)
	}
	if ingestRequest.Batch.FromAuditSequence != 1 || ingestRequest.Batch.ThroughAuditSequence != 2 {
		t.Fatalf("unexpected audit sequence range in ingest request: %#v", ingestRequest.Batch)
	}
	if ingestRequest.Batch.EventCount != 2 || len(ingestRequest.Batch.Events) != 2 {
		t.Fatalf("unexpected event count in ingest request: %#v", ingestRequest.Batch)
	}
}
