package loopgate

import (
	"os"
	"path/filepath"
	"testing"

	"loopgate/internal/config"
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
      ledger: ledger.log
      model: model.log
`

func TestNewServer_DiagnosticConfigFollowsYAMLDespiteFrozenRuntimeJSON(t *testing.T) {
	repoRoot := t.TempDir()
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
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
