package loopgate

import (
	"context"
	"errors"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"loopgate/internal/ledger"
)

func TestExecuteCapabilityRequest_DeniesNeedsApprovalWhenApprovalCreationDisabledWithoutApprovedExecution(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	server.mu.Lock()
	baseToken := server.sessionState.tokens[client.capabilityToken]
	server.mu.Unlock()

	response := server.executeCapabilityRequest(context.Background(), baseToken, controlapipkg.CapabilityRequest{
		RequestID:  "req-no-approval-bypass",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "blocked.txt",
			"content": "approval bypass should fail closed",
		},
	}, false)
	if response.Status != controlapipkg.ResponseStatusDenied || response.DenialCode != controlapipkg.DenialCodeApprovalRequired || !response.ApprovalRequired {
		t.Fatalf("expected approval-required denial without execution, got %#v", response)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "blocked.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected blocked file to remain unwritten, stat err=%v", err)
	}

	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.approvalState.records) != 0 {
		t.Fatalf("expected no pending approvals to be created on fail-closed path, got %#v", server.approvalState.records)
	}
}

func TestExecuteCapabilityRequest_ApprovalRollbackIsNeverVisibleToReaders(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	server.mu.Lock()
	baseToken := server.sessionState.tokens[client.capabilityToken]
	server.mu.Unlock()

	auditStarted := make(chan struct{}, 1)
	releaseAudit := make(chan struct{})
	originalAppendAuditEvent := server.appendAuditEvent
	server.appendAuditEvent = func(ledgerPath string, auditEvent ledger.Event) error {
		if auditEvent.Type == "approval.created" {
			select {
			case auditStarted <- struct{}{}:
			default:
			}
			<-releaseAudit
			return context.DeadlineExceeded
		}
		return originalAppendAuditEvent(ledgerPath, auditEvent)
	}

	stopReader := make(chan struct{})
	var sawPendingApproval atomic.Bool
	go func() {
		for {
			select {
			case <-stopReader:
				return
			default:
			}
			server.mu.Lock()
			if len(server.approvalState.records) > 0 {
				sawPendingApproval.Store(true)
			}
			server.mu.Unlock()
			time.Sleep(time.Millisecond)
		}
	}()

	responseCh := make(chan controlapipkg.CapabilityResponse, 1)
	go func() {
		responseCh <- server.executeCapabilityRequest(context.Background(), baseToken, controlapipkg.CapabilityRequest{
			RequestID:  "req-approval-rollback-hidden",
			Capability: "fs_write",
			Arguments: map[string]string{
				"path":    "blocked.txt",
				"content": "approval should roll back invisibly",
			},
		}, true)
	}()

	<-auditStarted
	time.Sleep(30 * time.Millisecond)
	close(releaseAudit)

	response := <-responseCh
	close(stopReader)

	if response.Status != controlapipkg.ResponseStatusError || response.DenialCode != controlapipkg.DenialCodeAuditUnavailable {
		t.Fatalf("expected audit unavailable response, got %#v", response)
	}
	if sawPendingApproval.Load() {
		t.Fatalf("expected readers to never observe rolled-back pending approvals")
	}

	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.approvalState.records) != 0 {
		t.Fatalf("expected no pending approvals after rollback, got %#v", server.approvalState.records)
	}
}
