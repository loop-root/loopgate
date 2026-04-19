package troubleshoot

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"loopgate/internal/config"
	"loopgate/internal/ledger"
)

func TestExplainApprovalRequest_DeniedApprovalTimeline(t *testing.T) {
	repoRoot := t.TempDir()
	activeAuditPath := ActiveAuditPath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(activeAuditPath), 0o755); err != nil {
		t.Fatalf("mkdir runtime state: %v", err)
	}

	appendApprovalAuditEventForTest(t, activeAuditPath, "2026-04-19T15:00:00Z", "hook.pre_validate", 1, map[string]interface{}{
		"hook_approval_request_id": "approval-123",
		"decision":                 "ask",
		"tool_name":                "Bash",
		"command_redacted_preview": "printf hi",
		"reason":                   "policy requires operator approval",
	})
	appendApprovalAuditEventForTest(t, activeAuditPath, "2026-04-19T15:00:01Z", "approval.created", 2, map[string]interface{}{
		"approval_request_id":      "approval-123",
		"approval_class":           "claude_builtin_inline",
		"tool_name":                "Bash",
		"command_redacted_preview": "printf hi",
		"reason":                   "policy requires operator approval",
	})
	appendApprovalAuditEventForTest(t, activeAuditPath, "2026-04-19T15:00:03Z", "approval.denied", 3, map[string]interface{}{
		"approval_request_id":      "approval-123",
		"approval_class":           "claude_builtin_inline",
		"tool_name":                "Bash",
		"command_redacted_preview": "printf hi",
		"reason":                   "outside allowed change window",
		"denial_code":              "policy_denied",
	})
	appendApprovalAuditEventForTest(t, activeAuditPath, "2026-04-19T15:00:04Z", "approval.denied", 4, map[string]interface{}{
		"approval_request_id": "approval-other",
		"reason":              "should not be matched",
		"denial_code":         "policy_denied",
	})

	explanation, err := ExplainApprovalRequest(repoRoot, config.DefaultRuntimeConfig(), "approval-123")
	if err != nil {
		t.Fatalf("explain approval request: %v", err)
	}
	if explanation.CurrentStatus != "DENIED" {
		t.Fatalf("expected denied status, got %#v", explanation)
	}
	if explanation.ApprovalClass != "claude_builtin_inline" {
		t.Fatalf("expected approval class, got %#v", explanation)
	}
	if explanation.Action != "Bash printf hi" {
		t.Fatalf("expected action summary, got %#v", explanation)
	}
	if explanation.DenialCode != "policy_denied" {
		t.Fatalf("expected denial code, got %#v", explanation)
	}
	if explanation.Reason != "outside allowed change window" {
		t.Fatalf("expected denial reason, got %#v", explanation)
	}
	if len(explanation.Timeline) != 3 {
		t.Fatalf("expected three matching events in timeline, got %#v", explanation.Timeline)
	}
	if explanation.Timeline[0].Status != "ASK" || explanation.Timeline[2].Status != "DENIED" {
		t.Fatalf("unexpected timeline statuses: %#v", explanation.Timeline)
	}

	var rendered bytes.Buffer
	if err := WriteApprovalExplanation(&rendered, explanation); err != nil {
		t.Fatalf("write approval explanation: %v", err)
	}
	renderedText := rendered.String()
	if !strings.Contains(renderedText, "Approval request: approval-123") {
		t.Fatalf("expected approval id in output, got %q", renderedText)
	}
	if !strings.Contains(renderedText, "Current status: DENIED") {
		t.Fatalf("expected denied status in output, got %q", renderedText)
	}
	if !strings.Contains(renderedText, "Denial code: policy_denied") {
		t.Fatalf("expected denial code in output, got %q", renderedText)
	}
	if !strings.Contains(renderedText, "Timeline:") || !strings.Contains(renderedText, "approval.denied") {
		t.Fatalf("expected timeline in output, got %q", renderedText)
	}
}

func TestExplainApprovalRequest_PendingApproval(t *testing.T) {
	repoRoot := t.TempDir()
	activeAuditPath := ActiveAuditPath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(activeAuditPath), 0o755); err != nil {
		t.Fatalf("mkdir runtime state: %v", err)
	}

	appendApprovalAuditEventForTest(t, activeAuditPath, "2026-04-19T16:00:00Z", "approval.created", 1, map[string]interface{}{
		"approval_request_id":      "approval-pending",
		"approval_class":           "operator_review",
		"tool_name":                "Read",
		"resolved_target_path":     "/repo/README.md",
		"reason":                   "operator review required",
		"command_redacted_preview": "",
	})

	explanation, err := ExplainApprovalRequest(repoRoot, config.DefaultRuntimeConfig(), "approval-pending")
	if err != nil {
		t.Fatalf("explain pending approval request: %v", err)
	}
	if explanation.CurrentStatus != "PENDING" {
		t.Fatalf("expected pending status, got %#v", explanation)
	}
	if explanation.DenialCode != "" {
		t.Fatalf("expected no denial code, got %#v", explanation)
	}

	var rendered bytes.Buffer
	if err := WriteApprovalExplanation(&rendered, explanation); err != nil {
		t.Fatalf("write pending approval explanation: %v", err)
	}
	if !strings.Contains(rendered.String(), "not denied") {
		t.Fatalf("expected non-denied note, got %q", rendered.String())
	}
}

func TestExplainApprovalRequest_NotFound(t *testing.T) {
	repoRoot := t.TempDir()
	activeAuditPath := ActiveAuditPath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(activeAuditPath), 0o755); err != nil {
		t.Fatalf("mkdir runtime state: %v", err)
	}
	appendApprovalAuditEventForTest(t, activeAuditPath, "2026-04-19T17:00:00Z", "approval.created", 1, map[string]interface{}{
		"approval_request_id": "approval-other",
		"reason":              "other approval",
	})

	_, err := ExplainApprovalRequest(repoRoot, config.DefaultRuntimeConfig(), "approval-missing")
	if !errors.Is(err, ErrApprovalRequestNotFound) {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

func appendApprovalAuditEventForTest(t *testing.T, activeAuditPath string, timestamp string, eventType string, auditSequence int64, data map[string]interface{}) {
	t.Helper()

	copied := map[string]interface{}{}
	for key, value := range data {
		copied[key] = value
	}
	copied["audit_sequence"] = auditSequence
	if err := ledger.Append(activeAuditPath, ledger.NewEvent(timestamp, eventType, "session-1", copied)); err != nil {
		t.Fatalf("append audit event: %v", err)
	}
}
