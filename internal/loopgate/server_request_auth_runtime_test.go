package loopgate

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExpiredCapabilityTokenIsRefreshedForLocalClient(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	server.mu.Lock()
	tokenClaims := server.tokens[client.capabilityToken]
	tokenClaims.ExpiresAt = time.Now().UTC().Add(-1 * time.Minute)
	server.tokens[client.capabilityToken] = tokenClaims
	activeSession := server.sessions[tokenClaims.ControlSessionID]
	activeSession.ExpiresAt = time.Now().UTC().Add(-1 * time.Minute)
	server.sessions[tokenClaims.ControlSessionID] = activeSession
	server.mu.Unlock()

	response, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-expired",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		t.Fatalf("expected local client to refresh expired capability token, got %v", err)
	}
	if response.Status != ResponseStatusSuccess {
		t.Fatalf("expected refreshed capability execution to succeed, got %#v", response)
	}
}

func TestCapabilityTokenPeerBindingMismatchRefreshesForLocalClient(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	server.mu.Lock()
	tokenClaims := server.tokens[client.capabilityToken]
	tokenClaims.PeerIdentity.PID++
	server.tokens[client.capabilityToken] = tokenClaims
	server.mu.Unlock()

	response, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-peer-mismatch",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		t.Fatalf("expected local client to refresh peer-mismatched capability token, got %v", err)
	}
	if response.Status != ResponseStatusSuccess {
		t.Fatalf("expected refreshed capability execution to succeed, got %#v", response)
	}
}

func TestCapabilityExecuteRequiresSignedRequest(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	client.mu.Lock()
	client.sessionMACKey = ""
	client.mu.Unlock()

	response, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-missing-signature",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		t.Fatalf("execute capability: %v", err)
	}
	if response.Status != ResponseStatusDenied || response.DenialCode != DenialCodeRequestSignatureMissing {
		t.Fatalf("expected request signature missing denial, got %#v", response)
	}
}

func TestCapabilityExecuteRejectsInvalidSignature(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	client.mu.Lock()
	client.sessionMACKey = "wrong-session-mac-key"
	client.mu.Unlock()

	response, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-invalid-signature",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		t.Fatalf("execute capability: %v", err)
	}
	if response.Status != ResponseStatusDenied || response.DenialCode != DenialCodeRequestSignatureInvalid {
		t.Fatalf("expected request signature invalid denial, got %#v", response)
	}
}

func TestCapabilityExecuteRejectsReplayedRequestNonce(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	requestBody := CapabilityRequest{
		RequestID:  "req-replayed-nonce",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	}
	requestTimestamp := time.Now().UTC().Format(time.RFC3339Nano)
	requestNonce := "replayed-nonce"
	requestSignature := signRequest(client.sessionMACKey, http.MethodPost, "/v1/capabilities/execute", client.controlSessionID, requestTimestamp, requestNonce, mustJSON(t, requestBody))
	requestHeaders := map[string]string{
		"X-Loopgate-Control-Session":   client.controlSessionID,
		"X-Loopgate-Request-Timestamp": requestTimestamp,
		"X-Loopgate-Request-Nonce":     requestNonce,
		"X-Loopgate-Request-Signature": requestSignature,
	}

	var firstResponse CapabilityResponse
	if err := client.doJSONWithHeaders(context.Background(), http.MethodPost, "/v1/capabilities/execute", client.capabilityToken, requestBody, &firstResponse, requestHeaders); err != nil {
		t.Fatalf("first signed request: %v", err)
	}

	var secondResponse CapabilityResponse
	err := client.doJSONWithHeaders(context.Background(), http.MethodPost, "/v1/capabilities/execute", client.capabilityToken, requestBody, &secondResponse, requestHeaders)
	if err == nil || !strings.Contains(err.Error(), DenialCodeRequestNonceReplayDetected) {
		t.Fatalf("expected request nonce replay denial, got %v", err)
	}
}

func TestSignedRequestFailsClosedWhenNonceReplayPersistenceUnavailable(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.noncePath = filepath.Join(repoRoot, "runtime", "state")

	_, err := client.Status(context.Background())
	if err == nil || !strings.Contains(err.Error(), DenialCodeAuditUnavailable) {
		t.Fatalf("expected nonce replay persistence failure to fail closed, got %v", err)
	}
}

func TestApprovalDecisionRequiresMatchingCapabilityTokenOwner(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))

	response, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
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

	var approvalResponse CapabilityResponse
	approvalNonce := response.Metadata["approval_decision_nonce"].(string)
	approvalPath := "/v1/approvals/" + response.ApprovalRequestID + "/decision"
	err = otherClient.doJSON(context.Background(), http.MethodPost, approvalPath, "", ApprovalDecisionRequest{
		Approved:      true,
		DecisionNonce: approvalNonce,
	}, &approvalResponse, map[string]string{
		"X-Loopgate-Approval-Token": otherApprovalToken,
	})
	if err == nil || !strings.Contains(err.Error(), DenialCodeApprovalOwnerMismatch) {
		t.Fatalf("expected approval owner mismatch denial, got %v", err)
	}
}

func TestApprovalDecisionRequiresDecisionNonce(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))

	response, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
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

	response, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
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
	activeSession := server.sessions[client.controlSessionID]
	activeSession.PeerIdentity.PID++
	server.sessions[client.controlSessionID] = activeSession
	server.mu.Unlock()

	decisionResponse, err := client.DecideApproval(context.Background(), response.ApprovalRequestID, true)
	if err != nil {
		t.Fatalf("decide approval: %v", err)
	}
	if decisionResponse.Status != ResponseStatusDenied || decisionResponse.DenialCode != DenialCodeApprovalTokenInvalid {
		t.Fatalf("expected approval peer binding denial, got %#v", decisionResponse)
	}
}

func TestApprovalDecisionCannotBeReplayedAfterResolution(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))

	response, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
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
	if firstResponse.Status != ResponseStatusSuccess {
		t.Fatalf("expected successful execution after approval, got %#v", firstResponse)
	}

	controlSessionID := client.controlSessionID
	server.mu.Lock()
	controlSession := server.sessions[controlSessionID]
	server.mu.Unlock()
	manualReplayRequest := ApprovalDecisionRequest{
		Approved:      true,
		DecisionNonce: approvalNonce,
	}
	var replayResponse CapabilityResponse
	replayPath := "/v1/approvals/" + response.ApprovalRequestID + "/decision"
	err = client.doJSON(context.Background(), http.MethodPost, replayPath, "", manualReplayRequest, &replayResponse, map[string]string{
		"X-Loopgate-Approval-Token": controlSession.ApprovalToken,
	})
	if err == nil || !strings.Contains(err.Error(), DenialCodeApprovalStateConflict) {
		t.Fatalf("expected approval state conflict denial on replay, got %v", err)
	}
}

func TestExecuteCapabilityRequest_DeniesNeedsApprovalWhenApprovalCreationDisabledWithoutApprovedExecution(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	server.mu.Lock()
	baseToken := server.tokens[client.capabilityToken]
	server.mu.Unlock()

	response := server.executeCapabilityRequest(context.Background(), baseToken, CapabilityRequest{
		RequestID:  "req-no-approval-bypass",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "blocked.txt",
			"content": "approval bypass should fail closed",
		},
	}, false)
	if response.Status != ResponseStatusDenied || response.DenialCode != DenialCodeApprovalRequired || !response.ApprovalRequired {
		t.Fatalf("expected approval-required denial without execution, got %#v", response)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "blocked.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected blocked file to remain unwritten, stat err=%v", err)
	}

	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.approvals) != 0 {
		t.Fatalf("expected no pending approvals to be created on fail-closed path, got %#v", server.approvals)
	}
}

func TestCapabilityResponseJSONDoesNotExposeProviderTokenFields(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	response, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-json",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		t.Fatalf("execute fs_list: %v", err)
	}

	encodedResponse, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	lowerJSON := strings.ToLower(string(encodedResponse))
	for _, forbiddenField := range []string{"access_token", "refresh_token", "client_secret", "api_key"} {
		if strings.Contains(lowerJSON, forbiddenField) {
			t.Fatalf("response leaked forbidden token field %q: %s", forbiddenField, encodedResponse)
		}
	}
}
