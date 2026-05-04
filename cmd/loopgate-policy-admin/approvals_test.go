package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"loopgate/internal/loopgate"
	controlapipkg "loopgate/internal/loopgate/controlapi"
)

func TestRunApprovalsList_PrintsPendingApprovals(t *testing.T) {
	repoRoot := t.TempDir()
	signerFixture := newTestPolicySignerFixture(t)
	signerFixture.writeSignedPolicy(t, repoRoot, mustPolicyPresetTemplate(t, "strict"))

	socketPath := newTempSocketPath(t)
	_ = startPolicyAdminTestServer(t, repoRoot, socketPath)

	requestClient := loopgate.NewClient(socketPath)
	requestClient.ConfigureSession("approval-requester", "approval-requester-session", []string{"fs_write"})
	pendingResponse, err := requestClient.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-policy-admin-list",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "pending.txt",
			"content": "hello",
		},
	})
	if err != nil {
		t.Fatalf("execute pending approval: %v", err)
	}
	if !pendingResponse.ApprovalRequired {
		t.Fatalf("expected approval required response, got %#v", pendingResponse)
	}
	time.Sleep(600 * time.Millisecond)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"approvals", "list", "-repo", repoRoot, "-socket", socketPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}

	output := stdout.String()
	for _, expected := range []string{"APPROVAL ID", pendingResponse.ApprovalRequestID, "approval-requester", "fs_write"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected approvals list output to contain %q, got %q", expected, output)
		}
	}
}

func TestRunApprovalsApprove_CompletesApprovalAndWritesAuditReason(t *testing.T) {
	repoRoot := t.TempDir()
	signerFixture := newTestPolicySignerFixture(t)
	signerFixture.writeSignedPolicy(t, repoRoot, mustPolicyPresetTemplate(t, "strict"))

	socketPath := newTempSocketPath(t)
	workspaceRoot := startPolicyAdminTestServer(t, repoRoot, socketPath)

	requestClient := loopgate.NewClient(socketPath)
	requestClient.ConfigureSession("approval-requester", "approval-requester-session", []string{"fs_write"})
	pendingResponse, err := requestClient.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-policy-admin-approve",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "approved.txt",
			"content": "hello from approval",
		},
	})
	if err != nil {
		t.Fatalf("execute pending approval: %v", err)
	}
	if !pendingResponse.ApprovalRequired {
		t.Fatalf("expected approval required response, got %#v", pendingResponse)
	}
	time.Sleep(600 * time.Millisecond)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"approvals", "approve", pendingResponse.ApprovalRequestID, "-repo", repoRoot, "-socket", socketPath, "-reason", "ship it"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}

	writtenBytes, err := os.ReadFile(filepath.Join(workspaceRoot, "approved.txt"))
	if err != nil {
		t.Fatalf("read approved file: %v", err)
	}
	if string(writtenBytes) != "hello from approval" {
		t.Fatalf("unexpected approved file contents: %q", string(writtenBytes))
	}

	grantedEvent := readLastAuditEventOfType(t, repoRoot, "approval.granted")
	if got := grantedEvent.Data["operator_reason"]; got != "ship it" {
		t.Fatalf("expected operator_reason %q, got %#v", "ship it", got)
	}
	grantedEventHash, _ := grantedEvent.Data["event_hash"].(string)
	expectedOutput := "approval " + pendingResponse.ApprovalRequestID + " approved audit_event_hash=" + grantedEventHash
	if strings.TrimSpace(stdout.String()) != expectedOutput {
		t.Fatalf("unexpected approve output: %q", stdout.String())
	}
}

func TestRunApprovalsDeny_RecordsAuditReason(t *testing.T) {
	repoRoot := t.TempDir()
	signerFixture := newTestPolicySignerFixture(t)
	signerFixture.writeSignedPolicy(t, repoRoot, mustPolicyPresetTemplate(t, "strict"))

	socketPath := newTempSocketPath(t)
	workspaceRoot := startPolicyAdminTestServer(t, repoRoot, socketPath)

	requestClient := loopgate.NewClient(socketPath)
	requestClient.ConfigureSession("approval-requester", "approval-requester-session", []string{"fs_write"})
	pendingResponse, err := requestClient.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-policy-admin-deny",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "denied.txt",
			"content": "hello from denied approval",
		},
	})
	if err != nil {
		t.Fatalf("execute pending approval: %v", err)
	}
	if !pendingResponse.ApprovalRequired {
		t.Fatalf("expected approval required response, got %#v", pendingResponse)
	}
	time.Sleep(600 * time.Millisecond)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"approvals", "deny", pendingResponse.ApprovalRequestID, "-repo", repoRoot, "-socket", socketPath, "-reason", "not safe yet"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}

	if _, err := os.Stat(filepath.Join(workspaceRoot, "denied.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected denied file to stay absent, stat err=%v", err)
	}

	deniedEvent := readLastAuditEventOfType(t, repoRoot, "approval.denied")
	if got := deniedEvent.Data["operator_reason"]; got != "not safe yet" {
		t.Fatalf("expected operator_reason %q, got %#v", "not safe yet", got)
	}
	deniedEventHash, _ := deniedEvent.Data["event_hash"].(string)
	expectedOutput := "approval " + pendingResponse.ApprovalRequestID + " denied audit_event_hash=" + deniedEventHash
	if strings.TrimSpace(stdout.String()) != expectedOutput {
		t.Fatalf("unexpected deny output: %q", stdout.String())
	}
}
