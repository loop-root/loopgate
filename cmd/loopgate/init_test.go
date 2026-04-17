package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"loopgate/internal/config"
)

func TestRunInit_InitializesPolicySigningSetup(t *testing.T) {
	repoRoot := writeLoopgateInitTestRepo(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv(policySigningTrustDirEnv, filepath.Join(t.TempDir(), "trusted"))

	var stdout bytes.Buffer
	if err := runInit([]string{"-repo-root", repoRoot, "-key-id", "local-operator-test"}, &stdout, &stdout); err != nil {
		t.Fatalf("run init: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 init output lines, got %d: %q", len(lines), stdout.String())
	}
	if lines[0] != "key_id: local-operator-test" {
		t.Fatalf("unexpected key_id line: %q", lines[0])
	}
	if lines[2] != "next_command: go run ./cmd/loopgate" {
		t.Fatalf("unexpected next command line: %q", lines[2])
	}

	runtimeStateDir := filepath.Join(repoRoot, "runtime", "state")
	runtimeStateInfo, err := os.Stat(runtimeStateDir)
	if err != nil {
		t.Fatalf("stat runtime state dir: %v", err)
	}
	if runtimeStateInfo.Mode().Perm() != 0o700 {
		t.Fatalf("expected runtime state dir permissions 0700, got %04o", runtimeStateInfo.Mode().Perm())
	}

	privateKeyPath, err := defaultOperatorPolicySigningPrivateKeyPath("local-operator-test")
	if err != nil {
		t.Fatalf("default private key path: %v", err)
	}
	privateKeyInfo, err := os.Stat(privateKeyPath)
	if err != nil {
		t.Fatalf("stat private key: %v", err)
	}
	if privateKeyInfo.Mode().Perm() != 0o600 {
		t.Fatalf("expected private key permissions 0600, got %04o", privateKeyInfo.Mode().Perm())
	}

	if _, err := config.VerifyPolicySigningSetup(repoRoot, privateKeyPath, "local-operator-test"); err != nil {
		t.Fatalf("verify policy signing setup: %v", err)
	}
	if _, err := config.LoadPolicy(repoRoot); err != nil {
		t.Fatalf("load signed policy: %v", err)
	}
}

func TestRunInit_IsIdempotent(t *testing.T) {
	repoRoot := writeLoopgateInitTestRepo(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv(policySigningTrustDirEnv, filepath.Join(t.TempDir(), "trusted"))

	if err := runInit([]string{"-repo-root", repoRoot, "-key-id", "local-operator-test"}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("first init: %v", err)
	}

	privateKeyPath, err := defaultOperatorPolicySigningPrivateKeyPath("local-operator-test")
	if err != nil {
		t.Fatalf("default private key path: %v", err)
	}
	firstPrivateKeyBytes, err := os.ReadFile(privateKeyPath)
	if err != nil {
		t.Fatalf("read initial private key: %v", err)
	}

	var stdout bytes.Buffer
	if err := runInit([]string{"-repo-root", repoRoot, "-key-id", "local-operator-test"}, &stdout, &stdout); err != nil {
		t.Fatalf("second init: %v", err)
	}
	if strings.TrimSpace(stdout.String()) != "already initialized" {
		t.Fatalf("expected already initialized output, got %q", stdout.String())
	}

	secondPrivateKeyBytes, err := os.ReadFile(privateKeyPath)
	if err != nil {
		t.Fatalf("read second private key: %v", err)
	}
	if !bytes.Equal(firstPrivateKeyBytes, secondPrivateKeyBytes) {
		t.Fatal("idempotent init unexpectedly rotated the private key")
	}
}

func TestRunInit_ForceRotatesKeyMaterial(t *testing.T) {
	repoRoot := writeLoopgateInitTestRepo(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv(policySigningTrustDirEnv, filepath.Join(t.TempDir(), "trusted"))

	if err := runInit([]string{"-repo-root", repoRoot, "-key-id", "local-operator-test"}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("first init: %v", err)
	}

	privateKeyPath, err := defaultOperatorPolicySigningPrivateKeyPath("local-operator-test")
	if err != nil {
		t.Fatalf("default private key path: %v", err)
	}
	firstPrivateKeyBytes, err := os.ReadFile(privateKeyPath)
	if err != nil {
		t.Fatalf("read initial private key: %v", err)
	}

	if err := runInit([]string{"-repo-root", repoRoot, "-key-id", "local-operator-test", "-force"}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("force init: %v", err)
	}

	secondPrivateKeyBytes, err := os.ReadFile(privateKeyPath)
	if err != nil {
		t.Fatalf("read rotated private key: %v", err)
	}
	if bytes.Equal(firstPrivateKeyBytes, secondPrivateKeyBytes) {
		t.Fatal("force init did not rotate the private key")
	}

	backupMatches, err := filepath.Glob(privateKeyPath + ".bak*")
	if err != nil {
		t.Fatalf("glob private key backups: %v", err)
	}
	if len(backupMatches) == 0 {
		t.Fatalf("expected backup key matching %s.bak*", privateKeyPath)
	}

	if _, err := config.VerifyPolicySigningSetup(repoRoot, privateKeyPath, "local-operator-test"); err != nil {
		t.Fatalf("verify rotated policy signing setup: %v", err)
	}
}

func TestResolveLoopgateRepoRoot_PrefersLoopgateEnvOverLegacyMorphEnv(t *testing.T) {
	loopgateRepoRoot := filepath.Join(t.TempDir(), "loopgate-root")
	t.Setenv(loopgateRepoRootEnv, loopgateRepoRoot)

	resolvedRepoRoot, err := resolveLoopgateRepoRoot("")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	if resolvedRepoRoot != filepath.Clean(loopgateRepoRoot) {
		t.Fatalf("expected LOOPGATE_REPO_ROOT to win, got %q", resolvedRepoRoot)
	}
	t.Setenv("MORPH_REPO_ROOT", filepath.Join(t.TempDir(), "legacy-root"))
	resolvedRepoRoot, err = resolveLoopgateRepoRoot("")
	if err != nil {
		t.Fatalf("resolve repo root with legacy morph env present: %v", err)
	}
	if resolvedRepoRoot != filepath.Clean(loopgateRepoRoot) {
		t.Fatalf("expected LOOPGATE_REPO_ROOT to remain authoritative, got %q", resolvedRepoRoot)
	}
}

func TestResolveLoopgateRepoRoot_IgnoresLegacyMorphEnv(t *testing.T) {
	t.Setenv("MORPH_REPO_ROOT", filepath.Join(t.TempDir(), "legacy-root"))

	resolvedRepoRoot := resolveLoopgateRepoRootEnv()
	if resolvedRepoRoot != "" {
		t.Fatalf("expected MORPH_REPO_ROOT to be ignored, got %q", resolvedRepoRoot)
	}
}

func writeLoopgateInitTestRepo(t *testing.T) string {
	t.Helper()

	repoRoot := t.TempDir()
	policyBytes, err := os.ReadFile(filepath.Join("..", "..", "core", "policy", "policy.yaml"))
	if err != nil {
		t.Fatalf("read fixture policy yaml: %v", err)
	}

	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir policy dir: %v", err)
	}
	if err := os.WriteFile(policyPath, policyBytes, 0o600); err != nil {
		t.Fatalf("write fixture policy yaml: %v", err)
	}
	return repoRoot
}
