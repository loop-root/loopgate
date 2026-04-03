package loopgate

import (
	"os"
	"path/filepath"
	"testing"

	"morph/internal/config"
)

// runtimeYAMLDiagnosticOn is a valid runtime.yaml with diagnostic logging enabled.
// Used to prove YAML is authoritative even if a stale runtime/state/config/runtime.json exists.
const runtimeYAMLDiagnosticOn = `version: "1"
logging:
  audit_ledger:
    max_event_bytes: 262144
    rotate_at_bytes: 134217728
    segment_dir: "runtime/state/loopgate_event_segments"
    manifest_path: "runtime/state/loopgate_event_segments/manifest.jsonl"
    verify_closed_segments_on_startup: true
  diagnostic:
    enabled: true
    default_level: debug
    directory: runtime/logs
    levels: {}
    files:
      audit: audit.log
      server: server.log
      client: client.log
      socket: socket.log
      memory: memory.log
      ledger: ledger.log
      model: model.log
memory:
  candidate_panel_size: 3
  decomposition_preference: "hybrid_schema_guided"
  review_preference: "risk_tiered"
  soft_morphling_concurrency: 3
  batching_preference: "pause_on_wave_failure"
  explicit_fact_writes:
    window_seconds: 60
    max_writes_per_session: 50
    max_writes_per_peer_uid: 50
    max_value_bytes: 128
  corrections: []
  scoring:
    importance_base:
      not_important: 0
      somewhat_important: 30
      critical: 60
    approved_goal_anchor: 25
    explicit_user_bonus: 25
    stale_penalty_resolved_30d: 20
    hotness_base:
      not_important: 0
      somewhat_important: 20
      critical: 35
    active_goal_bonus: 25
    due_bonus_within_24h: 25
    due_bonus_within_7d: 10
    current_goal_match_bonus: 20
    stale_penalty_overdue: 10
    duplicate_family_penalty: 15
    positive_support_reviewed_accepted: 12
    negative_task_dismissal: 8
    negative_goal_rejection: 10
    negative_completion_rejection: 10
    promotion_threshold_emerging: 2
    promotion_threshold_active: 3
`

func TestNewServer_DiagnosticConfigFollowsYAMLDespiteFrozenRuntimeJSON(t *testing.T) {
	repoRoot := t.TempDir()
	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policyPath, []byte(loopgatePolicyYAML(false)), 0o600); err != nil {
		t.Fatal(err)
	}
	writeTestMorphlingClassPolicy(t, repoRoot)

	configDir := filepath.Join(repoRoot, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "runtime.yaml"), []byte(runtimeYAMLDiagnosticOn), 0o600); err != nil {
		t.Fatal(err)
	}

	stateDir := filepath.Join(repoRoot, "runtime", "state", "config")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	frozenRuntime := config.DefaultRuntimeConfig()
	frozenRuntime.Logging.Diagnostic.Enabled = false
	if err := config.SaveJSONConfig(stateDir, "runtime", frozenRuntime); err != nil {
		t.Fatalf("save frozen runtime json: %v", err)
	}

	socketPath := filepath.Join(t.TempDir(), "loopgate-diag-merge.sock")
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	defer server.CloseDiagnosticLogs()

	if server.diagnostic == nil {
		t.Fatal("expected diagnostic slog (YAML enabled:true); frozen JSON had enabled:false — merge from LoadRuntimeConfig is broken")
	}
}
