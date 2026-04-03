package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRuntimeConfig_DiagnosticOverrideMerge(t *testing.T) {
	repoRoot := t.TempDir()
	configDir := filepath.Join(repoRoot, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	runtimeYAML := `version: "1"
logging:
  diagnostic:
    enabled: false
    default_level: info
    directory: runtime/logs
`
	if err := os.WriteFile(filepath.Join(configDir, "runtime.yaml"), []byte(runtimeYAML), 0o600); err != nil {
		t.Fatalf("write runtime.yaml: %v", err)
	}
	stateDir := filepath.Join(repoRoot, "runtime", "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	overrideJSON := `{
  "enabled": true,
  "default_level": "debug"
}`
	if err := os.WriteFile(filepath.Join(stateDir, diagnosticLoggingOverrideFileName), []byte(overrideJSON), 0o600); err != nil {
		t.Fatalf("write override: %v", err)
	}

	cfg, err := LoadRuntimeConfig(repoRoot)
	if err != nil {
		t.Fatalf("LoadRuntimeConfig: %v", err)
	}
	if !cfg.Logging.Diagnostic.Enabled {
		t.Fatalf("expected enabled true from override, got false")
	}
	if cfg.Logging.Diagnostic.DefaultLevel != "debug" {
		t.Fatalf("expected default_level debug, got %q", cfg.Logging.Diagnostic.DefaultLevel)
	}
}

func TestLoadRuntimeConfig_DiagnosticOverrideInvalidJSON(t *testing.T) {
	repoRoot := t.TempDir()
	configDir := filepath.Join(repoRoot, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	runtimeYAML := `version: "1"
`
	if err := os.WriteFile(filepath.Join(configDir, "runtime.yaml"), []byte(runtimeYAML), 0o600); err != nil {
		t.Fatalf("write runtime.yaml: %v", err)
	}
	stateDir := filepath.Join(repoRoot, "runtime", "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, diagnosticLoggingOverrideFileName), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write override: %v", err)
	}

	_, err := LoadRuntimeConfig(repoRoot)
	if err == nil {
		t.Fatal("expected error for invalid override JSON")
	}
}

func TestSaveDiagnosticLoggingOverride(t *testing.T) {
	repoRoot := t.TempDir()
	if err := SaveDiagnosticLoggingOverride(repoRoot, true, "trace"); err != nil {
		t.Fatalf("SaveDiagnosticLoggingOverride: %v", err)
	}
	active, err := HasDiagnosticLoggingOverride(repoRoot)
	if err != nil {
		t.Fatalf("HasDiagnosticLoggingOverride: %v", err)
	}
	if !active {
		t.Fatal("expected override file to exist")
	}
	cfg, err := LoadRuntimeConfig(repoRoot)
	if err != nil {
		t.Fatalf("LoadRuntimeConfig: %v", err)
	}
	if !cfg.Logging.Diagnostic.Enabled || cfg.Logging.Diagnostic.DefaultLevel != "trace" {
		t.Fatalf("unexpected cfg: %+v", cfg.Logging.Diagnostic)
	}
	if err := RemoveDiagnosticLoggingOverride(repoRoot); err != nil {
		t.Fatalf("RemoveDiagnosticLoggingOverride: %v", err)
	}
	active, err = HasDiagnosticLoggingOverride(repoRoot)
	if err != nil {
		t.Fatalf("HasDiagnosticLoggingOverride: %v", err)
	}
	if active {
		t.Fatal("expected override file removed")
	}
}
