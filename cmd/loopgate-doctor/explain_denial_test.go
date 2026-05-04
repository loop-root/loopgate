package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"loopgate/internal/config"
)

func TestRunExplainDenial_PrintsDeniedApprovalSummary(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfig := config.DefaultRuntimeConfig()
	if err := config.WriteRuntimeConfigYAML(repoRoot, runtimeConfig); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	activeAuditPath := filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl")
	if err := os.MkdirAll(filepath.Dir(activeAuditPath), 0o755); err != nil {
		t.Fatalf("mkdir runtime state: %v", err)
	}
	appendDoctorAuditEventForTest(t, activeAuditPath, "2026-04-19T18:00:00Z", "approval.created", 1, map[string]interface{}{
		"approval_request_id":      "approval-cli",
		"approval_class":           "claude_builtin_inline",
		"tool_name":                "Bash",
		"command_redacted_preview": "git status",
		"reason":                   "policy requires operator approval",
	})
	appendDoctorAuditEventForTest(t, activeAuditPath, "2026-04-19T18:00:02Z", "approval.denied", 2, map[string]interface{}{
		"approval_request_id":      "approval-cli",
		"approval_class":           "claude_builtin_inline",
		"tool_name":                "Bash",
		"command_redacted_preview": "git status",
		"reason":                   "outside allowed change window",
		"denial_code":              "policy_denied",
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"explain-denial", "-repo", repoRoot, "-approval-id", "approval-cli"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected explain-denial success, got exit code %d stderr=%s", exitCode, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "Approval request: approval-cli") {
		t.Fatalf("expected approval id in output, got %q", output)
	}
	if !strings.Contains(output, "Current status: DENIED") {
		t.Fatalf("expected denied status in output, got %q", output)
	}
	if !strings.Contains(output, "Denial code: policy_denied") {
		t.Fatalf("expected denial code in output, got %q", output)
	}
	if !strings.Contains(output, "Timeline:") {
		t.Fatalf("expected timeline in output, got %q", output)
	}
}

func TestRunExplainDenial_RequiresApprovalID(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"explain-denial"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("expected flag usage exit code 2, got %d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "exactly one of -approval-id, -request-id, or -hook-session-id is required") {
		t.Fatalf("expected missing approval id error, got %q", stderr.String())
	}
}

func TestRunExplainDenial_RejectsBothIdentifiers(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"explain-denial", "-approval-id", "approval-1", "-request-id", "req-1"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("expected flag usage exit code 2, got %d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "exactly one of -approval-id, -request-id, or -hook-session-id is required") {
		t.Fatalf("expected exclusive flag error, got %q", stderr.String())
	}
}

func TestRunExplainDenial_HookFiltersRequireHookSessionID(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"explain-denial", "-tool-use-id", "toolu-1"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("expected flag usage exit code 2, got %d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "require -hook-session-id") {
		t.Fatalf("expected hook filter dependency error, got %q", stderr.String())
	}
}

func TestRunExplainDenial_NotFound(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfig := config.DefaultRuntimeConfig()
	if err := config.WriteRuntimeConfigYAML(repoRoot, runtimeConfig); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	activeAuditPath := filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl")
	if err := os.MkdirAll(filepath.Dir(activeAuditPath), 0o755); err != nil {
		t.Fatalf("mkdir runtime state: %v", err)
	}
	appendDoctorAuditEventForTest(t, activeAuditPath, "2026-04-19T18:05:00Z", "approval.created", 1, map[string]interface{}{
		"approval_request_id": "approval-other",
		"reason":              "other approval",
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"explain-denial", "-repo", repoRoot, "-approval-id", "approval-missing"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("expected explain-denial failure, got exit code %d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "approval request not found") {
		t.Fatalf("expected not-found error, got %q", stderr.String())
	}
}

func TestRunExplainDenial_RequestID_PrintsRequestSummary(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfig := config.DefaultRuntimeConfig()
	if err := config.WriteRuntimeConfigYAML(repoRoot, runtimeConfig); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	activeAuditPath := filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl")
	if err := os.MkdirAll(filepath.Dir(activeAuditPath), 0o755); err != nil {
		t.Fatalf("mkdir runtime state: %v", err)
	}
	appendDoctorAuditEventForTest(t, activeAuditPath, "2026-04-19T18:10:00Z", "capability.requested", 1, map[string]interface{}{
		"request_id":           "req-denied",
		"capability":           "fs_write",
		"resolved_target_path": "/repo/core/policy/policy.yaml",
		"reason":               "policy denied write",
	})
	appendDoctorAuditEventForTest(t, activeAuditPath, "2026-04-19T18:10:01Z", "capability.denied", 2, map[string]interface{}{
		"request_id":           "req-denied",
		"capability":           "fs_write",
		"resolved_target_path": "/repo/core/policy/policy.yaml",
		"reason":               "path denied by policy",
		"denial_code":          "policy_denied",
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"explain-denial", "-repo", repoRoot, "-request-id", "req-denied"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected explain-denial request success, got exit code %d stderr=%s", exitCode, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "Request: req-denied") {
		t.Fatalf("expected request id in output, got %q", output)
	}
	if !strings.Contains(output, "Current status: DENIED") {
		t.Fatalf("expected denied status in output, got %q", output)
	}
	if !strings.Contains(output, "Denial code: policy_denied") {
		t.Fatalf("expected denial code in output, got %q", output)
	}
}

func TestRunExplainDenial_HookSession_PrintsHookSummary(t *testing.T) {
	repoRoot := t.TempDir()
	runtimeConfig := config.DefaultRuntimeConfig()
	if err := config.WriteRuntimeConfigYAML(repoRoot, runtimeConfig); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	activeAuditPath := filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl")
	if err := os.MkdirAll(filepath.Dir(activeAuditPath), 0o755); err != nil {
		t.Fatalf("mkdir runtime state: %v", err)
	}
	appendDoctorAuditEventForSessionTest(t, activeAuditPath, "session-hook", "2026-04-19T18:15:00Z", "hook.pre_validate", 1, map[string]interface{}{
		"decision":           "block",
		"hook_event_name":    "PreToolUse",
		"tool_use_id":        "toolu-denied",
		"tool_name":          "Bash",
		"hook_surface_class": "primary_authority",
		"denial_code":        "policy_denied",
		"reason":             "tool not in governance map — denied by default",
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"explain-denial", "-repo", repoRoot, "-hook-session-id", "session-hook", "-tool-use-id", "toolu-denied"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected explain-denial hook success, got exit code %d stderr=%s", exitCode, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "Hook session: session-hook") {
		t.Fatalf("expected hook session in output, got %q", output)
	}
	if !strings.Contains(output, "Tool use id: toolu-denied") {
		t.Fatalf("expected tool use id in output, got %q", output)
	}
	if !strings.Contains(output, "Denial code: policy_denied") {
		t.Fatalf("expected denial code in output, got %q", output)
	}
}
