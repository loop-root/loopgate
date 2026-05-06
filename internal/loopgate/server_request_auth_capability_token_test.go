package loopgate

import (
	"context"
	"loopgate/internal/controlruntime"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
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
	server.nonceReplayStore = controlruntime.NewAppendOnlyNonceReplayStore(server.noncePath, "", requestReplayWindow)

	_, err := client.Status(context.Background())
	if err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeAuditUnavailable) {
		t.Fatalf("expected nonce replay persistence failure to fail closed, got %v", err)
	}
}
