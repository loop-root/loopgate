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

	"loopgate/internal/ledger"
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
	server.nonceReplayStore = appendOnlyNonceReplayStore{path: server.noncePath}

	_, err := client.Status(context.Background())
	if err == nil || !strings.Contains(err.Error(), DenialCodeAuditUnavailable) {
		t.Fatalf("expected nonce replay persistence failure to fail closed, got %v", err)
	}
}

func TestCapabilityAuthDenialsAreAudited(t *testing.T) {
	testCases := []struct {
		name               string
		configure          func(t *testing.T, client *Client, server *Server) string
		wantDenialCode     string
		wantControlSession bool
	}{
		{
			name: "missing capability token",
			configure: func(t *testing.T, client *Client, server *Server) string {
				t.Helper()
				return ""
			},
			wantDenialCode: DenialCodeCapabilityTokenMissing,
		},
		{
			name: "invalid capability token",
			configure: func(t *testing.T, client *Client, server *Server) string {
				t.Helper()
				return "invalid-capability-token"
			},
			wantDenialCode: DenialCodeCapabilityTokenInvalid,
		},
		{
			name: "expired capability token",
			configure: func(t *testing.T, client *Client, server *Server) string {
				t.Helper()
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
				return client.capabilityToken
			},
			wantDenialCode:     DenialCodeCapabilityTokenExpired,
			wantControlSession: true,
		},
		{
			name: "capability token peer binding mismatch",
			configure: func(t *testing.T, client *Client, server *Server) string {
				t.Helper()
				if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
					t.Fatalf("ensure capability token: %v", err)
				}
				server.mu.Lock()
				tokenClaims := server.tokens[client.capabilityToken]
				tokenClaims.PeerIdentity.PID++
				server.tokens[client.capabilityToken] = tokenClaims
				server.mu.Unlock()
				return client.capabilityToken
			},
			wantDenialCode:     DenialCodeCapabilityTokenInvalid,
			wantControlSession: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			repoRoot := t.TempDir()
			client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

			capabilityToken := testCase.configure(t, client, server)

			rawClient := NewClient(client.socketPath)
			rawClient.mu.Lock()
			rawClient.delegatedSession = true
			rawClient.mu.Unlock()

			var statusResponse StatusResponse
			err := rawClient.doJSON(context.Background(), http.MethodGet, "/v1/status", capabilityToken, nil, &statusResponse, nil)
			var denied RequestDeniedError
			if !errors.As(err, &denied) || denied.DenialCode != testCase.wantDenialCode {
				t.Fatalf("expected denied error %q, got %v", testCase.wantDenialCode, err)
			}

			authDeniedEvent := readLastAuditEventOfType(t, repoRoot, "auth.denied")
			if authDeniedEvent.Data["auth_kind"] != "capability_token" {
				t.Fatalf("expected auth_kind capability_token, got %#v", authDeniedEvent.Data["auth_kind"])
			}
			if authDeniedEvent.Data["denial_code"] != testCase.wantDenialCode {
				t.Fatalf("expected denial_code %q, got %#v", testCase.wantDenialCode, authDeniedEvent.Data["denial_code"])
			}
			if authDeniedEvent.Data["request_method"] != http.MethodGet {
				t.Fatalf("expected request_method GET, got %#v", authDeniedEvent.Data["request_method"])
			}
			if authDeniedEvent.Data["request_path"] != "/v1/status" {
				t.Fatalf("expected request_path /v1/status, got %#v", authDeniedEvent.Data["request_path"])
			}
			if _, ok := authDeniedEvent.Data["control_session_id"]; ok != testCase.wantControlSession {
				t.Fatalf("expected control_session_id presence %v, got %#v", testCase.wantControlSession, authDeniedEvent.Data)
			}
			if encodedEvent, err := json.Marshal(authDeniedEvent); err != nil {
				t.Fatalf("marshal auth denied event: %v", err)
			} else if capabilityToken != "" && strings.Contains(string(encodedEvent), capabilityToken) {
				t.Fatalf("auth denied audit event leaked raw capability token: %s", encodedEvent)
			}
		})
	}
}

func TestCapabilityAuthDenialFailsClosedWhenAuditUnavailable(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	originalAppendAuditEvent := server.appendAuditEvent
	server.appendAuditEvent = func(ledgerPath string, auditEvent ledger.Event) error {
		if auditEvent.Type == "auth.denied" {
			return errors.New("audit unavailable")
		}
		return originalAppendAuditEvent(ledgerPath, auditEvent)
	}

	rawClient := NewClient(client.socketPath)
	rawClient.mu.Lock()
	rawClient.delegatedSession = true
	rawClient.mu.Unlock()

	var statusResponse StatusResponse
	err := rawClient.doJSON(context.Background(), http.MethodGet, "/v1/status", "invalid-capability-token", nil, &statusResponse, nil)
	var denied RequestDeniedError
	if !errors.As(err, &denied) || denied.DenialCode != DenialCodeAuditUnavailable {
		t.Fatalf("expected audit unavailable denial, got %v", err)
	}
}

func TestApprovalAuthDenialIsAudited(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))

	response, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
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

	var decisionResponse CapabilityResponse
	approvalPath := "/v1/approvals/" + response.ApprovalRequestID + "/decision"
	err = client.doJSON(context.Background(), http.MethodPost, approvalPath, "", ApprovalDecisionRequest{
		Approved:      true,
		DecisionNonce: response.Metadata["approval_decision_nonce"].(string),
	}, &decisionResponse, nil)
	var denied RequestDeniedError
	if !errors.As(err, &denied) || denied.DenialCode != DenialCodeApprovalTokenMissing {
		t.Fatalf("expected approval token missing denial, got %v", err)
	}

	authDeniedEvent := readLastAuditEventOfType(t, repoRoot, "auth.denied")
	if authDeniedEvent.Data["auth_kind"] != "approval_token" {
		t.Fatalf("expected auth_kind approval_token, got %#v", authDeniedEvent.Data["auth_kind"])
	}
	if authDeniedEvent.Data["denial_code"] != DenialCodeApprovalTokenMissing {
		t.Fatalf("expected denial_code %q, got %#v", DenialCodeApprovalTokenMissing, authDeniedEvent.Data["denial_code"])
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

func readLastAuditEventOfType(t *testing.T, repoRoot string, eventType string) ledger.Event {
	t.Helper()

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(auditBytes)), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		line := strings.TrimSpace(lines[index])
		if line == "" {
			continue
		}
		var auditEvent ledger.Event
		if err := json.Unmarshal([]byte(line), &auditEvent); err != nil {
			t.Fatalf("decode audit event: %v", err)
		}
		if auditEvent.Type == eventType {
			return auditEvent
		}
	}

	t.Fatalf("expected audit event type %q", eventType)
	return ledger.Event{}
}
