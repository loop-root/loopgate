package loopgate

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"loopgate/internal/config"
	"loopgate/internal/ledger"
	modelpkg "loopgate/internal/model"
	policypkg "loopgate/internal/policy"
	"loopgate/internal/sandbox"
	"loopgate/internal/testutil"
	toolspkg "loopgate/internal/tools"
)

type delayedModelProvider struct {
	delay time.Duration
}

func (provider delayedModelProvider) Generate(ctx context.Context, request modelpkg.Request) (modelpkg.Response, error) {
	select {
	case <-time.After(provider.delay):
	case <-ctx.Done():
		return modelpkg.Response{}, ctx.Err()
	}
	return modelpkg.Response{
		AssistantText: fmt.Sprintf("delayed reply to %q", request.UserMessage),
		ProviderName:  "delayed",
		ModelName:     "delayed",
		FinishReason:  "stop",
	}, nil
}

type failingModelProvider struct{}

func (provider failingModelProvider) Generate(ctx context.Context, request modelpkg.Request) (modelpkg.Response, error) {
	_ = ctx
	return modelpkg.Response{
		ProviderName: "failing",
		ModelName:    "failing",
		Prompt: modelpkg.PromptMetadata{
			PersonaHash: "persona",
			PolicyHash:  "policy",
			PromptHash:  "prompt",
		},
		Timing: modelpkg.Timing{
			PromptCompile:     5 * time.Millisecond,
			SecretResolve:     4 * time.Millisecond,
			ProviderRoundTrip: 3 * time.Millisecond,
			ResponseDecode:    2 * time.Millisecond,
			TotalGenerate:     14 * time.Millisecond,
		},
	}, errors.New("synthetic model failure")
}

func TestClientExecuteCapability_ReadAndWrite(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	writeResponse, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-write",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "notes.txt",
			"content": "hello loopgate",
		},
	})
	if err != nil {
		t.Fatalf("execute fs_write: %v", err)
	}
	if writeResponse.Status != ResponseStatusSuccess {
		t.Fatalf("unexpected write response: %#v", writeResponse)
	}

	readResponse, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-read",
		Capability: "fs_read",
		Arguments: map[string]string{
			"path": "notes.txt",
		},
	})
	if err != nil {
		t.Fatalf("execute fs_read: %v", err)
	}
	if readResponse.StructuredResult["content"] != "hello loopgate" {
		t.Fatalf("unexpected structured read result: %#v", readResponse.StructuredResult)
	}
	if readResponse.QuarantineRef != "" {
		t.Fatalf("expected no quarantine ref for filesystem read, got %#v", readResponse)
	}
	if promptEligible, ok := readResponse.Metadata["prompt_eligible"].(bool); !ok || !promptEligible {
		t.Fatalf("expected fs_read to be prompt-eligible, got %#v", readResponse.Metadata)
	}
	if memoryEligible, ok := readResponse.Metadata["memory_eligible"].(bool); !ok || memoryEligible {
		t.Fatalf("expected fs_read to be non-memory-eligible, got %#v", readResponse.Metadata)
	}
	if displayOnly, ok := readResponse.Metadata["display_only"].(bool); !ok || displayOnly {
		t.Fatalf("expected fs_read to not be display_only, got %#v", readResponse.Metadata)
	}

	if len(status.Capabilities) == 0 {
		t.Fatal("expected capabilities in status")
	}
}

func TestClientExecuteCapability_DeniesRawMemoryFilesystemAccess(t *testing.T) {
	repoRoot := t.TempDir()
	memoryDir := filepath.Join(repoRoot, ".morph", "memory")
	if err := os.MkdirAll(memoryDir, 0o700); err != nil {
		t.Fatalf("mkdir memory dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoryDir, "keys.json"), []byte("{\"keys\":[]}\n"), 0o600); err != nil {
		t.Fatalf("write keys file: %v", err)
	}

	policyYAML := strings.Replace(loopgatePolicyYAML(false), "    denied_paths: []\n", "    denied_paths:\n      - \".morph/memory\"\n      - \"runtime/state/memory\"\n", 1)
	client, _, _ := startLoopgateServer(t, repoRoot, policyYAML)

	readResponse, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-memory-read",
		Capability: "fs_read",
		Arguments: map[string]string{
			"path": ".morph/memory/keys.json",
		},
	})
	if err != nil {
		t.Fatalf("execute fs_read: %v", err)
	}
	if readResponse.Status != ResponseStatusError {
		t.Fatalf("expected blocked read response, got %#v", readResponse)
	}
	if !strings.Contains(readResponse.DenialReason, "path denied") {
		t.Fatalf("expected path denial, got %#v", readResponse)
	}

	writeResponse, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-memory-write",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    ".morph/memory/keys.json",
			"content": "{\"keys\":[{\"id\":\"user.name\"}]}\n",
		},
	})
	if err != nil {
		t.Fatalf("execute fs_write: %v", err)
	}
	if writeResponse.Status != ResponseStatusError {
		t.Fatalf("expected blocked write response, got %#v", writeResponse)
	}
	if !strings.Contains(writeResponse.DenialReason, "path denied") {
		t.Fatalf("expected path denial, got %#v", writeResponse)
	}
}

func TestExecuteCapabilityRequest_ShellCommandOutsideAllowlistIsDeniedBeforeApproval(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgateShellPolicyYAML(true, []string{"git"}))

	response, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-shell-policy-deny",
		Capability: "shell_exec",
		Arguments: map[string]string{
			"command": "env",
		},
	})
	if err != nil {
		t.Fatalf("execute shell_exec: %v", err)
	}
	if response.Status != ResponseStatusDenied {
		t.Fatalf("expected denied shell response, got %#v", response)
	}
	if response.DenialCode != DenialCodePolicyDenied {
		t.Fatalf("expected policy denial code, got %#v", response)
	}
	if response.ApprovalRequired {
		t.Fatalf("expected allowlist denial before approval creation, got %#v", response)
	}
}

func TestClientExecuteCapability_RequiresApproval(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))

	response, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-approval",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "approved.txt",
			"content": "approved",
		},
	})
	if err != nil {
		t.Fatalf("execute pending approval: %v", err)
	}
	if !response.ApprovalRequired {
		t.Fatalf("expected approval required, got %#v", response)
	}
	if response.DenialCode != DenialCodeApprovalRequired {
		t.Fatalf("expected approval-required denial code, got %#v", response)
	}
	if approvalClass, _ := response.Metadata["approval_class"].(string); approvalClass != ApprovalClassWriteSandboxPath {
		t.Fatalf("expected approval_class %q, got %#v", ApprovalClassWriteSandboxPath, response.Metadata)
	}

	resolvedResponse, err := client.DecideApproval(context.Background(), response.ApprovalRequestID, true)
	if err != nil {
		t.Fatalf("approve request: %v", err)
	}
	if resolvedResponse.Status != ResponseStatusSuccess {
		t.Fatalf("unexpected resolved response: %#v", resolvedResponse)
	}

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read loopgate events: %v", err)
	}
	var foundApprovalCreated bool
	var foundApprovalGranted bool
	var foundCapabilityExecuted bool
	var grantedBeforeExecuted bool
	approvalID := response.ApprovalRequestID
	for _, line := range strings.Split(strings.TrimSpace(string(auditBytes)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var auditEvent ledger.Event
		if err := json.Unmarshal([]byte(line), &auditEvent); err != nil {
			t.Fatalf("decode audit event: %v", err)
		}
		switch auditEvent.Type {
		case "approval.created":
			if auditEvent.Data["approval_request_id"] != approvalID {
				continue
			}
			if auditEvent.Data["approval_class"] != ApprovalClassWriteSandboxPath {
				t.Fatalf("expected approval.created approval_class %q, got %#v", ApprovalClassWriteSandboxPath, auditEvent.Data)
			}
			foundApprovalCreated = true
		case "approval.granted":
			if auditEvent.Data["approval_request_id"] != approvalID {
				continue
			}
			if auditEvent.Data["approval_class"] != ApprovalClassWriteSandboxPath {
				t.Fatalf("expected approval.granted approval_class %q, got %#v", ApprovalClassWriteSandboxPath, auditEvent.Data)
			}
			foundApprovalGranted = true
			if !foundCapabilityExecuted {
				grantedBeforeExecuted = true
			}
		case "capability.executed":
			if auditEvent.Data["request_id"] != "req-approval" {
				continue
			}
			foundCapabilityExecuted = true
		}
	}
	if !foundApprovalCreated {
		t.Fatal("expected approval.created audit event for approved request")
	}
	if !foundApprovalGranted {
		t.Fatal("expected approval.granted audit event for approved request")
	}
	if !foundCapabilityExecuted {
		t.Fatal("expected capability.executed audit event after approval")
	}
	if !grantedBeforeExecuted {
		t.Fatal("expected approval.granted ledger line before capability.executed (audit must precede consumption-side execution record)")
	}
}

func TestExecuteCapabilityRequest_OperatorMountWriteRequiresApprovalForOperator(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))
	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}

	controlSessionID := "cs-operator-mount-write"
	server.mu.Lock()
	server.sessions[controlSessionID] = controlSession{
		ID:                       controlSessionID,
		ActorLabel:               "operator",
		ClientSessionLabel:       "operator-session",
		OperatorMountPaths:       []string{resolvedRepoRoot},
		PrimaryOperatorMountPath: resolvedRepoRoot,
		RequestedCapabilities:    capabilitySet([]string{"operator_mount.fs_write"}),
		ExpiresAt:                time.Now().UTC().Add(time.Hour),
		CreatedAt:                time.Now().UTC(),
	}
	server.mu.Unlock()

	response := server.executeCapabilityRequest(
		withOperatorMountControlSession(context.Background(), controlSessionID),
		capabilityToken{
			TokenID:             "tok-operator-mount-write",
			ControlSessionID:    controlSessionID,
			ActorLabel:          "operator",
			ClientSessionLabel:  "operator-session",
			AllowedCapabilities: capabilitySet([]string{"operator_mount.fs_write"}),
			ExpiresAt:           time.Now().UTC().Add(time.Hour),
		},
		CapabilityRequest{
			RequestID:  "req-operator-mount-write",
			Capability: "operator_mount.fs_write",
			Arguments: map[string]string{
				"path":    "test.md",
				"content": "# blocked until approval\n",
			},
		},
		true,
	)

	if !response.ApprovalRequired {
		t.Fatalf("expected approval required, got %#v", response)
	}
	if response.Status != ResponseStatusPendingApproval {
		t.Fatalf("expected pending approval, got %#v", response)
	}
	if response.DenialCode != DenialCodeApprovalRequired {
		t.Fatalf("expected approval-required denial code, got %#v", response)
	}
	if approvalClass, _ := response.Metadata["approval_class"].(string); approvalClass != ApprovalClassWriteHostFolder {
		t.Fatalf("expected approval_class %q, got %#v", ApprovalClassWriteHostFolder, response.Metadata)
	}
	if approvalReason, _ := response.Metadata["approval_reason"].(string); approvalReason != fmt.Sprintf("Grant write access to %s for 8 hours", resolvedRepoRoot) {
		t.Fatalf("expected approval_reason for root grant, got %#v", response.Metadata)
	}
	server.mu.Lock()
	pendingApproval, found := server.approvals[response.ApprovalRequestID]
	server.mu.Unlock()
	if !found {
		t.Fatalf("pending approval %q not found", response.ApprovalRequestID)
	}
	if pendingApproval.Reason != fmt.Sprintf("Grant write access to %s for 8 hours", resolvedRepoRoot) {
		t.Fatalf("pending approval reason = %q", pendingApproval.Reason)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "test.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected file to remain unwritten before approval, stat err=%v", err)
	}
}

func TestCommitApprovalGrantConsumed_EnablesOperatorMountWriteGrant(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))
	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("eval symlinks repoRoot: %v", err)
	}

	nowUTC := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	server.SetNowForTest(func() time.Time { return nowUTC })

	controlSessionID := "cs-operator-mount-grant"
	server.mu.Lock()
	server.sessions[controlSessionID] = controlSession{
		ID:                       controlSessionID,
		ActorLabel:               "operator",
		ClientSessionLabel:       "operator-session",
		OperatorMountPaths:       []string{repoRoot},
		PrimaryOperatorMountPath: repoRoot,
		RequestedCapabilities:    capabilitySet([]string{"operator_mount.fs_write"}),
		ExpiresAt:                nowUTC.Add(time.Hour),
		CreatedAt:                nowUTC,
	}
	server.mu.Unlock()

	pendingResponse := server.executeCapabilityRequest(
		withOperatorMountControlSession(context.Background(), controlSessionID),
		capabilityToken{
			TokenID:             "tok-operator-mount-write",
			ControlSessionID:    controlSessionID,
			ActorLabel:          "operator",
			ClientSessionLabel:  "operator-session",
			AllowedCapabilities: capabilitySet([]string{"operator_mount.fs_write"}),
			ExpiresAt:           nowUTC.Add(time.Hour),
		},
		CapabilityRequest{
			RequestID:  "req-operator-mount-grant-1",
			Capability: "operator_mount.fs_write",
			Arguments: map[string]string{
				"path":    "first.md",
				"content": "# first\n",
			},
		},
		true,
	)
	if !pendingResponse.ApprovalRequired {
		t.Fatalf("expected approval required, got %#v", pendingResponse)
	}
	decisionNonce, _ := pendingResponse.Metadata["approval_decision_nonce"].(string)
	if strings.TrimSpace(decisionNonce) == "" {
		t.Fatalf("expected approval_decision_nonce, got %#v", pendingResponse.Metadata)
	}

	if err := server.commitApprovalGrantConsumed(pendingResponse.ApprovalRequestID, decisionNonce); err != nil {
		t.Fatalf("commit approval grant consumed: %v", err)
	}

	server.mu.Lock()
	sessionAfterGrant := server.sessions[controlSessionID]
	grantExpiresAt, granted := sessionAfterGrant.OperatorMountWriteGrants[resolvedRepoRoot]
	server.mu.Unlock()
	if !granted {
		t.Fatalf("expected operator mount write grant for %q, got %#v", resolvedRepoRoot, sessionAfterGrant.OperatorMountWriteGrants)
	}
	if !grantExpiresAt.Equal(nowUTC.Add(operatorMountWriteGrantTTL)) {
		t.Fatalf("grant expires at %v want %v", grantExpiresAt, nowUTC.Add(operatorMountWriteGrantTTL))
	}

	secondResponse := server.executeCapabilityRequest(
		withOperatorMountControlSession(context.Background(), controlSessionID),
		capabilityToken{
			TokenID:             "tok-operator-mount-write-2",
			ControlSessionID:    controlSessionID,
			ActorLabel:          "operator",
			ClientSessionLabel:  "operator-session",
			AllowedCapabilities: capabilitySet([]string{"operator_mount.fs_write"}),
			ExpiresAt:           nowUTC.Add(time.Hour),
		},
		CapabilityRequest{
			RequestID:  "req-operator-mount-grant-2",
			Capability: "operator_mount.fs_write",
			Arguments: map[string]string{
				"path":    "second.md",
				"content": "# second\n",
			},
		},
		true,
	)
	if secondResponse.Status != ResponseStatusSuccess {
		t.Fatalf("expected granted write success, got %#v", secondResponse)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "second.md")); err != nil {
		t.Fatalf("expected second write to succeed: %v", err)
	}
}

func TestExecuteCapabilityRequest_ExpiredOperatorMountWriteGrantRequiresApprovalAgain(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))
	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("eval symlinks repoRoot: %v", err)
	}

	nowUTC := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	server.SetNowForTest(func() time.Time { return nowUTC })

	controlSessionID := "cs-operator-mount-expired-grant"
	server.mu.Lock()
	server.sessions[controlSessionID] = controlSession{
		ID:                       controlSessionID,
		ActorLabel:               "operator",
		ClientSessionLabel:       "operator-session",
		OperatorMountPaths:       []string{repoRoot},
		PrimaryOperatorMountPath: repoRoot,
		OperatorMountWriteGrants: map[string]time.Time{
			resolvedRepoRoot: nowUTC.Add(-time.Minute),
		},
		RequestedCapabilities: capabilitySet([]string{"operator_mount.fs_write"}),
		ExpiresAt:             nowUTC.Add(time.Hour),
		CreatedAt:             nowUTC,
	}
	server.mu.Unlock()

	response := server.executeCapabilityRequest(
		withOperatorMountControlSession(context.Background(), controlSessionID),
		capabilityToken{
			TokenID:             "tok-operator-mount-expired",
			ControlSessionID:    controlSessionID,
			ActorLabel:          "operator",
			ClientSessionLabel:  "operator-session",
			AllowedCapabilities: capabilitySet([]string{"operator_mount.fs_write"}),
			ExpiresAt:           nowUTC.Add(time.Hour),
		},
		CapabilityRequest{
			RequestID:  "req-operator-mount-expired",
			Capability: "operator_mount.fs_write",
			Arguments: map[string]string{
				"path":    "expired.md",
				"content": "# expired\n",
			},
		},
		true,
	)
	if !response.ApprovalRequired {
		t.Fatalf("expected approval required after grant expiry, got %#v", response)
	}
}

func TestNewServer_IgnoresStalePolicyJSONForOperatorMountWriteApproval(t *testing.T) {
	repoRoot := t.TempDir()
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(true))
	configStateDir := filepath.Join(repoRoot, "runtime", "state", "config")
	if err := os.MkdirAll(configStateDir, 0o700); err != nil {
		t.Fatalf("mkdir config state dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configStateDir, "policy.json"), []byte(`{
  "version": "0.1.0",
  "tools": {
    "filesystem": {
      "read_enabled": true,
      "write_enabled": true,
      "write_requires_approval": false,
      "allowed_roots": ["."],
      "denied_paths": ["runtime/state", "runtime/audit", "runtime/tmp", "core/policy", "config/runtime.yaml"]
    }
  }
}`), 0o600); err != nil {
		t.Fatalf("write stale policy json: %v", err)
	}
	writeTestMorphlingClassPolicy(t, repoRoot)

	server, err := NewServer(repoRoot, filepath.Join(t.TempDir(), "loopgate.sock"))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if !server.policy.Tools.Filesystem.WriteRequiresApproval {
		t.Fatal("expected repository policy yaml to remain authoritative over stale policy.json")
	}

	nowUTC := time.Date(2026, 4, 8, 1, 0, 0, 0, time.UTC)
	server.SetNowForTest(func() time.Time { return nowUTC })

	controlSessionID := "cs-stale-policy-json"
	server.mu.Lock()
	server.sessions[controlSessionID] = controlSession{
		ID:                       controlSessionID,
		ActorLabel:               "operator",
		ClientSessionLabel:       "operator-session",
		OperatorMountPaths:       []string{repoRoot},
		PrimaryOperatorMountPath: repoRoot,
		RequestedCapabilities:    capabilitySet([]string{"operator_mount.fs_write"}),
		ExpiresAt:                nowUTC.Add(time.Hour),
		CreatedAt:                nowUTC,
	}
	server.mu.Unlock()

	response := server.executeCapabilityRequest(
		withOperatorMountControlSession(context.Background(), controlSessionID),
		capabilityToken{
			TokenID:             "tok-stale-policy-json",
			ControlSessionID:    controlSessionID,
			ActorLabel:          "operator",
			ClientSessionLabel:  "operator-session",
			AllowedCapabilities: capabilitySet([]string{"operator_mount.fs_write"}),
			ExpiresAt:           nowUTC.Add(time.Hour),
		},
		CapabilityRequest{
			RequestID:  "req-stale-policy-json",
			Capability: "operator_mount.fs_write",
			Arguments: map[string]string{
				"path":    "stale.json.md",
				"content": "# stale json\n",
			},
		},
		true,
	)
	if !response.ApprovalRequired {
		t.Fatalf("expected first mounted write to require approval under repository yaml, got %#v", response)
	}
}

func TestSandboxImportAndStageAndExport(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	hostRootPath := t.TempDir()
	resolvedHostRootPath, err := filepath.EvalSymlinks(hostRootPath)
	if err != nil {
		t.Fatalf("eval host root symlinks: %v", err)
	}
	pinTestProcessAsExpectedClient(t, server)
	client.SetOperatorMountPaths([]string{hostRootPath}, hostRootPath)
	client.ConfigureSession("operator", "operator-sandbox-flow", advertisedSessionCapabilityNames(status))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure operator sandbox token: %v", err)
	}

	hostSourcePath := filepath.Join(hostRootPath, "example.txt")
	if err := os.WriteFile(hostSourcePath, []byte("sandbox flow"), 0o600); err != nil {
		t.Fatalf("write host source: %v", err)
	}

	importResponse, err := client.SandboxImport(context.Background(), SandboxImportRequest{
		HostSourcePath:  hostSourcePath,
		DestinationName: "example.txt",
	})
	if err != nil {
		t.Fatalf("sandbox import: %v", err)
	}
	if importResponse.Action != "import" {
		t.Fatalf("unexpected import response: %#v", importResponse)
	}
	if importResponse.SandboxRoot != sandbox.VirtualHome {
		t.Fatalf("expected virtual sandbox root %q, got %#v", sandbox.VirtualHome, importResponse)
	}
	if importResponse.SandboxAbsolutePath != "/morph/home/imports/example.txt" {
		t.Fatalf("expected virtual sandbox path, got %#v", importResponse)
	}

	stageResponse, err := client.SandboxStage(context.Background(), SandboxStageRequest{
		SandboxSourcePath: "/morph/home/imports/example.txt",
		OutputName:        "export-me.txt",
	})
	if err != nil {
		t.Fatalf("sandbox stage: %v", err)
	}
	if stageResponse.Action != "stage" {
		t.Fatalf("unexpected stage response: %#v", stageResponse)
	}
	if stageResponse.ArtifactRef == "" {
		t.Fatalf("expected staged artifact ref, got %#v", stageResponse)
	}
	if stageResponse.SourceSandboxPath != "/morph/home/imports/example.txt" {
		t.Fatalf("expected virtual source sandbox path, got %#v", stageResponse)
	}
	if stageResponse.SandboxAbsolutePath != "/morph/home/outputs/export-me.txt" {
		t.Fatalf("expected virtual staged path, got %#v", stageResponse)
	}

	metadataResponse, err := client.SandboxMetadata(context.Background(), SandboxMetadataRequest{
		SandboxSourcePath: "/morph/home/outputs/export-me.txt",
	})
	if err != nil {
		t.Fatalf("sandbox metadata: %v", err)
	}
	if metadataResponse.ArtifactRef != stageResponse.ArtifactRef {
		t.Fatalf("expected artifact ref %q, got %#v", stageResponse.ArtifactRef, metadataResponse)
	}
	if metadataResponse.ContentSHA256 != stageResponse.ContentSHA256 {
		t.Fatalf("expected content hash %q, got %#v", stageResponse.ContentSHA256, metadataResponse)
	}
	if metadataResponse.SourceSandboxPath != "/morph/home/imports/example.txt" {
		t.Fatalf("expected virtual metadata source path, got %#v", metadataResponse)
	}

	server.mu.Lock()
	controlSession := server.sessions[client.controlSessionID]
	if controlSession.OperatorMountWriteGrants == nil {
		controlSession.OperatorMountWriteGrants = make(map[string]time.Time)
	}
	controlSession.OperatorMountWriteGrants[resolvedHostRootPath] = server.now().UTC().Add(operatorMountWriteGrantTTL)
	server.sessions[client.controlSessionID] = controlSession
	server.mu.Unlock()

	hostDestinationPath := filepath.Join(hostRootPath, "exported.txt")
	exportResponse, err := client.SandboxExport(context.Background(), SandboxExportRequest{
		SandboxSourcePath:   "/morph/home/outputs/export-me.txt",
		HostDestinationPath: hostDestinationPath,
	})
	if err != nil {
		t.Fatalf("sandbox export: %v", err)
	}
	if exportResponse.Action != "export" {
		t.Fatalf("unexpected export response: %#v", exportResponse)
	}
	if exportResponse.SourceSandboxPath != "/morph/home/outputs/export-me.txt" {
		t.Fatalf("expected virtual export source path, got %#v", exportResponse)
	}

	exportedBytes, err := os.ReadFile(hostDestinationPath)
	if err != nil {
		t.Fatalf("read exported path: %v", err)
	}
	if string(exportedBytes) != "sandbox flow" {
		t.Fatalf("unexpected exported contents: %q", string(exportedBytes))
	}

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read loopgate events: %v", err)
	}
	auditText := string(auditBytes)
	for _, expectedEventType := range []string{"sandbox.imported", "sandbox.staged", "sandbox.metadata_viewed", "sandbox.exported"} {
		if !strings.Contains(auditText, expectedEventType) {
			t.Fatalf("expected audit to contain %s, got %s", expectedEventType, auditText)
		}
	}
}

func TestSandboxImportRequiresBoundOperatorMountPath(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	client.ConfigureSession("operator", "operator-sandbox-import-unbound", advertisedSessionCapabilityNames(status))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure operator sandbox token: %v", err)
	}

	hostSourcePath := filepath.Join(t.TempDir(), "example.txt")
	if err := os.WriteFile(hostSourcePath, []byte("sandbox flow"), 0o600); err != nil {
		t.Fatalf("write host source: %v", err)
	}

	_, err := client.SandboxImport(context.Background(), SandboxImportRequest{
		HostSourcePath:  hostSourcePath,
		DestinationName: "example.txt",
	})
	if err == nil {
		t.Fatal("expected sandbox import denial without operator mount binding")
	}
	if !strings.Contains(err.Error(), DenialCodeControlSessionBindingInvalid) {
		t.Fatalf("expected control session binding denial, got %v", err)
	}
}

func TestSandboxExportRequiresOperatorMountWriteGrant(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	hostRootPath := t.TempDir()
	pinTestProcessAsExpectedClient(t, server)
	client.SetOperatorMountPaths([]string{hostRootPath}, hostRootPath)
	client.ConfigureSession("operator", "operator-sandbox-export-needs-grant", advertisedSessionCapabilityNames(status))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure operator sandbox token: %v", err)
	}

	hostSourcePath := filepath.Join(hostRootPath, "example.txt")
	if err := os.WriteFile(hostSourcePath, []byte("sandbox flow"), 0o600); err != nil {
		t.Fatalf("write host source: %v", err)
	}
	if _, err := client.SandboxImport(context.Background(), SandboxImportRequest{
		HostSourcePath:  hostSourcePath,
		DestinationName: "example.txt",
	}); err != nil {
		t.Fatalf("sandbox import: %v", err)
	}
	if _, err := client.SandboxStage(context.Background(), SandboxStageRequest{
		SandboxSourcePath: "/morph/home/imports/example.txt",
		OutputName:        "export-me.txt",
	}); err != nil {
		t.Fatalf("sandbox stage: %v", err)
	}

	_, err := client.SandboxExport(context.Background(), SandboxExportRequest{
		SandboxSourcePath:   "/morph/home/outputs/export-me.txt",
		HostDestinationPath: filepath.Join(hostRootPath, "exported.txt"),
	})
	if err == nil {
		t.Fatal("expected sandbox export denial without operator mount write grant")
	}
	if !strings.Contains(err.Error(), DenialCodeApprovalRequired) {
		t.Fatalf("expected approval-required denial, got %v", err)
	}
}

func TestExpiredCapabilityTokenIsRefreshedForLocalClient(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	server.mu.Lock()
	tokenClaims := server.tokens[client.capabilityToken]
	tokenClaims.ExpiresAt = time.Now().UTC().Add(-1 * time.Minute)
	server.tokens[client.capabilityToken] = tokenClaims
	activeSession := server.sessions[tokenClaims.ControlSessionID]
	activeSession.ExpiresAt = time.Now().UTC().Add(-1 * time.Minute)
	server.sessions[tokenClaims.ControlSessionID] = activeSession
	server.mu.Unlock()

	response, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-expired",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		t.Fatalf("expected local client to refresh expired capability token, got %v", err)
	}
	if response.Status != ResponseStatusSuccess {
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
	tokenClaims := server.tokens[client.capabilityToken]
	tokenClaims.PeerIdentity.PID++
	server.tokens[client.capabilityToken] = tokenClaims
	server.mu.Unlock()

	response, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-peer-mismatch",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		t.Fatalf("expected local client to refresh peer-mismatched capability token, got %v", err)
	}
	if response.Status != ResponseStatusSuccess {
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

	response, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-missing-signature",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		t.Fatalf("execute capability: %v", err)
	}
	if response.Status != ResponseStatusDenied || response.DenialCode != DenialCodeRequestSignatureMissing {
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

	response, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-invalid-signature",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		t.Fatalf("execute capability: %v", err)
	}
	if response.Status != ResponseStatusDenied || response.DenialCode != DenialCodeRequestSignatureInvalid {
		t.Fatalf("expected request signature invalid denial, got %#v", response)
	}
}

func TestCapabilityExecuteRejectsReplayedRequestNonce(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	requestBody := CapabilityRequest{
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

	var firstResponse CapabilityResponse
	if err := client.doJSONWithHeaders(context.Background(), http.MethodPost, "/v1/capabilities/execute", client.capabilityToken, requestBody, &firstResponse, requestHeaders); err != nil {
		t.Fatalf("first signed request: %v", err)
	}

	var secondResponse CapabilityResponse
	err := client.doJSONWithHeaders(context.Background(), http.MethodPost, "/v1/capabilities/execute", client.capabilityToken, requestBody, &secondResponse, requestHeaders)
	if err == nil || !strings.Contains(err.Error(), DenialCodeRequestNonceReplayDetected) {
		t.Fatalf("expected request nonce replay denial, got %v", err)
	}
}

func TestSignedRequestFailsClosedWhenNonceReplayPersistenceUnavailable(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.noncePath = filepath.Join(repoRoot, "runtime", "state")

	_, err := client.Status(context.Background())
	if err == nil || !strings.Contains(err.Error(), DenialCodeAuditUnavailable) {
		t.Fatalf("expected nonce replay persistence failure to fail closed, got %v", err)
	}
}

func TestApprovalDecisionRequiresMatchingCapabilityTokenOwner(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))

	response, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
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

	var approvalResponse CapabilityResponse
	approvalNonce := response.Metadata["approval_decision_nonce"].(string)
	approvalPath := "/v1/approvals/" + response.ApprovalRequestID + "/decision"
	err = otherClient.doJSON(context.Background(), http.MethodPost, approvalPath, "", ApprovalDecisionRequest{
		Approved:      true,
		DecisionNonce: approvalNonce,
	}, &approvalResponse, map[string]string{
		"X-Loopgate-Approval-Token": otherApprovalToken,
	})
	if err == nil || !strings.Contains(err.Error(), DenialCodeApprovalOwnerMismatch) {
		t.Fatalf("expected approval owner mismatch denial, got %v", err)
	}
}

func TestApprovalDecisionRequiresDecisionNonce(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))

	response, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
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

	response, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
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
	activeSession := server.sessions[client.controlSessionID]
	activeSession.PeerIdentity.PID++
	server.sessions[client.controlSessionID] = activeSession
	server.mu.Unlock()

	decisionResponse, err := client.DecideApproval(context.Background(), response.ApprovalRequestID, true)
	if err != nil {
		t.Fatalf("decide approval: %v", err)
	}
	if decisionResponse.Status != ResponseStatusDenied || decisionResponse.DenialCode != DenialCodeApprovalTokenInvalid {
		t.Fatalf("expected approval peer binding denial, got %#v", decisionResponse)
	}
}

func TestApprovalDecisionCannotBeReplayedAfterResolution(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))

	response, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
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
	if firstResponse.Status != ResponseStatusSuccess {
		t.Fatalf("expected successful execution after approval, got %#v", firstResponse)
	}

	controlSessionID := client.controlSessionID
	server.mu.Lock()
	controlSession := server.sessions[controlSessionID]
	server.mu.Unlock()
	manualReplayRequest := ApprovalDecisionRequest{
		Approved:      true,
		DecisionNonce: approvalNonce,
	}
	var replayResponse CapabilityResponse
	replayPath := "/v1/approvals/" + response.ApprovalRequestID + "/decision"
	err = client.doJSON(context.Background(), http.MethodPost, replayPath, "", manualReplayRequest, &replayResponse, map[string]string{
		"X-Loopgate-Approval-Token": controlSession.ApprovalToken,
	})
	if err == nil || !strings.Contains(err.Error(), DenialCodeApprovalStateConflict) {
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
	baseToken := server.tokens[client.capabilityToken]
	server.mu.Unlock()

	response := server.executeCapabilityRequest(context.Background(), baseToken, CapabilityRequest{
		RequestID:  "req-no-approval-bypass",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "blocked.txt",
			"content": "approval bypass should fail closed",
		},
	}, false)
	if response.Status != ResponseStatusDenied || response.DenialCode != DenialCodeApprovalRequired || !response.ApprovalRequired {
		t.Fatalf("expected approval-required denial without execution, got %#v", response)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "blocked.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected blocked file to remain unwritten, stat err=%v", err)
	}

	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.approvals) != 0 {
		t.Fatalf("expected no pending approvals to be created on fail-closed path, got %#v", server.approvals)
	}
}

func TestCapabilityResponseJSONDoesNotExposeProviderTokenFields(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	response, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
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

func TestOpenSessionRejectsEmptyCapabilityScope(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	client.ConfigureSession("test-actor", "test-session", nil)

	_, err := client.ensureCapabilityToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityScopeRequired) {
		t.Fatalf("expected empty capability scope denial, got %v", err)
	}
}

func TestOpenSessionRejectsTraversalAndShellLikeLabels(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	client.ConfigureSession("../../etc/passwd", "safe-session", capabilityNames(status.Capabilities))
	if _, err := client.ensureCapabilityToken(context.Background()); err == nil || !strings.Contains(err.Error(), "actor") {
		t.Fatalf("expected invalid actor denial, got %v", err)
	}

	client.ConfigureSession("safe-actor", "$rm", capabilityNames(status.Capabilities))
	if _, err := client.ensureCapabilityToken(context.Background()); err == nil || !strings.Contains(err.Error(), "session_id") {
		t.Fatalf("expected invalid session_id denial, got %v", err)
	}

	client.ConfigureSession("safe-actor", "safe-session", []string{"../../etc"})
	if _, err := client.ensureCapabilityToken(context.Background()); err == nil || !strings.Contains(err.Error(), "requested capability") {
		t.Fatalf("expected invalid requested capability denial, got %v", err)
	}
}

func TestOpenSessionRejectsMismatchedWorkspaceBinding(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	client.SetWorkspaceID("workspace-other-repo")
	client.ConfigureSession("safe-actor", "safe-session", capabilityNames(status.Capabilities))
	if _, err := client.ensureCapabilityToken(context.Background()); err == nil || !strings.Contains(err.Error(), DenialCodeControlSessionBindingInvalid) {
		t.Fatalf("expected mismatched workspace binding denial, got %v", err)
	}

	matchingClient := NewClient(server.socketPath)
	matchingClient.SetWorkspaceID(server.deriveWorkspaceIDFromRepoRoot())
	matchingClient.ConfigureSession("safe-actor", "safe-session", capabilityNames(status.Capabilities))
	if _, err := matchingClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("expected matching workspace binding to succeed, got %v", err)
	}
}

func TestOpenSessionRejectsOperatorMountBindingWithoutExpectedClientPin(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	hostRootPath := t.TempDir()

	client.SetOperatorMountPaths([]string{hostRootPath}, hostRootPath)
	client.ConfigureSession("operator", "operator-mount-unpinned", []string{"fs_list"})
	if _, err := client.ensureCapabilityToken(context.Background()); err == nil || !strings.Contains(err.Error(), DenialCodeControlSessionBindingInvalid) {
		t.Fatalf("expected operator mount binding denial without expected client pin, got %v", err)
	}
}

func TestOpenSessionRejectsPinnedClientWhenExecutableResolverUnavailable(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServerWithRuntime(t, repoRoot, loopgatePolicyYAML(false), nil, false)
	server.expectedClientPath = "/Applications/Loopgate.app/Contents/MacOS/Loopgate"
	server.resolveExePath = nil

	client.ConfigureSession("safe-actor", "safe-session", []string{"fs_list"})
	if _, err := client.ensureCapabilityToken(context.Background()); err == nil || !strings.Contains(err.Error(), DenialCodeProcessBindingRejected) {
		t.Fatalf("expected process binding rejection when resolver is unavailable, got %v", err)
	}
}

func TestOpenSessionRateLimitByPeerUID(t *testing.T) {
	repoRoot := t.TempDir()
	_, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.sessionOpenMinInterval = time.Hour
	server.maxActiveSessionsPerUID = 32

	firstClient := NewClient(server.socketPath)
	firstClient.ConfigureSession("first-actor", "first-session", capabilityNames(status.Capabilities))
	if _, err := firstClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("first session open: %v", err)
	}

	secondClient := NewClient(server.socketPath)
	secondClient.ConfigureSession("second-actor", "second-session", capabilityNames(status.Capabilities))
	_, err := secondClient.ensureCapabilityToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), DenialCodeSessionOpenRateLimited) {
		t.Fatalf("expected session-open rate limit denial, got %v", err)
	}
}

func TestOpenSessionActiveLimitByPeerUID(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.sessionOpenMinInterval = 0
	server.maxActiveSessionsPerUID = 1

	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("first session open: %v", err)
	}

	secondClient := NewClient(client.socketPath)
	secondClient.ConfigureSession("second-actor", "second-session", capabilityNames(status.Capabilities))
	_, err := secondClient.ensureCapabilityToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), DenialCodeSessionActiveLimitReached) {
		t.Fatalf("expected active-session-limit denial, got %v", err)
	}
}

func TestCloseSessionReleasesActiveLimitByPeerUID(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.sessionOpenMinInterval = 0
	server.maxActiveSessionsPerUID = 1

	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("first session open: %v", err)
	}
	if err := client.CloseSession(context.Background()); err != nil {
		t.Fatalf("close session: %v", err)
	}

	server.mu.Lock()
	if len(server.sessions) != 0 {
		server.mu.Unlock()
		t.Fatalf("expected no active sessions after close, got %d", len(server.sessions))
	}
	server.mu.Unlock()

	secondClient := NewClient(client.socketPath)
	secondClient.ConfigureSession("second-actor", "second-session", capabilityNames(status.Capabilities))
	if _, err := secondClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("expected session open after close to succeed, got %v", err)
	}
}

func TestCloseSessionDeniedWhenPendingApprovalsExist(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	server.mu.Lock()
	server.approvals["approval-pending-close"] = pendingApproval{
		ID:               "approval-pending-close",
		ControlSessionID: client.controlSessionID,
		CreatedAt:        server.now().UTC(),
		ExpiresAt:        server.now().UTC().Add(time.Minute),
		State:            approvalStatePending,
	}
	server.mu.Unlock()

	err := client.CloseSession(context.Background())
	if err == nil || !strings.Contains(err.Error(), DenialCodeSessionCloseBlocked) {
		t.Fatalf("expected session-close-blocked denial, got %v", err)
	}

	server.mu.Lock()
	_, sessionStillPresent := server.sessions[client.controlSessionID]
	server.mu.Unlock()
	if !sessionStillPresent {
		t.Fatalf("expected session %q to remain active after denied close", client.controlSessionID)
	}
}

func TestSessionOpenAuditFailureRestoresReplacedSession(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure original capability token: %v", err)
	}
	originalControlSessionID := client.controlSessionID
	originalCapabilityToken := client.capabilityToken

	server.appendAuditEvent = func(string, ledger.Event) error {
		return errors.New("audit down")
	}

	replacingClient := NewClient(client.socketPath)
	replacingClient.ConfigureSession("test-actor", "test-session", capabilityNames(status.Capabilities))
	if _, err := replacingClient.ensureCapabilityToken(context.Background()); err == nil || !strings.Contains(err.Error(), DenialCodeAuditUnavailable) {
		t.Fatalf("expected session open audit failure, got %v", err)
	}

	server.mu.Lock()
	_, originalSessionStillPresent := server.sessions[originalControlSessionID]
	_, originalTokenStillPresent := server.tokens[originalCapabilityToken]
	server.mu.Unlock()
	if !originalSessionStillPresent {
		t.Fatalf("expected original session %q to be restored after audit failure", originalControlSessionID)
	}
	if !originalTokenStillPresent {
		t.Fatalf("expected original capability token for %q to remain valid after audit failure", originalControlSessionID)
	}

	if _, err := client.Status(context.Background()); err != nil {
		t.Fatalf("expected original client status to succeed after replacement audit failure: %v", err)
	}
}

func TestSessionOpenRateLimitDenialPreservesExistingSession(t *testing.T) {
	repoRoot := t.TempDir()
	_, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.sessionOpenMinInterval = time.Hour
	server.maxActiveSessionsPerUID = 32

	firstClient := NewClient(server.socketPath)
	firstClient.ConfigureSession("reopen-actor", "reopen-session", capabilityNames(status.Capabilities))
	if _, err := firstClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure original capability token: %v", err)
	}
	originalControlSessionID := firstClient.controlSessionID
	originalCapabilityToken := firstClient.capabilityToken

	replacingClient := NewClient(server.socketPath)
	replacingClient.ConfigureSession("reopen-actor", "reopen-session", capabilityNames(status.Capabilities))
	if _, err := replacingClient.ensureCapabilityToken(context.Background()); err == nil || !strings.Contains(err.Error(), DenialCodeSessionOpenRateLimited) {
		t.Fatalf("expected session open rate-limit denial, got %v", err)
	}

	server.mu.Lock()
	_, originalSessionStillPresent := server.sessions[originalControlSessionID]
	_, originalTokenStillPresent := server.tokens[originalCapabilityToken]
	server.mu.Unlock()
	if !originalSessionStillPresent {
		t.Fatalf("expected original session %q to remain after replacement rate-limit denial", originalControlSessionID)
	}
	if !originalTokenStillPresent {
		t.Fatalf("expected original capability token for %q to remain valid after replacement rate-limit denial", originalControlSessionID)
	}

	if _, err := firstClient.Status(context.Background()); err != nil {
		t.Fatalf("expected original client status to succeed after replacement rate-limit denial: %v", err)
	}
}

func TestPruneExpiredLockedSkipsBeforeScheduledSweep(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	nowUTC := server.now().UTC()
	server.mu.Lock()
	server.expirySweepMaxInterval = time.Hour
	server.nextExpirySweepAt = nowUTC.Add(30 * time.Minute)
	server.tokens["expired-token"] = capabilityToken{
		TokenID:      "expired-token-id",
		Token:        "expired-token",
		ExpiresAt:    nowUTC.Add(-1 * time.Minute),
		PeerIdentity: peerIdentity{UID: 1234},
	}
	server.pruneExpiredLocked()
	_, found := server.tokens["expired-token"]
	server.mu.Unlock()

	if !found {
		t.Fatal("expected scheduled cleanup gate to skip expired token pruning before next sweep")
	}
}

func TestPruneExpiredLockedSchedulesEarliestExpiry(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	nowUTC := server.now().UTC()
	tokenExpiryUTC := nowUTC.Add(2 * time.Minute)
	sessionExpiryUTC := nowUTC.Add(5 * time.Minute)

	server.mu.Lock()
	server.expirySweepMaxInterval = time.Hour
	server.nextExpirySweepAt = time.Time{}
	server.tokens["live-token"] = capabilityToken{
		TokenID:      "live-token-id",
		Token:        "live-token",
		ExpiresAt:    tokenExpiryUTC,
		PeerIdentity: peerIdentity{UID: 1234},
	}
	server.sessions["live-session"] = controlSession{
		ID:           "live-session",
		ExpiresAt:    sessionExpiryUTC,
		PeerIdentity: peerIdentity{UID: 1234},
	}
	server.pruneExpiredLocked()
	scheduledSweepUTC := server.nextExpirySweepAt
	server.mu.Unlock()

	if !scheduledSweepUTC.Equal(tokenExpiryUTC) {
		t.Fatalf("expected next expiry sweep at %s, got %s", tokenExpiryUTC.Format(time.RFC3339Nano), scheduledSweepUTC.Format(time.RFC3339Nano))
	}
}

func TestNoteExpiryCandidateLockedPullsScheduleEarlier(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	nowUTC := server.now().UTC()
	earlierExpiryUTC := nowUTC.Add(10 * time.Minute)
	laterExpiryUTC := nowUTC.Add(2 * time.Hour)

	server.mu.Lock()
	server.expirySweepMaxInterval = time.Hour
	server.nextExpirySweepAt = laterExpiryUTC
	server.noteExpiryCandidateLocked(earlierExpiryUTC)
	scheduledSweepUTC := server.nextExpirySweepAt
	server.mu.Unlock()

	if !scheduledSweepUTC.Equal(earlierExpiryUTC) {
		t.Fatalf("expected expiry candidate to pull next sweep to %s, got %s", earlierExpiryUTC.Format(time.RFC3339Nano), scheduledSweepUTC.Format(time.RFC3339Nano))
	}
}

func TestHTTPStatusForResponseMapsTypedCapabilityResponses(t *testing.T) {
	testCases := []struct {
		name         string
		response     CapabilityResponse
		expectedHTTP int
	}{
		{
			name:         "success",
			response:     CapabilityResponse{Status: ResponseStatusSuccess},
			expectedHTTP: http.StatusOK,
		},
		{
			name:         "pending approval",
			response:     CapabilityResponse{Status: ResponseStatusPendingApproval},
			expectedHTTP: http.StatusAccepted,
		},
		{
			name:         "denied unauthorized",
			response:     CapabilityResponse{Status: ResponseStatusDenied, DenialCode: DenialCodeCapabilityTokenInvalid},
			expectedHTTP: http.StatusUnauthorized,
		},
		{
			name:         "denied rate limited",
			response:     CapabilityResponse{Status: ResponseStatusDenied, DenialCode: DenialCodeSessionOpenRateLimited},
			expectedHTTP: http.StatusTooManyRequests,
		},
		{
			name:         "error audit unavailable",
			response:     CapabilityResponse{Status: ResponseStatusError, DenialCode: DenialCodeAuditUnavailable},
			expectedHTTP: http.StatusServiceUnavailable,
		},
	}

	for _, testCase := range testCases {
		if gotHTTP := httpStatusForResponse(testCase.response); gotHTTP != testCase.expectedHTTP {
			t.Fatalf("%s: expected %d, got %d", testCase.name, testCase.expectedHTTP, gotHTTP)
		}
	}
}

func TestServerConnContextReportsPeerCredentialFailure(t *testing.T) {
	repoRoot := t.TempDir()
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	writeTestMorphlingClassPolicy(t, repoRoot)

	server, err := NewServer(repoRoot, filepath.Join(t.TempDir(), "loopgate.sock"))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	var reportedSecurityCode string
	server.resolvePeerIdentity = func(net.Conn) (peerIdentity, error) {
		return peerIdentity{}, errors.New("synthetic peer credential failure")
	}
	server.reportSecurityWarning = func(eventCode string, cause error) {
		reportedSecurityCode = eventCode
		_ = cause
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	ctx := server.server.ConnContext(context.Background(), clientConn)
	if _, ok := peerIdentityFromContext(ctx); ok {
		t.Fatal("expected peer identity to be absent after credential lookup failure")
	}
	if reportedSecurityCode != "unix_peer_resolve_failed" {
		t.Fatalf("expected unix_peer_resolve_failed security event, got %q", reportedSecurityCode)
	}
}

func TestDuplicateRequestIDIsRejected(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	firstResponse, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-duplicate",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		t.Fatalf("first execute: %v", err)
	}
	if firstResponse.Status != ResponseStatusSuccess {
		t.Fatalf("unexpected first response: %#v", firstResponse)
	}

	secondResponse, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-duplicate",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		t.Fatalf("second execute should return typed denial, got %v", err)
	}
	if secondResponse.Status != ResponseStatusDenied || secondResponse.DenialCode != DenialCodeRequestReplayDetected {
		t.Fatalf("expected replay denial, got %#v", secondResponse)
	}
}

func TestAuditFailureIsSurfacedExplicitly(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.appendAuditEvent = func(string, ledger.Event) error {
		return errors.New("audit sink unavailable")
	}
	client.ConfigureSession("audit-test", "audit-session", []string{"fs_list"})

	_, err := client.ensureCapabilityToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), DenialCodeAuditUnavailable) {
		t.Fatalf("expected audit unavailable error, got %v", err)
	}
}

func TestCapabilityExecutionAuditFailureReturnsAuditUnavailable(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	server.appendAuditEvent = func(string, ledger.Event) error {
		return errors.New("audit sink unavailable")
	}

	response, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-audit-fail-after-open",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		t.Fatalf("expected typed audit unavailable response, got %v", err)
	}
	if response.Status != ResponseStatusError || response.DenialCode != DenialCodeAuditUnavailable {
		t.Fatalf("expected audit unavailable response, got %#v", response)
	}
}

func TestSingleUseExecutionTokenIsDeniedOnReuse(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	server.mu.Lock()
	baseToken := server.tokens[client.capabilityToken]
	server.mu.Unlock()

	capabilityRequest := normalizeCapabilityRequest(CapabilityRequest{
		RequestID:  "req-single-use",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "notes.txt",
			"content": "hello",
		},
	})
	executionToken := deriveExecutionToken(baseToken, capabilityRequest)

	firstResponse := server.executeCapabilityRequest(context.Background(), executionToken, capabilityRequest, false)
	if firstResponse.Status != ResponseStatusSuccess {
		t.Fatalf("expected first single-use execution to succeed, got %#v", firstResponse)
	}
	server.mu.Lock()
	consumedToken, found := server.usedTokens[executionToken.TokenID]
	server.mu.Unlock()
	if !found {
		t.Fatal("expected single-use execution token to be recorded in used token registry")
	}
	if consumedToken.ParentTokenID != baseToken.TokenID {
		t.Fatalf("expected parent token id %q, got %#v", baseToken.TokenID, consumedToken)
	}
	if consumedToken.Capability != capabilityRequest.Capability {
		t.Fatalf("expected consumed capability %q, got %#v", capabilityRequest.Capability, consumedToken)
	}
	if consumedToken.NormalizedArgHash != normalizedArgumentHash(capabilityRequest.Arguments) {
		t.Fatalf("expected normalized argument hash to be recorded, got %#v", consumedToken)
	}

	secondResponse := server.executeCapabilityRequest(context.Background(), executionToken, capabilityRequest, false)
	if secondResponse.Status != ResponseStatusDenied || secondResponse.DenialCode != DenialCodeCapabilityTokenReused {
		t.Fatalf("expected reused single-use token denial, got %#v", secondResponse)
	}
}

type fakeLoopgateTool struct {
	name        string
	category    string
	operation   string
	description string
	output      string
}

func (fakeTool fakeLoopgateTool) Name() string      { return fakeTool.name }
func (fakeTool fakeLoopgateTool) Category() string  { return fakeTool.category }
func (fakeTool fakeLoopgateTool) Operation() string { return fakeTool.operation }
func (fakeTool fakeLoopgateTool) Schema() toolspkg.Schema {
	return toolspkg.Schema{Description: fakeTool.description}
}
func (fakeTool fakeLoopgateTool) Execute(context.Context, map[string]string) (string, error) {
	return fakeTool.output, nil
}

func TestIsHighRiskCapability_UntrustedWriteRemainsHighRisk(t *testing.T) {
	tool := fakeLoopgateTool{
		name:      "fs_write",
		category:  "filesystem",
		operation: toolspkg.OpWrite,
	}

	if !isHighRiskCapability(tool, policypkg.CheckResult{Decision: policypkg.Allow}) {
		t.Fatalf("expected untrusted write to remain high risk")
	}
}

type secretHeuristicOptOutTool struct {
	fakeLoopgateTool
}

func (t secretHeuristicOptOutTool) Schema() toolspkg.Schema {
	return toolspkg.Schema{
		Description: t.description,
		Args:        []toolspkg.ArgDef{{Name: "path", Required: true, Type: "path"}},
	}
}

func (secretHeuristicOptOutTool) SecretExportNameHeuristicOptOut() bool { return true }

type rawSecretExportExplicitlyBlockedTool struct {
	fakeLoopgateTool
}

func (rawSecretExportExplicitlyBlockedTool) RawSecretExportProhibited() bool { return true }

func TestCapabilityProhibitsRawSecretExport_OptOutAllowsRegisteredTool(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.registry.Register(secretHeuristicOptOutTool{fakeLoopgateTool{
		name:        "secret.notexport",
		category:    "filesystem",
		operation:   toolspkg.OpRead,
		description: "test opt-out of secret name heuristic",
		output:      "ok",
	}})
	capabilities := append(capabilityNames(status.Capabilities), "secret.notexport")
	client.ConfigureSession("test-actor", "test-session", capabilities)
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}
	resp, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-optout",
		Capability: "secret.notexport",
		Arguments:  map[string]string{"path": "."},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if resp.Status != ResponseStatusSuccess {
		t.Fatalf("expected successful read for heuristic opt-out tool, got %#v", resp)
	}
}

func TestCapabilityProhibitsRawSecretExport_UnregisteredHeuristicNameDenied(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}
	resp, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-heur",
		Capability: "secret.no.such.tool",
		Arguments:  map[string]string{},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if resp.Status != ResponseStatusDenied || resp.DenialCode != DenialCodeSecretExportProhibited {
		t.Fatalf("expected secret export prohibition for unregistered heuristic name, got %#v", resp)
	}
}

func TestCapabilityProhibitsRawSecretExport_RegisteredBlockedNameStillDenied(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.registry.Register(rawSecretExportExplicitlyBlockedTool{fakeLoopgateTool{
		name:        "secret.blocked",
		category:    "filesystem",
		operation:   toolspkg.OpRead,
		description: "test blocked via RawSecretExportProhibited",
		output:      "ok",
	}})
	capabilities := append(capabilityNames(status.Capabilities), "secret.blocked")
	client.ConfigureSession("test-actor", "test-session", capabilities)
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}
	resp, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-blocked",
		Capability: "secret.blocked",
		Arguments:  map[string]string{},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if resp.Status != ResponseStatusDenied || resp.DenialCode != DenialCodeSecretExportProhibited {
		t.Fatalf("expected secret export prohibition, got %#v", resp)
	}
}

func TestApprovalExecuteDeniesWhenStoredExecutionBodyMutated(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}
	pendingResponse, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-integrity",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "pending.txt",
			"content": "hidden",
		},
	})
	if err != nil {
		t.Fatalf("execute pending approval: %v", err)
	}
	if !pendingResponse.ApprovalRequired {
		t.Fatalf("expected pending approval, got %#v", pendingResponse)
	}
	server.mu.Lock()
	pa := server.approvals[pendingResponse.ApprovalRequestID]
	pa.Request.Arguments["path"] = "evil.txt"
	server.approvals[pendingResponse.ApprovalRequestID] = pa
	server.mu.Unlock()
	approvedResponse, err := client.UIDecideApproval(context.Background(), pendingResponse.ApprovalRequestID, true)
	if err != nil {
		t.Fatalf("ui approval decision: %v", err)
	}
	if approvedResponse.DenialCode != DenialCodeApprovalExecutionBodyMismatch {
		t.Fatalf("expected execution body mismatch, got %#v", approvedResponse)
	}
	if approvedResponse.Status != ResponseStatusError {
		t.Fatalf("expected error status, got %#v", approvedResponse)
	}
}

func TestPendingApprovalLimitPerControlSession(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))
	server.maxPendingApprovalsPerControlSession = 2
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}
	for i := range 2 {
		resp, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
			RequestID:  fmt.Sprintf("req-ap-limit-%d", i),
			Capability: "fs_write",
			Arguments: map[string]string{
				"path":    fmt.Sprintf("limit-%d.txt", i),
				"content": "x",
			},
		})
		if err != nil {
			t.Fatalf("execute %d: %v", i, err)
		}
		if !resp.ApprovalRequired {
			t.Fatalf("expected pending approval %d, got %#v", i, resp)
		}
	}
	resp, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-ap-limit-2",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "limit-2.txt",
			"content": "x",
		},
	})
	if err != nil {
		t.Fatalf("execute third: %v", err)
	}
	if resp.Status != ResponseStatusDenied || resp.DenialCode != DenialCodePendingApprovalLimitReached {
		t.Fatalf("expected pending approval limit, got %#v", resp)
	}
}

func TestRequestReplayStoreSaturates(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))
	server.maxSeenRequestReplayEntries = 2
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}
	for i := range 2 {
		_, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
			RequestID:  fmt.Sprintf("req-replay-sat-%d", i),
			Capability: "fs_write",
			Arguments: map[string]string{
				"path":    fmt.Sprintf("rs%d.txt", i),
				"content": "x",
			},
		})
		if err != nil {
			t.Fatalf("execute %d: %v", i, err)
		}
	}
	resp, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-replay-sat-2",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "rs2.txt",
			"content": "x",
		},
	})
	if err != nil {
		t.Fatalf("execute third: %v", err)
	}
	if resp.Status != ResponseStatusDenied || resp.DenialCode != DenialCodeReplayStateSaturated {
		t.Fatalf("expected replay store saturated, got %#v", resp)
	}
}

func TestBoundExecutionTokenRejectsDifferentNormalizedArguments(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	server.mu.Lock()
	baseToken := server.tokens[client.capabilityToken]
	server.mu.Unlock()

	approvedRequest := normalizeCapabilityRequest(CapabilityRequest{
		RequestID:  "req-bound",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "./notes.txt",
			"content": "hello",
		},
	})
	executionToken := deriveExecutionToken(baseToken, approvedRequest)

	mutatedRequest := normalizeCapabilityRequest(CapabilityRequest{
		RequestID:  "req-bound-mutated",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "other.txt",
			"content": "hello",
		},
	})
	response := server.executeCapabilityRequest(context.Background(), executionToken, mutatedRequest, false)
	if response.Status != ResponseStatusDenied || response.DenialCode != DenialCodeCapabilityTokenBindingInvalid {
		t.Fatalf("expected bound token mismatch denial, got %#v", response)
	}
}

func TestLoopgateAuditEventsIncludeHashChainMetadata(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	_, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-audit-chain",
		Capability: "fs_list",
		Arguments: map[string]string{
			"path": ".",
		},
	})
	if err != nil {
		t.Fatalf("execute fs_list: %v", err)
	}

	auditPath := filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl")
	auditFile, err := os.Open(auditPath)
	if err != nil {
		t.Fatalf("open audit log: %v", err)
	}
	defer auditFile.Close()

	scanner := bufio.NewScanner(auditFile)
	lineCount := 0
	var previousEventHash string
	for scanner.Scan() {
		lineCount++
		var auditEvent ledger.Event
		if err := json.Unmarshal(scanner.Bytes(), &auditEvent); err != nil {
			t.Fatalf("decode audit event: %v", err)
		}
		eventHash, _ := auditEvent.Data["event_hash"].(string)
		if strings.TrimSpace(eventHash) == "" {
			t.Fatalf("expected event_hash on audit event %#v", auditEvent)
		}
		if sequenceValue, found := auditEvent.Data["audit_sequence"]; !found || sequenceValue == nil {
			t.Fatalf("expected audit_sequence on audit event %#v", auditEvent)
		}
		previousHash, _ := auditEvent.Data["previous_event_hash"].(string)
		if previousHash != previousEventHash {
			t.Fatalf("expected previous_event_hash %q, got %q", previousEventHash, previousHash)
		}
		previousEventHash = eventHash
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan audit log: %v", err)
	}
	if lineCount < 2 {
		t.Fatalf("expected multiple chained audit events, got %d", lineCount)
	}
}

func TestHashAuditEventMatchesStoredLedgerHash(t *testing.T) {
	auditEvent := ledger.Event{
		TS:      time.Now().UTC().Format(time.RFC3339Nano),
		Type:    "test.audit",
		Session: "session-a",
		Data: map[string]interface{}{
			"audit_sequence":      uint64(1),
			"ledger_sequence":     uint64(1),
			"previous_event_hash": "",
			"step":                "one",
		},
	}

	precomputedHash, err := hashAuditEvent(auditEvent)
	if err != nil {
		t.Fatalf("hash audit event: %v", err)
	}
	auditEvent.Data["event_hash"] = precomputedHash

	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	if err := ledger.Append(auditPath, auditEvent); err != nil {
		t.Fatalf("append audit event: %v", err)
	}

	auditBytes, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	lines := bytes.Split(bytes.TrimSpace(auditBytes), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("expected one audit line, got %d", len(lines))
	}
	storedEvent, ok := ledger.ParseEvent(lines[0])
	if !ok {
		t.Fatalf("parse stored audit event: %s", string(lines[0]))
	}
	storedHash, _ := storedEvent.Data["event_hash"].(string)
	if storedHash != precomputedHash {
		t.Fatalf("expected stored hash %q to match precomputed hash %q, got event %#v", storedHash, precomputedHash, storedEvent)
	}
}

func TestLogEventWritesVerifiableAuditChain(t *testing.T) {
	repoRoot := t.TempDir()
	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	writeTestMorphlingClassPolicy(t, repoRoot)
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, "runtime", "state"), 0o700); err != nil {
		t.Fatalf("mkdir runtime state: %v", err)
	}

	verifyAuditChain := func(expectedSequence int64) {
		t.Helper()
		auditFile, err := os.Open(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
		if err != nil {
			t.Fatalf("open audit file: %v", err)
		}
		defer auditFile.Close()
		lastSequence, _, err := ledger.ReadVerifiedChainState(auditFile, "audit_sequence")
		if err != nil {
			t.Fatalf("verify audit chain: %v", err)
		}
		if lastSequence != expectedSequence {
			t.Fatalf("expected audit sequence %d, got %d", expectedSequence, lastSequence)
		}
	}

	if err := server.logEvent("test.audit", "session-a", map[string]interface{}{"step": "one"}); err != nil {
		t.Fatalf("log first audit event: %v", err)
	}
	verifyAuditChain(1)

	if err := server.logEvent("test.audit", "session-a", map[string]interface{}{"step": "two"}); err != nil {
		t.Fatalf("log second audit event: %v", err)
	}
	verifyAuditChain(2)
}

func TestHookPreValidateWritesAuditSequenceMetadata(t *testing.T) {
	repoRoot := t.TempDir()
	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	writeTestMorphlingClassPolicy(t, repoRoot)
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	requestBody := bytes.NewBufferString(`{"tool_name":"Bash","session_id":"session-hook"}`)
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
		t.Fatalf("expected blocked Bash hook response, got %#v", response)
	}

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(auditBytes)), "\n")
	if len(lines) == 0 {
		t.Fatal("expected hook audit event")
	}
	lastAuditEvent, ok := ledger.ParseEvent([]byte(lines[len(lines)-1]))
	if !ok {
		t.Fatalf("parse hook audit event: %s", lines[len(lines)-1])
	}
	if lastAuditEvent.Type != "hook.pre_validate" {
		t.Fatalf("expected hook.pre_validate event, got %#v", lastAuditEvent)
	}
	if _, found := lastAuditEvent.Data["audit_sequence"]; !found {
		t.Fatalf("expected audit_sequence on hook audit event %#v", lastAuditEvent)
	}
	if decisionValue, _ := lastAuditEvent.Data["decision"].(string); decisionValue != "block" {
		t.Fatalf("expected hook audit decision block, got %#v", lastAuditEvent.Data["decision"])
	}
}

func ageQuarantineRecordForPrune(t *testing.T, repoRoot string, quarantineRef string) {
	t.Helper()

	recordPath, err := quarantinePathFromRef(repoRoot, quarantineRef)
	if err != nil {
		t.Fatalf("quarantine path from ref: %v", err)
	}
	recordBytes, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read quarantine record: %v", err)
	}
	var sourceRecord quarantinedPayloadRecord
	if err := json.Unmarshal(recordBytes, &sourceRecord); err != nil {
		t.Fatalf("unmarshal quarantine record: %v", err)
	}
	sourceRecord.StoredAtUTC = time.Now().UTC().Add(-quarantineBlobRetentionPeriod - time.Hour).Format(time.RFC3339Nano)
	if err := writeQuarantinedPayloadRecord(recordPath, sourceRecord); err != nil {
		t.Fatalf("rewrite quarantine record: %v", err)
	}
}

func newShortLoopgateTestRepoRoot(t *testing.T) string {
	t.Helper()

	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	workspaceRoot := filepath.Clean(filepath.Join(workingDirectory, "..", ".."))
	baseDir := filepath.Join(workspaceRoot, ".tmp-loopgate-tests")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatalf("mkdir short test base dir: %v", err)
	}
	repoRoot, err := os.MkdirTemp(baseDir, "rt-")
	if err != nil {
		t.Fatalf("mkdir short test repo root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(repoRoot) })
	return repoRoot
}

func newShortLoopgateSocketPath(t *testing.T) string {
	t.Helper()

	socketFile, err := os.CreateTemp(os.TempDir(), "loopgate-*.sock")
	if err != nil {
		t.Fatalf("create short socket file: %v", err)
	}
	socketPath := socketFile.Name()
	_ = socketFile.Close()
	_ = os.Remove(socketPath)
	t.Cleanup(func() { _ = os.Remove(socketPath) })
	return socketPath
}

func startLoopgateServer(t *testing.T, repoRoot string, policyYAML string) (*Client, StatusResponse, *Server) {
	return startLoopgateServerWithRuntime(t, repoRoot, policyYAML, nil, true)
}

func writeSignedTestPolicyYAML(t *testing.T, repoRoot string, policyYAML string) {
	t.Helper()

	policySigner, err := testutil.NewPolicyTestSigner()
	if err != nil {
		t.Fatalf("new test policy signer: %v", err)
	}
	policySigner.ConfigureEnv(t.Setenv)
	if err := policySigner.WriteSignedPolicyYAML(repoRoot, policyYAML); err != nil {
		t.Fatalf("write signed policy: %v", err)
	}
}

func pinTestProcessAsExpectedClient(t *testing.T, server *Server) {
	t.Helper()

	testExecutablePath, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	normalizedExecutablePath := normalizeSessionExecutablePinPath(testExecutablePath)
	if strings.TrimSpace(normalizedExecutablePath) == "" {
		t.Fatal("expected normalized test executable path")
	}
	server.expectedClientPath = normalizedExecutablePath
}

// startLoopgateServerWithRuntime starts Loopgate in a temp repo. When runSessionBootstrap is false,
// the server is healthy but no control session is opened (for tests where session open must fail).
func startLoopgateServerWithRuntime(t *testing.T, repoRoot string, policyYAML string, runtimeCfg *config.RuntimeConfig, runSessionBootstrap bool) (*Client, StatusResponse, *Server) {
	t.Helper()

	policySigner, err := testutil.NewPolicyTestSigner()
	if err != nil {
		t.Fatalf("new test policy signer: %v", err)
	}
	policySigner.ConfigureEnv(t.Setenv)
	if err := policySigner.WriteSignedPolicyYAML(repoRoot, policyYAML); err != nil {
		t.Fatalf("write signed policy: %v", err)
	}
	writeTestMorphlingClassPolicy(t, repoRoot)
	if runtimeCfg != nil {
		if err := config.WriteRuntimeConfigYAML(repoRoot, *runtimeCfg); err != nil {
			t.Fatalf("write runtime config: %v", err)
		}
	}

	socketPath := newShortLoopgateSocketPath(t)
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	server.sessionOpenMinInterval = 0
	server.maxActiveSessionsPerUID = 64
	server.expirySweepMaxInterval = 0

	serverContext, cancel := context.WithCancel(context.Background())
	serverDone := make(chan struct{})
	serveErrCh := make(chan error, 1)
	go func() {
		defer close(serverDone)
		serveErrCh <- server.Serve(serverContext)
	}()
	t.Cleanup(func() {
		cancel()
		<-serverDone
	})

	client := NewClient(socketPath)
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, err = client.Health(context.Background())
		if err == nil {
			break
		}
		select {
		case serveErr := <-serveErrCh:
			t.Fatalf("loopgate serve exited before health check: %v", serveErr)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("wait for loopgate health: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}

	if !runSessionBootstrap {
		return client, StatusResponse{}, server
	}

	// Bootstrap session with a capability that exists on all default test policies so we can
	// perform a signed GET /v1/status and learn the full registered capability set.
	client.ConfigureSession("test-actor", "test-session", []string{"fs_list"})
	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("bootstrap status after session: %v", err)
	}
	client.ConfigureSession("test-actor", "test-session", advertisedSessionCapabilityNames(status))
	status, err = client.Status(context.Background())
	if err != nil {
		t.Fatalf("final status after advertised session bootstrap: %v", err)
	}
	// Bootstrap performed two session opens; clear open-spacing state so tests that tighten
	// sessionOpenMinInterval observe only their own opens.
	server.mu.Lock()
	server.sessionOpenByUID = make(map[uint32]time.Time)
	server.mu.Unlock()
	return client, status, server
}

func writeTestMorphlingClassPolicy(t *testing.T, repoRoot string) {
	t.Helper()
	_ = repoRoot
}

func defaultTestMorphlingClassPolicyYAML() string {
	return "version: \"1\"\n\n" +
		"classes:\n" +
		"  - name: reviewer\n" +
		"    description: \"Read-only analysis\"\n" +
		"    capabilities:\n" +
		"      allowed:\n" +
		"        - fs_list\n" +
		"        - fs_read\n" +
		"    sandbox:\n" +
		"      allowed_zones:\n" +
		"        - imports\n" +
		"        - scratch\n" +
		"        - workspace\n" +
		"    resource_limits:\n" +
		"      max_time_seconds: 300\n" +
		"      max_tokens: 50000\n" +
		"      max_disk_bytes: 52428800\n" +
		"    ttl:\n" +
		"      spawn_approval_ttl_seconds: 300\n" +
		"      capability_token_ttl_seconds: 360\n" +
		"      review_ttl_seconds: 86400\n" +
		"    spawn_requires_approval: false\n" +
		"    completion_requires_review: true\n" +
		"    max_concurrent: 3\n" +
		"  - name: editor\n" +
		"    description: \"Read and write files\"\n" +
		"    capabilities:\n" +
		"      allowed:\n" +
		"        - fs_list\n" +
		"        - fs_read\n" +
		"        - fs_write\n" +
		"    sandbox:\n" +
		"      allowed_zones:\n" +
		"        - agents\n" +
		"        - imports\n" +
		"        - outputs\n" +
		"        - scratch\n" +
		"        - workspace\n" +
		"    resource_limits:\n" +
		"      max_time_seconds: 600\n" +
		"      max_tokens: 100000\n" +
		"      max_disk_bytes: 104857600\n" +
		"    ttl:\n" +
		"      spawn_approval_ttl_seconds: 300\n" +
		"      capability_token_ttl_seconds: 660\n" +
		"      review_ttl_seconds: 86400\n" +
		"    spawn_requires_approval: true\n" +
		"    completion_requires_review: true\n" +
		"    max_concurrent: 2\n" +
		"  - name: tester\n" +
		"    description: \"Inspect tests and logs\"\n" +
		"    capabilities:\n" +
		"      allowed:\n" +
		"        - fs_list\n" +
		"        - fs_read\n" +
		"    sandbox:\n" +
		"      allowed_zones:\n" +
		"        - imports\n" +
		"        - logs\n" +
		"        - scratch\n" +
		"        - workspace\n" +
		"    resource_limits:\n" +
		"      max_time_seconds: 300\n" +
		"      max_tokens: 30000\n" +
		"      max_disk_bytes: 52428800\n" +
		"    ttl:\n" +
		"      spawn_approval_ttl_seconds: 300\n" +
		"      capability_token_ttl_seconds: 360\n" +
		"      review_ttl_seconds: 86400\n" +
		"    spawn_requires_approval: false\n" +
		"    completion_requires_review: true\n" +
		"    max_concurrent: 2\n" +
		"  - name: researcher\n" +
		"    description: \"Inspect imported evidence\"\n" +
		"    capabilities:\n" +
		"      allowed:\n" +
		"        - fs_list\n" +
		"        - fs_read\n" +
		"    sandbox:\n" +
		"      allowed_zones:\n" +
		"        - imports\n" +
		"        - quarantine\n" +
		"        - scratch\n" +
		"    resource_limits:\n" +
		"      max_time_seconds: 120\n" +
		"      max_tokens: 40000\n" +
		"      max_disk_bytes: 26214400\n" +
		"    ttl:\n" +
		"      spawn_approval_ttl_seconds: 300\n" +
		"      capability_token_ttl_seconds: 180\n" +
		"      review_ttl_seconds: 86400\n" +
		"    spawn_requires_approval: false\n" +
		"    completion_requires_review: true\n" +
		"    max_concurrent: 3\n" +
		"  - name: builder\n" +
		"    description: \"Build outputs in the sandbox\"\n" +
		"    capabilities:\n" +
		"      allowed:\n" +
		"        - fs_list\n" +
		"        - fs_read\n" +
		"        - fs_write\n" +
		"    sandbox:\n" +
		"      allowed_zones:\n" +
		"        - imports\n" +
		"        - outputs\n" +
		"        - scratch\n" +
		"        - workspace\n" +
		"    resource_limits:\n" +
		"      max_time_seconds: 900\n" +
		"      max_tokens: 50000\n" +
		"      max_disk_bytes: 524288000\n" +
		"    ttl:\n" +
		"      spawn_approval_ttl_seconds: 300\n" +
		"      capability_token_ttl_seconds: 960\n" +
		"      review_ttl_seconds: 86400\n" +
		"    spawn_requires_approval: true\n" +
		"    completion_requires_review: true\n" +
		"    max_concurrent: 1\n"
}

func mustJSON(t *testing.T, value interface{}) []byte {
	t.Helper()
	encodedBytes, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal test json: %v", err)
	}
	return encodedBytes
}

func capabilityNames(capabilities []CapabilitySummary) []string {
	names := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		names = append(names, capability.Name)
	}
	return names
}

func advertisedSessionCapabilityNames(status StatusResponse) []string {
	advertisedCapabilities := capabilityNames(status.Capabilities)
	// Control-plane routes are scoped separately from executable tools.
	// Tests that want a full control session need both sets or they silently miss
	// control-only surfaces such as governed memory operations.
	advertisedCapabilities = append(advertisedCapabilities, capabilityNames(status.ControlCapabilities)...)
	return advertisedCapabilities
}

func containsCapability(capabilities []CapabilitySummary, capabilityName string) bool {
	for _, capability := range capabilities {
		if capability.Name == capabilityName {
			return true
		}
	}
	return false
}

func loopgatePolicyYAML(writeRequiresApproval bool) string {
	approvalValue := "false"
	if writeRequiresApproval {
		approvalValue = "true"
	}

	return "version: 0.1.0\n\n" +
		"tools:\n" +
		"  filesystem:\n" +
		"    allowed_roots:\n" +
		"      - \".\"\n" +
		"    denied_paths: []\n" +
		"    read_enabled: true\n" +
		"    write_enabled: true\n" +
		"    write_requires_approval: " + approvalValue + "\n" +
		"  http:\n" +
		"    enabled: false\n" +
		"    allowed_domains: []\n" +
		"    requires_approval: true\n" +
		"    timeout_seconds: 10\n" +
		"  shell:\n" +
		"    enabled: false\n" +
		"    allowed_commands: []\n" +
		"    requires_approval: true\n" +
		"logging:\n" +
		"  log_commands: true\n" +
		"  log_tool_calls: true\n" +
		"safety:\n" +
		"  allow_persona_modification: false\n" +
		"  allow_policy_modification: false\n"
}

func loopgateHTTPPolicyYAML(requiresApproval bool) string {
	approvalValue := "false"
	if requiresApproval {
		approvalValue = "true"
	}

	return "version: 0.1.0\n\n" +
		"tools:\n" +
		"  filesystem:\n" +
		"    allowed_roots:\n" +
		"      - \".\"\n" +
		"    denied_paths: []\n" +
		"    read_enabled: true\n" +
		"    write_enabled: true\n" +
		"    write_requires_approval: false\n" +
		"  http:\n" +
		"    enabled: true\n" +
		"    allowed_domains: []\n" +
		"    requires_approval: " + approvalValue + "\n" +
		"    timeout_seconds: 10\n" +
		"  shell:\n" +
		"    enabled: false\n" +
		"    allowed_commands: []\n" +
		"    requires_approval: true\n" +
		"logging:\n" +
		"  log_commands: true\n" +
		"  log_tool_calls: true\n" +
		"safety:\n" +
		"  allow_persona_modification: false\n" +
		"  allow_policy_modification: false\n"
}

func loopgateShellPolicyYAML(requiresApproval bool, allowedCommands []string) string {
	approvalValue := "false"
	if requiresApproval {
		approvalValue = "true"
	}
	quotedCommands := make([]string, 0, len(allowedCommands))
	for _, allowedCommand := range allowedCommands {
		quotedCommands = append(quotedCommands, fmt.Sprintf("      - %q\n", allowedCommand))
	}

	return "version: 0.1.0\n\n" +
		"tools:\n" +
		"  filesystem:\n" +
		"    allowed_roots:\n" +
		"      - \".\"\n" +
		"    denied_paths: []\n" +
		"    read_enabled: true\n" +
		"    write_enabled: true\n" +
		"    write_requires_approval: false\n" +
		"  http:\n" +
		"    enabled: false\n" +
		"    allowed_domains: []\n" +
		"    requires_approval: true\n" +
		"    timeout_seconds: 10\n" +
		"  shell:\n" +
		"    enabled: true\n" +
		"    allowed_commands:\n" +
		strings.Join(quotedCommands, "") +
		"    requires_approval: " + approvalValue + "\n" +
		"logging:\n" +
		"  log_commands: true\n" +
		"  log_tool_calls: true\n" +
		"safety:\n" +
		"  allow_persona_modification: false\n" +
		"  allow_policy_modification: false\n"
}

func writeConfiguredConnectionYAML(t *testing.T, repoRoot string, providerBaseURL string) {
	t.Helper()

	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: example\n" +
		"grant_type: client_credentials\n" +
		"subject: service-bot\n" +
		"client_id: example-client\n" +
		"token_url: " + providerBaseURL + "/oauth/token\n" +
		"api_base_url: " + providerBaseURL + "/api\n" +
		"allowed_hosts:\n" +
		"  - 127.0.0.1\n" +
		"scopes:\n" +
		"  - status.read\n" +
		"credential:\n" +
		"  id: example-client-secret\n" +
		"  backend: env\n" +
		"  account_name: LOOPGATE_EXAMPLE_SECRET\n" +
		"  scope: example.status_read\n" +
		"capabilities:\n" +
		"  - name: example.status_get\n" +
		"    description: Read example provider status.\n" +
		"    method: GET\n" +
		"    path: /status\n" +
		"    content_class: structured_json\n" +
		"    extractor: json_field_allowlist\n" +
		"    response_fields:\n" +
		"      - name: service\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 256\n" +
		"      - name: healthy\n" +
		"        sensitivity: benign\n" +
		"        max_inline_bytes: 32\n"
	if err := os.WriteFile(filepath.Join(connectionDir, "example.yaml"), []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured connection yaml: %v", err)
	}
}

func writeConfiguredConnectionYAMLWithBlobFallback(t *testing.T, repoRoot string, providerBaseURL string) {
	t.Helper()

	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: example\n" +
		"grant_type: client_credentials\n" +
		"subject: service-bot\n" +
		"client_id: example-client\n" +
		"token_url: " + providerBaseURL + "/oauth/token\n" +
		"api_base_url: " + providerBaseURL + "/api\n" +
		"allowed_hosts:\n" +
		"  - 127.0.0.1\n" +
		"scopes:\n" +
		"  - status.read\n" +
		"credential:\n" +
		"  id: example-client-secret\n" +
		"  backend: env\n" +
		"  account_name: LOOPGATE_EXAMPLE_SECRET\n" +
		"  scope: example.status_read\n" +
		"capabilities:\n" +
		"  - name: example.status_get\n" +
		"    description: Read example provider status.\n" +
		"    method: GET\n" +
		"    path: /status\n" +
		"    content_class: structured_json\n" +
		"    extractor: json_field_allowlist\n" +
		"    response_fields:\n" +
		"      - name: service\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 256\n" +
		"        allow_blob_ref_fallback: true\n" +
		"      - name: healthy\n" +
		"        sensitivity: benign\n" +
		"        max_inline_bytes: 32\n"
	if err := os.WriteFile(filepath.Join(connectionDir, "example.yaml"), []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured connection yaml with blob fallback: %v", err)
	}
}

func writeConfiguredMarkdownFrontmatterYAML(t *testing.T, repoRoot string, providerBaseURL string) {
	t.Helper()

	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: docs\n" +
		"grant_type: client_credentials\n" +
		"subject: docs-bot\n" +
		"client_id: docs-client\n" +
		"token_url: " + providerBaseURL + "/oauth/token\n" +
		"api_base_url: " + providerBaseURL + "/api\n" +
		"allowed_hosts:\n" +
		"  - 127.0.0.1\n" +
		"scopes:\n" +
		"  - docs.read\n" +
		"credential:\n" +
		"  id: docs-client-secret\n" +
		"  backend: env\n" +
		"  account_name: LOOPGATE_EXAMPLE_SECRET\n" +
		"  scope: docs.read\n" +
		"capabilities:\n" +
		"  - name: docs.release_get\n" +
		"    description: Read release metadata.\n" +
		"    method: GET\n" +
		"    path: /release.md\n" +
		"    content_class: markdown\n" +
		"    extractor: markdown_frontmatter_keys\n" +
		"    response_fields:\n" +
		"      - name: version\n" +
		"        frontmatter_key: version\n" +
		"        sensitivity: benign\n" +
		"        max_inline_bytes: 64\n" +
		"      - name: published\n" +
		"        frontmatter_key: published\n" +
		"        sensitivity: benign\n" +
		"        max_inline_bytes: 16\n"
	if err := os.WriteFile(filepath.Join(connectionDir, "docs.yaml"), []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured markdown frontmatter yaml: %v", err)
	}
}

func writeConfiguredMarkdownSectionYAML(t *testing.T, repoRoot string, providerBaseURL string) {
	t.Helper()

	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: docs\n" +
		"grant_type: client_credentials\n" +
		"subject: docs-bot\n" +
		"client_id: docs-client\n" +
		"token_url: " + providerBaseURL + "/oauth/token\n" +
		"api_base_url: " + providerBaseURL + "/api\n" +
		"allowed_hosts:\n" +
		"  - 127.0.0.1\n" +
		"scopes:\n" +
		"  - docs.read\n" +
		"credential:\n" +
		"  id: docs-client-secret\n" +
		"  backend: env\n" +
		"  account_name: LOOPGATE_EXAMPLE_SECRET\n" +
		"  scope: docs.read\n" +
		"capabilities:\n" +
		"  - name: docs.section_get\n" +
		"    description: Read release section.\n" +
		"    method: GET\n" +
		"    path: /release.md\n" +
		"    content_class: markdown\n" +
		"    extractor: markdown_section_selector\n" +
		"    response_fields:\n" +
		"      - name: summary\n" +
		"        heading_path:\n" +
		"          - Release Notes\n" +
		"          - Overview\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 256\n"
	if err := os.WriteFile(filepath.Join(connectionDir, "docs.yaml"), []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured markdown section yaml: %v", err)
	}
}

func writeConfiguredHTMLMetaYAML(t *testing.T, repoRoot string, providerBaseURL string) {
	t.Helper()

	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: docshtml\n" +
		"grant_type: client_credentials\n" +
		"subject: docs-bot\n" +
		"client_id: docs-client\n" +
		"token_url: " + providerBaseURL + "/oauth/token\n" +
		"api_base_url: " + providerBaseURL + "/api\n" +
		"allowed_hosts:\n" +
		"  - 127.0.0.1\n" +
		"scopes:\n" +
		"  - docs.read\n" +
		"credential:\n" +
		"  id: docs-client-secret\n" +
		"  backend: env\n" +
		"  account_name: LOOPGATE_EXAMPLE_SECRET\n" +
		"  scope: docs.read\n" +
		"capabilities:\n" +
		"  - name: docshtml.page_get\n" +
		"    description: Read HTML page metadata.\n" +
		"    method: GET\n" +
		"    path: /page.html\n" +
		"    content_class: html\n" +
		"    extractor: html_meta_allowlist\n" +
		"    response_fields:\n" +
		"      - name: page_title\n" +
		"        html_title: true\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 128\n" +
		"      - name: description\n" +
		"        meta_name: description\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 128\n" +
		"      - name: site_name\n" +
		"        meta_property: og:site_name\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 128\n"
	if err := os.WriteFile(filepath.Join(connectionDir, "docshtml.yaml"), []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured html metadata yaml: %v", err)
	}
}

func writeConfiguredPublicHTMLMetaYAML(t *testing.T, repoRoot string, providerBaseURL string) {
	t.Helper()

	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: statuspage\n" +
		"grant_type: public_read\n" +
		"subject: stripe\n" +
		"api_base_url: " + providerBaseURL + "\n" +
		"allowed_hosts:\n" +
		"  - 127.0.0.1\n" +
		"capabilities:\n" +
		"  - name: statuspage.summary_get\n" +
		"    description: Read public status page metadata.\n" +
		"    method: GET\n" +
		"    path: /\n" +
		"    content_class: html\n" +
		"    extractor: html_meta_allowlist\n" +
		"    response_fields:\n" +
		"      - name: page_title\n" +
		"        html_title: true\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 128\n" +
		"      - name: description\n" +
		"        meta_name: description\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 128\n"
	if err := os.WriteFile(filepath.Join(connectionDir, "statuspage.yaml"), []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured public html yaml: %v", err)
	}
}

func writeConfiguredPublicJSONNestedYAML(t *testing.T, repoRoot string, providerBaseURL string) {
	t.Helper()

	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: statuspage\n" +
		"grant_type: public_read\n" +
		"subject: github\n" +
		"api_base_url: " + providerBaseURL + "\n" +
		"allowed_hosts:\n" +
		"  - 127.0.0.1\n" +
		"capabilities:\n" +
		"  - name: statuspage.summary_get\n" +
		"    description: Read public status summary fields.\n" +
		"    method: GET\n" +
		"    path: /\n" +
		"    content_class: structured_json\n" +
		"    extractor: json_nested_selector\n" +
		"    response_fields:\n" +
		"      - name: status_description\n" +
		"        json_path: status.description\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 128\n" +
		"      - name: status_indicator\n" +
		"        json_path: status.indicator\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 32\n"
	if err := os.WriteFile(filepath.Join(connectionDir, "statuspage.yaml"), []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured public nested json yaml: %v", err)
	}
}

func writeConfiguredPublicJSONIssueListYAML(t *testing.T, repoRoot string, providerBaseURL string) {
	t.Helper()

	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: repoapi\n" +
		"grant_type: public_read\n" +
		"subject: sample-repo\n" +
		"api_base_url: " + providerBaseURL + "\n" +
		"allowed_hosts:\n" +
		"  - 127.0.0.1\n" +
		"capabilities:\n" +
		"  - name: repo.issues_list\n" +
		"    description: Read recent open repository issues.\n" +
		"    method: GET\n" +
		"    path: /\n" +
		"    content_class: structured_json\n" +
		"    extractor: json_object_list_selector\n" +
		"    response_fields:\n" +
		"      - name: issues\n" +
		"        json_path: issues.items\n" +
		"        json_list_item_fields:\n" +
		"          - number\n" +
		"          - title\n" +
		"          - state\n" +
		"          - updated_at\n" +
		"          - html_url\n" +
		"        max_items: 2\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 4096\n"
	if err := os.WriteFile(filepath.Join(connectionDir, "issues.yaml"), []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured public issue list yaml: %v", err)
	}
}

func writeConfiguredPKCEYAML(t *testing.T, repoRoot string, providerBaseURL string) {
	t.Helper()

	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		t.Fatalf("mkdir connection config dir: %v", err)
	}
	connectionYAML := "" +
		"provider: examplepkce\n" +
		"grant_type: pkce\n" +
		"subject: workspace-user\n" +
		"client_id: pkce-client\n" +
		"authorization_url: " + providerBaseURL + "/oauth/authorize\n" +
		"token_url: " + providerBaseURL + "/oauth/token\n" +
		"redirect_url: http://127.0.0.1/callback\n" +
		"api_base_url: " + providerBaseURL + "/api\n" +
		"allowed_hosts:\n" +
		"  - 127.0.0.1\n" +
		"scopes:\n" +
		"  - status.read\n" +
		"credential:\n" +
		"  id: pkce-refresh-token\n" +
		"  backend: secure\n" +
		"  account_name: loopgate.examplepkce.workspace-user\n" +
		"  scope: examplepkce.status_read\n" +
		"capabilities:\n" +
		"  - name: examplepkce.status_get\n" +
		"    description: Read example PKCE provider status.\n" +
		"    method: GET\n" +
		"    path: /status\n" +
		"    content_class: structured_json\n" +
		"    extractor: json_field_allowlist\n" +
		"    response_fields:\n" +
		"      - name: service\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 256\n" +
		"      - name: healthy\n" +
		"        sensitivity: benign\n" +
		"        max_inline_bytes: 32\n" +
		"      - name: generation\n" +
		"        sensitivity: benign\n" +
		"        max_inline_bytes: 32\n"
	if err := os.WriteFile(filepath.Join(connectionDir, "examplepkce.yaml"), []byte(connectionYAML), 0o600); err != nil {
		t.Fatalf("write configured pkce yaml: %v", err)
	}
}

func readUIReplayEvents(t *testing.T, client *Client, lastEventID string) []UIEventEnvelope {
	t.Helper()

	capabilityToken, err := client.ensureCapabilityToken(context.Background())
	if err != nil {
		t.Fatalf("ensure capability token for ui events: %v", err)
	}

	requestContext, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, client.baseURL+"/v1/ui/events", nil)
	if err != nil {
		t.Fatalf("build ui events request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+capabilityToken)
	if lastEventID != "" {
		request.Header.Set("Last-Event-ID", lastEventID)
	}
	if err := client.attachRequestSignature(request, "/v1/ui/events", nil); err != nil {
		t.Fatalf("attach ui events signature: %v", err)
	}

	httpResponse, err := client.httpClient.Do(request)
	if err != nil {
		t.Fatalf("do ui events request: %v", err)
	}
	defer httpResponse.Body.Close()
	if httpResponse.StatusCode != http.StatusOK {
		t.Fatalf("unexpected ui events status: %d", httpResponse.StatusCode)
	}

	reader := bufio.NewReader(httpResponse.Body)
	events := make([]UIEventEnvelope, 0, 8)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var uiEvent UIEventEnvelope
		if err := json.Unmarshal([]byte(strings.TrimPrefix(strings.TrimSpace(line), "data: ")), &uiEvent); err != nil {
			t.Fatalf("decode ui event: %v", err)
		}
		events = append(events, uiEvent)
	}
	return events
}

func readUIRecentEvents(t *testing.T, client *Client, lastEventID string) []UIEventEnvelope {
	t.Helper()

	capabilityToken, err := client.ensureCapabilityToken(context.Background())
	if err != nil {
		t.Fatalf("ensure capability token for recent ui events: %v", err)
	}

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, client.baseURL+"/v1/ui/events/recent", nil)
	if err != nil {
		t.Fatalf("build recent ui events request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+capabilityToken)
	if lastEventID != "" {
		request.Header.Set("Last-Event-ID", lastEventID)
	}
	if err := client.attachRequestSignature(request, "/v1/ui/events/recent", nil); err != nil {
		t.Fatalf("attach recent ui events signature: %v", err)
	}

	httpResponse, err := client.httpClient.Do(request)
	if err != nil {
		t.Fatalf("do recent ui events request: %v", err)
	}
	defer httpResponse.Body.Close()
	if httpResponse.StatusCode != http.StatusOK {
		t.Fatalf("unexpected recent ui events status: %d", httpResponse.StatusCode)
	}

	var response UIRecentEventsResponse
	if err := json.NewDecoder(httpResponse.Body).Decode(&response); err != nil {
		t.Fatalf("decode recent ui events response: %v", err)
	}
	return response.Events
}

func containsUIEventType(events []UIEventEnvelope, expectedType string) bool {
	for _, uiEvent := range events {
		if uiEvent.Type == expectedType {
			return true
		}
	}
	return false
}

func containsUICapabilityEvent(events []UIEventEnvelope, capability string) bool {
	for _, uiEvent := range events {
		encodedEvent, err := json.Marshal(uiEvent)
		if err != nil {
			continue
		}
		if strings.Contains(string(encodedEvent), fmt.Sprintf("\"capability\":\"%s\"", capability)) {
			return true
		}
	}
	return false
}

// --- Security hardening tests ---
