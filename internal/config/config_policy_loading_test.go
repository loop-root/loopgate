package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPolicy_StrictRejectsUnknownField(t *testing.T) {
	repoRoot := t.TempDir()
	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rawPolicy := `version: 0.1.0
tools:
  filesystem:
    allowed_roots: ["."]
    denied_paths: []
    read_enabled: true
    write_enabled: true
    write_requires_approval: true
unknown_section:
  enabled: true
`
	writeSignedPolicyForConfigTest(t, repoRoot, rawPolicy)

	_, err := LoadPolicy(repoRoot)
	if err == nil {
		t.Fatal("expected strict decode error for unknown field, got nil")
	}
}

func TestLoadPolicy_ExpandsHomePathPrefixes(t *testing.T) {
	repoRoot := t.TempDir()
	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rawPolicy := `version: 0.1.0
tools:
  filesystem:
    allowed_roots:
      - "~/loopgate/tests"
    denied_paths:
      - "~/loopgate/secret"
    read_enabled: true
    write_enabled: true
    write_requires_approval: true
  http:
    enabled: false
    allowed_domains: []
    requires_approval: true
    timeout_seconds: 10
  shell:
    enabled: false
    allowed_commands: []
    requires_approval: true
logging:
  log_commands: true
  log_tool_calls: true
safety:
  allow_persona_modification: false
  allow_policy_modification: false
`
	writeSignedPolicyForConfigTest(t, repoRoot, rawPolicy)

	policy, err := LoadPolicy(repoRoot)
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home dir: %v", err)
	}
	if len(policy.Tools.Filesystem.AllowedRoots) != 1 {
		t.Fatalf("expected 1 allowed root, got %d", len(policy.Tools.Filesystem.AllowedRoots))
	}
	if !strings.HasPrefix(policy.Tools.Filesystem.AllowedRoots[0], homeDir) {
		t.Fatalf("allowed root not expanded: %q", policy.Tools.Filesystem.AllowedRoots[0])
	}
	if len(policy.Tools.Filesystem.DeniedPaths) != 1 {
		t.Fatalf("expected 1 denied path, got %d", len(policy.Tools.Filesystem.DeniedPaths))
	}
	if !strings.HasPrefix(policy.Tools.Filesystem.DeniedPaths[0], homeDir) {
		t.Fatalf("denied path not expanded: %q", policy.Tools.Filesystem.DeniedPaths[0])
	}
}

func TestLoadPolicy_MissingFileFailsClosed(t *testing.T) {
	repoRoot := t.TempDir()

	_, err := LoadPolicy(repoRoot)
	if err == nil {
		t.Fatal("expected missing repository policy file to fail closed")
	}
	if !strings.Contains(err.Error(), "required policy file not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveRequiredLoadPath_EvalSymlinks(t *testing.T) {
	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "policy.yaml")
	if err := os.WriteFile(targetPath, []byte("version: 0.1.0\n"), 0o600); err != nil {
		t.Fatalf("write target policy: %v", err)
	}

	linkDir := t.TempDir()
	linkPath := filepath.Join(linkDir, "policy.yaml")
	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Fatalf("symlink policy: %v", err)
	}

	resolvedPath, err := resolveRequiredLoadPath(linkPath, "policy file")
	if err != nil {
		t.Fatalf("resolve required load path: %v", err)
	}
	canonicalTargetPath, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		t.Fatalf("eval symlinks on target path: %v", err)
	}
	if resolvedPath != canonicalTargetPath {
		t.Fatalf("expected resolved path %q, got %q", canonicalTargetPath, resolvedPath)
	}
}

func TestLoadPolicy_EmptyFilesystemAllowedRootsFailsClosed(t *testing.T) {
	repoRoot := t.TempDir()
	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rawPolicy := `version: 0.1.0
tools:
  filesystem:
    allowed_roots: []
    denied_paths: []
    read_enabled: true
    write_enabled: false
    write_requires_approval: true
  http:
    enabled: false
    allowed_domains: []
    requires_approval: true
    timeout_seconds: 10
  shell:
    enabled: false
    allowed_commands: []
    requires_approval: true
logging:
  log_commands: true
  log_tool_calls: true
safety:
  allow_persona_modification: false
  allow_policy_modification: false
`
	writeSignedPolicyForConfigTest(t, repoRoot, rawPolicy)

	_, err := LoadPolicy(repoRoot)
	if err == nil {
		t.Fatal("expected empty allowed_roots to fail closed when filesystem is enabled")
	}
	if !strings.Contains(err.Error(), "allowed_roots") {
		t.Fatalf("unexpected error: %v", err)
	}
}
