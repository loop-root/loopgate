package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunExplain_GrepAllowsWithoutApproval(t *testing.T) {
	repoRoot := prepareOperatorTestRepo(t, "balanced")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runExplain([]string{
		"-repo-root", repoRoot,
		"-tool", "Grep",
		"-path", ".",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("runExplain: %v stderr=%s", err, stderr.String())
	}

	output := stdout.String()
	requireExplainOutput(t, output, "decision: allow")
	requireExplainOutput(t, output, "reason_code: policy_allowed")
	requireExplainOutput(t, output, "operator_override.class: repo_read_search")
	if strings.Contains(output, "approval_owner:") {
		t.Fatalf("expected allow output to omit approval owner, got:\n%s", output)
	}
}

func TestRunExplain_WriteShowsHarnessApprovalOptions(t *testing.T) {
	repoRoot := prepareOperatorTestRepo(t, "balanced")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runExplain([]string{
		"-repo-root", repoRoot,
		"-tool", "Write",
		"-path", "README.md",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("runExplain: %v stderr=%s", err, stderr.String())
	}

	output := stdout.String()
	requireExplainOutput(t, output, "decision: ask")
	requireExplainOutput(t, output, "reason_code: approval_required")
	requireExplainOutput(t, output, "approval_owner: harness")
	requireExplainOutput(t, output, "approval_options: once,session")
	requireExplainOutput(t, output, "operator_override.class: repo_write_safe")
	requireExplainOutput(t, output, "operator_override.max_delegation: session")
	requireExplainOutput(t, output, "operator_override.maximum_grant_scope: session")
}

func TestRunExplain_BashShowsHarnessApprovalOptions(t *testing.T) {
	repoRoot := prepareOperatorTestRepo(t, "balanced")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runExplain([]string{
		"-repo-root", repoRoot,
		"-tool", "Bash",
		"-command", "git status --short",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("runExplain: %v stderr=%s", err, stderr.String())
	}

	output := stdout.String()
	requireExplainOutput(t, output, "decision: ask")
	requireExplainOutput(t, output, "reason_code: approval_required")
	requireExplainOutput(t, output, "approval_owner: harness")
	requireExplainOutput(t, output, "approval_options: once,session")
	requireExplainOutput(t, output, "operator_override.class: repo_bash_safe")
}

func TestRunExplain_DeniedPathHardBlocks(t *testing.T) {
	repoRoot := prepareOperatorTestRepo(t, "balanced")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runExplain([]string{
		"-repo-root", repoRoot,
		"-tool", "Write",
		"-path", "core/policy/policy.yaml",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("runExplain: %v stderr=%s", err, stderr.String())
	}

	output := stdout.String()
	requireExplainOutput(t, output, "decision: block")
	requireExplainOutput(t, output, "reason_code: policy_denied")
	requireExplainOutput(t, output, "denial_code: policy_denied")
	if strings.Contains(output, "approval_owner:") {
		t.Fatalf("expected hard deny output to omit approval owner, got:\n%s", output)
	}
}

func TestRunExplain_RequiresToolSpecificInput(t *testing.T) {
	repoRoot := prepareOperatorTestRepo(t, "balanced")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runExplain([]string{
		"-repo-root", repoRoot,
		"-tool", "Bash",
	}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "-command is required for Bash") {
		t.Fatalf("expected missing command error, got %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
}

func requireExplainOutput(t *testing.T, output string, want string) {
	t.Helper()
	if !strings.Contains(output, want) {
		t.Fatalf("expected output to contain %q, got:\n%s", want, output)
	}
}
