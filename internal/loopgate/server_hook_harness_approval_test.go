package loopgate

import (
	"bytes"
	"context"
	"encoding/json"
	"loopgate/internal/config"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHookPreValidate_WriteApprovalDelegatesToHarnessWithoutLoopgateApprovalRecord(t *testing.T) {
	repoRoot := t.TempDir()
	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	policyYAML := strings.Replace(loopgatePolicyYAML(false), "tools:\n", "tools:\n  claude_code:\n    tool_policies:\n      Write:\n        enabled: true\n        requires_approval: true\n        allowed_roots:\n          - \".\"\n", 1)
	policyYAML = strings.Replace(policyYAML, "logging:\n", "operator_overrides:\n  classes:\n    repo_write_safe:\n      max_delegation: persistent\nlogging:\n", 1)
	writeSignedTestPolicyYAML(t, repoRoot, policyYAML)
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	writePath := filepath.Join(repoRoot, "notes.md")
	requestBody := bytes.NewBufferString(`{"hook_event_name":"PreToolUse","tool_name":"Write","tool_use_id":"toolu_write_harness_approval","tool_input":{"file_path":"` + writePath + `","content":"hello"},"cwd":"` + repoRoot + `","session_id":"session-hook"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", requestBody)
	request = request.WithContext(context.WithValue(request.Context(), peerIdentityContextKey, peerIdentity{
		UID: uint32(os.Getuid()),
		PID: 4242,
	}))
	recorder := httptest.NewRecorder()

	server.handleHookPreValidate(recorder, request)

	var response controlapipkg.HookPreValidateResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode hook response: %v", err)
	}
	if response.Decision != "ask" {
		t.Fatalf("expected write inside root to ask the harness, got %#v", response)
	}
	if response.ReasonCode != controlapipkg.HookReasonCodeApprovalRequired {
		t.Fatalf("expected approval required reason code, got %#v", response)
	}
	if response.ApprovalRequestID != "" {
		t.Fatalf("expected harness-owned approval without a Loopgate approval id, got %#v", response)
	}
	if response.ApprovalOwner != controlapipkg.HookApprovalOwnerHarness {
		t.Fatalf("expected harness approval owner, got %#v", response)
	}
	requireHookApprovalOptions(t, response.ApprovalOptions, controlapipkg.HookApprovalOptionOnce, controlapipkg.HookApprovalOptionPersistent)
	if response.OperatorOverrideClass != config.OperatorOverrideClassRepoWriteSafe {
		t.Fatalf("expected repo_write_safe override class, got %#v", response)
	}
	if response.OperatorOverrideMaxDelegation != config.OperatorOverrideDelegationPersistent {
		t.Fatalf("expected persistent root delegation ceiling, got %#v", response)
	}
	if response.OperatorOverrideMaxGrantScope != "permanent" {
		t.Fatalf("expected permanent max grant scope, got %#v", response)
	}
	if claudeHookApprovalStateFileExists(t, repoRoot, "session-hook") {
		t.Fatalf("expected harness-owned approval not to create Loopgate approval state")
	}
	if auditEventTypesContain(t, repoRoot, "approval.created") {
		t.Fatalf("expected harness-owned approval not to emit Loopgate approval.created")
	}

	lastAuditEvent := readLastHookAuditEvent(t, repoRoot)
	if reasonCode, _ := lastAuditEvent.Data["reason_code"].(string); reasonCode != controlapipkg.HookReasonCodeApprovalRequired {
		t.Fatalf("expected approval required reason code in audit, got %#v", lastAuditEvent.Data["reason_code"])
	}
	if approvalOwner, _ := lastAuditEvent.Data["approval_owner"].(string); approvalOwner != controlapipkg.HookApprovalOwnerHarness {
		t.Fatalf("expected harness approval owner in audit, got %#v", lastAuditEvent.Data["approval_owner"])
	}
	requireHookApprovalOptions(t, auditStringSlice(lastAuditEvent.Data["approval_options"]), controlapipkg.HookApprovalOptionOnce, controlapipkg.HookApprovalOptionPersistent)
	if maxGrantScope, _ := lastAuditEvent.Data["operator_override_max_grant_scope"].(string); maxGrantScope != "permanent" {
		t.Fatalf("expected permanent max grant scope in audit, got %#v", lastAuditEvent.Data["operator_override_max_grant_scope"])
	}
}

func TestHookPreValidate_NeedsApprovalReturnsHarnessOwnedAsk(t *testing.T) {
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
	var response controlapipkg.HookPreValidateResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode hook response: %v", err)
	}
	if response.Decision != "ask" {
		t.Fatalf("expected approval-required Bash call to ask, got %#v", response)
	}
	if response.ReasonCode != controlapipkg.HookReasonCodeApprovalRequired {
		t.Fatalf("expected approval required reason code, got %#v", response)
	}
	if response.ApprovalRequestID != "" {
		t.Fatalf("expected harness-owned ask without Loopgate approval id, got %#v", response)
	}
	if response.ApprovalOwner != controlapipkg.HookApprovalOwnerHarness {
		t.Fatalf("expected harness approval owner, got %#v", response)
	}
	requireHookApprovalOptions(t, response.ApprovalOptions, controlapipkg.HookApprovalOptionOnce)
	if response.OperatorOverrideClass != "repo_bash_safe" {
		t.Fatalf("expected repo_bash_safe operator override class, got %#v", response)
	}
	if response.OperatorOverrideMaxDelegation != "none" {
		t.Fatalf("expected none operator override delegation, got %#v", response)
	}

	if claudeHookApprovalStateFileExists(t, repoRoot, "session-hook") {
		t.Fatalf("expected harness-owned ask not to create Loopgate approval state")
	}

	lastAuditEvent := readLastHookAuditEvent(t, repoRoot)
	if decision, _ := lastAuditEvent.Data["decision"].(string); decision != "ask" {
		t.Fatalf("expected ask decision in audit, got %#v", lastAuditEvent.Data["decision"])
	}
	if reasonCode, _ := lastAuditEvent.Data["reason_code"].(string); reasonCode != controlapipkg.HookReasonCodeApprovalRequired {
		t.Fatalf("expected approval required reason code in audit, got %#v", lastAuditEvent.Data["reason_code"])
	}
	if approvalOwner, _ := lastAuditEvent.Data["approval_owner"].(string); approvalOwner != controlapipkg.HookApprovalOwnerHarness {
		t.Fatalf("expected harness approval owner in audit, got %#v", lastAuditEvent.Data["approval_owner"])
	}
	requireHookApprovalOptions(t, auditStringSlice(lastAuditEvent.Data["approval_options"]), controlapipkg.HookApprovalOptionOnce)
	if approvalRequestID, _ := lastAuditEvent.Data["hook_approval_request_id"].(string); approvalRequestID != "" {
		t.Fatalf("expected no Loopgate hook approval id in audit, got %#v", lastAuditEvent.Data["hook_approval_request_id"])
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
	if overrideClass, _ := lastAuditEvent.Data["operator_override_class"].(string); overrideClass != "repo_bash_safe" {
		t.Fatalf("expected repo_bash_safe override class in audit, got %#v", lastAuditEvent.Data["operator_override_class"])
	}
	if overrideDelegation, _ := lastAuditEvent.Data["operator_override_max_delegation"].(string); overrideDelegation != "none" {
		t.Fatalf("expected none override delegation in audit, got %#v", lastAuditEvent.Data["operator_override_max_delegation"])
	}
	if auditEventTypesContain(t, repoRoot, "approval.created") {
		t.Fatalf("expected harness-owned ask not to emit approval.created")
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
	var response controlapipkg.HookPreValidateResponse
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
	if hookApprovalRequestID, _ := lastAuditEvent.Data["hook_approval_request_id"].(string); hookApprovalRequestID != "" {
		t.Fatalf("expected PermissionRequest not to match a Loopgate-tracked approval id, got %#v", lastAuditEvent.Data["hook_approval_request_id"])
	}
	if hookApprovalSurface, _ := lastAuditEvent.Data["hook_approval_surface"].(string); hookApprovalSurface != "" {
		t.Fatalf("expected PermissionRequest not to carry a Loopgate approval surface, got %#v", lastAuditEvent.Data["hook_approval_surface"])
	}
	if toolRequestFingerprint, _ := lastAuditEvent.Data["tool_request_fingerprint_sha256"].(string); toolRequestFingerprint == "" {
		t.Fatalf("expected tool request fingerprint in audit, got %#v", lastAuditEvent.Data["tool_request_fingerprint_sha256"])
	}
}

func TestHookPreValidate_PostToolUseRecordsWithoutPendingHarnessApprovalState(t *testing.T) {
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
	var response controlapipkg.HookPreValidateResponse
	if err := json.Unmarshal(posttoolRecorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode hook response: %v", err)
	}
	if response.Decision != "allow" {
		t.Fatalf("expected PostToolUse to allow, got %#v", response)
	}

	if claudeHookApprovalStateFileExists(t, repoRoot, "session-hook") {
		t.Fatalf("expected PostToolUse not to create Loopgate approval state for harness-owned approval")
	}

	lastAuditEvent := readLastHookAuditEvent(t, repoRoot)
	if hookHandlingMode, _ := lastAuditEvent.Data["hook_handling_mode"].(string); hookHandlingMode != claudeCodeHookHandlingModeEnforced {
		t.Fatalf("expected enforced handling mode, got %#v", lastAuditEvent.Data["hook_handling_mode"])
	}
	if hookApprovalState, _ := lastAuditEvent.Data["hook_approval_state"].(string); hookApprovalState != "" {
		t.Fatalf("expected no Loopgate hook approval state in audit, got %#v", lastAuditEvent.Data["hook_approval_state"])
	}
	if auditEventTypesContain(t, repoRoot, "approval.granted") {
		t.Fatalf("expected harness-owned approval not to emit Loopgate approval.granted")
	}
}
