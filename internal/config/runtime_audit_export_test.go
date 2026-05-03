package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRuntimeConfig_RejectsEnabledAuditExportWithoutDestinationKind(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rawRuntimeConfig := `version: "1"
logging:
  audit_export:
    enabled: true
    destination_label: "corp-admin"
    endpoint_url: "https://admin.example.com/v1/admin/audit/ingest"`
	if err := os.WriteFile(runtimeConfigPath, []byte(rawRuntimeConfig), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	_, err := LoadRuntimeConfig(repoRoot)
	if err == nil {
		t.Fatal("expected enabled audit export without destination kind to fail")
	}
	if !strings.Contains(err.Error(), "destination_kind") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRuntimeConfig_AcceptsEnabledAuditExport(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rawRuntimeConfig := `version: "1"
logging:
  audit_export:
    enabled: true
    destination_kind: "admin_node"
    destination_label: "corp-admin"
    endpoint_url: "https://admin.example.com/v1/admin/audit/ingest"
    authorization:
      scheme: "bearer"
      secret_ref:
        id: "audit_export_admin_bearer"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_TOKEN"
        scope: "test"
    tls:
      enabled: true
      server_name: "admin.example.com"
      pinned_server_public_key_sha256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
      minimum_remaining_validity_seconds: 3600
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
    state_path: "runtime/state/audit_export_state.json"
    max_batch_events: 250
    max_batch_bytes: 524288
    min_flush_interval_seconds: 15`
	if err := os.WriteFile(runtimeConfigPath, []byte(rawRuntimeConfig), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	runtimeConfig, err := LoadRuntimeConfig(repoRoot)
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}
	if !runtimeConfig.Logging.AuditExport.Enabled {
		t.Fatal("expected audit export enabled")
	}
	if runtimeConfig.Logging.AuditExport.DestinationKind != "admin_node" {
		t.Fatalf("unexpected audit export destination kind: %q", runtimeConfig.Logging.AuditExport.DestinationKind)
	}
	if runtimeConfig.Logging.AuditExport.DestinationLabel != "corp-admin" {
		t.Fatalf("unexpected audit export destination label: %q", runtimeConfig.Logging.AuditExport.DestinationLabel)
	}
	if runtimeConfig.Logging.AuditExport.EndpointURL != "https://admin.example.com/v1/admin/audit/ingest" {
		t.Fatalf("unexpected audit export endpoint url: %q", runtimeConfig.Logging.AuditExport.EndpointURL)
	}
	if runtimeConfig.Logging.AuditExport.Authorization.SecretRef == nil {
		t.Fatal("expected audit export authorization secret ref")
	}
	if !runtimeConfig.Logging.AuditExport.TLS.Enabled {
		t.Fatal("expected audit export tls enabled")
	}
	if runtimeConfig.Logging.AuditExport.TLS.RootCASecretRef == nil {
		t.Fatal("expected audit export tls root CA secret ref")
	}
	if runtimeConfig.Logging.AuditExport.TLS.PinnedServerPublicKeySHA256 == "" {
		t.Fatal("expected pinned server public key sha256")
	}
}

func TestLoadRuntimeConfig_RejectsEnabledAuditExportWithoutEndpointURL(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rawRuntimeConfig := `version: "1"
logging:
  audit_export:
    enabled: true
    destination_kind: "admin_node"
    destination_label: "corp-admin"`
	if err := os.WriteFile(runtimeConfigPath, []byte(rawRuntimeConfig), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	_, err := LoadRuntimeConfig(repoRoot)
	if err == nil {
		t.Fatal("expected enabled audit export without endpoint_url to fail")
	}
	if !strings.Contains(err.Error(), "endpoint_url") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRuntimeConfig_RejectsEnabledAdminNodeAuditExportWithoutAuthorizationSecretRef(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rawRuntimeConfig := `version: "1"
logging:
  audit_export:
    enabled: true
    destination_kind: "admin_node"
    destination_label: "corp-admin"
    endpoint_url: "https://admin.example.com/v1/admin/audit/ingest"`
	if err := os.WriteFile(runtimeConfigPath, []byte(rawRuntimeConfig), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	_, err := LoadRuntimeConfig(repoRoot)
	if err == nil {
		t.Fatal("expected enabled admin_node audit export without authorization.secret_ref to fail")
	}
	if !strings.Contains(err.Error(), "authorization.secret_ref") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRuntimeConfig_RejectsEnabledRemoteAdminNodeAuditExportWithoutTLSEnabled(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
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
        scope: "test"`
	if err := os.WriteFile(runtimeConfigPath, []byte(rawRuntimeConfig), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	_, err := LoadRuntimeConfig(repoRoot)
	if err == nil {
		t.Fatal("expected enabled remote admin_node audit export without tls.enabled to fail")
	}
	if !strings.Contains(err.Error(), "tls.enabled") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRuntimeConfig_RejectsAuditExportEndpointURLWithEmbeddedCredentials(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rawRuntimeConfig := `version: "1"
logging:
  audit_export:
    enabled: true
    destination_kind: "admin_node"
    destination_label: "corp-admin"
    endpoint_url: "https://user:pass@admin.example.com/v1/admin/audit/ingest"
    authorization:
      secret_ref:
        id: "audit_export_admin_bearer"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_TOKEN"
        scope: "test"`
	if err := os.WriteFile(runtimeConfigPath, []byte(rawRuntimeConfig), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	_, err := LoadRuntimeConfig(repoRoot)
	if err == nil {
		t.Fatal("expected embedded audit export credentials to fail")
	}
	if !strings.Contains(err.Error(), "embedded credentials") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRuntimeConfig_RejectsAuditExportEndpointURLWithoutHTTPSForRemoteHost(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rawRuntimeConfig := `version: "1"
logging:
  audit_export:
    enabled: true
    destination_kind: "admin_node"
    destination_label: "corp-admin"
    endpoint_url: "http://admin.example.com/v1/admin/audit/ingest"
    authorization:
      secret_ref:
        id: "audit_export_admin_bearer"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_TOKEN"
        scope: "test"`
	if err := os.WriteFile(runtimeConfigPath, []byte(rawRuntimeConfig), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	_, err := LoadRuntimeConfig(repoRoot)
	if err == nil {
		t.Fatal("expected remote non-https audit export endpoint to fail")
	}
	if !strings.Contains(err.Error(), "must use https") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRuntimeConfig_RejectsAuditExportAuthorizationWithUnsupportedScheme(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rawRuntimeConfig := `version: "1"
logging:
  audit_export:
    enabled: true
    destination_kind: "admin_node"
    destination_label: "corp-admin"
    endpoint_url: "https://admin.example.com/v1/admin/audit/ingest"
    authorization:
      scheme: "basic"
      secret_ref:
        id: "audit_export_admin_bearer"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_TOKEN"
        scope: "test"`
	if err := os.WriteFile(runtimeConfigPath, []byte(rawRuntimeConfig), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	_, err := LoadRuntimeConfig(repoRoot)
	if err == nil {
		t.Fatal("expected unsupported audit export authorization scheme to fail")
	}
	if !strings.Contains(err.Error(), "authorization.scheme") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRuntimeConfig_RejectsEnabledAuditExportTLSWithoutRootCASecretRef(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
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
      client_certificate_secret_ref:
        id: "audit_export_client_certificate"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_CLIENT_CERTIFICATE"
        scope: "test"
      client_private_key_secret_ref:
        id: "audit_export_client_private_key"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_CLIENT_PRIVATE_KEY"
        scope: "test"`
	if err := os.WriteFile(runtimeConfigPath, []byte(rawRuntimeConfig), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	_, err := LoadRuntimeConfig(repoRoot)
	if err == nil {
		t.Fatal("expected enabled audit export tls without root CA secret ref to fail")
	}
	if !strings.Contains(err.Error(), "root_ca_secret_ref") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRuntimeConfig_RejectsEnabledAuditExportTLSWithoutClientCertificateSecretRef(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
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
      root_ca_secret_ref:
        id: "audit_export_root_ca"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_ROOT_CA"
        scope: "test"
      client_private_key_secret_ref:
        id: "audit_export_client_private_key"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_CLIENT_PRIVATE_KEY"
        scope: "test"`
	if err := os.WriteFile(runtimeConfigPath, []byte(rawRuntimeConfig), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	_, err := LoadRuntimeConfig(repoRoot)
	if err == nil {
		t.Fatal("expected enabled audit export tls without client certificate secret ref to fail")
	}
	if !strings.Contains(err.Error(), "client_certificate_secret_ref") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRuntimeConfig_RejectsEnabledAuditExportTLSWithoutClientPrivateKeySecretRef(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
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
      root_ca_secret_ref:
        id: "audit_export_root_ca"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_ROOT_CA"
        scope: "test"
      client_certificate_secret_ref:
        id: "audit_export_client_certificate"
        backend: "env"
        account_name: "LOOPGATE_AUDIT_EXPORT_CLIENT_CERTIFICATE"
        scope: "test"`
	if err := os.WriteFile(runtimeConfigPath, []byte(rawRuntimeConfig), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	_, err := LoadRuntimeConfig(repoRoot)
	if err == nil {
		t.Fatal("expected enabled audit export tls without client private key secret ref to fail")
	}
	if !strings.Contains(err.Error(), "client_private_key_secret_ref") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRuntimeConfig_RejectsAuditExportTLSWithInvalidPinnedServerPublicKeySHA256(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
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
      pinned_server_public_key_sha256: "not-hex"
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
        scope: "test"`
	if err := os.WriteFile(runtimeConfigPath, []byte(rawRuntimeConfig), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	_, err := LoadRuntimeConfig(repoRoot)
	if err == nil {
		t.Fatal("expected invalid pinned server public key sha256 to fail")
	}
	if !strings.Contains(err.Error(), "pinned_server_public_key_sha256") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRuntimeConfig_RejectsAuditExportTLSWithNegativeMinimumRemainingValiditySeconds(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
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
      minimum_remaining_validity_seconds: -1
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
        scope: "test"`
	if err := os.WriteFile(runtimeConfigPath, []byte(rawRuntimeConfig), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	_, err := LoadRuntimeConfig(repoRoot)
	if err == nil {
		t.Fatal("expected negative minimum remaining validity seconds to fail")
	}
	if !strings.Contains(err.Error(), "minimum_remaining_validity_seconds") {
		t.Fatalf("unexpected error: %v", err)
	}
}
