package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const diagnosticLoggingOverrideFileName = "loopgate_diagnostic_logging.override.json"

type diagnosticLoggingOverrideFile struct {
	Enabled      *bool   `json:"enabled,omitempty"`
	DefaultLevel *string `json:"default_level,omitempty"`
}

func diagnosticLoggingOverridePath(repoRoot string) string {
	return filepath.Join(repoRoot, "runtime", "state", diagnosticLoggingOverrideFileName)
}

// HasDiagnosticLoggingOverride reports whether the local override file exists.
func HasDiagnosticLoggingOverride(repoRoot string) (bool, error) {
	path := diagnosticLoggingOverridePath(repoRoot)
	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if st.IsDir() {
		return false, fmt.Errorf("%s is a directory", path)
	}
	return true, nil
}

// ApplyDiagnosticLoggingOverride merges runtime/state/loopgate_diagnostic_logging.override.json
// into cfg when the file exists. Invalid JSON is a load error (fail closed).
func ApplyDiagnosticLoggingOverride(repoRoot string, cfg *RuntimeConfig) error {
	if cfg == nil {
		return nil
	}
	path := diagnosticLoggingOverridePath(repoRoot)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read diagnostic logging override: %w", err)
	}
	var o diagnosticLoggingOverrideFile
	if err := json.Unmarshal(raw, &o); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if o.Enabled != nil {
		cfg.Logging.Diagnostic.Enabled = *o.Enabled
	}
	if o.DefaultLevel != nil {
		trimmed := strings.TrimSpace(*o.DefaultLevel)
		if trimmed != "" {
			cfg.Logging.Diagnostic.DefaultLevel = trimmed
		}
	}
	return nil
}

// SaveDiagnosticLoggingOverride writes the override file (0600). Loopgate reads it on next startup.
func SaveDiagnosticLoggingOverride(repoRoot string, enabled bool, defaultLevel string) error {
	trimmedLevel := strings.TrimSpace(defaultLevel)
	if err := validateDiagnosticLevel(trimmedLevel); err != nil {
		return fmt.Errorf("default_level: %w", err)
	}
	stateDir := filepath.Join(repoRoot, "runtime", "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("create runtime state dir: %w", err)
	}
	payload := diagnosticLoggingOverrideFile{
		Enabled:      boolPtr(enabled),
		DefaultLevel: stringPtr(trimmedLevel),
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal override: %w", err)
	}
	path := diagnosticLoggingOverridePath(repoRoot)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("write diagnostic logging override: %w", err)
	}
	return nil
}

// RemoveDiagnosticLoggingOverride deletes the override file if present.
func RemoveDiagnosticLoggingOverride(repoRoot string) error {
	path := diagnosticLoggingOverridePath(repoRoot)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove diagnostic logging override: %w", err)
	}
	return nil
}

func boolPtr(b bool) *bool { return &b }

func stringPtr(s string) *string { return &s }
