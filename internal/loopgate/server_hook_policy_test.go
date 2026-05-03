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

	var response controlapipkg.HookPreValidateResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode hook response: %v", err)
	}
	if response.Decision != "block" {
		t.Fatalf("expected unknown tool to block, got %#v", response)
	}
	if response.DenialCode != controlapipkg.DenialCodeHookUnknownTool {
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

	var response controlapipkg.HookPreValidateResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode hook response: %v", err)
	}
	if response.Decision != "block" {
		t.Fatalf("expected disabled Read tool to block, got %#v", response)
	}
	if !strings.Contains(response.Reason, "disabled by Claude Code tool policy") {
		t.Fatalf("expected disablement reason, got %#v", response)
	}
	if response.OperatorOverrideClass != "repo_read_search" {
		t.Fatalf("expected repo_read_search operator override class, got %#v", response)
	}
	if response.OperatorOverrideMaxDelegation != "none" {
		t.Fatalf("expected none operator override delegation, got %#v", response)
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

	var response controlapipkg.HookPreValidateResponse
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

func TestHookPreValidate_DeniesBashCommandDeniedPrefixWithNonSpaceWhitespace(t *testing.T) {
	repoRoot := t.TempDir()
	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	policyYAML := strings.Replace(loopgatePolicyYAML(false), "tools:\n", "tools:\n  claude_code:\n    tool_policies:\n      Bash:\n        enabled: true\n        denied_command_prefixes:\n          - \"rm -rf\"\n", 1)
	writeSignedTestPolicyYAML(t, repoRoot, policyYAML)
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	requestBody := bytes.NewBufferString("{\"tool_name\":\"Bash\",\"tool_input\":{\"command\":\"rm\\t-rf /tmp/demo\"},\"session_id\":\"session-hook\"}")
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
	if response.Decision != "block" {
		t.Fatalf("expected denied prefix with tab separator to block, got %#v", response)
	}
	if !strings.Contains(response.Reason, "denied prefix") {
		t.Fatalf("expected denied prefix reason, got %#v", response)
	}
}

func TestHookPreValidate_AllowsBashCommandAllowedPrefixWithNonSpaceWhitespace(t *testing.T) {
	repoRoot := t.TempDir()
	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	policyYAML := strings.Replace(loopgatePolicyYAML(false), "tools:\n", "tools:\n  claude_code:\n    tool_policies:\n      Bash:\n        enabled: true\n        allowed_command_prefixes:\n          - \"git status\"\n", 1)
	writeSignedTestPolicyYAML(t, repoRoot, policyYAML)
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	requestBody := bytes.NewBufferString("{\"tool_name\":\"Bash\",\"tool_input\":{\"command\":\"git\\tstatus --short\"},\"session_id\":\"session-hook\"}")
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
	if response.Decision != "allow" {
		t.Fatalf("expected allowed prefix with tab separator to allow, got %#v", response)
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

	var response controlapipkg.HookPreValidateResponse
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

func TestHookPreValidate_AllowsRepoReadSearchWithoutApproval(t *testing.T) {
	repoRoot := t.TempDir()
	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	policyYAML := strings.Replace(loopgatePolicyYAML(false), "tools:\n", "tools:\n  claude_code:\n    tool_policies:\n      Grep:\n        enabled: true\n        allowed_roots:\n          - \".\"\n", 1)
	writeSignedTestPolicyYAML(t, repoRoot, policyYAML)
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	requestBody := bytes.NewBufferString(`{"hook_event_name":"PreToolUse","tool_name":"Grep","tool_use_id":"toolu_grep_safe","tool_input":{"pattern":"Loopgate","path":"` + repoRoot + `"},"cwd":"` + repoRoot + `","session_id":"session-hook"}`)
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
	if response.Decision != "allow" {
		t.Fatalf("expected safe repo search to allow without approval, got %#v", response)
	}
	if response.ReasonCode != controlapipkg.HookReasonCodePolicyAllowed {
		t.Fatalf("expected policy allowed reason code, got %#v", response)
	}
	if response.ApprovalRequestID != "" {
		t.Fatalf("expected safe repo search to avoid approval request ids, got %#v", response)
	}
	if response.ApprovalOwner != "" || len(response.ApprovalOptions) != 0 {
		t.Fatalf("expected safe repo search to omit approval metadata, got %#v", response)
	}
	if claudeHookApprovalStateFileExists(t, repoRoot, "session-hook") {
		t.Fatalf("expected safe repo search not to create Loopgate approval state")
	}

	lastAuditEvent := readLastHookAuditEvent(t, repoRoot)
	if reasonCode, _ := lastAuditEvent.Data["reason_code"].(string); reasonCode != controlapipkg.HookReasonCodePolicyAllowed {
		t.Fatalf("expected policy allowed reason code in audit, got %#v", lastAuditEvent.Data["reason_code"])
	}
}
