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
	"strings"
	"testing"
)

func TestHookPreValidate_SessionStartStaysAuditOnly(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	requestBody := bytes.NewBufferString(`{"hook_event_name":"SessionStart","session_id":"session-hook"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", requestBody)
	request = request.WithContext(context.WithValue(request.Context(), peerIdentityContextKey, peerIdentity{
		UID: uint32(os.Getuid()),
		PID: 4242,
	}))
	recorder := httptest.NewRecorder()

	server.handleHookPreValidate(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response controlapipkg.HookPreValidateResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode hook response: %v", err)
	}
	if response.Decision != "allow" {
		t.Fatalf("expected session start to allow, got %#v", response)
	}
	if response.AdditionalContext != "" {
		t.Fatalf("expected no additional context on session start, got %#v", response)
	}

	lastAuditEvent := readLastHookAuditEvent(t, repoRoot)
	if hookHandlingMode, _ := lastAuditEvent.Data["hook_handling_mode"].(string); hookHandlingMode != claudeCodeHookHandlingModeAuditOnly {
		t.Fatalf("expected audit-only handling mode, got %#v", lastAuditEvent.Data["hook_handling_mode"])
	}

	sessionState := readClaudeHookSessionState(t, repoRoot)
	if len(sessionState.Sessions) != 1 {
		t.Fatalf("expected one claude hook session record, got %#v", sessionState)
	}
	if sessionState.Sessions[0].SessionID != "session-hook" {
		t.Fatalf("expected session-hook session id, got %#v", sessionState.Sessions[0])
	}
	if sessionState.Sessions[0].State != claudeHookSessionStateActive {
		t.Fatalf("expected active session state, got %#v", sessionState.Sessions[0])
	}
}

func TestHookPreValidate_UserPromptSubmitStaysAuditOnlyEvenWhenMemoryWouldMatch(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	requestBody := bytes.NewBufferString(`{"hook_event_name":"UserPromptSubmit","prompt":"please use my name when drafting the command","session_id":"session-hook"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", requestBody)
	request = request.WithContext(context.WithValue(request.Context(), peerIdentityContextKey, peerIdentity{
		UID: uint32(os.Getuid()),
		PID: 4242,
	}))
	recorder := httptest.NewRecorder()

	server.handleHookPreValidate(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response controlapipkg.HookPreValidateResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode hook response: %v", err)
	}
	if response.Decision != "allow" {
		t.Fatalf("expected user prompt submit to allow, got %#v", response)
	}
	if response.AdditionalContext != "" {
		t.Fatalf("expected no additional context on user prompt submit, got %#v", response)
	}

	lastAuditEvent := readLastHookAuditEvent(t, repoRoot)
	if hookEventName, _ := lastAuditEvent.Data["hook_event_name"].(string); hookEventName != claudeCodeHookEventUserPromptSubmit {
		t.Fatalf("expected user prompt submit hook event, got %#v", lastAuditEvent.Data["hook_event_name"])
	}
	if hookHandlingMode, _ := lastAuditEvent.Data["hook_handling_mode"].(string); hookHandlingMode != claudeCodeHookHandlingModeAuditOnly {
		t.Fatalf("expected audit-only handling mode, got %#v", lastAuditEvent.Data["hook_handling_mode"])
	}
}

func TestHookPreValidate_UserPromptSubmitNoMatchStaysAuditOnly(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	requestBody := bytes.NewBufferString(`{"hook_event_name":"UserPromptSubmit","prompt":"tell me a joke about nebula potatoes","session_id":"session-hook"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", requestBody)
	request = request.WithContext(context.WithValue(request.Context(), peerIdentityContextKey, peerIdentity{
		UID: uint32(os.Getuid()),
		PID: 4242,
	}))
	recorder := httptest.NewRecorder()

	server.handleHookPreValidate(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response controlapipkg.HookPreValidateResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode hook response: %v", err)
	}
	if response.Decision != "allow" {
		t.Fatalf("expected user prompt submit to allow, got %#v", response)
	}
	if response.AdditionalContext != "" {
		t.Fatalf("expected no additional context for no-match prompt, got %#v", response)
	}

	lastAuditEvent := readLastHookAuditEvent(t, repoRoot)
	if hookHandlingMode, _ := lastAuditEvent.Data["hook_handling_mode"].(string); hookHandlingMode != claudeCodeHookHandlingModeAuditOnly {
		t.Fatalf("expected audit-only handling mode, got %#v", lastAuditEvent.Data["hook_handling_mode"])
	}
	if _, exists := lastAuditEvent.Data["continuity_thread_id"]; exists {
		t.Fatalf("expected no continuity_thread_id in audit, got %#v", lastAuditEvent.Data["continuity_thread_id"])
	}
}

func TestHookPreValidate_SessionEndRecordsLifecycleReason(t *testing.T) {
	repoRoot := t.TempDir()
	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	requestBody := bytes.NewBufferString(`{"hook_event_name":"SessionEnd","reason":"prompt_input_exit","session_id":"session-hook"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", requestBody)
	request = request.WithContext(context.WithValue(request.Context(), peerIdentityContextKey, peerIdentity{
		UID: uint32(os.Getuid()),
		PID: 4242,
	}))
	recorder := httptest.NewRecorder()

	server.handleHookPreValidate(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response controlapipkg.HookPreValidateResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode hook response: %v", err)
	}
	if response.Decision != "allow" {
		t.Fatalf("expected session end to allow, got %#v", response)
	}

	lastAuditEvent := readLastHookAuditEvent(t, repoRoot)
	if hookEventName, _ := lastAuditEvent.Data["hook_event_name"].(string); hookEventName != claudeCodeHookEventSessionEnd {
		t.Fatalf("expected session end hook event, got %#v", lastAuditEvent.Data["hook_event_name"])
	}
	if hookReason, _ := lastAuditEvent.Data["hook_reason"].(string); hookReason != "prompt_input_exit" {
		t.Fatalf("expected prompt_input_exit hook reason, got %#v", lastAuditEvent.Data["hook_reason"])
	}
	if hookHandlingMode, _ := lastAuditEvent.Data["hook_handling_mode"].(string); hookHandlingMode != claudeCodeHookHandlingModeAuditOnly {
		t.Fatalf("expected audit-only handling mode, got %#v", lastAuditEvent.Data["hook_handling_mode"])
	}

	sessionState := readClaudeHookSessionState(t, repoRoot)
	if len(sessionState.Sessions) != 1 {
		t.Fatalf("expected one claude hook session record, got %#v", sessionState)
	}
	if sessionState.Sessions[0].State != claudeHookSessionStateEnded {
		t.Fatalf("expected ended session state, got %#v", sessionState.Sessions[0])
	}
	if sessionState.Sessions[0].ExitReason != "prompt_input_exit" {
		t.Fatalf("expected prompt_input_exit exit reason, got %#v", sessionState.Sessions[0])
	}
}

func TestHookPreValidate_SessionEndDoesNotAbandonHarnessOwnedApprovals(t *testing.T) {
	repoRoot := t.TempDir()
	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	policyYAML := strings.Replace(loopgatePolicyYAML(false), "tools:\n", "tools:\n  claude_code:\n    tool_policies:\n      Bash:\n        enabled: true\n        requires_approval: true\n", 1)
	writeSignedTestPolicyYAML(t, repoRoot, policyYAML)
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	pretoolBody := bytes.NewBufferString(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_use_id":"toolu_approve_shell","tool_input":{"command":"git status"},"session_id":"session-hook"}`)
	pretoolRequest := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", pretoolBody)
	pretoolRequest = pretoolRequest.WithContext(context.WithValue(pretoolRequest.Context(), peerIdentityContextKey, peerIdentity{
		UID: uint32(os.Getuid()),
		PID: 4242,
	}))
	pretoolRecorder := httptest.NewRecorder()
	server.handleHookPreValidate(pretoolRecorder, pretoolRequest)

	sessionEndBody := bytes.NewBufferString(`{"hook_event_name":"SessionEnd","reason":"prompt_input_exit","session_id":"session-hook"}`)
	sessionEndRequest := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", sessionEndBody)
	sessionEndRequest = sessionEndRequest.WithContext(context.WithValue(sessionEndRequest.Context(), peerIdentityContextKey, peerIdentity{
		UID: uint32(os.Getuid()),
		PID: 4242,
	}))
	sessionEndRecorder := httptest.NewRecorder()

	server.handleHookPreValidate(sessionEndRecorder, sessionEndRequest)

	if sessionEndRecorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", sessionEndRecorder.Code, sessionEndRecorder.Body.String())
	}

	if claudeHookApprovalStateFileExists(t, repoRoot, "session-hook") {
		t.Fatalf("expected SessionEnd not to create Loopgate approval state for harness-owned approval")
	}

	lastAuditEvent := readLastHookAuditEvent(t, repoRoot)
	if hookHandlingMode, _ := lastAuditEvent.Data["hook_handling_mode"].(string); hookHandlingMode != claudeCodeHookHandlingModeAuditOnly {
		t.Fatalf("expected audit-only handling mode, got %#v", lastAuditEvent.Data["hook_handling_mode"])
	}
	if auditEventTypesContain(t, repoRoot, "approval.cancelled") {
		t.Fatalf("expected harness-owned approval not to emit approval.cancelled")
	}
}
