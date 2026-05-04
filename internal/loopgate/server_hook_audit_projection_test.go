package loopgate

import (
	"bytes"
	"context"
	"encoding/json"
	"loopgate/internal/ledger"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
