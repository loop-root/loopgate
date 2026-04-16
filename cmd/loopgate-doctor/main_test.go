package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"loopgate/internal/config"
	"loopgate/internal/loopgate"
	"loopgate/internal/secrets"
	"loopgate/internal/testutil"
)

func TestRunTrustCheck_UsesRunningLoopgateServer(t *testing.T) {
	repoRoot, err := os.MkdirTemp("/tmp", "lgd-repo-")
	if err != nil {
		t.Fatalf("create repo root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(repoRoot) })
	policySigner, err := testutil.NewPolicyTestSigner()
	if err != nil {
		t.Fatalf("new policy test signer: %v", err)
	}
	policySigner.ConfigureEnv(t.Setenv)
	if err := policySigner.WriteSignedPolicyYAML(repoRoot, "version: \"1\"\n"); err != nil {
		t.Fatalf("write signed policy yaml: %v", err)
	}
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
	if err := config.WriteRuntimeConfigYAML(repoRoot, runtimeConfig); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	testCertificates := generateAuditExportTestCertificates(t)
	t.Setenv("LOOPGATE_AUDIT_EXPORT_TOKEN", "test-admin-export-token")
	t.Setenv("LOOPGATE_AUDIT_EXPORT_ROOT_CA", testCertificates.RootCAPEM)
	t.Setenv("LOOPGATE_AUDIT_EXPORT_CLIENT_CERTIFICATE", testCertificates.ClientCertificatePEM)
	t.Setenv("LOOPGATE_AUDIT_EXPORT_CLIENT_PRIVATE_KEY", testCertificates.ClientPrivateKeyPEM)

	socketPath := filepath.Join(repoRoot, "runtime", "state", "loopgate.sock")
	calledSocketPath := ""
	previousCheckAuditExportTrust := checkAuditExportTrust
	checkAuditExportTrust = func(actualSocketPath string) (loopgate.AuditExportTrustCheckResponse, error) {
		calledSocketPath = actualSocketPath
		return loopgate.AuditExportTrustCheckResponse{
			Status:       "healthy",
			Summary:      "audit export trust is healthy",
			EndpointHost: "admin.example.com",
		}, nil
	}
	t.Cleanup(func() {
		checkAuditExportTrust = previousCheckAuditExportTrust
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"trust-check", "-repo", repoRoot, "-socket", socketPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected trust-check success, got exit code %d stderr=%s", exitCode, stderr.String())
	}

	var response loopgate.AuditExportTrustCheckResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode trust-check output: %v\nstdout=%s", err, stdout.String())
	}
	if response.Status != "healthy" {
		t.Fatalf("expected healthy trust-check response, got %#v", response)
	}
	if response.ActionNeeded {
		t.Fatalf("expected no action needed, got %#v", response)
	}
	if response.Summary == "" {
		t.Fatalf("expected summary in trust-check response, got %#v", response)
	}
	if response.EndpointHost != "admin.example.com" {
		t.Fatalf("unexpected endpoint host in trust-check response: %#v", response)
	}
	if calledSocketPath != socketPath {
		t.Fatalf("expected trust-check to use socket path %q, got %q", socketPath, calledSocketPath)
	}
}

func TestResolveSocketPath_PrefersEnvOverRepoDefault(t *testing.T) {
	t.Setenv("LOOPGATE_SOCKET", "/tmp/loopgate-env.sock")
	resolvedSocketPath := resolveSocketPath("/repo/root", "")
	if resolvedSocketPath != filepath.Clean("/tmp/loopgate-env.sock") {
		t.Fatalf("expected env socket path, got %q", resolvedSocketPath)
	}
}

type auditExportTestCertificates struct {
	RootCAPEM            string
	ClientCertificatePEM string
	ClientPrivateKeyPEM  string
}

func generateAuditExportTestCertificates(t *testing.T) auditExportTestCertificates {
	t.Helper()

	rootPrivateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate root private key: %v", err)
	}
	rootTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Loopgate Test Root CA"},
		NotBefore:             time.Now().UTC().Add(-1 * time.Hour),
		NotAfter:              time.Now().UTC().Add(7 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}
	rootCertificateDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, &rootPrivateKey.PublicKey, rootPrivateKey)
	if err != nil {
		t.Fatalf("create root certificate: %v", err)
	}
	rootCertificate, err := x509.ParseCertificate(rootCertificateDER)
	if err != nil {
		t.Fatalf("parse root certificate: %v", err)
	}
	rootCAPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootCertificateDER}))

	clientPrivateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate client private key: %v", err)
	}
	clientTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "Loopgate Test Client"},
		NotBefore:             time.Now().UTC().Add(-1 * time.Hour),
		NotAfter:              time.Now().UTC().Add(48 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	clientCertificateDER, err := x509.CreateCertificate(rand.Reader, clientTemplate, rootCertificate, &clientPrivateKey.PublicKey, rootPrivateKey)
	if err != nil {
		t.Fatalf("create client certificate: %v", err)
	}
	clientCertificatePEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientCertificateDER}))
	clientPrivateKeyDER, err := x509.MarshalPKCS8PrivateKey(clientPrivateKey)
	if err != nil {
		t.Fatalf("marshal client private key: %v", err)
	}
	clientPrivateKeyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: clientPrivateKeyDER}))

	return auditExportTestCertificates{
		RootCAPEM:            rootCAPEM,
		ClientCertificatePEM: clientCertificatePEM,
		ClientPrivateKeyPEM:  clientPrivateKeyPEM,
	}
}
