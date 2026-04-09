package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteRuntimeConfigYAMLRoundTrip(t *testing.T) {
	repoRoot := t.TempDir()
	rc := DefaultRuntimeConfig()
	rc.Logging.Diagnostic.Enabled = true
	rc.Logging.Diagnostic.DefaultLevel = "debug"
	if err := WriteRuntimeConfigYAML(repoRoot, rc); err != nil {
		t.Fatalf("WriteRuntimeConfigYAML: %v", err)
	}
	loaded, err := LoadRuntimeConfig(repoRoot)
	if err != nil {
		t.Fatalf("LoadRuntimeConfig after write: %v", err)
	}
	if !loaded.Logging.Diagnostic.Enabled {
		t.Fatal("expected diagnostic enabled after round trip")
	}
	if loaded.Memory.Backend != DefaultMemoryBackend {
		t.Fatalf("expected backend %q after round trip, got %q", DefaultMemoryBackend, loaded.Memory.Backend)
	}
	if loaded.Logging.Diagnostic.DefaultLevel != "debug" {
		t.Fatalf("expected default_level debug, got %q", loaded.Logging.Diagnostic.DefaultLevel)
	}
	staleDir := filepath.Join(repoRoot, "runtime", "state", "config")
	if err := os.MkdirAll(staleDir, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(staleDir, "runtime.json")
	if err := os.WriteFile(stale, []byte(`{"version":"bogus"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteRuntimeConfigYAML(repoRoot, loaded); err != nil {
		t.Fatalf("second write: %v", err)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Fatal("expected stale runtime.json removed after WriteRuntimeConfigYAML")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat stale json: %v", err)
	}
}

func TestWritePolicyYAMLRoundTrip(t *testing.T) {
	repoRoot := t.TempDir()
	policy := Policy{}
	policy.Tools.Filesystem.ReadEnabled = true
	policy.Tools.Filesystem.WriteEnabled = true
	policy.Tools.Filesystem.WriteRequiresApproval = true
	policy.Tools.Filesystem.AllowedRoots = []string{"."}
	policy.Tools.Filesystem.DeniedPaths = []string{"runtime/state"}
	if err := WritePolicyYAML(repoRoot, policy); err != nil {
		t.Fatalf("WritePolicyYAML: %v", err)
	}
	loaded, err := LoadPolicy(repoRoot)
	if err != nil {
		t.Fatalf("LoadPolicy after write: %v", err)
	}
	if !loaded.Tools.Filesystem.WriteRequiresApproval {
		t.Fatal("expected write_requires_approval true after round trip")
	}
	if len(loaded.Tools.Filesystem.AllowedRoots) != 1 {
		t.Fatalf("unexpected allowed roots: %#v", loaded.Tools.Filesystem.AllowedRoots)
	}
	staleDir := filepath.Join(repoRoot, "runtime", "state", "config")
	if err := os.MkdirAll(staleDir, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(staleDir, "policy.json")
	if err := os.WriteFile(stale, []byte(`{"version":"0.1.0","tools":{"filesystem":{"write_requires_approval":false}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WritePolicyYAML(repoRoot, loaded); err != nil {
		t.Fatalf("second policy write: %v", err)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Fatal("expected stale policy.json removed after WritePolicyYAML")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat stale policy json: %v", err)
	}
}

func TestWriteGoalAliasesYAMLRoundTrip(t *testing.T) {
	repoRoot := t.TempDir()
	ga := DefaultGoalAliases()
	ga.Aliases["workflow_followup"] = []string{"carry_forward"}
	if err := WriteGoalAliasesYAML(repoRoot, ga); err != nil {
		t.Fatalf("WriteGoalAliasesYAML: %v", err)
	}
	loaded, err := LoadGoalAliases(repoRoot)
	if err != nil {
		t.Fatalf("LoadGoalAliases: %v", err)
	}
	if len(loaded.Aliases["workflow_followup"]) != 1 || loaded.Aliases["workflow_followup"][0] != "carry_forward" {
		t.Fatalf("unexpected aliases: %#v", loaded.Aliases["workflow_followup"])
	}
}
