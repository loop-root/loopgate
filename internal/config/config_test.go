package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
	if err := os.WriteFile(policyPath, []byte(rawPolicy), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}

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
      - "~/morph/tests"
    denied_paths:
      - "~/morph/secret"
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
  morphlings:
    spawn_enabled: false
    max_active: 5
    require_template: true
logging:
  log_commands: true
  log_tool_calls: true
  log_memory_promotions: true
memory:
  auto_distillate: true
  require_promotion_approval: true
safety:
  allow_persona_modification: false
  allow_policy_modification: false
`
	if err := os.WriteFile(policyPath, []byte(rawPolicy), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}

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
  morphlings:
    spawn_enabled: false
    max_active: 5
    require_template: true
logging:
  log_commands: true
  log_tool_calls: true
  log_memory_promotions: true
memory:
  auto_distillate: true
  require_promotion_approval: true
safety:
  allow_persona_modification: false
  allow_policy_modification: false
`
	if err := os.WriteFile(policyPath, []byte(rawPolicy), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}

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
	if runtimeConfig.Memory.CandidatePanelSize != 3 {
		t.Fatalf("unexpected candidate panel size: %d", runtimeConfig.Memory.CandidatePanelSize)
	}
	if runtimeConfig.Memory.Backend != DefaultMemoryBackend {
		t.Fatalf("unexpected memory backend default: %q", runtimeConfig.Memory.Backend)
	}
	if runtimeConfig.Memory.Scoring.PositiveSupportReviewedAccepted != 12 {
		t.Fatalf("unexpected positive support default: %d", runtimeConfig.Memory.Scoring.PositiveSupportReviewedAccepted)
	}
	if runtimeConfig.Memory.Scoring.PromotionThresholdActive != 3 {
		t.Fatalf("unexpected active promotion threshold: %d", runtimeConfig.Memory.Scoring.PromotionThresholdActive)
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
	if runtimeConfig.Memory.ExplicitFactWrites.WindowSeconds != 60 {
		t.Fatalf("unexpected explicit_fact_writes.window_seconds default: %d", runtimeConfig.Memory.ExplicitFactWrites.WindowSeconds)
	}
	if runtimeConfig.Memory.ExplicitFactWrites.MaxWritesPerSession != 50 {
		t.Fatalf("unexpected explicit_fact_writes.max_writes_per_session default: %d", runtimeConfig.Memory.ExplicitFactWrites.MaxWritesPerSession)
	}
	if runtimeConfig.Memory.ExplicitFactWrites.MaxWritesPerPeerUID != 50 {
		t.Fatalf("unexpected explicit_fact_writes.max_writes_per_peer_uid default: %d", runtimeConfig.Memory.ExplicitFactWrites.MaxWritesPerPeerUID)
	}
	if runtimeConfig.Memory.ExplicitFactWrites.MaxValueBytes != 128 {
		t.Fatalf("unexpected explicit_fact_writes.max_value_bytes default: %d", runtimeConfig.Memory.ExplicitFactWrites.MaxValueBytes)
	}
	if DefaultSupersededLineageRetentionWindow != 30*24*time.Hour {
		t.Fatalf("unexpected superseded lineage retention default: %s", DefaultSupersededLineageRetentionWindow)
	}
}

func TestLoadGoalAliases_MissingFileGetsDefaults(t *testing.T) {
	repoRoot := t.TempDir()

	goalAliases, err := LoadGoalAliases(repoRoot)
	if err != nil {
		t.Fatalf("load default goal aliases: %v", err)
	}
	if len(goalAliases.Aliases) == 0 {
		t.Fatal("expected default goal aliases")
	}
	if _, found := goalAliases.Aliases["technical_review"]; !found {
		t.Fatal("expected technical_review aliases in defaults")
	}
}

func TestLoadRuntimeConfig_StrictRejectsUnknownField(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rawRuntimeConfig := `version: "1"
memory:
  candidate_panel_size: 3
  decomposition_preference: "hybrid_schema_guided"
  review_preference: "risk_tiered"
  soft_morphling_concurrency: 3
  batching_preference: "pause_on_wave_failure"
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
    verify_closed_segments_on_startup: true
memory:
  candidate_panel_size: 3
  decomposition_preference: "hybrid_schema_guided"
  review_preference: "risk_tiered"
  soft_morphling_concurrency: 3
  batching_preference: "pause_on_wave_failure"
`
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
    verify_closed_segments_on_startup: false
memory:
  candidate_panel_size: 3
  decomposition_preference: "hybrid_schema_guided"
  review_preference: "risk_tiered"
  soft_morphling_concurrency: 3
  batching_preference: "pause_on_wave_failure"
`
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
    directory: tmp/evil
memory:
  candidate_panel_size: 3
  decomposition_preference: "hybrid_schema_guided"
  review_preference: "risk_tiered"
  soft_morphling_concurrency: 3
  batching_preference: "pause_on_wave_failure"
`
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
    directory: runtime/logs
memory:
  candidate_panel_size: 3
  decomposition_preference: "hybrid_schema_guided"
  review_preference: "risk_tiered"
  soft_morphling_concurrency: 3
  batching_preference: "pause_on_wave_failure"
`
	if err := os.WriteFile(runtimeConfigPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}
	_, err := LoadRuntimeConfig(repoRoot)
	if err == nil {
		t.Fatal("expected invalid diagnostic default_level to fail")
	}
}

func TestLoadRuntimeConfig_RejectsUnknownMemoryBackend(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	raw := `version: "1"
memory:
  backend: "mystery_backend"
  candidate_panel_size: 3
  decomposition_preference: "hybrid_schema_guided"
  review_preference: "risk_tiered"
  soft_morphling_concurrency: 3
  batching_preference: "pause_on_wave_failure"
`
	if err := os.WriteFile(runtimeConfigPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	_, err := LoadRuntimeConfig(repoRoot)
	if err == nil {
		t.Fatal("expected unknown memory backend to fail closed")
	}
	if !strings.Contains(err.Error(), "memory.backend") {
		t.Fatalf("unexpected error: %v", err)
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
  morphlings:
    spawn_enabled: false
    max_active: 5
    require_template: true
logging:
  log_commands: true
  log_tool_calls: true
  log_memory_promotions: true
memory:
  auto_distillate: true
  require_promotion_approval: true
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
	if err := os.WriteFile(policyPath, []byte(testPolicyYAML()), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}

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

func TestVerifyPolicyHash_FirstRunWritesHash(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, "runtime", "state"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	matched, storedHash, err := VerifyPolicyHash(repoRoot, "abc123")
	if err != nil {
		t.Fatalf("verify policy hash: %v", err)
	}
	if !matched {
		t.Fatal("first run should match (write initial hash)")
	}
	if storedHash != "" {
		t.Fatalf("first run stored hash should be empty, got %q", storedHash)
	}

	// Second call with same hash should match.
	matched, _, err = VerifyPolicyHash(repoRoot, "abc123")
	if err != nil {
		t.Fatalf("verify again: %v", err)
	}
	if !matched {
		t.Fatal("same hash should match")
	}
}

func TestVerifyPolicyHash_DetectsMismatch(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, "runtime", "state"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write initial hash.
	_, _, err := VerifyPolicyHash(repoRoot, "original_hash")
	if err != nil {
		t.Fatalf("write initial hash: %v", err)
	}

	// Verify with a different hash should fail.
	matched, storedHash, err := VerifyPolicyHash(repoRoot, "changed_hash")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if matched {
		t.Fatal("different hash should not match")
	}
	if storedHash != "original_hash" {
		t.Fatalf("expected stored hash %q, got %q", "original_hash", storedHash)
	}
}

func TestAcceptPolicyHash_UpdatesStoredHash(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, "runtime", "state"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write initial, then accept a new hash.
	_, _, _ = VerifyPolicyHash(repoRoot, "old_hash")
	if err := AcceptPolicyHash(repoRoot, "new_hash"); err != nil {
		t.Fatalf("accept: %v", err)
	}
	matched, _, err := VerifyPolicyHash(repoRoot, "new_hash")
	if err != nil {
		t.Fatalf("verify after accept: %v", err)
	}
	if !matched {
		t.Fatal("accepted hash should match")
	}
}

func TestLoadGoalAliases_StrictRejectsUnknownField(t *testing.T) {
	repoRoot := t.TempDir()
	goalAliasesPath := filepath.Join(repoRoot, "config", "goal_aliases.yaml")
	if err := os.MkdirAll(filepath.Dir(goalAliasesPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rawGoalAliases := `version: "1"
aliases:
  technical_review:
    - rfc_review
unknown_field: true
`
	if err := os.WriteFile(goalAliasesPath, []byte(rawGoalAliases), 0o600); err != nil {
		t.Fatalf("write goal aliases: %v", err)
	}

	_, err := LoadGoalAliases(repoRoot)
	if err == nil {
		t.Fatal("expected strict decode error for unknown goal aliases field")
	}
}

const testRuntimeConfigMemoryBlock = `memory:
  candidate_panel_size: 3
  decomposition_preference: "hybrid_schema_guided"
  review_preference: "risk_tiered"
  soft_morphling_concurrency: 3
  batching_preference: "pause_on_wave_failure"
`

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
` + testRuntimeConfigMemoryBlock
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
` + testRuntimeConfigMemoryBlock
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
