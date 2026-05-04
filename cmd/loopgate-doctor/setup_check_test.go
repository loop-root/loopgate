package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"loopgate/internal/config"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"loopgate/internal/testutil"
)

func TestRunSetupCheck_PrintsHumanReadableReadiness(t *testing.T) {
	repoRoot := t.TempDir()
	policySigner, err := testutil.NewPolicyTestSigner()
	if err != nil {
		t.Fatalf("new policy test signer: %v", err)
	}
	policySigner.ConfigureEnv(t.Setenv)
	preset, err := config.ResolvePolicyTemplatePreset("balanced")
	if err != nil {
		t.Fatalf("resolve policy preset: %v", err)
	}
	if err := policySigner.WriteSignedPolicyYAML(repoRoot, preset.TemplateYAML); err != nil {
		t.Fatalf("write signed policy yaml: %v", err)
	}

	claudeDir := filepath.Join(repoRoot, "claude-home")
	hooksDir := filepath.Join(claudeDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("mkdir hooks dir: %v", err)
	}
	for _, scriptName := range setupCheckLoopgateHookScripts {
		if err := os.WriteFile(filepath.Join(hooksDir, scriptName), []byte(scriptName), 0o755); err != nil {
			t.Fatalf("write hook script %s: %v", scriptName, err)
		}
	}
	settingsPath := filepath.Join(claudeDir, "settings.json")
	settingsJSON := `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"python3 hooks/loopgate_pretool.py"}]}],"PostToolUse":[{"hooks":[{"type":"command","command":"python3 hooks/loopgate_posttool.py"}]}],"PostToolUseFailure":[{"hooks":[{"type":"command","command":"python3 hooks/loopgate_posttoolfailure.py"}]}],"SessionStart":[{"hooks":[{"type":"command","command":"python3 hooks/loopgate_sessionstart.py"}]}],"SessionEnd":[{"hooks":[{"type":"command","command":"python3 hooks/loopgate_sessionend.py"}]}],"UserPromptSubmit":[{"hooks":[{"type":"command","command":"python3 hooks/loopgate_userpromptsubmit.py"}]}],"PermissionRequest":[{"hooks":[{"type":"command","command":"python3 hooks/loopgate_permissionrequest.py"}]}]}}`
	if err := os.WriteFile(settingsPath, []byte(settingsJSON), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	socketPath := filepath.Join(repoRoot, "runtime", "state", "loopgate.sock")
	calledSocketPath := ""
	previousCheckLoopgateHealth := checkLoopgateHealth
	checkLoopgateHealth = func(actualSocketPath string) (controlapipkg.HealthResponse, error) {
		calledSocketPath = actualSocketPath
		return controlapipkg.HealthResponse{OK: true, Version: "test"}, nil
	}
	t.Cleanup(func() {
		checkLoopgateHealth = previousCheckLoopgateHealth
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"setup-check", "-repo", repoRoot, "-claude-dir", claudeDir, "-socket", socketPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected setup-check success, got exit code %d stderr=%s", exitCode, stderr.String())
	}

	output := stdout.String()
	if !strings.Contains(output, "Loopgate setup check") {
		t.Fatalf("expected heading in output, got %q", output)
	}
	if !strings.Contains(output, "policy: ok profile=balanced") {
		t.Fatalf("expected signed balanced policy status, got %q", output)
	}
	if !strings.Contains(output, "operator_overrides: not present") {
		t.Fatalf("expected missing operator overrides to be non-fatal, got %q", output)
	}
	if !strings.Contains(output, "daemon: healthy version=test") {
		t.Fatalf("expected healthy daemon status, got %q", output)
	}
	if !strings.Contains(output, "claude_hooks: installed installed=true configured_entries=7 copied_scripts=8") {
		t.Fatalf("expected installed Claude hook status, got %q", output)
	}
	if !strings.Contains(output, "repo search: allow reason_code=policy_allowed") {
		t.Fatalf("expected repo search sample allow, got %q", output)
	}
	if !strings.Contains(output, "repo write: ask reason_code=approval_required approval_owner=harness") {
		t.Fatalf("expected repo write sample to be harness-owned ask, got %q", output)
	}
	if strings.Contains(output, "next_steps:") {
		t.Fatalf("expected no next steps for ready setup, got %q", output)
	}
	if calledSocketPath != socketPath {
		t.Fatalf("expected setup-check to use socket path %q, got %q", socketPath, calledSocketPath)
	}
}

func TestRunSetupCheck_PrintsNextStepsForMissingSetup(t *testing.T) {
	repoRoot := t.TempDir()
	claudeDir := filepath.Join(repoRoot, "claude-home")

	previousCheckLoopgateHealth := checkLoopgateHealth
	checkLoopgateHealth = func(string) (controlapipkg.HealthResponse, error) {
		return controlapipkg.HealthResponse{}, os.ErrNotExist
	}
	t.Cleanup(func() {
		checkLoopgateHealth = previousCheckLoopgateHealth
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"setup-check", "-repo", repoRoot, "-claude-dir", claudeDir}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected setup-check success, got exit code %d stderr=%s", exitCode, stderr.String())
	}

	output := stdout.String()
	for _, expected := range []string{
		"policy: error:",
		"daemon: offline:",
		"claude_hooks: missing installed=false",
		"create and sign a root policy",
		"install Claude Code hooks",
		"start Loopgate",
		"rerun after policy errors are fixed",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected output to contain %q, got %q", expected, output)
		}
	}
}

func TestRunSetupCheck_JSONIncludesMachineReadableReadiness(t *testing.T) {
	repoRoot := t.TempDir()
	policySigner, err := testutil.NewPolicyTestSigner()
	if err != nil {
		t.Fatalf("new policy test signer: %v", err)
	}
	policySigner.ConfigureEnv(t.Setenv)
	preset, err := config.ResolvePolicyTemplatePreset("balanced")
	if err != nil {
		t.Fatalf("resolve policy preset: %v", err)
	}
	if err := policySigner.WriteSignedPolicyYAML(repoRoot, preset.TemplateYAML); err != nil {
		t.Fatalf("write signed policy yaml: %v", err)
	}

	claudeDir := filepath.Join(repoRoot, "claude-home")
	socketPath := filepath.Join(repoRoot, "runtime", "state", "loopgate.sock")
	previousCheckLoopgateHealth := checkLoopgateHealth
	checkLoopgateHealth = func(actualSocketPath string) (controlapipkg.HealthResponse, error) {
		if actualSocketPath != socketPath {
			t.Fatalf("expected socket path %q, got %q", socketPath, actualSocketPath)
		}
		return controlapipkg.HealthResponse{OK: false, Version: "test"}, nil
	}
	t.Cleanup(func() {
		checkLoopgateHealth = previousCheckLoopgateHealth
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"setup-check", "-repo", repoRoot, "-claude-dir", claudeDir, "-socket", socketPath, "-json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected setup-check json success, got exit code %d stderr=%s", exitCode, stderr.String())
	}

	var report setupCheckReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode setup-check json: %v\nstdout=%s", err, stdout.String())
	}
	if report.RepoRoot != repoRoot {
		t.Fatalf("expected repo root %q, got %#v", repoRoot, report)
	}
	if !report.Policy.Loaded || report.Policy.Profile != "balanced" {
		t.Fatalf("expected loaded balanced policy, got %#v", report.Policy)
	}
	if report.Daemon.Healthy {
		t.Fatalf("expected unhealthy daemon projection, got %#v", report.Daemon)
	}
	if report.ClaudeHooks.State != "missing" || report.ClaudeHooks.Installed {
		t.Fatalf("expected missing hook projection, got %#v", report.ClaudeHooks)
	}
	if len(report.SampleDecisions) != 3 {
		t.Fatalf("expected three sample decisions, got %#v", report.SampleDecisions)
	}
	if report.SampleDecisions[0].Label != "repo search" || report.SampleDecisions[0].Decision != "allow" {
		t.Fatalf("expected repo search allow sample, got %#v", report.SampleDecisions[0])
	}
	if len(report.NextSteps) == 0 {
		t.Fatalf("expected next steps for missing hooks/unhealthy daemon, got %#v", report)
	}
}

func TestResolveSocketPath_PrefersEnvOverRepoDefault(t *testing.T) {
	t.Setenv("LOOPGATE_SOCKET", "/tmp/loopgate-env.sock")
	resolvedSocketPath := resolveSocketPath("/repo/root", "")
	if resolvedSocketPath != filepath.Clean("/tmp/loopgate-env.sock") {
		t.Fatalf("expected env socket path, got %q", resolvedSocketPath)
	}
}
