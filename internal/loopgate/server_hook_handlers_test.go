package loopgate

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"loopgate/internal/ledger"
)

type claudeHookSessionStateTestFile struct {
	SchemaVersion string                      `json:"schema_version"`
	Sessions      []claudeHookSessionTestWire `json:"sessions,omitempty"`
}

type claudeHookSessionTestWire struct {
	SessionID    string `json:"session_id"`
	State        string `json:"state"`
	StartedAtUTC string `json:"started_at_utc"`
	EndedAtUTC   string `json:"ended_at_utc,omitempty"`
	ExitReason   string `json:"exit_reason,omitempty"`
}

type claudeHookApprovalStateTestFile struct {
	SchemaVersion string                       `json:"schema_version"`
	Approvals     []claudeHookApprovalTestWire `json:"approvals,omitempty"`
}

type claudeHookApprovalTestWire struct {
	ApprovalRequestID string `json:"approval_request_id"`
	SessionID         string `json:"session_id"`
	ToolUseID         string `json:"tool_use_id"`
	ToolName          string `json:"tool_name"`
	ApprovalSurface   string `json:"approval_surface,omitempty"`
	Reason            string `json:"reason,omitempty"`
	State             string `json:"state"`
	CreatedAtUTC      string `json:"created_at_utc"`
	ResolvedAtUTC     string `json:"resolved_at_utc,omitempty"`
	ResolutionReason  string `json:"resolution_reason,omitempty"`
	HookEventName     string `json:"hook_event_name,omitempty"`
}

func TestHookPreValidate_DeniesUnknownToolByDefault(t *testing.T) {
	repoRoot := t.TempDir()
	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	requestBody := bytes.NewBufferString(`{"tool_name":"TodoWrite","session_id":"session-hook"}`)
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

	var response HookPreValidateResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode hook response: %v", err)
	}
	if response.Decision != "block" {
		t.Fatalf("expected unknown tool to block, got %#v", response)
	}
	if response.DenialCode != DenialCodeHookUnknownTool {
		t.Fatalf("expected hook unknown tool denial code, got %#v", response)
	}
}

func TestHookPreValidate_DeniesToolDisabledByClaudeCodePolicy(t *testing.T) {
	repoRoot := t.TempDir()
	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	policyYAML := strings.Replace(loopgatePolicyYAML(false), "tools:\n", "tools:\n  claude_code:\n    tool_policies:\n      Read:\n        enabled: false\n", 1)
	writeSignedTestPolicyYAML(t, repoRoot, policyYAML)
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	readPath := filepath.Join(repoRoot, "README.md")
	requestBody := bytes.NewBufferString(`{"tool_name":"Read","tool_input":{"file_path":"` + readPath + `"},"session_id":"session-hook"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", requestBody)
	request = request.WithContext(context.WithValue(request.Context(), peerIdentityContextKey, peerIdentity{
		UID: uint32(os.Getuid()),
		PID: 4242,
	}))
	recorder := httptest.NewRecorder()

	server.handleHookPreValidate(recorder, request)

	var response HookPreValidateResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode hook response: %v", err)
	}
	if response.Decision != "block" {
		t.Fatalf("expected disabled Read tool to block, got %#v", response)
	}
	if !strings.Contains(response.Reason, "disabled by Claude Code tool policy") {
		t.Fatalf("expected disablement reason, got %#v", response)
	}
}

func TestHookPreValidate_DeniesBashCommandDeniedPrefix(t *testing.T) {
	repoRoot := t.TempDir()
	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	policyYAML := strings.Replace(loopgatePolicyYAML(false), "tools:\n", "tools:\n  claude_code:\n    tool_policies:\n      Bash:\n        enabled: true\n        denied_command_prefixes:\n          - \"rm -rf\"\n", 1)
	writeSignedTestPolicyYAML(t, repoRoot, policyYAML)
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	requestBody := bytes.NewBufferString(`{"tool_name":"Bash","tool_input":{"command":"rm -rf /tmp/demo"},"session_id":"session-hook"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", requestBody)
	request = request.WithContext(context.WithValue(request.Context(), peerIdentityContextKey, peerIdentity{
		UID: uint32(os.Getuid()),
		PID: 4242,
	}))
	recorder := httptest.NewRecorder()

	server.handleHookPreValidate(recorder, request)

	var response HookPreValidateResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode hook response: %v", err)
	}
	if response.Decision != "block" {
		t.Fatalf("expected denied prefix to block, got %#v", response)
	}
	if !strings.Contains(response.Reason, "denied prefix") {
		t.Fatalf("expected denied prefix reason, got %#v", response)
	}
}

func TestHookPreValidate_DeniesReadOutsideAllowedRoots(t *testing.T) {
	repoRoot := t.TempDir()
	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	policyYAML := strings.Replace(loopgatePolicyYAML(false), "tools:\n", "tools:\n  claude_code:\n    tool_policies:\n      Read:\n        enabled: true\n        allowed_roots:\n          - \"docs\"\n", 1)
	writeSignedTestPolicyYAML(t, repoRoot, policyYAML)
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	requestBody := bytes.NewBufferString(`{"tool_name":"Read","tool_input":{"file_path":"/tmp/outside.txt"},"session_id":"session-hook"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", requestBody)
	request = request.WithContext(context.WithValue(request.Context(), peerIdentityContextKey, peerIdentity{
		UID: uint32(os.Getuid()),
		PID: 4242,
	}))
	recorder := httptest.NewRecorder()

	server.handleHookPreValidate(recorder, request)

	var response HookPreValidateResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode hook response: %v", err)
	}
	if response.Decision != "block" {
		t.Fatalf("expected read outside allowed roots to block, got %#v", response)
	}
	if !strings.Contains(response.Reason, "outside allowed roots") {
		t.Fatalf("expected allowed roots reason, got %#v", response)
	}
}

func TestHookPreValidate_NeedsApprovalReturnsAskAndPersistsLocalHookApproval(t *testing.T) {
	repoRoot := t.TempDir()
	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	policyYAML := strings.Replace(loopgatePolicyYAML(false), "tools:\n", "tools:\n  claude_code:\n    tool_policies:\n      Bash:\n        enabled: true\n        requires_approval: true\n", 1)
	writeSignedTestPolicyYAML(t, repoRoot, policyYAML)
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	requestBody := bytes.NewBufferString(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_use_id":"toolu_approve_shell","tool_input":{"command":"git status"},"session_id":"session-hook"}`)
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
	var response HookPreValidateResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode hook response: %v", err)
	}
	if response.Decision != "ask" {
		t.Fatalf("expected approval-required Bash call to ask, got %#v", response)
	}
	if response.ApprovalRequestID == "" {
		t.Fatalf("expected approval request id, got %#v", response)
	}

	approvalState := readClaudeHookApprovalState(t, repoRoot, "session-hook")
	if len(approvalState.Approvals) != 1 {
		t.Fatalf("expected one local hook approval, got %#v", approvalState)
	}
	if approvalState.Approvals[0].State != claudeHookApprovalStatePending {
		t.Fatalf("expected pending hook approval state, got %#v", approvalState.Approvals[0])
	}
	if approvalState.Approvals[0].ToolUseID != "toolu_approve_shell" {
		t.Fatalf("expected tracked tool_use_id, got %#v", approvalState.Approvals[0])
	}
	if approvalState.Approvals[0].ApprovalSurface != claudeHookApprovalSurfaceInlineClaude {
		t.Fatalf("expected inline Claude approval surface, got %#v", approvalState.Approvals[0])
	}

	lastAuditEvent := readLastHookAuditEvent(t, repoRoot)
	if decision, _ := lastAuditEvent.Data["decision"].(string); decision != "ask" {
		t.Fatalf("expected ask decision in audit, got %#v", lastAuditEvent.Data["decision"])
	}
	if approvalRequestID, _ := lastAuditEvent.Data["hook_approval_request_id"].(string); approvalRequestID == "" {
		t.Fatalf("expected hook_approval_request_id in audit, got %#v", lastAuditEvent.Data["hook_approval_request_id"])
	}
	if toolTargetKind, _ := lastAuditEvent.Data["tool_target_kind"].(string); toolTargetKind != "shell_command" {
		t.Fatalf("expected shell_command tool target kind, got %#v", lastAuditEvent.Data["tool_target_kind"])
	}
	if commandVerb, _ := lastAuditEvent.Data["command_verb"].(string); commandVerb != "git" {
		t.Fatalf("expected git command verb, got %#v", lastAuditEvent.Data["command_verb"])
	}
	if actorRef, _ := lastAuditEvent.Data["actor_ref"].(string); actorRef != "claude_session:session-hook" {
		t.Fatalf("expected claude session actor ref, got %#v", lastAuditEvent.Data["actor_ref"])
	}
	if !auditEventTypesContain(t, repoRoot, "approval.created") {
		t.Fatalf("expected approval.created ledger event for inline Claude approval")
	}
}

func TestHookPreValidate_PermissionRequestMatchesPendingClaudeApproval(t *testing.T) {
	repoRoot := t.TempDir()
	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	policyYAML := strings.Replace(loopgatePolicyYAML(false), "tools:\n", "tools:\n  claude_code:\n    tool_policies:\n      Bash:\n        enabled: true\n        requires_approval: true\n", 1)
	writeSignedTestPolicyYAML(t, repoRoot, policyYAML)
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	pretoolBody := bytes.NewBufferString(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_use_id":"toolu_approve_shell","tool_input":{"command":"git status"},"cwd":"` + repoRoot + `","session_id":"session-hook"}`)
	pretoolRequest := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", pretoolBody)
	pretoolRequest = pretoolRequest.WithContext(context.WithValue(pretoolRequest.Context(), peerIdentityContextKey, peerIdentity{
		UID: uint32(os.Getuid()),
		PID: 4242,
	}))
	pretoolRecorder := httptest.NewRecorder()
	server.handleHookPreValidate(pretoolRecorder, pretoolRequest)

	permissionBody := bytes.NewBufferString(`{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"git status"},"cwd":"` + repoRoot + `","session_id":"session-hook"}`)
	permissionRequest := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", permissionBody)
	permissionRequest = permissionRequest.WithContext(context.WithValue(permissionRequest.Context(), peerIdentityContextKey, peerIdentity{
		UID: uint32(os.Getuid()),
		PID: 4242,
	}))
	permissionRecorder := httptest.NewRecorder()

	server.handleHookPreValidate(permissionRecorder, permissionRequest)

	if permissionRecorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", permissionRecorder.Code, permissionRecorder.Body.String())
	}
	var response HookPreValidateResponse
	if err := json.Unmarshal(permissionRecorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode hook response: %v", err)
	}
	if response.Decision != "allow" {
		t.Fatalf("expected PermissionRequest to allow current inline flow, got %#v", response)
	}

	lastAuditEvent := readLastHookAuditEvent(t, repoRoot)
	if hookEventName, _ := lastAuditEvent.Data["hook_event_name"].(string); hookEventName != "PermissionRequest" {
		t.Fatalf("expected PermissionRequest hook event, got %#v", lastAuditEvent.Data["hook_event_name"])
	}
	if hookApprovalRequestID, _ := lastAuditEvent.Data["hook_approval_request_id"].(string); hookApprovalRequestID == "" {
		t.Fatalf("expected matched approval id in audit, got %#v", lastAuditEvent.Data["hook_approval_request_id"])
	}
	if hookApprovalSurface, _ := lastAuditEvent.Data["hook_approval_surface"].(string); hookApprovalSurface != claudeHookApprovalSurfaceInlineClaude {
		t.Fatalf("expected inline Claude approval surface in audit, got %#v", lastAuditEvent.Data["hook_approval_surface"])
	}
	if toolRequestFingerprint, _ := lastAuditEvent.Data["tool_request_fingerprint_sha256"].(string); toolRequestFingerprint == "" {
		t.Fatalf("expected tool request fingerprint in audit, got %#v", lastAuditEvent.Data["tool_request_fingerprint_sha256"])
	}
}

func TestHookPreValidate_AuditIncludesHookSurfaceClassification(t *testing.T) {
	repoRoot := t.TempDir()
	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	requestBody := bytes.NewBufferString(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git status"},"session_id":"session-hook"}`)
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

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(auditBytes)), "\n")
	lastAuditEvent, ok := ledger.ParseEvent([]byte(lines[len(lines)-1]))
	if !ok {
		t.Fatalf("parse hook audit event: %s", lines[len(lines)-1])
	}
	if hookEventName, _ := lastAuditEvent.Data["hook_event_name"].(string); hookEventName != "PreToolUse" {
		t.Fatalf("expected hook_event_name PreToolUse, got %#v", lastAuditEvent.Data["hook_event_name"])
	}
	if hookSurfaceClass, _ := lastAuditEvent.Data["hook_surface_class"].(string); hookSurfaceClass != claudeCodeHookSurfacePrimaryAuthority {
		t.Fatalf("expected primary authority surface class, got %#v", lastAuditEvent.Data["hook_surface_class"])
	}
	if hookHandlingMode, _ := lastAuditEvent.Data["hook_handling_mode"].(string); hookHandlingMode != claudeCodeHookHandlingModeEnforced {
		t.Fatalf("expected enforced hook handling mode, got %#v", lastAuditEvent.Data["hook_handling_mode"])
	}
}

func TestHookPreValidate_AllowsObservabilityHookEventAuditOnly(t *testing.T) {
	repoRoot := t.TempDir()
	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	requestBody := bytes.NewBufferString(`{"hook_event_name":"ConfigChange","session_id":"session-hook"}`)
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
	var response HookPreValidateResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode hook response: %v", err)
	}
	if response.Decision != "allow" {
		t.Fatalf("expected observability hook to allow, got %#v", response)
	}

	lastAuditEvent := readLastHookAuditEvent(t, repoRoot)
	if hookSurfaceClass, _ := lastAuditEvent.Data["hook_surface_class"].(string); hookSurfaceClass != claudeCodeHookSurfaceObservability {
		t.Fatalf("expected observability surface class, got %#v", lastAuditEvent.Data["hook_surface_class"])
	}
	if hookHandlingMode, _ := lastAuditEvent.Data["hook_handling_mode"].(string); hookHandlingMode != claudeCodeHookHandlingModeAuditOnly {
		t.Fatalf("expected audit-only handling mode, got %#v", lastAuditEvent.Data["hook_handling_mode"])
	}
}

func TestHookPreValidate_BlocksSecondaryGovernanceHookEventUntilImplemented(t *testing.T) {
	repoRoot := t.TempDir()
	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	requestBody := bytes.NewBufferString(`{"hook_event_name":"TaskCreated","session_id":"session-hook"}`)
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
	var response HookPreValidateResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode hook response: %v", err)
	}
	if response.Decision != "block" {
		t.Fatalf("expected secondary governance hook to block, got %#v", response)
	}
	if response.DenialCode != DenialCodeHookEventUnimplemented {
		t.Fatalf("expected hook event unimplemented denial code, got %#v", response)
	}

	lastAuditEvent := readLastHookAuditEvent(t, repoRoot)
	if hookSurfaceClass, _ := lastAuditEvent.Data["hook_surface_class"].(string); hookSurfaceClass != claudeCodeHookSurfaceSecondaryGovernance {
		t.Fatalf("expected secondary governance surface class, got %#v", lastAuditEvent.Data["hook_surface_class"])
	}
	if hookHandlingMode, _ := lastAuditEvent.Data["hook_handling_mode"].(string); hookHandlingMode != claudeCodeHookHandlingModeEnforced {
		t.Fatalf("expected enforced handling mode, got %#v", lastAuditEvent.Data["hook_handling_mode"])
	}
}

func TestHookPreValidate_PostToolUseResolvesPendingLocalHookApproval(t *testing.T) {
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

	posttoolBody := bytes.NewBufferString(`{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_use_id":"toolu_approve_shell","tool_input":{"command":"git status"},"session_id":"session-hook"}`)
	posttoolRequest := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", posttoolBody)
	posttoolRequest = posttoolRequest.WithContext(context.WithValue(posttoolRequest.Context(), peerIdentityContextKey, peerIdentity{
		UID: uint32(os.Getuid()),
		PID: 4242,
	}))
	posttoolRecorder := httptest.NewRecorder()

	server.handleHookPreValidate(posttoolRecorder, posttoolRequest)

	if posttoolRecorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", posttoolRecorder.Code, posttoolRecorder.Body.String())
	}
	var response HookPreValidateResponse
	if err := json.Unmarshal(posttoolRecorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode hook response: %v", err)
	}
	if response.Decision != "allow" {
		t.Fatalf("expected PostToolUse to allow, got %#v", response)
	}

	approvalState := readClaudeHookApprovalState(t, repoRoot, "session-hook")
	if len(approvalState.Approvals) != 1 {
		t.Fatalf("expected one local hook approval, got %#v", approvalState)
	}
	if approvalState.Approvals[0].State != claudeHookApprovalStateExecuted {
		t.Fatalf("expected executed hook approval state, got %#v", approvalState.Approvals[0])
	}
	if approvalState.Approvals[0].HookEventName != claudeCodeHookEventPostToolUse {
		t.Fatalf("expected PostToolUse hook event recorded on approval, got %#v", approvalState.Approvals[0])
	}

	lastAuditEvent := readLastHookAuditEvent(t, repoRoot)
	if hookHandlingMode, _ := lastAuditEvent.Data["hook_handling_mode"].(string); hookHandlingMode != claudeCodeHookHandlingModeStateTransition {
		t.Fatalf("expected state transition handling mode, got %#v", lastAuditEvent.Data["hook_handling_mode"])
	}
	if hookApprovalState, _ := lastAuditEvent.Data["hook_approval_state"].(string); hookApprovalState != claudeHookApprovalStateExecuted {
		t.Fatalf("expected executed hook approval state in audit, got %#v", lastAuditEvent.Data["hook_approval_state"])
	}
	if !auditEventTypesContain(t, repoRoot, "approval.granted") {
		t.Fatalf("expected approval.granted ledger event for inline Claude approval execution")
	}
}

func TestHookPreValidate_ReadAuditIncludesResolvedTargetPath(t *testing.T) {
	repoRoot := t.TempDir()
	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	readPath := filepath.Join(repoRoot, "README.md")
	requestBody := bytes.NewBufferString(`{"hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"` + readPath + `"},"session_id":"session-hook"}`)
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
	lastAuditEvent := readLastHookAuditEvent(t, repoRoot)
	if toolTargetKind, _ := lastAuditEvent.Data["tool_target_kind"].(string); toolTargetKind != "filesystem_path" {
		t.Fatalf("expected filesystem_path tool target kind, got %#v", lastAuditEvent.Data["tool_target_kind"])
	}
	if resolvedTargetPath, _ := lastAuditEvent.Data["resolved_target_path"].(string); resolvedTargetPath != readPath {
		t.Fatalf("expected resolved target path %q, got %#v", readPath, lastAuditEvent.Data["resolved_target_path"])
	}
}

func TestHookPreValidate_MinimalHookAuditProjectionOmitsCommandPreview(t *testing.T) {
	repoRoot := t.TempDir()
	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	policyYAML := strings.Replace(loopgatePolicyYAML(false), "logging:\n", "logging:\n  audit_detail:\n    hook_projection_level: minimal\n", 1)
	writeSignedTestPolicyYAML(t, repoRoot, policyYAML)
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	requestBody := bytes.NewBufferString(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git status --short"},"session_id":"session-hook"}`)
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

	lastAuditEvent := readLastHookAuditEvent(t, repoRoot)
	if commandPreview, exists := lastAuditEvent.Data["command_redacted_preview"]; exists {
		t.Fatalf("expected minimal hook audit projection to omit command preview, got %#v", commandPreview)
	}
	if commandSHA256, _ := lastAuditEvent.Data["command_sha256"].(string); commandSHA256 == "" {
		t.Fatalf("expected command sha256 in minimal hook audit projection, got %#v", lastAuditEvent.Data["command_sha256"])
	}
	if commandVerb, _ := lastAuditEvent.Data["command_verb"].(string); commandVerb != "git" {
		t.Fatalf("expected git command verb in minimal hook audit projection, got %#v", lastAuditEvent.Data["command_verb"])
	}
}

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
	var response HookPreValidateResponse
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
	var response HookPreValidateResponse
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
	var response HookPreValidateResponse
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
	var response HookPreValidateResponse
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

func TestHookPreValidate_SessionEndAbandonsPendingLocalHookApprovals(t *testing.T) {
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

	approvalState := readClaudeHookApprovalState(t, repoRoot, "session-hook")
	if len(approvalState.Approvals) != 1 {
		t.Fatalf("expected one local hook approval, got %#v", approvalState)
	}
	if approvalState.Approvals[0].State != claudeHookApprovalStateAbandoned {
		t.Fatalf("expected abandoned hook approval state, got %#v", approvalState.Approvals[0])
	}
	if approvalState.Approvals[0].HookEventName != claudeCodeHookEventSessionEnd {
		t.Fatalf("expected SessionEnd hook event recorded on approval, got %#v", approvalState.Approvals[0])
	}

	lastAuditEvent := readLastHookAuditEvent(t, repoRoot)
	if hookHandlingMode, _ := lastAuditEvent.Data["hook_handling_mode"].(string); hookHandlingMode != claudeCodeHookHandlingModeStateTransition {
		t.Fatalf("expected state transition handling mode, got %#v", lastAuditEvent.Data["hook_handling_mode"])
	}
	if !auditEventTypesContain(t, repoRoot, "approval.cancelled") {
		t.Fatalf("expected approval.cancelled ledger event for abandoned inline Claude approval")
	}
}

func readLastHookAuditEvent(t *testing.T, repoRoot string) ledger.Event {
	t.Helper()

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(auditBytes)), "\n")
	lastAuditEvent, ok := ledger.ParseEvent([]byte(lines[len(lines)-1]))
	if !ok {
		t.Fatalf("parse hook audit event: %s", lines[len(lines)-1])
	}
	return lastAuditEvent
}

func auditEventTypesContain(t *testing.T, repoRoot string, wantedType string) bool {
	t.Helper()

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(auditBytes)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		ev, ok := ledger.ParseEvent([]byte(line))
		if !ok {
			t.Fatalf("parse hook audit event: %s", line)
		}
		if ev.Type == wantedType {
			return true
		}
	}
	return false
}

func readClaudeHookSessionState(t *testing.T, repoRoot string) claudeHookSessionStateTestFile {
	t.Helper()

	stateBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "claude_hook_sessions.json"))
	if err != nil {
		t.Fatalf("read claude hook session state: %v", err)
	}
	var stateFile claudeHookSessionStateTestFile
	if err := json.Unmarshal(stateBytes, &stateFile); err != nil {
		t.Fatalf("decode claude hook session state: %v", err)
	}
	return stateFile
}

func readClaudeHookApprovalState(t *testing.T, repoRoot string, sessionID string) claudeHookApprovalStateTestFile {
	t.Helper()

	storageKey := claudeHookSessionStorageKey(sessionID)
	statePath := filepath.Join(repoRoot, "runtime", "state", "claude_hook_sessions", storageKey, claudeHookApprovalsFileName)
	stateBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read claude hook approval state: %v", err)
	}
	var stateFile claudeHookApprovalStateTestFile
	if err := json.Unmarshal(stateBytes, &stateFile); err != nil {
		t.Fatalf("decode claude hook approval state: %v", err)
	}
	return stateFile
}
