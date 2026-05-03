package loopgate

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

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
