package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const shellDevOverrideFileName = "loopgate_shell_dev.override.json"

type shellDevOverrideFile struct {
	Enabled bool `json:"enabled"`
}

func shellDevOverridePath(repoRoot string) string {
	return filepath.Join(repoRoot, "runtime", "state", shellDevOverrideFileName)
}

// IsShellDevModeEnabled reports whether shell_exec is enabled for operator sessions.
// Defaults to false (hidden from the model) when the override file is absent.
// The tool remains registered in the capability registry — only the operator
// path's access to it is gated here.
func IsShellDevModeEnabled(repoRoot string) (bool, error) {
	raw, err := os.ReadFile(shellDevOverridePath(repoRoot))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read shell dev override: %w", err)
	}
	var o shellDevOverrideFile
	if err := json.Unmarshal(raw, &o); err != nil {
		return false, fmt.Errorf("parse shell dev override: %w", err)
	}
	return o.Enabled, nil
}

// SaveShellDevOverride writes the override file. Takes effect immediately for
// new operator requests (no Loopgate restart required).
func SaveShellDevOverride(repoRoot string, enabled bool) error {
	stateDir := filepath.Join(repoRoot, "runtime", "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("create runtime state dir: %w", err)
	}
	raw, err := json.MarshalIndent(shellDevOverrideFile{Enabled: enabled}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal shell dev override: %w", err)
	}
	if err := os.WriteFile(shellDevOverridePath(repoRoot), raw, 0o600); err != nil {
		return fmt.Errorf("write shell dev override: %w", err)
	}
	return nil
}

// RemoveShellDevOverride deletes the override file, restoring the safe default (disabled).
func RemoveShellDevOverride(repoRoot string) error {
	if err := os.Remove(shellDevOverridePath(repoRoot)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove shell dev override: %w", err)
	}
	return nil
}
