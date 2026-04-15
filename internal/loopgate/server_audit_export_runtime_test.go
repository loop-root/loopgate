package loopgate

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewServerWithOptions_SeedsAuditExportStateWhenEnabled(t *testing.T) {
	repoRoot := t.TempDir()

	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	writeTestMorphlingClassPolicy(t, repoRoot)
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
	writeTestMorphlingClassPolicy(t, repoRoot)
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
	writeTestMorphlingClassPolicy(t, repoRoot)
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
	writeTestMorphlingClassPolicy(t, repoRoot)
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
	writeTestMorphlingClassPolicy(t, repoRoot)
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
	writeTestMorphlingClassPolicy(t, repoRoot)
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
	writeTestMorphlingClassPolicy(t, repoRoot)
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

func TestFlushAuditExportToConfiguredDestination_AdminNodeMTLSSucceedsAndAdvancesCursor(t *testing.T) {
	repoRoot := t.TempDir()

	testCertificates := generateAuditExportTestCertificates(t)
	pinnedServerPublicKeySHA256 := certificatePublicKeyPinSHA256(t, testCertificates.ServerCertificatePEM)
	rootPool := x509.NewCertPool()
	if !rootPool.AppendCertsFromPEM([]byte(testCertificates.RootCAPEM)) {
		t.Fatal("append root CA PEM")
	}
	serverCertificate, err := tls.X509KeyPair([]byte(testCertificates.ServerCertificatePEM), []byte(testCertificates.ServerPrivateKeyPEM))
	if err != nil {
		t.Fatalf("parse test server certificate: %v", err)
	}

	var capturedRequest adminNodeAuditIngestRequest
	adminIngestServer := httptest.NewUnstartedServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		if request.TLS == nil || len(request.TLS.PeerCertificates) != 1 {
			t.Fatalf("expected verified client certificate, got %#v", request.TLS)
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
	adminIngestServer.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{serverCertificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    rootPool,
	}
	adminIngestServer.StartTLS()
	defer adminIngestServer.Close()

	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	writeTestMorphlingClassPolicy(t, repoRoot)
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
    tls:
      enabled: true
      pinned_server_public_key_sha256: "`+pinnedServerPublicKeySHA256+`"
      minimum_remaining_validity_seconds: 300
      root_ca_secret_ref:
        id: "audit_export_root_ca"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_ROOT_CA"
        scope: "test"
      client_certificate_secret_ref:
        id: "audit_export_client_certificate"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_CLIENT_CERTIFICATE"
        scope: "test"
      client_private_key_secret_ref:
        id: "audit_export_client_private_key"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_CLIENT_PRIVATE_KEY"
        scope: "test"
    max_batch_events: 50
    max_batch_bytes: 1048576
    min_flush_interval_seconds: 5`, adminIngestServer.URL)
	t.Setenv("LOOPGATE_AUDIT_EXPORT_TOKEN", "test-admin-export-token")
	t.Setenv("LOOPGATE_AUDIT_EXPORT_ROOT_CA", testCertificates.RootCAPEM)
	t.Setenv("LOOPGATE_AUDIT_EXPORT_CLIENT_CERTIFICATE", testCertificates.ClientCertificatePEM)
	t.Setenv("LOOPGATE_AUDIT_EXPORT_CLIENT_PRIVATE_KEY", testCertificates.ClientPrivateKeyPEM)
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
		t.Fatalf("flush audit export with mTLS: %v", err)
	}

	if capturedRequest.Batch.EventCount != 2 {
		t.Fatalf("expected 2 exported events, got %#v", capturedRequest)
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
	if exportState.ConsecutiveFailures != 0 {
		t.Fatalf("expected zero consecutive failures after success, got %#v", exportState)
	}
}

func TestFlushAuditExportToConfiguredDestination_AdminNodeMTLSPinMismatchLeavesCursorUnadvanced(t *testing.T) {
	repoRoot := t.TempDir()

	testCertificates := generateAuditExportTestCertificates(t)
	rootPool := x509.NewCertPool()
	if !rootPool.AppendCertsFromPEM([]byte(testCertificates.RootCAPEM)) {
		t.Fatal("append root CA PEM")
	}
	serverCertificate, err := tls.X509KeyPair([]byte(testCertificates.ServerCertificatePEM), []byte(testCertificates.ServerPrivateKeyPEM))
	if err != nil {
		t.Fatalf("parse test server certificate: %v", err)
	}

	adminIngestServer := httptest.NewUnstartedServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		t.Fatal("expected pinned identity mismatch to fail before handler execution")
	}))
	adminIngestServer.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{serverCertificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    rootPool,
	}
	adminIngestServer.StartTLS()
	defer adminIngestServer.Close()

	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	writeTestMorphlingClassPolicy(t, repoRoot)
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
    tls:
      enabled: true
      pinned_server_public_key_sha256: "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
      minimum_remaining_validity_seconds: 300
      root_ca_secret_ref:
        id: "audit_export_root_ca"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_ROOT_CA"
        scope: "test"
      client_certificate_secret_ref:
        id: "audit_export_client_certificate"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_CLIENT_CERTIFICATE"
        scope: "test"
      client_private_key_secret_ref:
        id: "audit_export_client_private_key"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_CLIENT_PRIVATE_KEY"
        scope: "test"
    max_batch_events: 50
    max_batch_bytes: 1048576
    min_flush_interval_seconds: 5`, adminIngestServer.URL)
	t.Setenv("LOOPGATE_AUDIT_EXPORT_TOKEN", "test-admin-export-token")
	t.Setenv("LOOPGATE_AUDIT_EXPORT_ROOT_CA", testCertificates.RootCAPEM)
	t.Setenv("LOOPGATE_AUDIT_EXPORT_CLIENT_CERTIFICATE", testCertificates.ClientCertificatePEM)
	t.Setenv("LOOPGATE_AUDIT_EXPORT_CLIENT_PRIVATE_KEY", testCertificates.ClientPrivateKeyPEM)
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
		t.Fatal("expected admin-node pinned identity mismatch")
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
	if exportState.LastErrorClass != "server_identity_mismatch" {
		t.Fatalf("expected server_identity_mismatch error class, got %#v", exportState)
	}
}

func TestFlushAuditExportToConfiguredDestination_ClientCertificateExpiringSoonLeavesCursorUnadvanced(t *testing.T) {
	repoRoot := t.TempDir()

	testCertificates := generateAuditExportTestCertificates(t)
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	writeTestMorphlingClassPolicy(t, repoRoot)
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
    endpoint_url: "https://admin.example.com/v1/admin/audit/ingest"
    authorization:
      secret_ref:
        id: "audit_export_admin_bearer"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_TOKEN"
        scope: "test"
    tls:
      enabled: true
      minimum_remaining_validity_seconds: 172800
      root_ca_secret_ref:
        id: "audit_export_root_ca"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_ROOT_CA"
        scope: "test"
      client_certificate_secret_ref:
        id: "audit_export_client_certificate"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_CLIENT_CERTIFICATE"
        scope: "test"
      client_private_key_secret_ref:
        id: "audit_export_client_private_key"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_CLIENT_PRIVATE_KEY"
        scope: "test"
    max_batch_events: 50
    max_batch_bytes: 1048576
    min_flush_interval_seconds: 5`
	t.Setenv("LOOPGATE_AUDIT_EXPORT_TOKEN", "test-admin-export-token")
	t.Setenv("LOOPGATE_AUDIT_EXPORT_ROOT_CA", testCertificates.RootCAPEM)
	t.Setenv("LOOPGATE_AUDIT_EXPORT_CLIENT_CERTIFICATE", testCertificates.ClientCertificatePEM)
	t.Setenv("LOOPGATE_AUDIT_EXPORT_CLIENT_PRIVATE_KEY", testCertificates.ClientPrivateKeyPEM)
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
		t.Fatal("expected expiring-soon client certificate failure")
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
	if exportState.LastErrorClass != "client_certificate_expiring_soon" {
		t.Fatalf("expected client_certificate_expiring_soon error class, got %#v", exportState)
	}
}

type auditExportTestCertificates struct {
	RootCAPEM            string
	ServerCertificatePEM string
	ServerPrivateKeyPEM  string
	ClientCertificatePEM string
	ClientPrivateKeyPEM  string
}

func generateAuditExportTestCertificates(t *testing.T) auditExportTestCertificates {
	t.Helper()

	rootPublicKey, rootPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate root key: %v", err)
	}
	rootTemplate := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "loopgate-test-root",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(30 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, &rootTemplate, &rootTemplate, rootPublicKey, rootPrivateKey)
	if err != nil {
		t.Fatalf("create root certificate: %v", err)
	}

	serverPublicKey, serverPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	serverTemplate := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			CommonName: "127.0.0.1",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		DNSNames:              []string{"localhost"},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, &serverTemplate, &rootTemplate, serverPublicKey, rootPrivateKey)
	if err != nil {
		t.Fatalf("create server certificate: %v", err)
	}

	clientPublicKey, clientPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate client key: %v", err)
	}
	clientTemplate := x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject: pkix.Name{
			CommonName: "loopgate-test-client",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	clientDER, err := x509.CreateCertificate(rand.Reader, &clientTemplate, &rootTemplate, clientPublicKey, rootPrivateKey)
	if err != nil {
		t.Fatalf("create client certificate: %v", err)
	}

	return auditExportTestCertificates{
		RootCAPEM:            encodeCertificatePEM(rootDER),
		ServerCertificatePEM: encodeCertificatePEM(serverDER),
		ServerPrivateKeyPEM:  encodePrivateKeyPEM(t, serverPrivateKey),
		ClientCertificatePEM: encodeCertificatePEM(clientDER),
		ClientPrivateKeyPEM:  encodePrivateKeyPEM(t, clientPrivateKey),
	}
}

func encodeCertificatePEM(derBytes []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes}))
}

func encodePrivateKeyPEM(t *testing.T, privateKey ed25519.PrivateKey) string {
	t.Helper()

	privateKeyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal pkcs8 private key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyBytes}))
}

func certificatePublicKeyPinSHA256(t *testing.T, certificatePEM string) string {
	t.Helper()

	decodedBlock, _ := pem.Decode([]byte(certificatePEM))
	if decodedBlock == nil {
		t.Fatal("decode certificate pem")
	}
	parsedCertificate, err := x509.ParseCertificate(decodedBlock.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(parsedCertificate.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	publicKeyDigest := sha256.Sum256(publicKeyBytes)
	return hex.EncodeToString(publicKeyDigest[:])
}
