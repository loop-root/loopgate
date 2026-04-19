package troubleshoot

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"loopgate/internal/config"
)

func TestExplainCapabilityRequest_DirectDenial(t *testing.T) {
	repoRoot := t.TempDir()
	activeAuditPath := ActiveAuditPath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(activeAuditPath), 0o755); err != nil {
		t.Fatalf("mkdir runtime state: %v", err)
	}

	appendApprovalAuditEventForTest(t, activeAuditPath, "2026-04-19T19:00:00Z", "capability.requested", 1, map[string]interface{}{
		"request_id":           "req-denied",
		"capability":           "fs_write",
		"resolved_target_path": "/repo/core/policy/policy.yaml",
		"reason":               "policy denied write",
	})
	appendApprovalAuditEventForTest(t, activeAuditPath, "2026-04-19T19:00:01Z", "capability.denied", 2, map[string]interface{}{
		"request_id":           "req-denied",
		"capability":           "fs_write",
		"resolved_target_path": "/repo/core/policy/policy.yaml",
		"reason":               "path denied by policy",
		"denial_code":          "policy_denied",
	})

	explanation, err := ExplainCapabilityRequest(repoRoot, config.DefaultRuntimeConfig(), "req-denied")
	if err != nil {
		t.Fatalf("explain capability request: %v", err)
	}
	if explanation.CurrentStatus != "DENIED" {
		t.Fatalf("expected denied status, got %#v", explanation)
	}
	if explanation.Capability != "fs_write" {
		t.Fatalf("expected capability, got %#v", explanation)
	}
	if explanation.DenialCode != "policy_denied" {
		t.Fatalf("expected denial code, got %#v", explanation)
	}
	if explanation.Reason != "path denied by policy" {
		t.Fatalf("expected denial reason, got %#v", explanation)
	}
	if len(explanation.Timeline) != 2 {
		t.Fatalf("expected request timeline, got %#v", explanation.Timeline)
	}

	var rendered bytes.Buffer
	if err := WriteCapabilityRequestExplanation(&rendered, explanation); err != nil {
		t.Fatalf("write capability request explanation: %v", err)
	}
	renderedText := rendered.String()
	if !strings.Contains(renderedText, "Request: req-denied") {
		t.Fatalf("expected request id in output, got %q", renderedText)
	}
	if !strings.Contains(renderedText, "Current status: DENIED") {
		t.Fatalf("expected denied status in output, got %q", renderedText)
	}
	if !strings.Contains(renderedText, "Denial code: policy_denied") {
		t.Fatalf("expected denial code in output, got %q", renderedText)
	}
}

func TestExplainCapabilityRequest_ExecutionFailure(t *testing.T) {
	repoRoot := t.TempDir()
	activeAuditPath := ActiveAuditPath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(activeAuditPath), 0o755); err != nil {
		t.Fatalf("mkdir runtime state: %v", err)
	}

	appendApprovalAuditEventForTest(t, activeAuditPath, "2026-04-19T19:05:00Z", "capability.requested", 1, map[string]interface{}{
		"request_id": "req-error",
		"capability": "fs_write",
		"reason":     "policy allowed request",
	})
	appendApprovalAuditEventForTest(t, activeAuditPath, "2026-04-19T19:05:02Z", "capability.error", 2, map[string]interface{}{
		"request_id":           "req-error",
		"capability":           "fs_write",
		"error":                "path denied after canonical resolution",
		"operator_error_class": "path_policy_denied",
	})

	explanation, err := ExplainCapabilityRequest(repoRoot, config.DefaultRuntimeConfig(), "req-error")
	if err != nil {
		t.Fatalf("explain capability request error: %v", err)
	}
	if explanation.CurrentStatus != "ERROR" {
		t.Fatalf("expected error status, got %#v", explanation)
	}
	if explanation.OperatorErrorClass != "path_policy_denied" {
		t.Fatalf("expected operator error class, got %#v", explanation)
	}
	if explanation.Reason != "path denied after canonical resolution" {
		t.Fatalf("expected failure reason, got %#v", explanation)
	}
}

func TestExplainCapabilityRequest_PendingApprovalReferral(t *testing.T) {
	repoRoot := t.TempDir()
	activeAuditPath := ActiveAuditPath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(activeAuditPath), 0o755); err != nil {
		t.Fatalf("mkdir runtime state: %v", err)
	}

	appendApprovalAuditEventForTest(t, activeAuditPath, "2026-04-19T19:10:00Z", "capability.requested", 1, map[string]interface{}{
		"request_id": "req-approval",
		"capability": "bash.exec",
		"reason":     "approval required",
	})
	appendApprovalAuditEventForTest(t, activeAuditPath, "2026-04-19T19:10:01Z", "approval.created", 2, map[string]interface{}{
		"request_id":          "req-approval",
		"approval_request_id": "approval-456",
		"approval_class":      "operator_review",
		"capability":          "bash.exec",
		"reason":              "approval required",
	})

	explanation, err := ExplainCapabilityRequest(repoRoot, config.DefaultRuntimeConfig(), "req-approval")
	if err != nil {
		t.Fatalf("explain request with approval: %v", err)
	}
	if explanation.CurrentStatus != "PENDING_APPROVAL" {
		t.Fatalf("expected pending approval status, got %#v", explanation)
	}
	if explanation.ApprovalRequestID != "approval-456" {
		t.Fatalf("expected approval request id, got %#v", explanation)
	}

	var rendered bytes.Buffer
	if err := WriteCapabilityRequestExplanation(&rendered, explanation); err != nil {
		t.Fatalf("write capability request explanation: %v", err)
	}
	if !strings.Contains(rendered.String(), "Next step: use loopgate-doctor explain-denial -approval-id approval-456") {
		t.Fatalf("expected approval referral, got %q", rendered.String())
	}
}

func TestExplainCapabilityRequest_NotFound(t *testing.T) {
	repoRoot := t.TempDir()
	activeAuditPath := ActiveAuditPath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(activeAuditPath), 0o755); err != nil {
		t.Fatalf("mkdir runtime state: %v", err)
	}
	appendApprovalAuditEventForTest(t, activeAuditPath, "2026-04-19T19:15:00Z", "capability.requested", 1, map[string]interface{}{
		"request_id": "req-other",
		"capability": "fs_read",
	})

	_, err := ExplainCapabilityRequest(repoRoot, config.DefaultRuntimeConfig(), "req-missing")
	if !errors.Is(err, ErrCapabilityRequestNotFound) {
		t.Fatalf("expected not-found error, got %v", err)
	}
}
