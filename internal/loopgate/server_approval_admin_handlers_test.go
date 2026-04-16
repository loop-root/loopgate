package loopgate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestControlApprovalsApproveExecutesPendingRequest(t *testing.T) {
	repoRoot := newShortLoopgateTestRepoRoot(t)
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))
	pinTestProcessAsExpectedClient(t, server)

	requestClient := NewClient(server.socketPath)
	requestClient.ConfigureSession("approval-requester", "approval-requester-session", []string{"fs_write"})
	pendingResponse, err := requestClient.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-control-approvals-approve",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "approved-via-control-route.txt",
			"content": "hello from control approvals",
		},
	})
	if err != nil {
		t.Fatalf("execute pending approval: %v", err)
	}
	if !pendingResponse.ApprovalRequired {
		t.Fatalf("expected pending approval response, got %#v", pendingResponse)
	}

	adminClient := NewClient(server.socketPath)
	adminClient.ConfigureSession("loopgate-policy-admin", "policy-admin-approvals", []string{controlCapabilityApprovalRead, controlCapabilityApprovalWrite})
	approvalResponse, err := adminClient.ListPendingApprovals(context.Background())
	if err != nil {
		t.Fatalf("list pending approvals: %v", err)
	}
	if len(approvalResponse.Approvals) != 1 || approvalResponse.Approvals[0].ApprovalRequestID != pendingResponse.ApprovalRequestID {
		t.Fatalf("unexpected pending approvals: %#v", approvalResponse)
	}

	decisionResponse, err := adminClient.DecidePendingApproval(context.Background(), pendingResponse.ApprovalRequestID, true, "ship it")
	if err != nil {
		t.Fatalf("approve pending approval: %v", err)
	}
	if decisionResponse.Status != ResponseStatusSuccess {
		t.Fatalf("expected success response, got %#v", decisionResponse)
	}
	if decisionResponse.AuditEventHash == "" {
		t.Fatalf("expected audit event hash, got %#v", decisionResponse)
	}

	writtenBytes, err := os.ReadFile(filepath.Join(server.sandboxPaths.Home, "approved-via-control-route.txt"))
	if err != nil {
		entries, _ := os.ReadDir(repoRoot)
		entryNames := make([]string, 0, len(entries))
		for _, entry := range entries {
			entryNames = append(entryNames, entry.Name())
		}
		t.Fatalf("read approved file: %v response=%#v repo_entries=%v", err, decisionResponse, entryNames)
	}
	if string(writtenBytes) != "hello from control approvals" {
		t.Fatalf("unexpected approved file content: %q", string(writtenBytes))
	}
}
