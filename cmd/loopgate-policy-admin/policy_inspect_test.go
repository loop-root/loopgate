package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunValidate_ValidatesSignedRepoPolicy(t *testing.T) {
	repoRoot := t.TempDir()
	writeSignedPolicyFixture(t, repoRoot, mustPolicyPresetTemplate(t, "strict"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"validate", "-repo", repoRoot}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "policy validation OK") {
		t.Fatalf("expected validation success output, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "signature_verified: true") {
		t.Fatalf("expected signature verification output, got %q", stdout.String())
	}
}

func TestRunValidate_ValidatesUnsignedPolicyFile(t *testing.T) {
	repoRoot := t.TempDir()
	policyPath := filepath.Join(repoRoot, "policy.yaml")
	if err := os.WriteFile(policyPath, []byte(mustPolicyPresetTemplate(t, "strict")), 0o600); err != nil {
		t.Fatalf("write unsigned policy: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"validate", "-repo", repoRoot, "-policy-file", "policy.yaml"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "signature_verified: false") {
		t.Fatalf("expected unsigned validation output, got %q", stdout.String())
	}
}

func TestRunExplain_PrintsToolExplanation(t *testing.T) {
	repoRoot := t.TempDir()
	writeSignedPolicyFixture(t, repoRoot, mustPolicyPresetTemplate(t, "developer"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"explain", "-repo", repoRoot, "-tool", "Bash"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "[Bash]") {
		t.Fatalf("expected Bash section, got %q", output)
	}
	if !strings.Contains(output, "base_policy: approval_required (tools.shell.requires_approval=true)") {
		t.Fatalf("expected base policy explanation, got %q", output)
	}
	if !strings.Contains(output, "operator_override.class: repo_bash_safe") {
		t.Fatalf("expected operator override class in explanation, got %q", output)
	}
	if !strings.Contains(output, "operator_override.max_delegation: persistent") {
		t.Fatalf("expected operator override delegation in explanation, got %q", output)
	}
	if !strings.Contains(output, "operator_override.maximum_grant_scope: permanent") {
		t.Fatalf("expected operator grant scope in explanation, got %q", output)
	}
	if !strings.Contains(output, "tool_policy.allowed_command_prefixes: ls, pwd, find, grep, cat, sed -n, head, tail, wc, sort, git status, git diff, go test, rg") {
		t.Fatalf("expected command prefixes in explanation, got %q", output)
	}
}

func TestRunRenderTemplate_RendersPreset(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"render-template", "-preset", "strict"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "deny_unknown_tools: true") {
		t.Fatalf("expected strict template output, got %q", output)
	}
	if !strings.Contains(output, "enabled: false") {
		t.Fatalf("expected strict template to disable at least one tool, got %q", output)
	}
}

func TestRunRenderTemplate_RendersBalancedPreset(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"render-template", "-preset", "balanced"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "enabled: true") {
		t.Fatalf("expected balanced template to enable at least one guarded tool, got %q", output)
	}
	if !strings.Contains(output, "timeout_seconds: 10") {
		t.Fatalf("expected balanced template to retain explicit HTTP timeout, got %q", output)
	}
}

func TestRunRenderTemplate_RendersReadOnlyPreset(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"render-template", "-preset", "read-only"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "shell:\n    enabled: false") {
		t.Fatalf("expected read-only template to disable shell, got %q", output)
	}
	if !strings.Contains(output, "Edit:\n        enabled: false") {
		t.Fatalf("expected read-only template to disable Claude Edit, got %q", output)
	}
}

func TestRunExplain_RejectsUnsupportedToolName(t *testing.T) {
	repoRoot := t.TempDir()
	writeSignedPolicyFixture(t, repoRoot, mustPolicyPresetTemplate(t, "strict"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"explain", "-repo", repoRoot, "-tool", "NotARealTool"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "unsupported Claude Code tool") {
		t.Fatalf("expected unsupported tool error, got %q", stderr.String())
	}
}

func TestRunDiff_PrintsNormalizedPolicyDifferences(t *testing.T) {
	repoRoot := t.TempDir()
	writeSignedPolicyFixture(t, repoRoot, mustPolicyPresetTemplate(t, "strict"))

	rightPolicyPath := filepath.Join(repoRoot, "developer-policy.yaml")
	if err := os.WriteFile(rightPolicyPath, []byte(mustPolicyPresetTemplate(t, "developer")), 0o600); err != nil {
		t.Fatalf("write right policy: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"diff", "-repo", repoRoot, "-right-policy-file", "developer-policy.yaml"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "normalized_policy_diff:") {
		t.Fatalf("expected diff header, got %q", output)
	}
	if !strings.Contains(output, "comparison_mode: normalized_effective_policy") {
		t.Fatalf("expected explicit comparison mode, got %q", output)
	}
	if !strings.Contains(output, "comparison_note: not a literal line-by-line source diff") {
		t.Fatalf("expected explicit comparison note, got %q", output)
	}
	if !strings.Contains(output, "tools.claude_code.tool_policies.Bash.enabled: false => true") {
		t.Fatalf("expected Bash enabled diff, got %q", output)
	}
	if !strings.Contains(output, "tools.http.enabled: false => true") {
		t.Fatalf("expected http enabled diff, got %q", output)
	}
}

func TestRunDiff_PrintsNoDiffForEquivalentPolicies(t *testing.T) {
	repoRoot := t.TempDir()
	writeSignedPolicyFixture(t, repoRoot, mustPolicyPresetTemplate(t, "strict"))

	rightPolicyPath := filepath.Join(repoRoot, "strict-copy.yaml")
	if err := os.WriteFile(rightPolicyPath, []byte(mustPolicyPresetTemplate(t, "strict")), 0o600); err != nil {
		t.Fatalf("write right policy: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"diff", "-repo", repoRoot, "-right-policy-file", "strict-copy.yaml"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "normalized_policy_diff: (none)") {
		t.Fatalf("expected no diff output, got %q", stdout.String())
	}
}
