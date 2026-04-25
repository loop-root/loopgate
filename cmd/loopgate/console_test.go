package main

import (
	"bytes"
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

func TestPrintConsoleSnapshot_RendersOperatorGrantsAndDecisionSummary(t *testing.T) {
	snapshot := consoleSnapshot{
		FetchedAtUTC:  "2026-04-24T12:00:00Z",
		AuditVerified: true,
		OperatorGrants: consoleOperatorGrantStatus{
			Present:           true,
			SignatureKeyID:    "test-key",
			ContentSHA256:     "0123456789abcdef9999",
			ActiveGrantCount:  2,
			RevokedGrantCount: 1,
			ActiveByClass: map[string]int{
				"repo_edit_safe":   1,
				"repo_read_search": 1,
			},
		},
		DecisionSummary: consoleDecisionSummary{
			Allow: 18,
			Ask:   3,
			Block: 1,
		},
		Status: operatorStatusReport{
			OperatorMode: "source-checkout",
			DaemonMode:   "foreground-or-manual",
			SocketPath:   "/tmp/loopgate.sock",
			Policy: operatorPolicyStatus{
				Profile:        "strict",
				SignatureKeyID: "test-key",
			},
		},
	}

	var output bytes.Buffer
	printConsoleSnapshot(&output, snapshot, false)
	rendered := output.String()
	for _, expected := range []string{
		"Operator Grants",
		"operator_policy: signed key_id=test-key sha256=0123456789abcdef",
		"active_grants: 2",
		"revoked_grants: 1",
		"class.repo_edit_safe: 1",
		"class.repo_read_search: 1",
		"Recent Decisions",
		"allow: 18",
		"ask: 3",
		"block: 1",
	} {
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

func TestSummarizeConsoleDecisions_CountsAllowAskBlock(t *testing.T) {
	summary := summarizeConsoleDecisions([]consoleAuditEvent{
		{Decision: "allow"},
		{Decision: "ASK"},
		{Decision: " block "},
		{Decision: "deny"},
		{Status: "success"},
	})

	if summary.Allow != 1 || summary.Ask != 1 || summary.Block != 1 {
		t.Fatalf("expected allow/ask/block counts, got %#v", summary)
	}
}

func TestPrintConsoleHelp_IsReadOnly(t *testing.T) {
	var output bytes.Buffer
	printConsoleHelp(&output)
	rendered := output.String()

	for _, expected := range []string{"refresh | r", "help | h", "quit | q"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected console help to contain %q, got %q", expected, rendered)
		}
	}
	for _, forbidden := range []string{"approve", "deny"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("expected read-only console help to omit %q, got %q", forbidden, rendered)
		}
	}
}
