package main

import (
	"bytes"
	"context"
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

	"morph/internal/config"
	"morph/internal/loopgate"
	"morph/internal/secrets"
	"morph/internal/testutil"
)

func TestRunTrustCheck_UsesRunningLoopgateServer(t *testing.T) {
	repoRoot := t.TempDir()
	policySigner, err := testutil.NewPolicyTestSigner()
	if err != nil {
		t.Fatalf("new policy test signer: %v", err)
	}
	policySigner.ConfigureEnv(t.Setenv)
	if err := policySigner.WriteSignedPolicyYAML(repoRoot, "version: \"1\"\n"); err != nil {
		t.Fatalf("write signed policy yaml: %v", err)
	}
	writeTestMorphlingClassPolicy(t, repoRoot)

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

	socketFile, err := os.CreateTemp("", "loopgate-doctor-*.sock")
	if err != nil {
		t.Fatalf("create temp socket file: %v", err)
	}
	socketPath := socketFile.Name()
	_ = socketFile.Close()
	_ = os.Remove(socketPath)
	t.Cleanup(func() { _ = os.Remove(socketPath) })

	server, err := loopgate.NewServerForIntegrationHarness(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	serverContext, cancel := context.WithCancel(context.Background())
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		_ = server.Serve(serverContext)
	}()
	t.Cleanup(func() {
		cancel()
		<-serverDone
	})

	healthClient := loopgate.NewClient(socketPath)
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, healthErr := healthClient.Health(context.Background())
		if healthErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("wait for loopgate health: %v", healthErr)
		}
		time.Sleep(25 * time.Millisecond)
	}

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

func writeTestMorphlingClassPolicy(t *testing.T, repoRoot string) {
	t.Helper()

	classPolicyPath := filepath.Join(repoRoot, "core", "policy", "morphling_classes.yaml")
	if err := os.MkdirAll(filepath.Dir(classPolicyPath), 0o755); err != nil {
		t.Fatalf("mkdir morphling class policy dir: %v", err)
	}
	if err := os.WriteFile(classPolicyPath, []byte(defaultTestMorphlingClassPolicyYAML()), 0o600); err != nil {
		t.Fatalf("write morphling class policy: %v", err)
	}
}

func defaultTestMorphlingClassPolicyYAML() string {
	return "version: \"1\"\n\n" +
		"classes:\n" +
		"  - name: reviewer\n" +
		"    description: \"Read-only analysis\"\n" +
		"    capabilities:\n" +
		"      allowed:\n" +
		"        - fs_list\n" +
		"        - fs_read\n" +
		"    sandbox:\n" +
		"      allowed_zones:\n" +
		"        - imports\n" +
		"        - scratch\n" +
		"        - workspace\n" +
		"    resource_limits:\n" +
		"      max_time_seconds: 300\n" +
		"      max_tokens: 50000\n" +
		"      max_disk_bytes: 52428800\n" +
		"    ttl:\n" +
		"      spawn_approval_ttl_seconds: 300\n" +
		"      capability_token_ttl_seconds: 360\n" +
		"      review_ttl_seconds: 86400\n" +
		"    spawn_requires_approval: false\n" +
		"    completion_requires_review: true\n" +
		"    max_concurrent: 3\n" +
		"  - name: editor\n" +
		"    description: \"Read and write files\"\n" +
		"    capabilities:\n" +
		"      allowed:\n" +
		"        - fs_list\n" +
		"        - fs_read\n" +
		"        - fs_write\n" +
		"    sandbox:\n" +
		"      allowed_zones:\n" +
		"        - agents\n" +
		"        - imports\n" +
		"        - outputs\n" +
		"        - scratch\n" +
		"        - workspace\n" +
		"    resource_limits:\n" +
		"      max_time_seconds: 600\n" +
		"      max_tokens: 100000\n" +
		"      max_disk_bytes: 104857600\n" +
		"    ttl:\n" +
		"      spawn_approval_ttl_seconds: 300\n" +
		"      capability_token_ttl_seconds: 660\n" +
		"      review_ttl_seconds: 86400\n" +
		"    spawn_requires_approval: true\n" +
		"    completion_requires_review: true\n" +
		"    max_concurrent: 2\n"
}
