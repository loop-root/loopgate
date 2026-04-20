package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeSignedPolicyForConfigTest(t *testing.T, repoRoot string, rawPolicy string) {
	t.Helper()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("new test policy signer: %v", err)
	}
	t.Setenv(testPolicySigningKeyIDEnv, defaultTestPolicySigningKeyID)
	t.Setenv(testPolicySigningPublicKeyEnv, base64.StdEncoding.EncodeToString(publicKey))

	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir policy dir: %v", err)
	}
	rawPolicyBytes := []byte(rawPolicy)
	if err := os.WriteFile(policyPath, rawPolicyBytes, 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	signatureFile, err := SignPolicyDocument(rawPolicyBytes, defaultTestPolicySigningKeyID, privateKey)
	if err != nil {
		t.Fatalf("sign policy: %v", err)
	}
	if err := WritePolicySignatureYAML(repoRoot, signatureFile); err != nil {
		t.Fatalf("write policy signature: %v", err)
	}
}

func TestLoadPolicy_StrictRejectsUnknownField(t *testing.T) {
	repoRoot := t.TempDir()
	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rawPolicy := `version: 0.1.0
tools:
  filesystem:
    allowed_roots: ["."]
    denied_paths: []
    read_enabled: true
    write_enabled: true
    write_requires_approval: true
unknown_section:
  enabled: true
`
	writeSignedPolicyForConfigTest(t, repoRoot, rawPolicy)

	_, err := LoadPolicy(repoRoot)
	if err == nil {
		t.Fatal("expected strict decode error for unknown field, got nil")
	}
}

func TestLoadPolicy_ExpandsHomePathPrefixes(t *testing.T) {
	repoRoot := t.TempDir()
	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rawPolicy := `version: 0.1.0
tools:
  filesystem:
    allowed_roots:
      - "~/loopgate/tests"
    denied_paths:
      - "~/loopgate/secret"
    read_enabled: true
    write_enabled: true
    write_requires_approval: true
  http:
    enabled: false
    allowed_domains: []
    requires_approval: true
    timeout_seconds: 10
  shell:
    enabled: false
    allowed_commands: []
    requires_approval: true
logging:
  log_commands: true
  log_tool_calls: true
safety:
  allow_persona_modification: false
  allow_policy_modification: false
`
	writeSignedPolicyForConfigTest(t, repoRoot, rawPolicy)

	policy, err := LoadPolicy(repoRoot)
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home dir: %v", err)
	}
	if len(policy.Tools.Filesystem.AllowedRoots) != 1 {
		t.Fatalf("expected 1 allowed root, got %d", len(policy.Tools.Filesystem.AllowedRoots))
	}
	if !strings.HasPrefix(policy.Tools.Filesystem.AllowedRoots[0], homeDir) {
		t.Fatalf("allowed root not expanded: %q", policy.Tools.Filesystem.AllowedRoots[0])
	}
	if len(policy.Tools.Filesystem.DeniedPaths) != 1 {
		t.Fatalf("expected 1 denied path, got %d", len(policy.Tools.Filesystem.DeniedPaths))
	}
	if !strings.HasPrefix(policy.Tools.Filesystem.DeniedPaths[0], homeDir) {
		t.Fatalf("denied path not expanded: %q", policy.Tools.Filesystem.DeniedPaths[0])
	}
}

func TestLoadPolicy_MissingFileFailsClosed(t *testing.T) {
	repoRoot := t.TempDir()

	_, err := LoadPolicy(repoRoot)
	if err == nil {
		t.Fatal("expected missing repository policy file to fail closed")
	}
	if !strings.Contains(err.Error(), "required policy file not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveRequiredLoadPath_EvalSymlinks(t *testing.T) {
	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "policy.yaml")
	if err := os.WriteFile(targetPath, []byte("version: 0.1.0\n"), 0o600); err != nil {
		t.Fatalf("write target policy: %v", err)
	}

	linkDir := t.TempDir()
	linkPath := filepath.Join(linkDir, "policy.yaml")
	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Fatalf("symlink policy: %v", err)
	}

	resolvedPath, err := resolveRequiredLoadPath(linkPath, "policy file")
	if err != nil {
		t.Fatalf("resolve required load path: %v", err)
	}
	canonicalTargetPath, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		t.Fatalf("eval symlinks on target path: %v", err)
	}
	if resolvedPath != canonicalTargetPath {
		t.Fatalf("expected resolved path %q, got %q", canonicalTargetPath, resolvedPath)
	}
}

func TestLoadPolicy_EmptyFilesystemAllowedRootsFailsClosed(t *testing.T) {
	repoRoot := t.TempDir()
	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rawPolicy := `version: 0.1.0
tools:
  filesystem:
    allowed_roots: []
    denied_paths: []
    read_enabled: true
    write_enabled: false
    write_requires_approval: true
  http:
    enabled: false
    allowed_domains: []
    requires_approval: true
    timeout_seconds: 10
  shell:
    enabled: false
    allowed_commands: []
    requires_approval: true
logging:
  log_commands: true
  log_tool_calls: true
safety:
  allow_persona_modification: false
  allow_policy_modification: false
`
	writeSignedPolicyForConfigTest(t, repoRoot, rawPolicy)

	_, err := LoadPolicy(repoRoot)
	if err == nil {
		t.Fatal("expected empty allowed_roots to fail closed when filesystem is enabled")
	}
	if !strings.Contains(err.Error(), "allowed_roots") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadPersona_StrictRejectsUnknownField(t *testing.T) {
	repoRoot := t.TempDir()
	personaPath := filepath.Join(repoRoot, "persona", "default.yaml")
	if err := os.MkdirAll(filepath.Dir(personaPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rawPersona := `name: Operator
version: 0.1.0
defaults:
  tone: helpful
unknown_field: true
`
	if err := os.WriteFile(personaPath, []byte(rawPersona), 0o600); err != nil {
		t.Fatalf("write persona: %v", err)
	}

	_, err := LoadPersona(repoRoot)
	if err == nil {
		t.Fatal("expected strict decode error for unknown persona field, got nil")
	}
}

func TestLoadPersona_MissingFileGetsSecureDefaults(t *testing.T) {
	repoRoot := t.TempDir()

	persona, err := LoadPersona(repoRoot)
	if err != nil {
		t.Fatalf("load default persona: %v", err)
	}
	if !persona.Trust.TreatModelOutputAsUntrusted {
		t.Fatal("expected default persona to treat model output as untrusted")
	}
	if !persona.HallucinationControls.RefuseToInventFacts {
		t.Fatal("expected default persona to refuse inventing facts")
	}
	if !persona.RiskControls.RequireExplicitApprovalFor.FilesystemWrites {
		t.Fatal("expected default persona to require approval for filesystem writes")
	}
	if persona.Defaults.PreferredResponseFormat == "" {
		t.Fatal("expected default preferred response format")
	}
}

func TestLoadRuntimeConfig_MissingFileGetsDefaults(t *testing.T) {
	repoRoot := t.TempDir()

	runtimeConfig, err := LoadRuntimeConfig(repoRoot)
	if err != nil {
		t.Fatalf("load default runtime config: %v", err)
	}
	if runtimeConfig.Logging.AuditLedger.MaxEventBytes != 256*1024 {
		t.Fatalf("unexpected audit max_event_bytes default: %d", runtimeConfig.Logging.AuditLedger.MaxEventBytes)
	}
	if runtimeConfig.Logging.AuditLedger.RotateAtBytes != 128*1024*1024 {
		t.Fatalf("unexpected audit rotate_at_bytes default: %d", runtimeConfig.Logging.AuditLedger.RotateAtBytes)
	}
	if runtimeConfig.Logging.AuditLedger.VerifyClosedSegmentsOnStartup == nil || !*runtimeConfig.Logging.AuditLedger.VerifyClosedSegmentsOnStartup {
		t.Fatal("expected audit verify_closed_segments_on_startup default to true")
	}
	if runtimeConfig.Logging.AuditExport.Enabled {
		t.Fatal("expected audit export to default disabled")
	}
	if runtimeConfig.Logging.AuditExport.StatePath != "runtime/state/audit_export_state.json" {
		t.Fatalf("unexpected audit export state path default: %q", runtimeConfig.Logging.AuditExport.StatePath)
	}
	if runtimeConfig.Logging.AuditExport.MaxBatchEvents != 500 {
		t.Fatalf("unexpected audit export max batch events default: %d", runtimeConfig.Logging.AuditExport.MaxBatchEvents)
	}
	if runtimeConfig.Logging.AuditExport.MaxBatchBytes != 1024*1024 {
		t.Fatalf("unexpected audit export max batch bytes default: %d", runtimeConfig.Logging.AuditExport.MaxBatchBytes)
	}
	if runtimeConfig.Logging.AuditExport.MinFlushIntervalSeconds != 5 {
		t.Fatalf("unexpected audit export min flush interval default: %d", runtimeConfig.Logging.AuditExport.MinFlushIntervalSeconds)
	}
	if DefaultSupersededLineageRetentionWindow != 30*24*time.Hour {
		t.Fatalf("unexpected superseded lineage retention default: %s", DefaultSupersededLineageRetentionWindow)
	}
}

func TestLoadRuntimeConfig_StrictRejectsUnknownField(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rawRuntimeConfig := `version: "1"
logging:
  diagnostic:
    enabled: true
unknown_field: true
`
	if err := os.WriteFile(runtimeConfigPath, []byte(rawRuntimeConfig), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	_, err := LoadRuntimeConfig(repoRoot)
	if err == nil {
		t.Fatal("expected strict decode error for unknown runtime config field")
	}
}

func TestLoadRuntimeConfig_RejectsRuntimeTraversalPaths(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rawRuntimeConfig := `version: "1"
logging:
  audit_ledger:
    max_event_bytes: 262144
    rotate_at_bytes: 134217728
    segment_dir: "../segments"
    manifest_path: "runtime/state/loopgate_event_segments/manifest.jsonl"
    verify_closed_segments_on_startup: true`
	if err := os.WriteFile(runtimeConfigPath, []byte(rawRuntimeConfig), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	_, err := LoadRuntimeConfig(repoRoot)
	if err == nil {
		t.Fatal("expected runtime traversal path to fail closed")
	}
	if !strings.Contains(err.Error(), "runtime/state") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRuntimeConfig_RejectsRelativeSessionExecutablePin(t *testing.T) {
	repoRoot := t.TempDir()
	cfg := DefaultRuntimeConfig()
	cfg.ControlPlane.ExpectedSessionClientExecutable = "relative/client/path"
	writeErr := WriteRuntimeConfigYAML(repoRoot, cfg)
	if writeErr == nil {
		t.Fatal("expected WriteRuntimeConfigYAML to reject relative control_plane.expected_session_client_executable")
	}
	if !strings.Contains(writeErr.Error(), "absolute") {
		t.Fatalf("expected absolute-path validation error, got %v", writeErr)
	}
}

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

func TestLoadRuntimeConfig_UsesExpectedSessionClientExecutableEnvWhenUnset(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv(expectedSessionClientExecutableEnv, "/Applications/Loopgate.app/Contents/MacOS/Loopgate")

	runtimeConfig, err := LoadRuntimeConfig(repoRoot)
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}
	if got := runtimeConfig.ControlPlane.ExpectedSessionClientExecutable; got != "/Applications/Loopgate.app/Contents/MacOS/Loopgate" {
		t.Fatalf("unexpected expected session client executable: %q", got)
	}
}

func TestLoadRuntimeConfig_RejectsRelativeSessionExecutableEnv(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv(expectedSessionClientExecutableEnv, "relative/operator")

	_, err := LoadRuntimeConfig(repoRoot)
	if err == nil {
		t.Fatal("expected relative env override to fail closed")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("expected absolute-path validation error, got %v", err)
	}
}

func TestLoadRuntimeConfig_DoesNotOverrideExplicitSessionExecutableWithEnv(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv(expectedSessionClientExecutableEnv, "/Applications/Other.app/Contents/MacOS/Other")

	cfg := DefaultRuntimeConfig()
	cfg.ControlPlane.ExpectedSessionClientExecutable = "/Applications/Loopgate.app/Contents/MacOS/Loopgate"
	if err := WriteRuntimeConfigYAML(repoRoot, cfg); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	runtimeConfig, err := LoadRuntimeConfig(repoRoot)
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}
	if got := runtimeConfig.ControlPlane.ExpectedSessionClientExecutable; got != "/Applications/Loopgate.app/Contents/MacOS/Loopgate" {
		t.Fatalf("expected explicit config value to win, got %q", got)
	}
}

func TestLoadRuntimeConfig_PreservesExplicitFalseForClosedSegmentVerification(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rawRuntimeConfig := `version: "1"
logging:
  audit_ledger:
    max_event_bytes: 262144
    rotate_at_bytes: 134217728
    segment_dir: "runtime/state/loopgate_event_segments"
    manifest_path: "runtime/state/loopgate_event_segments/manifest.jsonl"
    verify_closed_segments_on_startup: false`
	if err := os.WriteFile(runtimeConfigPath, []byte(rawRuntimeConfig), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	runtimeConfig, err := LoadRuntimeConfig(repoRoot)
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}
	if runtimeConfig.Logging.AuditLedger.VerifyClosedSegmentsOnStartup == nil {
		t.Fatal("expected verify_closed_segments_on_startup to be populated")
	}
	if *runtimeConfig.Logging.AuditLedger.VerifyClosedSegmentsOnStartup {
		t.Fatal("expected explicit false verify_closed_segments_on_startup to be preserved")
	}
}

func TestLoadRuntimeConfig_DiagnosticEnabledRejectsDisallowedDirectory(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `version: "1"
logging:
  audit_ledger:
    max_event_bytes: 262144
    rotate_at_bytes: 134217728
    segment_dir: "runtime/state/loopgate_event_segments"
    manifest_path: "runtime/state/loopgate_event_segments/manifest.jsonl"
  diagnostic:
    enabled: true
    default_level: info
    directory: tmp/evil`
	if err := os.WriteFile(runtimeConfigPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}
	_, err := LoadRuntimeConfig(repoRoot)
	if err == nil {
		t.Fatal("expected diagnostic directory outside runtime/logs or runtime/state to fail")
	}
}

func TestLoadRuntimeConfig_DiagnosticEnabledRejectsInvalidLevel(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `version: "1"
logging:
  audit_ledger:
    max_event_bytes: 262144
    rotate_at_bytes: 134217728
    segment_dir: "runtime/state/loopgate_event_segments"
    manifest_path: "runtime/state/loopgate_event_segments/manifest.jsonl"
  diagnostic:
    enabled: true
    default_level: verbose
    directory: runtime/logs`
	if err := os.WriteFile(runtimeConfigPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}
	_, err := LoadRuntimeConfig(repoRoot)
	if err == nil {
		t.Fatal("expected invalid diagnostic default_level to fail")
	}
}

func testPolicyYAML() string {
	return `version: 0.1.0
tools:
  filesystem:
    allowed_roots: ["."]
    denied_paths: []
    read_enabled: true
    write_enabled: false
    write_requires_approval: true
  http:
    enabled: false
    allowed_domains: []
    requires_approval: true
    timeout_seconds: 10
  shell:
    enabled: false
    allowed_commands: []
    requires_approval: true
logging:
  log_commands: true
  log_tool_calls: true
safety:
  allow_persona_modification: false
  allow_policy_modification: false
`
}

func TestLoadPolicyWithHash_ReturnsConsistentHash(t *testing.T) {
	repoRoot := t.TempDir()
	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeSignedPolicyForConfigTest(t, repoRoot, testPolicyYAML())

	result1, err := LoadPolicyWithHash(repoRoot)
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	result2, err := LoadPolicyWithHash(repoRoot)
	if err != nil {
		t.Fatalf("load policy again: %v", err)
	}
	if result1.ContentSHA256 != result2.ContentSHA256 {
		t.Fatalf("expected consistent hash, got %q and %q", result1.ContentSHA256, result2.ContentSHA256)
	}
	if result1.ContentSHA256 == "" {
		t.Fatal("hash should not be empty")
	}
}

func TestLoadPolicy_RejectsMissingDetachedSignature(t *testing.T) {
	repoRoot := t.TempDir()
	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(policyPath, []byte(testPolicyYAML()), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	if _, err := LoadPolicy(repoRoot); err == nil || !strings.Contains(err.Error(), "policy signature") {
		t.Fatalf("expected unsigned policy to fail closed, got %v", err)
	}
}

func TestLoadPolicy_RejectsTamperedSignature(t *testing.T) {
	repoRoot := t.TempDir()
	writeSignedPolicyForConfigTest(t, repoRoot, testPolicyYAML())

	signaturePath := filepath.Join(repoRoot, "core", "policy", "policy.yaml.sig")
	rawSignatureBytes, err := os.ReadFile(signaturePath)
	if err != nil {
		t.Fatalf("read signature: %v", err)
	}
	rawSignatureBytes[len(rawSignatureBytes)-2] ^= 0x01
	if err := os.WriteFile(signaturePath, rawSignatureBytes, 0o600); err != nil {
		t.Fatalf("rewrite tampered signature: %v", err)
	}

	if _, err := LoadPolicy(repoRoot); err == nil || (!strings.Contains(err.Error(), "verification failed") && !strings.Contains(err.Error(), "decode policy signature")) {
		t.Fatalf("expected tampered policy signature to fail, got %v", err)
	}
}

func TestLoadRuntimeConfig_RejectsHMACCheckpointEnabledWithoutSecretRef(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rawRuntimeConfig := `version: "1"
logging:
  audit_ledger:
    max_event_bytes: 262144
    rotate_at_bytes: 134217728
    segment_dir: "runtime/state/loopgate_event_segments"
    manifest_path: "runtime/state/loopgate_event_segments/manifest.jsonl"
    verify_closed_segments_on_startup: true
    hmac_checkpoint:
      enabled: true
      interval_events: 2
`
	if err := os.WriteFile(runtimeConfigPath, []byte(rawRuntimeConfig), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	_, err := LoadRuntimeConfig(repoRoot)
	if err == nil {
		t.Fatal("expected validation error when hmac_checkpoint.enabled without secret_ref")
	}
	if !strings.Contains(err.Error(), "secret_ref") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRuntimeConfig_RejectsHMACCheckpointEnabledWithNonPositiveInterval(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rawRuntimeConfig := `version: "1"
logging:
  audit_ledger:
    max_event_bytes: 262144
    rotate_at_bytes: 134217728
    segment_dir: "runtime/state/loopgate_event_segments"
    manifest_path: "runtime/state/loopgate_event_segments/manifest.jsonl"
    verify_closed_segments_on_startup: true
    hmac_checkpoint:
      enabled: true
      interval_events: -1
      secret_ref:
        id: "audit_ledger_hmac"
        backend: "env"
        account_name: "SOME_VAR"
        scope: "test"
`
	if err := os.WriteFile(runtimeConfigPath, []byte(rawRuntimeConfig), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	_, err := LoadRuntimeConfig(repoRoot)
	if err == nil {
		t.Fatal("expected validation error for non-positive interval_events")
	}
	if !strings.Contains(err.Error(), "interval_events") {
		t.Fatalf("unexpected error: %v", err)
	}
}
