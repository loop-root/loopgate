package loopgate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFlushAuditExportToConfiguredDestination_PostsBatchAndAdvancesCursor(t *testing.T) {
	repoRoot := t.TempDir()

	var capturedRequest adminNodeAuditIngestRequest
	adminIngestServer := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", request.Method)
		}
		if gotAuthorization := request.Header.Get("Authorization"); gotAuthorization != "Bearer test-admin-export-token" {
			t.Fatalf("unexpected audit export authorization header: %q", gotAuthorization)
		}
		defer request.Body.Close()
		if err := json.NewDecoder(request.Body).Decode(&capturedRequest); err != nil {
			t.Fatalf("decode admin ingest request: %v", err)
		}
		responseWriter.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(responseWriter).Encode(adminNodeAuditIngestResponse{
			SchemaVersion:        adminNodeAuditIngestSchemaVersion,
			Status:               "accepted",
			ThroughAuditSequence: capturedRequest.Batch.ThroughAuditSequence,
			ThroughEventHash:     capturedRequest.Batch.ThroughEventHash,
		})
	}))
	defer adminIngestServer.Close()

	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir runtime config dir: %v", err)
	}
	rawRuntimeConfig := fmt.Sprintf(`version: "1"
logging:
  audit_export:
    enabled: true
    destination_kind: "admin_node"
    destination_label: "corp-admin"
    endpoint_url: %q
    authorization:
      secret_ref:
        id: "audit_export_admin_bearer"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_TOKEN"
        scope: "test"
    max_batch_events: 50
    max_batch_bytes: 1048576
    min_flush_interval_seconds: 5`, adminIngestServer.URL)
	t.Setenv("LOOPGATE_AUDIT_EXPORT_TOKEN", "test-admin-export-token")
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

	if err := server.flushAuditExportToConfiguredDestination(context.Background()); err != nil {
		t.Fatalf("flush audit export: %v", err)
	}

	if capturedRequest.Batch.EventCount != 2 {
		t.Fatalf("expected 2 exported events, got %#v", capturedRequest)
	}
	if capturedRequest.Batch.FromAuditSequence != 1 || capturedRequest.Batch.ThroughAuditSequence != 2 {
		t.Fatalf("unexpected exported sequence range: %#v", capturedRequest.Batch)
	}

	server.auditExportMu.Lock()
	exportState, err := server.loadAuditExportStateLocked()
	server.auditExportMu.Unlock()
	if err != nil {
		t.Fatalf("load audit export state: %v", err)
	}
	if exportState.LastExportedAuditSequence != 2 {
		t.Fatalf("expected export cursor through sequence 2, got %#v", exportState)
	}
	if strings.TrimSpace(exportState.LastExportedEventHash) == "" {
		t.Fatalf("expected exported event hash recorded, got %#v", exportState)
	}
	if exportState.ConsecutiveFailures != 0 {
		t.Fatalf("expected zero consecutive failures after success, got %#v", exportState)
	}
}

func TestFlushAuditExportToConfiguredDestination_FailedPostLeavesCursorUnadvanced(t *testing.T) {
	repoRoot := t.TempDir()

	adminIngestServer := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		http.Error(responseWriter, "admin unavailable", http.StatusBadGateway)
	}))
	defer adminIngestServer.Close()

	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir runtime config dir: %v", err)
	}
	rawRuntimeConfig := fmt.Sprintf(`version: "1"
logging:
  audit_export:
    enabled: true
    destination_kind: "admin_node"
    destination_label: "corp-admin"
    endpoint_url: %q
    authorization:
      secret_ref:
        id: "audit_export_admin_bearer"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_TOKEN"
        scope: "test"
    max_batch_events: 50
    max_batch_bytes: 1048576
    min_flush_interval_seconds: 5`, adminIngestServer.URL)
	t.Setenv("LOOPGATE_AUDIT_EXPORT_TOKEN", "test-admin-export-token")
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

	if err := server.logEvent("test.audit", "session-a", map[string]interface{}{"step": 1}); err != nil {
		t.Fatalf("log event: %v", err)
	}

	if err := server.flushAuditExportToConfiguredDestination(context.Background()); err == nil {
		t.Fatal("expected admin ingest failure")
	}

	server.auditExportMu.Lock()
	exportState, err := server.loadAuditExportStateLocked()
	server.auditExportMu.Unlock()
	if err != nil {
		t.Fatalf("load audit export state: %v", err)
	}
	if exportState.LastExportedAuditSequence != 0 {
		t.Fatalf("expected export cursor to remain unadvanced, got %#v", exportState)
	}
	if exportState.ConsecutiveFailures != 1 {
		t.Fatalf("expected one consecutive failure recorded, got %#v", exportState)
	}
	if exportState.LastErrorClass != "http_status_502" {
		t.Fatalf("expected http_status_502 error class, got %#v", exportState)
	}
}

func TestFlushAuditExportToConfiguredDestination_MissingAuthorizationSecretLeavesCursorUnadvanced(t *testing.T) {
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
        account_name: "LOOPGATE_AUDIT_EXPORT_TOKEN_MISSING"
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

	if err := server.logEvent("test.audit", "session-a", map[string]interface{}{"step": 1}); err != nil {
		t.Fatalf("log event: %v", err)
	}

	if err := server.flushAuditExportToConfiguredDestination(context.Background()); err == nil {
		t.Fatal("expected authorization secret resolution failure")
	}

	server.auditExportMu.Lock()
	exportState, err := server.loadAuditExportStateLocked()
	server.auditExportMu.Unlock()
	if err != nil {
		t.Fatalf("load audit export state: %v", err)
	}
	if exportState.LastExportedAuditSequence != 0 {
		t.Fatalf("expected export cursor to remain unadvanced, got %#v", exportState)
	}
	if exportState.ConsecutiveFailures != 1 {
		t.Fatalf("expected one consecutive failure recorded, got %#v", exportState)
	}
	if exportState.LastErrorClass != "authorization_resolve_failed" {
		t.Fatalf("expected authorization_resolve_failed error class, got %#v", exportState)
	}
}
