package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestLoadRuntimeConfig_RejectsRequiredHMACCheckpointWhenDisabled(t *testing.T) {
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
    require_hmac_checkpoint: true
    hmac_checkpoint:
      enabled: false
`
	if err := os.WriteFile(runtimeConfigPath, []byte(rawRuntimeConfig), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	_, err := LoadRuntimeConfig(repoRoot)
	if err == nil {
		t.Fatal("expected validation error when hmac checkpoints are required but disabled")
	}
	if !strings.Contains(err.Error(), "require_hmac_checkpoint") {
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
