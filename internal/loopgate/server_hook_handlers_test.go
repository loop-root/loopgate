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
	"time"

	"loopgate/internal/config"
	"loopgate/internal/ledger"
	"loopgate/internal/testutil"
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
	if response.ApprovalRequestID != "" {
		t.Fatalf("expected safe repo search to avoid approval request ids, got %#v", response)
	}
	if claudeHookApprovalStateFileExists(t, repoRoot, "session-hook") {
		t.Fatalf("expected safe repo search not to create Loopgate approval state")
	}
}

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
	if response.ApprovalRequestID != "" {
		t.Fatalf("expected harness-owned approval without a Loopgate approval id, got %#v", response)
	}
	if response.OperatorOverrideClass != config.OperatorOverrideClassRepoWriteSafe {
		t.Fatalf("expected repo_write_safe override class, got %#v", response)
	}
	if response.OperatorOverrideMaxDelegation != config.OperatorOverrideDelegationPersistent {
		t.Fatalf("expected persistent root delegation ceiling, got %#v", response)
	}
	if claudeHookApprovalStateFileExists(t, repoRoot, "session-hook") {
		t.Fatalf("expected harness-owned approval not to create Loopgate approval state")
	}
	if auditEventTypesContain(t, repoRoot, "approval.created") {
		t.Fatalf("expected harness-owned approval not to emit Loopgate approval.created")
	}
}

func TestHookPreValidate_AllowsWriteWithPersistentOperatorOverrideWithinRoot(t *testing.T) {
	repoRoot := t.TempDir()
	policyYAML := strings.Replace(loopgatePolicyYAML(false), "tools:\n", "tools:\n  claude_code:\n    tool_policies:\n      Write:\n        enabled: true\n        requires_approval: true\n        allowed_roots:\n          - \".\"\n", 1)
	policyYAML = strings.Replace(policyYAML, "logging:\n", "operator_overrides:\n  classes:\n    repo_write_safe:\n      max_delegation: persistent\nlogging:\n", 1)

	policySigner, err := testutil.NewPolicyTestSigner()
	if err != nil {
		t.Fatalf("new test policy signer: %v", err)
	}
	_, _, server := startLoopgateServerWithSignerAndRuntime(t, repoRoot, policyYAML, policySigner, nil, true)
	writeSignedTestOperatorOverrideDocument(t, repoRoot, policySigner, config.OperatorOverrideDocument{
		Version: "1",
		Grants: []config.OperatorOverrideGrant{
			{
				ID:           "override-20260424010101-write12345678",
				Class:        config.OperatorOverrideClassRepoWriteSafe,
				State:        "active",
				PathPrefixes: []string{"."},
				CreatedAtUTC: time.Now().UTC().Format(time.RFC3339),
			},
		},
	})
	overrideRuntime, err := server.reloadOperatorOverrideRuntimeFromDisk()
	if err != nil {
		t.Fatalf("reload operator override runtime: %v", err)
	}
	server.storeOperatorOverrideRuntime(overrideRuntime)

	writePath := filepath.Join(repoRoot, "notes.md")
	requestBody := bytes.NewBufferString(`{"hook_event_name":"PreToolUse","tool_name":"Write","tool_use_id":"toolu_write_operator_override","tool_input":{"file_path":"` + writePath + `","content":"hello"},"cwd":"` + repoRoot + `","session_id":"session-hook"}`)
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
		t.Fatalf("expected persistent operator override to allow write, got %#v", response)
	}
	if response.ApprovalRequestID != "" {
		t.Fatalf("expected delegated write to avoid approval request ids, got %#v", response)
	}
	if response.OperatorOverrideClass != config.OperatorOverrideClassRepoWriteSafe {
		t.Fatalf("expected repo_write_safe override class, got %#v", response)
	}
	if response.OperatorOverrideMaxDelegation != config.OperatorOverrideDelegationPersistent {
		t.Fatalf("expected persistent root delegation ceiling, got %#v", response)
	}
}

func TestHookPreValidate_RootDeniedPathHardDeniesDespiteOperatorOverride(t *testing.T) {
	repoRoot := t.TempDir()
	secretsDir := filepath.Join(repoRoot, "secrets")
	if err := os.MkdirAll(secretsDir, 0o755); err != nil {
		t.Fatalf("mkdir secrets dir: %v", err)
	}
	secretPath := filepath.Join(secretsDir, "token.txt")
	if err := os.WriteFile(secretPath, []byte("secret\n"), 0o600); err != nil {
		t.Fatalf("write secret target: %v", err)
	}

	policyYAML := strings.Replace(loopgatePolicyYAML(false), "tools:\n", "tools:\n  claude_code:\n    tool_policies:\n      Edit:\n        enabled: true\n        requires_approval: true\n        allowed_roots:\n          - \".\"\n        denied_paths:\n          - \"secrets\"\n", 1)
	policyYAML = strings.Replace(policyYAML, "logging:\n", "operator_overrides:\n  classes:\n    repo_edit_safe:\n      max_delegation: persistent\nlogging:\n", 1)

	policySigner, err := testutil.NewPolicyTestSigner()
	if err != nil {
		t.Fatalf("new test policy signer: %v", err)
	}
	_, _, server := startLoopgateServerWithSignerAndRuntime(t, repoRoot, policyYAML, policySigner, nil, true)
	writeSignedTestOperatorOverrideDocument(t, repoRoot, policySigner, config.OperatorOverrideDocument{
		Version: "1",
		Grants: []config.OperatorOverrideGrant{
			{
				ID:           "override-20260424010101-edit123456789",
				Class:        config.OperatorOverrideClassRepoEditSafe,
				State:        "active",
				PathPrefixes: []string{"."},
				CreatedAtUTC: time.Now().UTC().Format(time.RFC3339),
			},
		},
	})
	overrideRuntime, err := server.reloadOperatorOverrideRuntimeFromDisk()
	if err != nil {
		t.Fatalf("reload operator override runtime: %v", err)
	}
	server.storeOperatorOverrideRuntime(overrideRuntime)

	requestBody := bytes.NewBufferString(`{"hook_event_name":"PreToolUse","tool_name":"Edit","tool_use_id":"toolu_edit_denied_path","tool_input":{"file_path":"` + secretPath + `","old_string":"secret","new_string":"changed"},"cwd":"` + repoRoot + `","session_id":"session-hook"}`)
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
		t.Fatalf("expected denied path to hard deny despite operator override, got %#v", response)
	}
	if !strings.Contains(response.Reason, "matches denied path policy") {
		t.Fatalf("expected denied path reason, got %#v", response)
	}
	if response.ApprovalRequestID != "" {
		t.Fatalf("expected hard deny not to carry approval id, got %#v", response)
	}
	if claudeHookApprovalStateFileExists(t, repoRoot, "session-hook") {
		t.Fatalf("expected hard deny not to create Loopgate approval state")
	}
	if auditEventTypesContain(t, repoRoot, "approval.created") {
		t.Fatalf("expected hard deny not to emit approval.created")
	}
}

func TestHookPreValidate_AllowsEditWithDelegatedOperatorOverride(t *testing.T) {
	repoRoot := t.TempDir()
	docsDir := filepath.Join(repoRoot, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("mkdir docs dir: %v", err)
	}
	editPath := filepath.Join(docsDir, "guide.md")
	if err := os.WriteFile(editPath, []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("write edit target: %v", err)
	}

	policyYAML := strings.Replace(loopgatePolicyYAML(false), "tools:\n", "tools:\n  claude_code:\n    tool_policies:\n      Edit:\n        enabled: true\n        requires_approval: true\n        allowed_roots:\n          - \"docs\"\n      MultiEdit:\n        enabled: true\n        requires_approval: true\n        allowed_roots:\n          - \"docs\"\n", 1)
	policyYAML = strings.Replace(policyYAML, "logging:\n", "operator_overrides:\n  classes:\n    repo_edit_safe:\n      max_delegation: persistent\nlogging:\n", 1)

	policySigner, err := testutil.NewPolicyTestSigner()
	if err != nil {
		t.Fatalf("new test policy signer: %v", err)
	}
	_, _, server := startLoopgateServerWithSignerAndRuntime(t, repoRoot, policyYAML, policySigner, nil, true)

	firstBody := bytes.NewBufferString(`{"hook_event_name":"PreToolUse","tool_name":"Edit","tool_use_id":"toolu_edit_before_override","tool_input":{"file_path":"` + editPath + `","old_string":"hello","new_string":"hi"},"session_id":"session-hook"}`)
	firstRequest := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", firstBody)
	firstRequest = firstRequest.WithContext(context.WithValue(firstRequest.Context(), peerIdentityContextKey, peerIdentity{
		UID: uint32(os.Getuid()),
		PID: 4242,
	}))
	firstRecorder := httptest.NewRecorder()
	server.handleHookPreValidate(firstRecorder, firstRequest)

	var firstResponse controlapipkg.HookPreValidateResponse
	if err := json.Unmarshal(firstRecorder.Body.Bytes(), &firstResponse); err != nil {
		t.Fatalf("decode first hook response: %v", err)
	}
	if firstResponse.Decision != "ask" {
		t.Fatalf("expected edit without override to require approval, got %#v", firstResponse)
	}
	if firstResponse.OperatorOverrideClass != config.OperatorOverrideClassRepoEditSafe {
		t.Fatalf("expected repo_edit_safe operator override class, got %#v", firstResponse)
	}
	if firstResponse.OperatorOverrideMaxDelegation != config.OperatorOverrideDelegationPersistent {
		t.Fatalf("expected persistent operator override delegation, got %#v", firstResponse)
	}

	writeSignedTestOperatorOverrideDocument(t, repoRoot, policySigner, config.OperatorOverrideDocument{
		Version: "1",
		Grants: []config.OperatorOverrideGrant{
			{
				ID:           "override-20260421010101-abcd1234ef56",
				Class:        config.OperatorOverrideClassRepoEditSafe,
				State:        "active",
				PathPrefixes: []string{"docs"},
				CreatedAtUTC: time.Now().UTC().Format(time.RFC3339),
			},
		},
	})
	overrideRuntime, err := server.reloadOperatorOverrideRuntimeFromDisk()
	if err != nil {
		t.Fatalf("reload operator override runtime: %v", err)
	}
	server.storeOperatorOverrideRuntime(overrideRuntime)

	secondBody := bytes.NewBufferString(`{"hook_event_name":"PreToolUse","tool_name":"Edit","tool_use_id":"toolu_edit_after_override","tool_input":{"file_path":"` + editPath + `","old_string":"hello","new_string":"hi"},"session_id":"session-hook"}`)
	secondRequest := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", secondBody)
	secondRequest = secondRequest.WithContext(context.WithValue(secondRequest.Context(), peerIdentityContextKey, peerIdentity{
		UID: uint32(os.Getuid()),
		PID: 4242,
	}))
	secondRecorder := httptest.NewRecorder()
	server.handleHookPreValidate(secondRecorder, secondRequest)

	if secondRecorder.Code != http.StatusOK {
		t.Fatalf("expected status 200 after delegated override, got %d body=%s", secondRecorder.Code, secondRecorder.Body.String())
	}
	var secondResponse controlapipkg.HookPreValidateResponse
	if err := json.Unmarshal(secondRecorder.Body.Bytes(), &secondResponse); err != nil {
		t.Fatalf("decode second hook response: %v", err)
	}
	if secondResponse.Decision != "allow" {
		t.Fatalf("expected delegated override to allow edit, got %#v", secondResponse)
	}
	if secondResponse.OperatorOverrideClass != config.OperatorOverrideClassRepoEditSafe {
		t.Fatalf("expected repo_edit_safe operator override class on allow, got %#v", secondResponse)
	}
	if secondResponse.OperatorOverrideMaxDelegation != config.OperatorOverrideDelegationPersistent {
		t.Fatalf("expected persistent operator override delegation on allow, got %#v", secondResponse)
	}

	lastAuditEvent := readLastHookAuditEvent(t, repoRoot)
	if decision, _ := lastAuditEvent.Data["decision"].(string); decision != "allow" {
		t.Fatalf("expected delegated override audit decision allow, got %#v", lastAuditEvent.Data["decision"])
	}
	if reason, _ := lastAuditEvent.Data["reason"].(string); !strings.Contains(reason, "delegated operator override override-20260421010101-abcd1234ef56") {
		t.Fatalf("expected delegated override reason in audit, got %#v", lastAuditEvent.Data["reason"])
	}
	if overrideClass, _ := lastAuditEvent.Data["operator_override_class"].(string); overrideClass != config.OperatorOverrideClassRepoEditSafe {
		t.Fatalf("expected repo_edit_safe operator override class in audit, got %#v", lastAuditEvent.Data["operator_override_class"])
	}
	if overrideDelegation, _ := lastAuditEvent.Data["operator_override_max_delegation"].(string); overrideDelegation != config.OperatorOverrideDelegationPersistent {
		t.Fatalf("expected persistent operator override delegation in audit, got %#v", lastAuditEvent.Data["operator_override_max_delegation"])
	}
}

func TestHookPreValidate_DoesNotBypassDisabledEditWithDelegatedOperatorOverride(t *testing.T) {
	repoRoot := t.TempDir()
	docsDir := filepath.Join(repoRoot, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("mkdir docs dir: %v", err)
	}
	editPath := filepath.Join(docsDir, "guide.md")
	if err := os.WriteFile(editPath, []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("write edit target: %v", err)
	}

	policyYAML := strings.Replace(loopgatePolicyYAML(false), "tools:\n", "tools:\n  claude_code:\n    tool_policies:\n      Edit:\n        enabled: false\n        allowed_roots:\n          - \"docs\"\n      MultiEdit:\n        enabled: false\n        allowed_roots:\n          - \"docs\"\n", 1)
	policyYAML = strings.Replace(policyYAML, "logging:\n", "operator_overrides:\n  classes:\n    repo_edit_safe:\n      max_delegation: persistent\nlogging:\n", 1)

	policySigner, err := testutil.NewPolicyTestSigner()
	if err != nil {
		t.Fatalf("new test policy signer: %v", err)
	}
	_, _, server := startLoopgateServerWithSignerAndRuntime(t, repoRoot, policyYAML, policySigner, nil, true)
	writeSignedTestOperatorOverrideDocument(t, repoRoot, policySigner, config.OperatorOverrideDocument{
		Version: "1",
		Grants: []config.OperatorOverrideGrant{
			{
				ID:           "override-20260421010101-fedcba987654",
				Class:        config.OperatorOverrideClassRepoEditSafe,
				State:        "active",
				PathPrefixes: []string{"docs"},
				CreatedAtUTC: time.Now().UTC().Format(time.RFC3339),
			},
		},
	})
	overrideRuntime, err := server.reloadOperatorOverrideRuntimeFromDisk()
	if err != nil {
		t.Fatalf("reload operator override runtime: %v", err)
	}
	server.storeOperatorOverrideRuntime(overrideRuntime)

	requestBody := bytes.NewBufferString(`{"hook_event_name":"PreToolUse","tool_name":"Edit","tool_use_id":"toolu_edit_disabled_with_override","tool_input":{"file_path":"` + editPath + `","old_string":"hello","new_string":"hi"},"session_id":"session-hook"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/hook/pre-validate", requestBody)
	request = request.WithContext(context.WithValue(request.Context(), peerIdentityContextKey, peerIdentity{
		UID: uint32(os.Getuid()),
		PID: 4242,
	}))
	recorder := httptest.NewRecorder()
	server.handleHookPreValidate(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200 for disabled edit, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response controlapipkg.HookPreValidateResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode hook response: %v", err)
	}
	if response.Decision != "block" {
		t.Fatalf("expected disabled edit to remain blocked, got %#v", response)
	}
	if !strings.Contains(response.Reason, "disabled by Claude Code tool policy") {
		t.Fatalf("expected disabled-tool reason, got %#v", response)
	}

	lastAuditEvent := readLastHookAuditEvent(t, repoRoot)
	if decision, _ := lastAuditEvent.Data["decision"].(string); decision != "block" {
		t.Fatalf("expected disabled edit audit decision block, got %#v", lastAuditEvent.Data["decision"])
	}
	if reason, _ := lastAuditEvent.Data["reason"].(string); !strings.Contains(reason, "disabled by Claude Code tool policy") {
		t.Fatalf("expected disabled-tool audit reason, got %#v", lastAuditEvent.Data["reason"])
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
	if response.ApprovalRequestID != "" {
		t.Fatalf("expected harness-owned ask without Loopgate approval id, got %#v", response)
	}
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
	var response controlapipkg.HookPreValidateResponse
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
	var response controlapipkg.HookPreValidateResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode hook response: %v", err)
	}
	if response.Decision != "block" {
		t.Fatalf("expected secondary governance hook to block, got %#v", response)
	}
	if response.DenialCode != controlapipkg.DenialCodeHookEventUnimplemented {
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

func claudeHookApprovalStateFileExists(t *testing.T, repoRoot string, sessionID string) bool {
	t.Helper()

	storageKey := claudeHookSessionStorageKey(sessionID)
	statePath := filepath.Join(repoRoot, "runtime", "state", "claude_hook_sessions", storageKey, claudeHookApprovalsFileName)
	_, err := os.Stat(statePath)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatalf("stat claude hook approval state: %v", err)
	return false
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
