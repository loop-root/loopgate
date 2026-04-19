package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"loopgate/internal/config"
)

func TestRunSetup_AppliesBalancedProfileAndInstallsHooks(t *testing.T) {
	repoRoot := makeSetupTestRepo(t)
	claudeDir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(policySigningTrustDirEnv, filepath.Join(t.TempDir(), "trusted"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runSetup([]string{
		"-repo-root", repoRoot,
		"-profile", "balanced",
		"-install-hooks",
		"-skip-launch-agent",
		"-claude-dir", claudeDir,
	}, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("runSetup: %v stderr=%s", err, stderr.String())
	}

	loadedPolicy, err := config.LoadPolicy(repoRoot)
	if err != nil {
		t.Fatalf("LoadPolicy after setup: %v", err)
	}
	if !loadedPolicy.Tools.Shell.Enabled {
		t.Fatal("expected balanced profile to enable shell")
	}
	if loadedPolicy.Tools.HTTP.Enabled {
		t.Fatal("expected balanced profile to keep HTTP disabled")
	}

	for _, scriptName := range loopgateHookBundleFiles {
		scriptPath := filepath.Join(claudeDir, claudeHooksDirname, scriptName)
		if _, err := os.Stat(scriptPath); err != nil {
			t.Fatalf("stat installed hook %s: %v", scriptPath, err)
		}
	}
	if !strings.Contains(stdout.String(), "setup OK") {
		t.Fatalf("expected setup OK output, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "profile: balanced") {
		t.Fatalf("expected profile output, got %q", stdout.String())
	}
}

func TestRunSetup_RequiresExplicitChoicesWhenNonInteractive(t *testing.T) {
	repoRoot := makeSetupTestRepo(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv(policySigningTrustDirEnv, filepath.Join(t.TempDir(), "trusted"))

	err := runSetup([]string{"-repo-root", repoRoot}, strings.NewReader(""), io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected non-interactive setup without explicit choices to fail")
	}
	if !strings.Contains(err.Error(), "-profile or -yes") {
		t.Fatalf("expected missing-choice error, got %v", err)
	}
}

func makeSetupTestRepo(t *testing.T) string {
	t.Helper()
	repoRoot := makeTestHookRepo(t)

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
