package loopgate

import (
	"context"
	"encoding/json"
	"errors"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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
	tokenClaims := server.sessionState.tokens[client.capabilityToken]
	tokenClaims.ExpiresAt = time.Now().UTC().Add(-1 * time.Minute)
	server.sessionState.tokens[client.capabilityToken] = tokenClaims
	activeSession := server.sessionState.sessions[tokenClaims.ControlSessionID]
	activeSession.ExpiresAt = time.Now().UTC().Add(-1 * time.Minute)
	server.sessionState.sessions[tokenClaims.ControlSessionID] = activeSession
	server.mu.Unlock()

	response, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-expired",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		t.Fatalf("expected local client to refresh expired capability token, got %v", err)
	}
	if response.Status != controlapipkg.ResponseStatusSuccess {
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
	tokenClaims := server.sessionState.tokens[client.capabilityToken]
	tokenClaims.PeerIdentity.PID++
	server.sessionState.tokens[client.capabilityToken] = tokenClaims
	server.mu.Unlock()

	response, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-peer-mismatch",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		t.Fatalf("expected local client to refresh peer-mismatched capability token, got %v", err)
	}
	if response.Status != controlapipkg.ResponseStatusSuccess {
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

	response, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-missing-signature",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		t.Fatalf("execute capability: %v", err)
	}
	if response.Status != controlapipkg.ResponseStatusDenied || response.DenialCode != controlapipkg.DenialCodeRequestSignatureMissing {
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

	response, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-invalid-signature",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		t.Fatalf("execute capability: %v", err)
	}
	if response.Status != controlapipkg.ResponseStatusDenied || response.DenialCode != controlapipkg.DenialCodeRequestSignatureInvalid {
		t.Fatalf("expected request signature invalid denial, got %#v", response)
	}
}

func TestCapabilityExecuteRejectsReplayedRequestNonce(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	requestBody := controlapipkg.CapabilityRequest{
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

	var firstResponse controlapipkg.CapabilityResponse
	if err := client.doJSONWithHeaders(context.Background(), http.MethodPost, "/v1/capabilities/execute", client.capabilityToken, requestBody, &firstResponse, requestHeaders); err != nil {
		t.Fatalf("first signed request: %v", err)
	}

	var secondResponse controlapipkg.CapabilityResponse
	err := client.doJSONWithHeaders(context.Background(), http.MethodPost, "/v1/capabilities/execute", client.capabilityToken, requestBody, &secondResponse, requestHeaders)
	if err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeRequestNonceReplayDetected) {
		t.Fatalf("expected request nonce replay denial, got %v", err)
	}
}

func TestSignedRequestFailsClosedWhenNonceReplayPersistenceUnavailable(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.noncePath = filepath.Join(repoRoot, "runtime", "state")
	server.nonceReplayStore = appendOnlyNonceReplayStore{path: server.noncePath}

	_, err := client.Status(context.Background())
	if err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeAuditUnavailable) {
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
			wantDenialCode: controlapipkg.DenialCodeCapabilityTokenMissing,
		},
		{
			name: "invalid capability token",
			configure: func(t *testing.T, client *Client, server *Server) string {
				t.Helper()
				return "invalid-capability-token"
			},
			wantDenialCode: controlapipkg.DenialCodeCapabilityTokenInvalid,
		},
		{
			name: "expired capability token",
			configure: func(t *testing.T, client *Client, server *Server) string {
				t.Helper()
				if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
					t.Fatalf("ensure capability token: %v", err)
				}
				server.mu.Lock()
				tokenClaims := server.sessionState.tokens[client.capabilityToken]
				tokenClaims.ExpiresAt = time.Now().UTC().Add(-1 * time.Minute)
				server.sessionState.tokens[client.capabilityToken] = tokenClaims
				activeSession := server.sessionState.sessions[tokenClaims.ControlSessionID]
				activeSession.ExpiresAt = time.Now().UTC().Add(-1 * time.Minute)
				server.sessionState.sessions[tokenClaims.ControlSessionID] = activeSession
				server.mu.Unlock()
				return client.capabilityToken
			},
			wantDenialCode:     controlapipkg.DenialCodeCapabilityTokenExpired,
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
				tokenClaims := server.sessionState.tokens[client.capabilityToken]
				tokenClaims.PeerIdentity.PID++
				server.sessionState.tokens[client.capabilityToken] = tokenClaims
				server.mu.Unlock()
				return client.capabilityToken
			},
			wantDenialCode:     controlapipkg.DenialCodeCapabilityTokenInvalid,
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

			var statusResponse controlapipkg.StatusResponse
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

	var statusResponse controlapipkg.StatusResponse
	err := rawClient.doJSON(context.Background(), http.MethodGet, "/v1/status", "invalid-capability-token", nil, &statusResponse, nil)
	var denied RequestDeniedError
	if !errors.As(err, &denied) || denied.DenialCode != controlapipkg.DenialCodeAuditUnavailable {
		t.Fatalf("expected audit unavailable denial, got %v", err)
	}
}

func TestCapabilityAuthDenialsSuppressRepeatedAuditWritesWithinBurstWindow(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	currentTime := time.Date(2026, time.April, 20, 12, 0, 0, 0, time.UTC)
	server.SetNowForTest(func() time.Time { return currentTime })

	rawClient := NewClient(client.socketPath)
	rawClient.mu.Lock()
	rawClient.delegatedSession = true
	rawClient.mu.Unlock()

	requestInvalidStatus := func() {
		t.Helper()
		var statusResponse controlapipkg.StatusResponse
		err := rawClient.doJSON(context.Background(), http.MethodGet, "/v1/status", "invalid-capability-token", nil, &statusResponse, nil)
		var denied RequestDeniedError
		if !errors.As(err, &denied) || denied.DenialCode != controlapipkg.DenialCodeCapabilityTokenInvalid {
			t.Fatalf("expected invalid capability token denial, got %v", err)
		}
	}

	requestInvalidStatus()
	requestInvalidStatus()

	authDeniedEvents := readAuditEventsOfType(t, repoRoot, "auth.denied")
	if len(authDeniedEvents) != 1 {
		t.Fatalf("expected exactly one must-persist auth.denied event in the burst window, got %d", len(authDeniedEvents))
	}

	currentTime = currentTime.Add(authDeniedAuditBurstWindow + time.Second)
	requestInvalidStatus()

	authDeniedEvents = readAuditEventsOfType(t, repoRoot, "auth.denied")
	if len(authDeniedEvents) != 2 {
		t.Fatalf("expected a second auth.denied event after the burst window rolled, got %d", len(authDeniedEvents))
	}

	suppressedEvents := readAuditEventsOfType(t, repoRoot, "auth.denied.suppressed")
	if len(suppressedEvents) != 1 {
		t.Fatalf("expected one auth.denied.suppressed aggregate event, got %d", len(suppressedEvents))
	}
	suppressedEvent := suppressedEvents[0]
	if suppressedEvent.Data["suppressed_count"] != float64(1) {
		t.Fatalf("expected suppressed_count 1, got %#v", suppressedEvent.Data["suppressed_count"])
	}
	if suppressedEvent.Data["denial_code"] != controlapipkg.DenialCodeCapabilityTokenInvalid {
		t.Fatalf("expected invalid capability token aggregate denial code, got %#v", suppressedEvent.Data["denial_code"])
	}
}

func TestCapabilityAuthDeniedSuppressionFailsClosedWhenAggregateAuditUnavailable(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	currentTime := time.Date(2026, time.April, 20, 12, 0, 0, 0, time.UTC)
	server.SetNowForTest(func() time.Time { return currentTime })

	originalAppendAuditEvent := server.appendAuditEvent
	server.appendAuditEvent = func(ledgerPath string, auditEvent ledger.Event) error {
		if auditEvent.Type == "auth.denied.suppressed" {
			return errors.New("aggregate audit unavailable")
		}
		return originalAppendAuditEvent(ledgerPath, auditEvent)
	}

	rawClient := NewClient(client.socketPath)
	rawClient.mu.Lock()
	rawClient.delegatedSession = true
	rawClient.mu.Unlock()

	var firstStatusResponse controlapipkg.StatusResponse
	firstErr := rawClient.doJSON(context.Background(), http.MethodGet, "/v1/status", "invalid-capability-token", nil, &firstStatusResponse, nil)
	var denied RequestDeniedError
	if !errors.As(firstErr, &denied) || denied.DenialCode != controlapipkg.DenialCodeCapabilityTokenInvalid {
		t.Fatalf("expected first invalid capability token denial, got %v", firstErr)
	}

	var secondStatusResponse controlapipkg.StatusResponse
	secondErr := rawClient.doJSON(context.Background(), http.MethodGet, "/v1/status", "invalid-capability-token", nil, &secondStatusResponse, nil)
	if !errors.As(secondErr, &denied) || denied.DenialCode != controlapipkg.DenialCodeCapabilityTokenInvalid {
		t.Fatalf("expected second invalid capability token denial in burst window, got %v", secondErr)
	}

	currentTime = currentTime.Add(authDeniedAuditBurstWindow + time.Second)

	var rolloverStatusResponse controlapipkg.StatusResponse
	rolloverErr := rawClient.doJSON(context.Background(), http.MethodGet, "/v1/status", "invalid-capability-token", nil, &rolloverStatusResponse, nil)
	if !errors.As(rolloverErr, &denied) || denied.DenialCode != controlapipkg.DenialCodeAuditUnavailable {
		t.Fatalf("expected aggregate audit failure to fail closed, got %v", rolloverErr)
	}
}

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

func TestCapabilityResponseJSONDoesNotExposeProviderTokenFields(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	response, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
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

	auditEvents := readAuditEventsOfType(t, repoRoot, eventType)
	if len(auditEvents) == 0 {
		t.Fatalf("expected audit event type %q", eventType)
	}
	return auditEvents[len(auditEvents)-1]
}

func readAuditEventsOfType(t *testing.T, repoRoot string, eventType string) []ledger.Event {
	t.Helper()

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(auditBytes)), "\n")
	auditEvents := make([]ledger.Event, 0, len(lines))
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		var auditEvent ledger.Event
		if err := json.Unmarshal([]byte(line), &auditEvent); err != nil {
			t.Fatalf("decode audit event: %v", err)
		}
		if auditEvent.Type == eventType {
			auditEvents = append(auditEvents, auditEvent)
		}
	}
	return auditEvents
}
