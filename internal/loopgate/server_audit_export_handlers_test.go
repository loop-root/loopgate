package loopgate

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"loopgate/internal/ledger"
)

func TestAuditExportFlushRouteRequiresScopedCapability(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	pinTestProcessAsExpectedClient(t, server)

	deniedClient := NewClient(client.socketPath)
	deniedClient.ConfigureSession("test-actor", "audit-export-denied", []string{controlCapabilityDiagnosticRead})
	if _, err := deniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied audit.export token: %v", err)
	}
	if _, err := deniedClient.FlushAuditExport(context.Background()); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected audit.export scope denial, got %v", err)
	}
}

func TestAuditExportFlushRoute_SignedRequestRequired(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	pinTestProcessAsExpectedClient(t, server)

	client.ConfigureSession("test-actor", "audit-export-signature", []string{controlCapabilityAuditExport})
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure audit.export token: %v", err)
	}

	client.mu.Lock()
	client.sessionMACKey = ""
	client.mu.Unlock()

	_, err := client.FlushAuditExport(context.Background())
	var denied RequestDeniedError
	if !errors.As(err, &denied) || denied.DenialCode != DenialCodeRequestSignatureMissing {
		t.Fatalf("expected request signature missing denial, got %v", err)
	}
}

func TestAuditExportFlushRoute_PostsBatchAndReturnsSummary(t *testing.T) {
	repoRoot := t.TempDir()
	var capturedRequest adminNodeAuditIngestRequest
	adminNode := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Fatalf("expected POST to admin node, got %s", request.Method)
		}
		if gotAuthorization := request.Header.Get("Authorization"); gotAuthorization != "Bearer test-admin-export-token" {
			t.Fatalf("unexpected audit export authorization header: %q", gotAuthorization)
		}
		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&capturedRequest); err != nil {
			t.Fatalf("decode admin-node request: %v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = encodeJSONResponse(writer, adminNodeAuditIngestResponse{
			SchemaVersion:        adminNodeAuditIngestSchemaVersion,
			Status:               "accepted",
			ThroughAuditSequence: capturedRequest.Batch.ThroughAuditSequence,
			ThroughEventHash:     capturedRequest.Batch.ThroughEventHash,
		})
	}))
	defer adminNode.Close()

	writeAuditExportRuntimeConfig(t, repoRoot, adminNode.URL)

	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	pinTestProcessAsExpectedClient(t, server)
	client.ConfigureSession("test-actor", "audit-export-success", []string{controlCapabilityAuditExport})
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure audit.export token: %v", err)
	}

	recordedTypes := make([]string, 0)
	originalAppend := server.appendAuditEvent
	server.appendAuditEvent = func(path string, auditEvent ledger.Event) error {
		recordedTypes = append(recordedTypes, auditEvent.Type)
		return originalAppend(path, auditEvent)
	}

	response, err := client.FlushAuditExport(context.Background())
	if err != nil {
		t.Fatalf("flush audit export: %v", err)
	}
	if response.Status != "flushed" {
		t.Fatalf("expected flushed status, got %#v", response)
	}
	if response.EventCount <= 0 {
		t.Fatalf("expected non-empty export batch, got %#v", response)
	}
	if response.ThroughAuditSequence == 0 || strings.TrimSpace(response.ThroughEventHash) == "" {
		t.Fatalf("expected through cursor in response, got %#v", response)
	}
	if capturedRequest.Batch.ThroughAuditSequence != response.ThroughAuditSequence {
		t.Fatalf("expected matching through sequence, got request=%d response=%d", capturedRequest.Batch.ThroughAuditSequence, response.ThroughAuditSequence)
	}
	if !containsAuditExportEvent(recordedTypes, "audit_export.requested") {
		t.Fatalf("expected audit_export.requested in audit stream, got %#v", recordedTypes)
	}
	if !containsAuditExportEvent(recordedTypes, "audit_export.completed") {
		t.Fatalf("expected audit_export.completed in audit stream, got %#v", recordedTypes)
	}
}

func TestAuditExportFlushRoute_AuditRequestFailureFailsClosedBeforeRemotePost(t *testing.T) {
	repoRoot := t.TempDir()
	var postCount int32
	adminNode := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		atomic.AddInt32(&postCount, 1)
		writer.Header().Set("Content-Type", "application/json")
		_ = encodeJSONResponse(writer, adminNodeAuditIngestResponse{
			SchemaVersion:        adminNodeAuditIngestSchemaVersion,
			Status:               "accepted",
			ThroughAuditSequence: 0,
			ThroughEventHash:     "",
		})
	}))
	defer adminNode.Close()

	writeAuditExportRuntimeConfig(t, repoRoot, adminNode.URL)

	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	pinTestProcessAsExpectedClient(t, server)
	client.ConfigureSession("test-actor", "audit-export-audit-fail", []string{controlCapabilityAuditExport})
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure audit.export token: %v", err)
	}

	server.appendAuditEvent = func(string, ledger.Event) error {
		return errors.New("synthetic audit failure")
	}

	_, err := client.FlushAuditExport(context.Background())
	var denied RequestDeniedError
	if !errors.As(err, &denied) || denied.DenialCode != DenialCodeAuditUnavailable {
		t.Fatalf("expected audit unavailable denial, got %v", err)
	}
	if got := atomic.LoadInt32(&postCount); got != 0 {
		t.Fatalf("expected no remote post after audit request failure, got %d", got)
	}
}

func writeAuditExportRuntimeConfig(t *testing.T, repoRoot string, endpointURL string) {
	t.Helper()
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
    endpoint_url: "` + endpointURL + `"
    authorization:
      secret_ref:
        id: "audit_export_admin_bearer"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_TOKEN"
        scope: "test"
memory:
  candidate_panel_size: 3
  decomposition_preference: "hybrid_schema_guided"
  review_preference: "risk_tiered"
  soft_worker_concurrency: 3
  batching_preference: "pause_on_wave_failure"
`
	t.Setenv("LOOPGATE_AUDIT_EXPORT_TOKEN", "test-admin-export-token")
	if err := os.WriteFile(runtimeConfigPath, []byte(rawRuntimeConfig), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}
}

func containsAuditExportEvent(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
