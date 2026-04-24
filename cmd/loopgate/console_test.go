package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"loopgate/internal/ledger"
	controlapipkg "loopgate/internal/loopgate/controlapi"
)

func TestPrintConsoleSnapshot_HidesEventsWhenAuditVerificationFails(t *testing.T) {
	snapshot := consoleSnapshot{
		FetchedAtUTC:     "2026-04-24T12:00:00Z",
		AuditVerifyError: "verify audit ledger: tampered chain",
		Status: operatorStatusReport{
			OperatorMode:    "source-checkout",
			DaemonMode:      "offline",
			SocketPath:      "/tmp/loopgate.sock",
			AuditLedgerPath: "/tmp/loopgate_events.jsonl",
			Policy: operatorPolicyStatus{
				Profile:        "balanced",
				SignatureKeyID: "test-key",
			},
			ClaudeHooks: operatorHooksStatus{
				State:             "installed",
				ManagedEventCount: 7,
				CopiedScriptCount: 7,
			},
		},
		RecentAuditEvents: []consoleAuditEvent{{
			Type:      "capability.executed",
			RequestID: "should-not-render",
			Summary:   "should-not-render",
		}},
	}

	var output bytes.Buffer
	printConsoleSnapshot(&output, snapshot, false)
	rendered := output.String()
	if !strings.Contains(rendered, "unavailable: verify audit ledger: tampered chain") {
		t.Fatalf("expected audit verification failure, got %q", rendered)
	}
	if strings.Contains(rendered, "should-not-render") {
		t.Fatalf("expected untrusted recent events to be hidden, got %q", rendered)
	}
}

func TestPrintConsoleSnapshot_RendersPendingApprovals(t *testing.T) {
	snapshot := consoleSnapshot{
		FetchedAtUTC:  "2026-04-24T12:00:00Z",
		AuditVerified: true,
		Status: operatorStatusReport{
			OperatorMode: "source-checkout",
			DaemonMode:   "foreground-or-manual",
			SocketPath:   "/tmp/loopgate.sock",
			Policy: operatorPolicyStatus{
				Profile:        "strict",
				SignatureKeyID: "test-key",
			},
		},
		Approvals: []controlapipkg.OperatorApprovalSummary{{
			UIApprovalSummary: controlapipkg.UIApprovalSummary{
				ApprovalRequestID: "approval-123",
				Requester:         "operator",
				Capability:        "fs_write",
				Path:              "README.md",
				ExpiresAtUTC:      "2026-04-24T12:05:00Z",
			},
		}},
	}

	var output bytes.Buffer
	printConsoleSnapshot(&output, snapshot, false)
	rendered := output.String()
	for _, expected := range []string{"approval-123", "fs_write", "README.md"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected rendered console to contain %q, got %q", expected, rendered)
		}
	}
}

func TestSummarizeConsoleAuditEvent_UsesRequestAndEventHash(t *testing.T) {
	summary := summarizeConsoleAuditEvent(ledger.Event{
		TS:      "2026-04-24T12:00:00Z",
		Type:    "capability.executed",
		Session: "session-1",
		Data: map[string]interface{}{
			"request_id": "request-1",
			"capability": "fs_list",
			"status":     "success",
			"event_hash": "0123456789abcdef9999",
		},
	})

	if summary.RequestID != "request-1" {
		t.Fatalf("expected request id, got %#v", summary)
	}
	if summary.EventHashPrefix != "0123456789abcdef" {
		t.Fatalf("expected event hash prefix, got %#v", summary)
	}
	if summary.Summary != "fs_list request_id=request-1" {
		t.Fatalf("expected concise summary, got %#v", summary)
	}
}

func TestRunConsoleApprovalDecisionRequiresReason(t *testing.T) {
	var output bytes.Buffer
	err := runConsoleApprovalDecision(context.Background(), &output, "/tmp/missing.sock", "approval-1", true, "")
	if err == nil {
		t.Fatal("expected missing approval reason to fail before opening a session")
	}
	if !strings.Contains(err.Error(), "approval reason is required") {
		t.Fatalf("expected reason error, got %v", err)
	}
}
