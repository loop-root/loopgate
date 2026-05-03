package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
	if runtimeConfig.ControlPlane.MaxInFlightHTTPRequests != DefaultMaxInFlightHTTPRequests {
		t.Fatalf("unexpected max in-flight HTTP request default: %d", runtimeConfig.ControlPlane.MaxInFlightHTTPRequests)
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

func TestLoadRuntimeConfig_RejectsNonPositiveMaxInFlightHTTPRequests(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfigPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	rawRuntimeConfig := `version: "1"
control_plane:
  max_in_flight_http_requests: -1
logging:
  audit_ledger:
    max_event_bytes: 262144
    rotate_at_bytes: 134217728
    segment_dir: "runtime/state/loopgate_event_segments"
    manifest_path: "runtime/state/loopgate_event_segments/manifest.jsonl"
`
	if err := os.WriteFile(runtimeConfigPath, []byte(rawRuntimeConfig), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	_, err := LoadRuntimeConfig(repoRoot)
	if err == nil {
		t.Fatal("expected non-positive max_in_flight_http_requests to fail")
	}
	if !strings.Contains(err.Error(), "max_in_flight_http_requests") {
		t.Fatalf("unexpected error: %v", err)
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
