package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// WriteRuntimeConfigYAML writes config/runtime.yaml atomically after defaults and validation.
// It removes runtime/state/config/runtime.json if present so operators are not confused by a stale frozen copy.
func WriteRuntimeConfigYAML(repoRoot string, runtimeConfig RuntimeConfig) error {
	applyRuntimeConfigDefaults(&runtimeConfig)
	if err := validateRuntimeConfig(repoRoot, runtimeConfig); err != nil {
		return fmt.Errorf("validate runtime config: %w", err)
	}
	destPath := filepath.Join(repoRoot, "config", "runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(&runtimeConfig); err != nil {
		return fmt.Errorf("marshal runtime yaml: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("close yaml encoder: %w", err)
	}
	if err := atomicWriteFile(destPath, buf.Bytes(), 0o600); err != nil {
		return err
	}
	staleJSON := filepath.Join(repoRoot, "runtime", "state", "config", "runtime.json")
	if rmErr := os.Remove(staleJSON); rmErr != nil && !os.IsNotExist(rmErr) {
		return fmt.Errorf("remove stale %s: %w", staleJSON, rmErr)
	}
	return nil
}

// WritePolicyYAML writes core/policy/policy.yaml atomically after defaults and validation.
// Callers must write a matching core/policy/policy.yaml.sig before restarting Loopgate;
// unsigned policy updates fail closed at startup.
// It removes runtime/state/config/policy.json if present so a stale frozen copy cannot override
// the repository policy on the next startup.
func WritePolicyYAML(repoRoot string, policy Policy) error {
	if err := applyPolicyDefaults(&policy); err != nil {
		return fmt.Errorf("validate policy: %w", err)
	}
	destPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create policy dir: %w", err)
	}
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(&policy); err != nil {
		return fmt.Errorf("marshal policy yaml: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("close yaml encoder: %w", err)
	}
	if err := atomicWriteFile(destPath, buf.Bytes(), 0o600); err != nil {
		return err
	}
	staleJSON := filepath.Join(repoRoot, "runtime", "state", "config", "policy.json")
	if rmErr := os.Remove(staleJSON); rmErr != nil && !os.IsNotExist(rmErr) {
		return fmt.Errorf("remove stale %s: %w", staleJSON, rmErr)
	}
	return nil
}

// WriteGoalAliasesYAML writes config/goal_aliases.yaml atomically after validation.
// It removes runtime/state/config/goal_aliases.json if present.
func WriteGoalAliasesYAML(repoRoot string, goalAliases GoalAliases) error {
	if err := validateGoalAliases(goalAliases); err != nil {
		return fmt.Errorf("validate goal aliases: %w", err)
	}
	destPath := filepath.Join(repoRoot, "config", "goal_aliases.yaml")
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(&goalAliases); err != nil {
		return fmt.Errorf("marshal goal_aliases yaml: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("close yaml encoder: %w", err)
	}
	if err := atomicWriteFile(destPath, buf.Bytes(), 0o600); err != nil {
		return err
	}
	staleJSON := filepath.Join(repoRoot, "runtime", "state", "config", "goal_aliases.json")
	if rmErr := os.Remove(staleJSON); rmErr != nil && !os.IsNotExist(rmErr) {
		return fmt.Errorf("remove stale %s: %w", staleJSON, rmErr)
	}
	return nil
}

func atomicWriteFile(destPath string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(destPath)
	tmpFile, err := os.CreateTemp(dir, ".runtime-config-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	_, writeErr := tmpFile.Write(data)
	closeErr := tmpFile.Close()
	if writeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp: %w", writeErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp: %w", closeErr)
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename to %s: %w", destPath, err)
	}
	return nil
}
