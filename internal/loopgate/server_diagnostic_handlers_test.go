package loopgate

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"loopgate/internal/config"
	"loopgate/internal/secrets"
	"loopgate/internal/troubleshoot"
)

func TestHandleDiagnosticReport_UnauthenticatedWithoutPeer(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, srv := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	req := httptest.NewRequest(http.MethodGet, "/v1/diagnostic/report", nil)
	rec := httptest.NewRecorder()
	srv.handleDiagnosticReport(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without peer, got %d", rec.Code)
	}
}

func TestClientFetchDiagnosticReport_OK(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	var decoded map[string]interface{}
	if err := client.FetchDiagnosticReport(context.Background(), &decoded); err != nil {
		t.Fatalf("fetch diagnostic report: %v", err)
	}
	if _, ok := decoded["ledger_verify"]; !ok {
		t.Fatalf("expected ledger_verify in response, got %#v", decoded)
	}
}

func TestDiagnosticReportRequiresSignedRequest(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	client.mu.Lock()
	client.sessionMACKey = ""
	client.mu.Unlock()

	var ignored map[string]interface{}
	err := client.FetchDiagnosticReport(context.Background(), &ignored)
	var denied RequestDeniedError
	if !errors.As(err, &denied) || denied.DenialCode != DenialCodeRequestSignatureMissing {
		t.Fatalf("expected request signature missing denial, got %v", err)
	}
}

func TestAuditExportTrustCheckRequiresSignedRequest(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	client.mu.Lock()
	client.sessionMACKey = ""
	client.mu.Unlock()

	_, err := client.CheckAuditExportTrust(context.Background())
	var denied RequestDeniedError
	if !errors.As(err, &denied) || denied.DenialCode != DenialCodeRequestSignatureMissing {
		t.Fatalf("expected request signature missing denial, got %v", err)
	}
}

func TestClientFetchDiagnosticReport_IncludesAuditExportTrustStatus(t *testing.T) {
	repoRoot := t.TempDir()
	testCertificates := generateAuditExportTestCertificates(t)

	runtimeConfig := config.DefaultRuntimeConfig()
	runtimeConfig.Logging.AuditExport.Enabled = true
	runtimeConfig.Logging.AuditExport.DestinationKind = "admin_node"
	runtimeConfig.Logging.AuditExport.DestinationLabel = "corp-admin"
	runtimeConfig.Logging.AuditExport.EndpointURL = "https://admin.example.com/v1/admin/audit/ingest"
	runtimeConfig.Logging.AuditExport.Authorization.SecretRef = &secrets.SecretRef{
		ID:          "audit_export_admin_bearer",
		Backend:     "env",
		AccountName: "LOOPGATE_AUDIT_EXPORT_TOKEN",
		Scope:       "test",
	}
	runtimeConfig.Logging.AuditExport.TLS.Enabled = true
	runtimeConfig.Logging.AuditExport.TLS.PinnedServerPublicKeySHA256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	runtimeConfig.Logging.AuditExport.TLS.MinimumRemainingValiditySeconds = 300
	runtimeConfig.Logging.AuditExport.TLS.RootCASecretRef = &secrets.SecretRef{
		ID:          "audit_export_root_ca",
		Backend:     "env",
		AccountName: "LOOPGATE_AUDIT_EXPORT_ROOT_CA",
		Scope:       "test",
	}
	runtimeConfig.Logging.AuditExport.TLS.ClientCertificateSecretRef = &secrets.SecretRef{
		ID:          "audit_export_client_certificate",
		Backend:     "env",
		AccountName: "LOOPGATE_AUDIT_EXPORT_CLIENT_CERTIFICATE",
		Scope:       "test",
	}
	runtimeConfig.Logging.AuditExport.TLS.ClientPrivateKeySecretRef = &secrets.SecretRef{
		ID:          "audit_export_client_private_key",
		Backend:     "env",
		AccountName: "LOOPGATE_AUDIT_EXPORT_CLIENT_PRIVATE_KEY",
		Scope:       "test",
	}

	t.Setenv("LOOPGATE_AUDIT_EXPORT_TOKEN", "test-admin-export-token")
	t.Setenv("LOOPGATE_AUDIT_EXPORT_ROOT_CA", testCertificates.RootCAPEM)
	t.Setenv("LOOPGATE_AUDIT_EXPORT_CLIENT_CERTIFICATE", testCertificates.ClientCertificatePEM)
	t.Setenv("LOOPGATE_AUDIT_EXPORT_CLIENT_PRIVATE_KEY", testCertificates.ClientPrivateKeyPEM)

	client, _, _ := startLoopgateServerWithRuntime(t, repoRoot, loopgatePolicyYAML(false), &runtimeConfig, true)
	var report troubleshoot.Report
	if err := client.FetchDiagnosticReport(context.Background(), &report); err != nil {
		t.Fatalf("fetch diagnostic report: %v", err)
	}
	if !report.AuditExport.Enabled {
		t.Fatal("expected audit export enabled in diagnostic report")
	}
	if report.AuditExport.EndpointHost != "admin.example.com" {
		t.Fatalf("unexpected audit export endpoint host: %#v", report.AuditExport)
	}
	if report.AuditExport.Trust.OverallStatus != "ok" {
		t.Fatalf("expected ok audit export trust status, got %#v", report.AuditExport.Trust)
	}
	if report.AuditExport.Trust.ClientCertificate.Status != "ok" {
		t.Fatalf("expected ok client certificate status, got %#v", report.AuditExport.Trust.ClientCertificate)
	}
	if report.AuditExport.Trust.ClientCertificate.RenewalThresholdAtUTC == "" {
		t.Fatalf("expected renewal threshold timestamp, got %#v", report.AuditExport.Trust.ClientCertificate)
	}
	if report.AuditExport.Trust.ClientCertificate.SecondsUntilRenewalThreshold <= 0 {
		t.Fatalf("expected positive seconds until renewal threshold, got %#v", report.AuditExport.Trust.ClientCertificate)
	}
	if report.AuditExport.Trust.ClientCertificate.RenewalWindowActive {
		t.Fatalf("expected renewal window inactive for healthy certificate, got %#v", report.AuditExport.Trust.ClientCertificate)
	}
	if !report.AuditExport.PinnedServerPublicKeyConfigured {
		t.Fatalf("expected pinned server public key configured, got %#v", report.AuditExport)
	}

	trustCheckResponse, err := client.CheckAuditExportTrust(context.Background())
	if err != nil {
		t.Fatalf("check audit export trust: %v", err)
	}
	if trustCheckResponse.Status != "healthy" {
		t.Fatalf("expected healthy trust check response, got %#v", trustCheckResponse)
	}
	if trustCheckResponse.ActionNeeded {
		t.Fatalf("expected no action needed for healthy trust check, got %#v", trustCheckResponse)
	}
	if trustCheckResponse.Summary == "" {
		t.Fatalf("expected trust check summary, got %#v", trustCheckResponse)
	}
}

func TestClientFetchDiagnosticReport_AuditExportTrustShowsRenewalWindowActive(t *testing.T) {
	repoRoot := t.TempDir()
	testCertificates := generateAuditExportTestCertificates(t)

	runtimeConfig := config.DefaultRuntimeConfig()
	runtimeConfig.Logging.AuditExport.Enabled = true
	runtimeConfig.Logging.AuditExport.DestinationKind = "admin_node"
	runtimeConfig.Logging.AuditExport.DestinationLabel = "corp-admin"
	runtimeConfig.Logging.AuditExport.EndpointURL = "https://admin.example.com/v1/admin/audit/ingest"
	runtimeConfig.Logging.AuditExport.Authorization.SecretRef = &secrets.SecretRef{
		ID:          "audit_export_admin_bearer",
		Backend:     "env",
		AccountName: "LOOPGATE_AUDIT_EXPORT_TOKEN",
		Scope:       "test",
	}
	runtimeConfig.Logging.AuditExport.TLS.Enabled = true
	runtimeConfig.Logging.AuditExport.TLS.MinimumRemainingValiditySeconds = 172800
	runtimeConfig.Logging.AuditExport.TLS.RootCASecretRef = &secrets.SecretRef{
		ID:          "audit_export_root_ca",
		Backend:     "env",
		AccountName: "LOOPGATE_AUDIT_EXPORT_ROOT_CA",
		Scope:       "test",
	}
	runtimeConfig.Logging.AuditExport.TLS.ClientCertificateSecretRef = &secrets.SecretRef{
		ID:          "audit_export_client_certificate",
		Backend:     "env",
		AccountName: "LOOPGATE_AUDIT_EXPORT_CLIENT_CERTIFICATE",
		Scope:       "test",
	}
	runtimeConfig.Logging.AuditExport.TLS.ClientPrivateKeySecretRef = &secrets.SecretRef{
		ID:          "audit_export_client_private_key",
		Backend:     "env",
		AccountName: "LOOPGATE_AUDIT_EXPORT_CLIENT_PRIVATE_KEY",
		Scope:       "test",
	}

	t.Setenv("LOOPGATE_AUDIT_EXPORT_TOKEN", "test-admin-export-token")
	t.Setenv("LOOPGATE_AUDIT_EXPORT_ROOT_CA", testCertificates.RootCAPEM)
	t.Setenv("LOOPGATE_AUDIT_EXPORT_CLIENT_CERTIFICATE", testCertificates.ClientCertificatePEM)
	t.Setenv("LOOPGATE_AUDIT_EXPORT_CLIENT_PRIVATE_KEY", testCertificates.ClientPrivateKeyPEM)

	client, _, _ := startLoopgateServerWithRuntime(t, repoRoot, loopgatePolicyYAML(false), &runtimeConfig, true)
	var report troubleshoot.Report
	if err := client.FetchDiagnosticReport(context.Background(), &report); err != nil {
		t.Fatalf("fetch diagnostic report: %v", err)
	}
	if report.AuditExport.Trust.ClientCertificate.Status != "expiring_soon" {
		t.Fatalf("expected expiring_soon client certificate status, got %#v", report.AuditExport.Trust.ClientCertificate)
	}
	if !report.AuditExport.Trust.ClientCertificate.RenewalWindowActive {
		t.Fatalf("expected renewal window active, got %#v", report.AuditExport.Trust.ClientCertificate)
	}
	if report.AuditExport.Trust.ClientCertificate.SecondsUntilRenewalThreshold >= 0 {
		t.Fatalf("expected negative seconds until renewal threshold once inside renewal window, got %#v", report.AuditExport.Trust.ClientCertificate)
	}

	trustCheckResponse, err := client.CheckAuditExportTrust(context.Background())
	if err != nil {
		t.Fatalf("check audit export trust: %v", err)
	}
	if trustCheckResponse.Status != "action_required" {
		t.Fatalf("expected action_required trust check response, got %#v", trustCheckResponse)
	}
	if !trustCheckResponse.ActionNeeded {
		t.Fatalf("expected action_needed for expiring trust check response, got %#v", trustCheckResponse)
	}
	if trustCheckResponse.RecommendedAction == "" {
		t.Fatalf("expected recommended action for expiring trust check response, got %#v", trustCheckResponse)
	}
}
