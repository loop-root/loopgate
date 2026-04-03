package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// LoadJSONConfig reads a JSON config file from {stateDir}/{section}.json
// and unmarshals it into T.
func LoadJSONConfig[T any](stateDir, section string) (T, error) {
	var zero T
	path := filepath.Join(stateDir, section+".json")
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		return zero, err
	}
	var result T
	if err := json.Unmarshal(rawBytes, &result); err != nil {
		return zero, fmt.Errorf("decode %s JSON config: %w", section, err)
	}
	return result, nil
}

// SaveJSONConfig writes config as indented JSON to {stateDir}/{section}.json
// using atomic write (temp file + rename).
func SaveJSONConfig[T any](stateDir, section string, config T) error {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("create config state dir: %w", err)
	}
	destPath := filepath.Join(stateDir, section+".json")
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s config: %w", section, err)
	}
	data = append(data, '\n')

	tmpFile, err := os.CreateTemp(stateDir, section+"-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp file for %s config: %w", section, err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write %s config: %w", section, err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close %s config: %w", section, err)
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename %s config: %w", section, err)
	}
	return nil
}

// LoadOrSeed tries to load a JSON config from the state directory.
// Loopgate runtime and goal aliases use YAML on disk instead; this remains for policy, morphling_classes, connections, and small local toggles.
// If the JSON file doesn't exist, it falls back to loading from a YAML seed file.
// If the YAML seed also doesn't exist, it uses the provided default function.
// When seeding from YAML or defaults, the result is persisted as JSON.
func LoadOrSeed[T any](stateDir, section, yamlSeedPath string, yamlLoader func(string) (T, error), defaultFn func() T) (T, error) {
	// Try JSON state first.
	result, err := LoadJSONConfig[T](stateDir, section)
	if err == nil {
		return result, nil
	}
	if !os.IsNotExist(err) {
		return result, fmt.Errorf("load %s JSON state: %w", section, err)
	}

	// Try YAML seed — check file exists before calling loader.
	if yamlSeedPath != "" {
		if _, statErr := os.Stat(yamlSeedPath); statErr == nil {
			result, err = yamlLoader(yamlSeedPath)
			if err != nil {
				return result, fmt.Errorf("load %s YAML seed: %w", section, err)
			}
			// Persist as JSON for future loads.
			if saveErr := SaveJSONConfig(stateDir, section, result); saveErr != nil {
				return result, fmt.Errorf("seed %s JSON state: %w", section, saveErr)
			}
			return result, nil
		}
	}

	// Use defaults.
	if defaultFn == nil {
		var zero T
		return zero, fmt.Errorf("no %s config found and no default provided", section)
	}
	result = defaultFn()
	if saveErr := SaveJSONConfig(stateDir, section, result); saveErr != nil {
		return result, fmt.Errorf("seed %s JSON state from defaults: %w", section, saveErr)
	}
	return result, nil
}
