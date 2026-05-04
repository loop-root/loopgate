package loopgate

import (
	"bytes"
	"context"
	"encoding/json"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHookPreValidate_RateLimitBlocksPreToolUse(t *testing.T) {
	repoRoot := t.TempDir()
	readPath := filepath.Join(repoRoot, "README.md")
	if err := os.WriteFile(readPath, []byte("loopgate\n"), 0o600); err != nil {
		t.Fatalf("write read target: %v", err)
	}

	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.hookPreValidateRateLimit = 1
	server.hookPreValidateRateWindow = time.Hour

	firstBody := bytes.NewBufferString(`{"hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"` + readPath + `"},"session_id":"session-hook"}`)
	firstRequest := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", firstBody)
	firstRequest = firstRequest.WithContext(context.WithValue(firstRequest.Context(), peerIdentityContextKey, peerIdentity{
		UID: uint32(os.Getuid()),
		PID: 4242,
	}))
	firstRecorder := httptest.NewRecorder()
	server.handleHookPreValidate(firstRecorder, firstRequest)

	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("expected first status 200, got %d body=%s", firstRecorder.Code, firstRecorder.Body.String())
	}
	var firstResponse controlapipkg.HookPreValidateResponse
	if err := json.Unmarshal(firstRecorder.Body.Bytes(), &firstResponse); err != nil {
		t.Fatalf("decode first hook response: %v", err)
	}
	if firstResponse.Decision != "allow" {
		t.Fatalf("expected first pretool request to allow, got %#v", firstResponse)
	}

	secondBody := bytes.NewBufferString(`{"hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"` + readPath + `"},"session_id":"session-hook"}`)
	secondRequest := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", secondBody)
	secondRequest = secondRequest.WithContext(context.WithValue(secondRequest.Context(), peerIdentityContextKey, peerIdentity{
		UID: uint32(os.Getuid()),
		PID: 4242,
	}))
	secondRecorder := httptest.NewRecorder()
	server.handleHookPreValidate(secondRecorder, secondRequest)

	if secondRecorder.Code != http.StatusOK {
		t.Fatalf("expected second status 200, got %d body=%s", secondRecorder.Code, secondRecorder.Body.String())
	}
	var secondResponse controlapipkg.HookPreValidateResponse
	if err := json.Unmarshal(secondRecorder.Body.Bytes(), &secondResponse); err != nil {
		t.Fatalf("decode second hook response: %v", err)
	}
	if secondResponse.Decision != "block" {
		t.Fatalf("expected second pretool request to block, got %#v", secondResponse)
	}
	if secondResponse.DenialCode != controlapipkg.DenialCodeHookRateLimitExceeded {
		t.Fatalf("expected hook rate-limit denial code, got %#v", secondResponse)
	}
}

func TestHookPreValidate_RateLimitDoesNotBlockSessionStart(t *testing.T) {
	repoRoot := t.TempDir()
	readPath := filepath.Join(repoRoot, "README.md")
	if err := os.WriteFile(readPath, []byte("loopgate\n"), 0o600); err != nil {
		t.Fatalf("write read target: %v", err)
	}

	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.hookPreValidateRateLimit = 1
	server.hookPreValidateRateWindow = time.Hour

	pretoolBody := bytes.NewBufferString(`{"hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"` + readPath + `"},"session_id":"session-hook"}`)
	pretoolRequest := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", pretoolBody)
	pretoolRequest = pretoolRequest.WithContext(context.WithValue(pretoolRequest.Context(), peerIdentityContextKey, peerIdentity{
		UID: uint32(os.Getuid()),
		PID: 4242,
	}))
	pretoolRecorder := httptest.NewRecorder()
	server.handleHookPreValidate(pretoolRecorder, pretoolRequest)
	if pretoolRecorder.Code != http.StatusOK {
		t.Fatalf("expected pretool status 200, got %d body=%s", pretoolRecorder.Code, pretoolRecorder.Body.String())
	}

	sessionStartBody := bytes.NewBufferString(`{"hook_event_name":"SessionStart","session_id":"session-hook"}`)
	sessionStartRequest := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", sessionStartBody)
	sessionStartRequest = sessionStartRequest.WithContext(context.WithValue(sessionStartRequest.Context(), peerIdentityContextKey, peerIdentity{
		UID: uint32(os.Getuid()),
		PID: 4242,
	}))
	sessionStartRecorder := httptest.NewRecorder()
	server.handleHookPreValidate(sessionStartRecorder, sessionStartRequest)

	if sessionStartRecorder.Code != http.StatusOK {
		t.Fatalf("expected session start status 200, got %d body=%s", sessionStartRecorder.Code, sessionStartRecorder.Body.String())
	}
	var response controlapipkg.HookPreValidateResponse
	if err := json.Unmarshal(sessionStartRecorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode session start response: %v", err)
	}
	if response.Decision != "allow" {
		t.Fatalf("expected session start to remain audit-only allow, got %#v", response)
	}

	lastAuditEvent := readLastHookAuditEvent(t, repoRoot)
	if hookEventName, _ := lastAuditEvent.Data["hook_event_name"].(string); hookEventName != claudeCodeHookEventSessionStart {
		t.Fatalf("expected session start audit event after rate-limited pretool use, got %#v", lastAuditEvent.Data["hook_event_name"])
	}
}

func TestHookPreValidate_PeerAuthFailureRateLimitBlocksRepeatedMissingPeerIdentity(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.hookPeerAuthFailureRateLimit = 1
	server.hookPeerAuthFailureWindow = time.Hour

	firstRequest := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", bytes.NewBufferString(`{"hook_event_name":"PreToolUse","tool_name":"Read","session_id":"session-hook"}`))
	firstRecorder := httptest.NewRecorder()
	server.handleHookPreValidate(firstRecorder, firstRequest)

	if firstRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected first missing-peer failure to return 401, got %d body=%s", firstRecorder.Code, firstRecorder.Body.String())
	}

	secondRequest := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", bytes.NewBufferString(`{"hook_event_name":"PreToolUse","tool_name":"Read","session_id":"session-hook"}`))
	secondRecorder := httptest.NewRecorder()
	server.handleHookPreValidate(secondRecorder, secondRequest)

	if secondRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("expected repeated missing-peer failure to return 429, got %d body=%s", secondRecorder.Code, secondRecorder.Body.String())
	}
	var response controlapipkg.HookPreValidateResponse
	if err := json.Unmarshal(secondRecorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode repeated missing-peer response: %v", err)
	}
	if response.Decision != "block" || response.DenialCode != controlapipkg.DenialCodeHookRateLimitExceeded {
		t.Fatalf("expected hook peer-auth rate-limit denial, got %#v", response)
	}
}

func TestHookPreValidate_PeerAuthFailureRateLimitBlocksRepeatedUIDMismatch(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.hookPeerAuthFailureRateLimit = 1
	server.hookPeerAuthFailureWindow = time.Hour

	wrongPeerContext := context.WithValue(context.Background(), peerIdentityContextKey, peerIdentity{
		UID: uint32(os.Getuid()) + 1,
		PID: 4242,
	})

	firstRequest := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", bytes.NewBufferString(`{"hook_event_name":"PreToolUse","tool_name":"Read","session_id":"session-hook"}`)).WithContext(wrongPeerContext)
	firstRecorder := httptest.NewRecorder()
	server.handleHookPreValidate(firstRecorder, firstRequest)

	if firstRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected first UID-mismatch failure to return 403, got %d body=%s", firstRecorder.Code, firstRecorder.Body.String())
	}

	secondRequest := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", bytes.NewBufferString(`{"hook_event_name":"PreToolUse","tool_name":"Read","session_id":"session-hook"}`)).WithContext(wrongPeerContext)
	secondRecorder := httptest.NewRecorder()
	server.handleHookPreValidate(secondRecorder, secondRequest)

	if secondRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("expected repeated UID-mismatch failure to return 429, got %d body=%s", secondRecorder.Code, secondRecorder.Body.String())
	}
	var response controlapipkg.HookPreValidateResponse
	if err := json.Unmarshal(secondRecorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode repeated UID-mismatch response: %v", err)
	}
	if response.Decision != "block" || response.DenialCode != controlapipkg.DenialCodeHookRateLimitExceeded {
		t.Fatalf("expected hook peer-auth rate-limit denial, got %#v", response)
	}
}
