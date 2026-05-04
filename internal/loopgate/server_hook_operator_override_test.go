package loopgate

import (
	"bytes"
	"context"
	"encoding/json"
	"loopgate/internal/config"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"loopgate/internal/testutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
	if response.ReasonCode != controlapipkg.HookReasonCodeOperatorOverrideAllowed {
		t.Fatalf("expected operator override reason code, got %#v", response)
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

	lastAuditEvent := readLastHookAuditEvent(t, repoRoot)
	if reasonCode, _ := lastAuditEvent.Data["reason_code"].(string); reasonCode != controlapipkg.HookReasonCodeOperatorOverrideAllowed {
		t.Fatalf("expected operator override reason code in audit, got %#v", lastAuditEvent.Data["reason_code"])
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
	if response.ReasonCode != controlapipkg.HookReasonCodePolicyDenied {
		t.Fatalf("expected policy denied reason code, got %#v", response)
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

	lastAuditEvent := readLastHookAuditEvent(t, repoRoot)
	if reasonCode, _ := lastAuditEvent.Data["reason_code"].(string); reasonCode != controlapipkg.HookReasonCodePolicyDenied {
		t.Fatalf("expected policy denied reason code in audit, got %#v", lastAuditEvent.Data["reason_code"])
	}
	if _, exists := lastAuditEvent.Data["approval_owner"]; exists {
		t.Fatalf("expected hard deny not to include approval owner, got %#v", lastAuditEvent.Data["approval_owner"])
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
	if firstResponse.ReasonCode != controlapipkg.HookReasonCodeApprovalRequired {
		t.Fatalf("expected approval required reason code, got %#v", firstResponse)
	}
	if firstResponse.ApprovalOwner != controlapipkg.HookApprovalOwnerHarness {
		t.Fatalf("expected harness approval owner, got %#v", firstResponse)
	}
	requireHookApprovalOptions(t, firstResponse.ApprovalOptions, controlapipkg.HookApprovalOptionOnce, controlapipkg.HookApprovalOptionPersistent)
	if firstResponse.OperatorOverrideClass != config.OperatorOverrideClassRepoEditSafe {
		t.Fatalf("expected repo_edit_safe operator override class, got %#v", firstResponse)
	}
	if firstResponse.OperatorOverrideMaxDelegation != config.OperatorOverrideDelegationPersistent {
		t.Fatalf("expected persistent operator override delegation, got %#v", firstResponse)
	}
	if firstResponse.OperatorOverrideMaxGrantScope != "permanent" {
		t.Fatalf("expected permanent operator grant scope, got %#v", firstResponse)
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
	if secondResponse.ReasonCode != controlapipkg.HookReasonCodeOperatorOverrideAllowed {
		t.Fatalf("expected operator override reason code, got %#v", secondResponse)
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
	if reasonCode, _ := lastAuditEvent.Data["reason_code"].(string); reasonCode != controlapipkg.HookReasonCodeOperatorOverrideAllowed {
		t.Fatalf("expected delegated override reason code in audit, got %#v", lastAuditEvent.Data["reason_code"])
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
