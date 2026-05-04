package loopgate

import (
	"context"
	"errors"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"strings"
	"testing"
)

func TestApprovalAuthDenialIsAudited(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))

	response, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-approval-auth-audit",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "guarded.txt",
			"content": "guarded",
		},
	})
	if err != nil {
		t.Fatalf("execute guarded write: %v", err)
	}
	if !response.ApprovalRequired {
		t.Fatalf("expected pending approval, got %#v", response)
	}

	var decisionResponse controlapipkg.CapabilityResponse
	approvalPath := "/v1/approvals/" + response.ApprovalRequestID + "/decision"
	err = client.doJSON(context.Background(), http.MethodPost, approvalPath, "", controlapipkg.ApprovalDecisionRequest{
		Approved:      true,
		DecisionNonce: response.Metadata["approval_decision_nonce"].(string),
	}, &decisionResponse, nil)
	var denied RequestDeniedError
	if !errors.As(err, &denied) || denied.DenialCode != controlapipkg.DenialCodeApprovalTokenMissing {
		t.Fatalf("expected approval token missing denial, got %v", err)
	}

	authDeniedEvent := readLastAuditEventOfType(t, repoRoot, "auth.denied")
	if authDeniedEvent.Data["auth_kind"] != "approval_token" {
		t.Fatalf("expected auth_kind approval_token, got %#v", authDeniedEvent.Data["auth_kind"])
	}
	if authDeniedEvent.Data["denial_code"] != controlapipkg.DenialCodeApprovalTokenMissing {
		t.Fatalf("expected denial_code %q, got %#v", controlapipkg.DenialCodeApprovalTokenMissing, authDeniedEvent.Data["denial_code"])
	}
	if authDeniedEvent.Data["request_method"] != http.MethodPost {
		t.Fatalf("expected request_method POST, got %#v", authDeniedEvent.Data["request_method"])
	}
	if authDeniedEvent.Data["request_path"] != approvalPath {
		t.Fatalf("expected request_path %q, got %#v", approvalPath, authDeniedEvent.Data["request_path"])
	}
}

func TestApprovalDecisionRequiresMatchingCapabilityTokenOwner(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))

	response, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-owner",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "guarded.txt",
			"content": "guarded",
		},
	})
	if err != nil {
		t.Fatalf("execute guarded write: %v", err)
	}
	if !response.ApprovalRequired {
		t.Fatalf("expected pending approval, got %#v", response)
	}

	otherClient := NewClient(client.socketPath)
	otherClient.ConfigureSession("other-actor", "other-session", []string{"fs_write"})
	if _, err = otherClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure other client capability token: %v", err)
	}
	otherApprovalToken, err := otherClient.ensureApprovalToken(context.Background())
	if err != nil {
		t.Fatalf("ensure other client approval token: %v", err)
	}

	var approvalResponse controlapipkg.CapabilityResponse
	approvalNonce := response.Metadata["approval_decision_nonce"].(string)
	approvalPath := "/v1/approvals/" + response.ApprovalRequestID + "/decision"
	err = otherClient.doJSON(context.Background(), http.MethodPost, approvalPath, "", controlapipkg.ApprovalDecisionRequest{
		Approved:      true,
		DecisionNonce: approvalNonce,
	}, &approvalResponse, map[string]string{
		"X-Loopgate-Approval-Token": otherApprovalToken,
	})
	if err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeApprovalOwnerMismatch) {
		t.Fatalf("expected approval owner mismatch denial, got %v", err)
	}
}

func TestApprovalDecisionRequiresDecisionNonce(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))

	response, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-missing-nonce",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "guarded.txt",
			"content": "guarded",
		},
	})
	if err != nil {
		t.Fatalf("execute guarded write: %v", err)
	}
	if !response.ApprovalRequired {
		t.Fatalf("expected pending approval, got %#v", response)
	}

	delete(client.approvalDecisionNonce, response.ApprovalRequestID)
	_, err = client.DecideApproval(context.Background(), response.ApprovalRequestID, true)
	if err == nil || !strings.Contains(err.Error(), "approval decision nonce is missing") {
		t.Fatalf("expected client-side missing nonce error, got %v", err)
	}
}

func TestApprovalTokenPeerBindingMismatchIsDenied(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))

	response, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-approval-peer-mismatch",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "guarded.txt",
			"content": "guarded",
		},
	})
	if err != nil {
		t.Fatalf("execute guarded write: %v", err)
	}
	if !response.ApprovalRequired {
		t.Fatalf("expected pending approval, got %#v", response)
	}

	server.mu.Lock()
	activeSession := server.sessionState.sessions[client.controlSessionID]
	activeSession.PeerIdentity.PID++
	server.sessionState.sessions[client.controlSessionID] = activeSession
	server.mu.Unlock()

	decisionResponse, err := client.DecideApproval(context.Background(), response.ApprovalRequestID, true)
	if err != nil {
		t.Fatalf("decide approval: %v", err)
	}
	if decisionResponse.Status != controlapipkg.ResponseStatusDenied || decisionResponse.DenialCode != controlapipkg.DenialCodeApprovalTokenInvalid {
		t.Fatalf("expected approval peer binding denial, got %#v", decisionResponse)
	}
}

func TestApprovalDecisionCannotBeReplayedAfterResolution(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))

	response, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-approval-replay",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "guarded.txt",
			"content": "guarded",
		},
	})
	if err != nil {
		t.Fatalf("execute guarded write: %v", err)
	}
	if !response.ApprovalRequired {
		t.Fatalf("expected pending approval, got %#v", response)
	}

	approvalNonce := response.Metadata["approval_decision_nonce"].(string)
	firstResponse, err := client.DecideApproval(context.Background(), response.ApprovalRequestID, true)
	if err != nil {
		t.Fatalf("first approval decision: %v", err)
	}
	if firstResponse.Status != controlapipkg.ResponseStatusSuccess {
		t.Fatalf("expected successful execution after approval, got %#v", firstResponse)
	}

	controlSessionID := client.controlSessionID
	server.mu.Lock()
	controlSession := server.sessionState.sessions[controlSessionID]
	server.mu.Unlock()
	manualReplayRequest := controlapipkg.ApprovalDecisionRequest{
		Approved:      true,
		DecisionNonce: approvalNonce,
	}
	var replayResponse controlapipkg.CapabilityResponse
	replayPath := "/v1/approvals/" + response.ApprovalRequestID + "/decision"
	err = client.doJSON(context.Background(), http.MethodPost, replayPath, "", manualReplayRequest, &replayResponse, map[string]string{
		"X-Loopgate-Approval-Token": controlSession.ApprovalToken,
	})
	if err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeApprovalStateConflict) {
		t.Fatalf("expected approval state conflict denial on replay, got %v", err)
	}
}
