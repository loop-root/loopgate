package main

import (
	"path/filepath"

	"morph/internal/config"
)

// LoopgateDiagnosticLoggingStatus is the effective diagnostic logging view for Settings UI.
// Values reflect config/runtime.yaml plus optional runtime/state/loopgate_diagnostic_logging.override.json.
// Loopgate applies changes on next process start.
type LoopgateDiagnosticLoggingStatus struct {
	Enabled              bool   `json:"enabled"`
	DefaultLevel         string `json:"default_level"`
	LogDirectoryHostPath string `json:"log_directory_host_path"`
	OverrideActive       bool   `json:"override_active"`
	ConfigLoadError      string `json:"config_load_error,omitempty"`
}

// GetLoopgateDiagnosticLogging returns merged runtime diagnostic logging settings.
func (app *HavenApp) GetLoopgateDiagnosticLogging() LoopgateDiagnosticLoggingStatus {
	overrideActive, err := config.HasDiagnosticLoggingOverride(app.repoRoot)
	if err != nil {
		return LoopgateDiagnosticLoggingStatus{ConfigLoadError: err.Error()}
	}
	runtimeConfig, err := config.LoadRuntimeConfig(app.repoRoot)
	if err != nil {
		return LoopgateDiagnosticLoggingStatus{
			OverrideActive:  overrideActive,
			ConfigLoadError: err.Error(),
		}
	}
	diag := runtimeConfig.Logging.Diagnostic
	logDir := filepath.Join(app.repoRoot, diag.ResolvedDirectory())
	return LoopgateDiagnosticLoggingStatus{
		Enabled:              diag.Enabled,
		DefaultLevel:         diag.DefaultLevel,
		LogDirectoryHostPath: logDir,
		OverrideActive:       overrideActive,
	}
}

// SaveLoopgateDiagnosticLogging writes runtime/state/loopgate_diagnostic_logging.override.json.
// Restart Loopgate to apply.
func (app *HavenApp) SaveLoopgateDiagnosticLogging(enabled bool, defaultLevel string) SaveSettingsResult {
	if err := config.SaveDiagnosticLoggingOverride(app.repoRoot, enabled, defaultLevel); err != nil {
		return SaveSettingsResult{Error: err.Error()}
	}
	return SaveSettingsResult{Success: true}
}

// ClearLoopgateDiagnosticLoggingOverride removes the override file so config/runtime.yaml alone applies.
func (app *HavenApp) ClearLoopgateDiagnosticLoggingOverride() SaveSettingsResult {
	if err := config.RemoveDiagnosticLoggingOverride(app.repoRoot); err != nil {
		return SaveSettingsResult{Error: err.Error()}
	}
	return SaveSettingsResult{Success: true}
}
