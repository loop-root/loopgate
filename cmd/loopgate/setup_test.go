package main

import (
	"bufio"
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
	editPolicy, ok := loadedPolicy.ClaudeCodeToolPolicy("Edit")
	if !ok {
		t.Fatal("expected Edit tool policy in balanced profile")
	}
	if editPolicy.RequiresApproval == nil || *editPolicy.RequiresApproval {
		t.Fatalf("expected balanced Edit policy to avoid approval, got %#v", editPolicy.RequiresApproval)
	}
	writePolicy, ok := loadedPolicy.ClaudeCodeToolPolicy("Write")
	if !ok {
		t.Fatal("expected Write tool policy in balanced profile")
	}
	if writePolicy.RequiresApproval == nil || !*writePolicy.RequiresApproval {
		t.Fatalf("expected balanced Write policy to require approval, got %#v", writePolicy.RequiresApproval)
	}
	bashPolicy, ok := loadedPolicy.ClaudeCodeToolPolicy("Bash")
	if !ok {
		t.Fatal("expected Bash tool policy in balanced profile")
	}
	if bashPolicy.RequiresApproval == nil || !*bashPolicy.RequiresApproval {
		t.Fatalf("expected balanced Bash policy to require approval, got %#v", bashPolicy.RequiresApproval)
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
	if !strings.Contains(stdout.String(), "operator_mode: source-checkout") {
		t.Fatalf("expected source-checkout operator mode in setup output, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "profile: balanced") {
		t.Fatalf("expected profile output, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "readiness_state: daemon-start-required") {
		t.Fatalf("expected daemon-start-required readiness state, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "python3_path:") {
		t.Fatalf("expected python3 path in setup output, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "audit_ledger_path:") {
		t.Fatalf("expected audit ledger path in setup output, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "next_steps:") {
		t.Fatalf("expected next_steps block in setup output, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Start Loopgate with ./bin/loopgate") {
		t.Fatalf("expected daemon start guidance in setup output, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "./bin/loopgate status") {
		t.Fatalf("expected status command hint in setup output, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "./bin/loopgate test") {
		t.Fatalf("expected test command hint in setup output, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "verify:") {
		t.Fatalf("expected verification hints in setup output, got %q", stdout.String())
	}
}

func TestRunSetup_ManagedInstallRootUsesBareCommandHints(t *testing.T) {
	repoRoot := makeSetupTestRepo(t)
	claudeDir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(policySigningTrustDirEnv, filepath.Join(t.TempDir(), "trusted"))
	if err := os.WriteFile(filepath.Join(repoRoot, managedInstallRootMarkerFilename), []byte("version=test\n"), 0o600); err != nil {
		t.Fatalf("write managed install marker: %v", err)
	}

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
	if !strings.Contains(stdout.String(), "operator_mode: managed-install") {
		t.Fatalf("expected managed-install operator mode in setup output, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "readiness_state: daemon-start-required") {
		t.Fatalf("expected daemon-start-required readiness state in managed install output, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "loopgate status") || strings.Contains(stdout.String(), "./bin/loopgate status") {
		t.Fatalf("expected bare status command hint in managed install output, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Start Loopgate with loopgate") || strings.Contains(stdout.String(), "Start Loopgate with ./bin/loopgate") {
		t.Fatalf("expected bare daemon start guidance in managed install output, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "loopgate-ledger tail -verbose") || strings.Contains(stdout.String(), "./bin/loopgate-ledger tail -verbose") {
		t.Fatalf("expected bare ledger command hint in managed install output, got %q", stdout.String())
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

func TestRunSetup_RejectsDeveloperProfileInGuidedSetup(t *testing.T) {
	repoRoot := makeSetupTestRepo(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv(policySigningTrustDirEnv, filepath.Join(t.TempDir(), "trusted"))

	err := runSetup([]string{
		"-repo-root", repoRoot,
		"-profile", "developer",
		"-skip-hooks",
		"-skip-launch-agent",
	}, strings.NewReader(""), io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected developer profile to be rejected in guided setup")
	}
	if !strings.Contains(err.Error(), "supported: strict, balanced, read-only") {
		t.Fatalf("expected supported setup profiles in error, got %v", err)
	}
}

func TestRunSetup_InstallHooksRequiresPython3(t *testing.T) {
	repoRoot := makeSetupTestRepo(t)
	claudeDir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", "")
	t.Setenv(policySigningTrustDirEnv, filepath.Join(t.TempDir(), "trusted"))

	err := runSetup([]string{
		"-repo-root", repoRoot,
		"-profile", "balanced",
		"-install-hooks",
		"-skip-launch-agent",
		"-claude-dir", claudeDir,
	}, strings.NewReader(""), io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected missing python3 to fail setup")
	}
	if !strings.Contains(err.Error(), "python3 on PATH") {
		t.Fatalf("expected python3 prerequisite error, got %v", err)
	}
}

func TestRunQuickstart_UsesRecommendedDefaultsNonInteractive(t *testing.T) {
	repoRoot := makeSetupTestRepo(t)
	claudeDir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(policySigningTrustDirEnv, filepath.Join(t.TempDir(), "trusted"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runQuickstart([]string{
		"-repo-root", repoRoot,
		"-claude-dir", claudeDir,
		"-skip-launch-agent",
	}, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("runQuickstart: %v stderr=%s", err, stderr.String())
	}

	loadedPolicy, err := config.LoadPolicy(repoRoot)
	if err != nil {
		t.Fatalf("LoadPolicy after quickstart: %v", err)
	}
	if !loadedPolicy.Tools.Shell.Enabled {
		t.Fatal("expected quickstart to apply the balanced profile")
	}
	if loadedPolicy.Tools.HTTP.Enabled {
		t.Fatal("expected quickstart balanced profile to keep HTTP disabled")
	}

	for _, scriptName := range loopgateHookBundleFiles {
		scriptPath := filepath.Join(claudeDir, claudeHooksDirname, scriptName)
		if _, err := os.Stat(scriptPath); err != nil {
			t.Fatalf("stat installed hook %s: %v", scriptPath, err)
		}
	}
	if !strings.Contains(stdout.String(), "profile: balanced") {
		t.Fatalf("expected balanced profile output, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "claude_hooks_installed: true") {
		t.Fatalf("expected quickstart to install hooks, got %q", stdout.String())
	}
}

func TestPromptForPolicyTemplatePreset_ShowsProfileDetails(t *testing.T) {
	var stdout bytes.Buffer
	preset, err := promptForPolicyTemplatePreset(bufio.NewReader(strings.NewReader("\n")), &stdout)
	if err != nil {
		t.Fatalf("promptForPolicyTemplatePreset: %v", err)
	}
	if preset.Name != "balanced" {
		t.Fatalf("expected default preset balanced, got %q", preset.Name)
	}
	renderedOutput := stdout.String()
	if !strings.Contains(renderedOutput, "balanced (recommended)") {
		t.Fatalf("expected recommended profile in prompt output, got %q", renderedOutput)
	}
	if !strings.Contains(renderedOutput, "read-only") {
		t.Fatalf("expected read-only profile in prompt output, got %q", renderedOutput)
	}
	if !strings.Contains(renderedOutput, "Approval required:") {
		t.Fatalf("expected approval details in prompt output, got %q", renderedOutput)
	}
	if !strings.Contains(renderedOutput, "Hard blocks:") {
		t.Fatalf("expected hard block details in prompt output, got %q", renderedOutput)
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
