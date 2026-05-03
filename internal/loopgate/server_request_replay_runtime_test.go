package loopgate

import (
	"context"
	"errors"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"strings"
	"testing"

	"loopgate/internal/ledger"
)

func TestDuplicateRequestIDIsRejected(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	firstResponse, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-duplicate",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		t.Fatalf("first execute: %v", err)
	}
	if firstResponse.Status != controlapipkg.ResponseStatusSuccess {
		t.Fatalf("unexpected first response: %#v", firstResponse)
	}

	secondResponse, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-duplicate",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		t.Fatalf("second execute should return typed denial, got %v", err)
	}
	if secondResponse.Status != controlapipkg.ResponseStatusDenied || secondResponse.DenialCode != controlapipkg.DenialCodeRequestReplayDetected {
		t.Fatalf("expected replay denial, got %#v", secondResponse)
	}
}

func TestAuditFailureIsSurfacedExplicitly(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.appendAuditEvent = func(string, ledger.Event) error {
		return errors.New("audit sink unavailable")
	}
	client.ConfigureSession("audit-test", "audit-session", []string{"fs_list"})

	_, err := client.ensureCapabilityToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeAuditUnavailable) {
		t.Fatalf("expected audit unavailable error, got %v", err)
	}
}

func TestCapabilityExecutionAuditFailureReturnsAuditUnavailable(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	server.appendAuditEvent = func(string, ledger.Event) error {
		return errors.New("audit sink unavailable")
	}

	response, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-audit-fail-after-open",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		t.Fatalf("expected typed audit unavailable response, got %v", err)
	}
	if response.Status != controlapipkg.ResponseStatusError || response.DenialCode != controlapipkg.DenialCodeAuditUnavailable {
		t.Fatalf("expected audit unavailable response, got %#v", response)
	}
}

func TestSingleUseExecutionTokenIsDeniedOnReuse(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	server.mu.Lock()
	baseToken := server.sessionState.tokens[client.capabilityToken]
	server.mu.Unlock()

	capabilityRequest := normalizeCapabilityRequest(controlapipkg.CapabilityRequest{
		RequestID:  "req-single-use",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "notes.txt",
			"content": "hello",
		},
	})
	executionToken := deriveExecutionToken(baseToken, capabilityRequest)

	firstResponse := server.executeCapabilityRequest(context.Background(), executionToken, capabilityRequest, false)
	if firstResponse.Status != controlapipkg.ResponseStatusSuccess {
		t.Fatalf("expected first single-use execution to succeed, got %#v", firstResponse)
	}
	server.mu.Lock()
	consumedToken, found := server.replayState.usedTokens[executionToken.TokenID]
	server.mu.Unlock()
	if !found {
		t.Fatal("expected single-use execution token to be recorded in used token registry")
	}
	if consumedToken.ParentTokenID != baseToken.TokenID {
		t.Fatalf("expected parent token id %q, got %#v", baseToken.TokenID, consumedToken)
	}
	if consumedToken.Capability != capabilityRequest.Capability {
		t.Fatalf("expected consumed capability %q, got %#v", capabilityRequest.Capability, consumedToken)
	}
	if consumedToken.NormalizedArgHash != normalizedArgumentHash(capabilityRequest.Arguments) {
		t.Fatalf("expected normalized argument hash to be recorded, got %#v", consumedToken)
	}

	secondResponse := server.executeCapabilityRequest(context.Background(), executionToken, capabilityRequest, false)
	if secondResponse.Status != controlapipkg.ResponseStatusDenied || secondResponse.DenialCode != controlapipkg.DenialCodeCapabilityTokenReused {
		t.Fatalf("expected reused single-use token denial, got %#v", secondResponse)
	}
}

func TestApprovalExecuteDeniesWhenStoredExecutionBodyMutated(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}
	pendingResponse, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-integrity",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "pending.txt",
			"content": "hidden",
		},
	})
	if err != nil {
		t.Fatalf("execute pending approval: %v", err)
	}
	if !pendingResponse.ApprovalRequired {
		t.Fatalf("expected pending approval, got %#v", pendingResponse)
	}
	server.mu.Lock()
	pa := server.approvalState.records[pendingResponse.ApprovalRequestID]
	pa.Request.Arguments["path"] = "evil.txt"
	server.approvalState.records[pendingResponse.ApprovalRequestID] = pa
	server.mu.Unlock()
	approvedResponse, err := client.UIDecideApproval(context.Background(), pendingResponse.ApprovalRequestID, true)
	if err != nil {
		t.Fatalf("ui approval decision: %v", err)
	}
	if approvedResponse.DenialCode != controlapipkg.DenialCodeApprovalExecutionBodyMismatch {
		t.Fatalf("expected execution body mismatch, got %#v", approvedResponse)
	}
	if approvedResponse.Status != controlapipkg.ResponseStatusError {
		t.Fatalf("expected error status, got %#v", approvedResponse)
	}
}

func TestPendingApprovalLimitPerControlSession(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))
	server.maxPendingApprovalsPerControlSession = 2
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}
	for i := range 2 {
		resp, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
			RequestID:  "req-ap-limit-" + string(rune('0'+i)),
			Capability: "fs_write",
			Arguments: map[string]string{
				"path":    "limit-" + string(rune('0'+i)) + ".txt",
				"content": "x",
			},
		})
		if err != nil {
			t.Fatalf("execute %d: %v", i, err)
		}
		if !resp.ApprovalRequired {
			t.Fatalf("expected pending approval %d, got %#v", i, resp)
		}
	}
	resp, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-ap-limit-2",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "limit-2.txt",
			"content": "x",
		},
	})
	if err != nil {
		t.Fatalf("execute third: %v", err)
	}
	if resp.Status != controlapipkg.ResponseStatusDenied || resp.DenialCode != controlapipkg.DenialCodePendingApprovalLimitReached {
		t.Fatalf("expected pending approval limit, got %#v", resp)
	}
}

func TestRequestReplayStoreSaturates(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))
	server.maxSeenRequestReplayEntries = 2
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}
	for i := range 2 {
		_, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
			RequestID:  "req-replay-sat-" + string(rune('0'+i)),
			Capability: "fs_write",
			Arguments: map[string]string{
				"path":    "rs" + string(rune('0'+i)) + ".txt",
				"content": "x",
			},
		})
		if err != nil {
			t.Fatalf("execute %d: %v", i, err)
		}
	}
	resp, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-replay-sat-2",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "rs2.txt",
			"content": "x",
		},
	})
	if err != nil {
		t.Fatalf("execute third: %v", err)
	}
	if resp.Status != controlapipkg.ResponseStatusDenied || resp.DenialCode != controlapipkg.DenialCodeReplayStateSaturated {
		t.Fatalf("expected replay store saturated, got %#v", resp)
	}
}

func TestBoundExecutionTokenRejectsDifferentNormalizedArguments(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	server.mu.Lock()
	baseToken := server.sessionState.tokens[client.capabilityToken]
	server.mu.Unlock()

	approvedRequest := normalizeCapabilityRequest(controlapipkg.CapabilityRequest{
		RequestID:  "req-bound",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "./notes.txt",
			"content": "hello",
		},
	})
	executionToken := deriveExecutionToken(baseToken, approvedRequest)

	mutatedRequest := normalizeCapabilityRequest(controlapipkg.CapabilityRequest{
		RequestID:  "req-bound-mutated",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "other.txt",
			"content": "hello",
		},
	})
	response := server.executeCapabilityRequest(context.Background(), executionToken, mutatedRequest, false)
	if response.Status != controlapipkg.ResponseStatusDenied || response.DenialCode != controlapipkg.DenialCodeCapabilityTokenBindingInvalid {
		t.Fatalf("expected bound token mismatch denial, got %#v", response)
	}
}
