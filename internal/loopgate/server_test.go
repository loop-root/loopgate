package loopgate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"loopgate/internal/ledger"
	"loopgate/internal/sandbox"
)

func TestClientExecuteCapability_ReadAndWrite(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	writeResponse, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
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
	if writeResponse.Status != controlapipkg.ResponseStatusSuccess {
		t.Fatalf("unexpected write response: %#v", writeResponse)
	}

	readResponse, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
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
	if displayOnly, ok := readResponse.Metadata["display_only"].(bool); !ok || displayOnly {
		t.Fatalf("expected fs_read to not be display_only, got %#v", readResponse.Metadata)
	}

	if len(status.Capabilities) == 0 {
		t.Fatal("expected capabilities in status")
	}
}

func TestClientExecuteCapability_FsReadRateLimitUsesDedicatedDenialCode(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "notes.txt"), []byte("hello loopgate"), 0o600); err != nil {
		t.Fatalf("write notes file: %v", err)
	}

	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	nowUTC := server.now().UTC()
	server.mu.Lock()
	preloadedReads := make([]time.Time, 0, defaultFsReadRateLimit)
	for i := 0; i < defaultFsReadRateLimit; i++ {
		preloadedReads = append(preloadedReads, nowUTC)
	}
	server.replayState.sessionReadCounts[client.controlSessionID] = preloadedReads
	server.mu.Unlock()

	deniedResponse, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-read-rate-limited",
		Capability: "fs_read",
		Arguments: map[string]string{
			"path": "notes.txt",
		},
	})
	if err != nil {
		t.Fatalf("execute rate-limited fs_read: %v", err)
	}
	if deniedResponse.Status != controlapipkg.ResponseStatusDenied {
		t.Fatalf("expected denied fs_read response, got %#v", deniedResponse)
	}
	if deniedResponse.DenialCode != controlapipkg.DenialCodeFsReadRateLimitExceeded {
		t.Fatalf("expected fs_read rate-limit denial code, got %#v", deniedResponse)
	}
}

func TestCheckFsReadRateLimit_DoesNotMutateCallerSliceBackingArray(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	controlSessionID := "session-rate-limit"
	nowUTC := server.now().UTC()
	expiredTimestamp := nowUTC.Add(-fsReadRateWindow - time.Second)
	recentTimestampOne := nowUTC.Add(-30 * time.Second)
	recentTimestampTwo := nowUTC.Add(-15 * time.Second)
	preloadedReads := []time.Time{expiredTimestamp, recentTimestampOne, recentTimestampTwo}
	expectedOriginalReads := append([]time.Time(nil), preloadedReads...)

	server.mu.Lock()
	server.replayState.sessionReadCounts[controlSessionID] = preloadedReads
	server.mu.Unlock()

	if denied := server.checkFsReadRateLimit(controlSessionID); denied {
		t.Fatal("expected fs_read rate limit check to allow when below the limit")
	}

	for index, expectedTimestamp := range expectedOriginalReads {
		if !preloadedReads[index].Equal(expectedTimestamp) {
			t.Fatalf("expected original caller slice to remain unchanged at index %d: want %s got %s", index, expectedTimestamp, preloadedReads[index])
		}
	}
}

func TestCheckFsReadRateLimit_UsesServerConfiguredLimit(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.fsReadRateLimit = 1

	controlSessionID := "session-rate-limit-configurable"
	nowUTC := server.now().UTC()
	server.mu.Lock()
	server.replayState.sessionReadCounts[controlSessionID] = []time.Time{nowUTC}
	server.mu.Unlock()

	if denied := server.checkFsReadRateLimit(controlSessionID); !denied {
		t.Fatal("expected fs_read rate limit denial at configured server limit")
	}
}

func TestCheckHookPreValidateRateLimit_IsPerUID(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.hookPreValidateRateLimit = 1
	server.hookPreValidateRateWindow = time.Hour

	if denied := server.checkHookPreValidateRateLimit(1001); denied {
		t.Fatal("expected first hook pre-validate check for uid 1001 to allow")
	}
	if denied := server.checkHookPreValidateRateLimit(1001); !denied {
		t.Fatal("expected second hook pre-validate check for uid 1001 to deny")
	}
	if denied := server.checkHookPreValidateRateLimit(2002); denied {
		t.Fatal("expected separate peer uid to keep its own rate-limit bucket")
	}
}

func TestClientExecuteCapability_DeniesRawMemoryFilesystemAccess(t *testing.T) {
	repoRoot := t.TempDir()
	memoryDir := filepath.Join(repoRoot, ".loopgate", "memory")
	if err := os.MkdirAll(memoryDir, 0o700); err != nil {
		t.Fatalf("mkdir memory dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoryDir, "keys.json"), []byte("{\"keys\":[]}\n"), 0o600); err != nil {
		t.Fatalf("write keys file: %v", err)
	}

	policyYAML := strings.Replace(loopgatePolicyYAML(false), "    denied_paths: []\n", "    denied_paths:\n      - \".loopgate/memory\"\n      - \"runtime/state/memory\"\n", 1)
	client, _, _ := startLoopgateServer(t, repoRoot, policyYAML)

	readResponse, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-memory-read",
		Capability: "fs_read",
		Arguments: map[string]string{
			"path": ".loopgate/memory/keys.json",
		},
	})
	if err != nil {
		t.Fatalf("execute fs_read: %v", err)
	}
	if readResponse.Status != controlapipkg.ResponseStatusError {
		t.Fatalf("expected blocked read response, got %#v", readResponse)
	}
	if !strings.Contains(readResponse.DenialReason, "path denied") {
		t.Fatalf("expected path denial, got %#v", readResponse)
	}

	writeResponse, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-memory-write",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    ".loopgate/memory/keys.json",
			"content": "{\"keys\":[{\"id\":\"user.name\"}]}\n",
		},
	})
	if err != nil {
		t.Fatalf("execute fs_write: %v", err)
	}
	if writeResponse.Status != controlapipkg.ResponseStatusError {
		t.Fatalf("expected blocked write response, got %#v", writeResponse)
	}
	if !strings.Contains(writeResponse.DenialReason, "path denied") {
		t.Fatalf("expected path denial, got %#v", writeResponse)
	}
}

func TestExecuteCapabilityRequest_ShellCommandOutsideAllowlistIsDeniedBeforeApproval(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgateShellPolicyYAML(true, []string{"git"}))

	response, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-shell-policy-deny",
		Capability: "shell_exec",
		Arguments: map[string]string{
			"command": "env",
		},
	})
	if err != nil {
		t.Fatalf("execute shell_exec: %v", err)
	}
	if response.Status != controlapipkg.ResponseStatusDenied {
		t.Fatalf("expected denied shell response, got %#v", response)
	}
	if response.DenialCode != controlapipkg.DenialCodePolicyDenied {
		t.Fatalf("expected policy denial code, got %#v", response)
	}
	if response.ApprovalRequired {
		t.Fatalf("expected allowlist denial before approval creation, got %#v", response)
	}
}

func TestClientExecuteCapability_RequiresApproval(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))

	response, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
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
	if response.DenialCode != controlapipkg.DenialCodeApprovalRequired {
		t.Fatalf("expected approval-required denial code, got %#v", response)
	}
	if approvalClass, _ := response.Metadata["approval_class"].(string); approvalClass != ApprovalClassWriteSandboxPath {
		t.Fatalf("expected approval_class %q, got %#v", ApprovalClassWriteSandboxPath, response.Metadata)
	}

	resolvedResponse, err := client.DecideApproval(context.Background(), response.ApprovalRequestID, true)
	if err != nil {
		t.Fatalf("approve request: %v", err)
	}
	if resolvedResponse.Status != controlapipkg.ResponseStatusSuccess {
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
	server.sessionState.sessions[controlSessionID] = controlSession{
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
		controlapipkg.CapabilityRequest{
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
	if response.Status != controlapipkg.ResponseStatusPendingApproval {
		t.Fatalf("expected pending approval, got %#v", response)
	}
	if response.DenialCode != controlapipkg.DenialCodeApprovalRequired {
		t.Fatalf("expected approval-required denial code, got %#v", response)
	}
	if approvalClass, _ := response.Metadata["approval_class"].(string); approvalClass != ApprovalClassWriteHostFolder {
		t.Fatalf("expected approval_class %q, got %#v", ApprovalClassWriteHostFolder, response.Metadata)
	}
	if approvalReason, _ := response.Metadata["approval_reason"].(string); approvalReason != fmt.Sprintf("Grant write access to %s for %s", resolvedRepoRoot, operatorMountWriteGrantTTL) {
		t.Fatalf("expected approval_reason for root grant, got %#v", response.Metadata)
	}
	server.mu.Lock()
	pendingApproval, found := server.approvalState.records[response.ApprovalRequestID]
	server.mu.Unlock()
	if !found {
		t.Fatalf("pending approval %q not found", response.ApprovalRequestID)
	}
	if pendingApproval.Reason != fmt.Sprintf("Grant write access to %s for %s", resolvedRepoRoot, operatorMountWriteGrantTTL) {
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
	server.sessionState.sessions[controlSessionID] = controlSession{
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
		controlapipkg.CapabilityRequest{
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

	if _, err := server.commitApprovalGrantConsumed(pendingResponse.ApprovalRequestID, decisionNonce, ""); err != nil {
		t.Fatalf("commit approval grant consumed: %v", err)
	}

	server.mu.Lock()
	sessionAfterGrant := server.sessionState.sessions[controlSessionID]
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
		controlapipkg.CapabilityRequest{
			RequestID:  "req-operator-mount-grant-2",
			Capability: "operator_mount.fs_write",
			Arguments: map[string]string{
				"path":    "second.md",
				"content": "# second\n",
			},
		},
		true,
	)
	if secondResponse.Status != controlapipkg.ResponseStatusSuccess {
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
	server.sessionState.sessions[controlSessionID] = controlSession{
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
		controlapipkg.CapabilityRequest{
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
	server.sessionState.sessions[controlSessionID] = controlSession{
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
		controlapipkg.CapabilityRequest{
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

	importResponse, err := client.SandboxImport(context.Background(), controlapipkg.SandboxImportRequest{
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
	if importResponse.SandboxAbsolutePath != "/loopgate/home/imports/example.txt" {
		t.Fatalf("expected virtual sandbox path, got %#v", importResponse)
	}

	stageResponse, err := client.SandboxStage(context.Background(), controlapipkg.SandboxStageRequest{
		SandboxSourcePath: "/loopgate/home/imports/example.txt",
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
	if stageResponse.SourceSandboxPath != "/loopgate/home/imports/example.txt" {
		t.Fatalf("expected virtual source sandbox path, got %#v", stageResponse)
	}
	if stageResponse.SandboxAbsolutePath != "/loopgate/home/outputs/export-me.txt" {
		t.Fatalf("expected virtual staged path, got %#v", stageResponse)
	}

	metadataResponse, err := client.SandboxMetadata(context.Background(), controlapipkg.SandboxMetadataRequest{
		SandboxSourcePath: "/loopgate/home/outputs/export-me.txt",
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
	if metadataResponse.SourceSandboxPath != "/loopgate/home/imports/example.txt" {
		t.Fatalf("expected virtual metadata source path, got %#v", metadataResponse)
	}

	server.mu.Lock()
	controlSession := server.sessionState.sessions[client.controlSessionID]
	if controlSession.OperatorMountWriteGrants == nil {
		controlSession.OperatorMountWriteGrants = make(map[string]time.Time)
	}
	controlSession.OperatorMountWriteGrants[resolvedHostRootPath] = server.now().UTC().Add(operatorMountWriteGrantTTL)
	server.sessionState.sessions[client.controlSessionID] = controlSession
	server.mu.Unlock()

	hostDestinationPath := filepath.Join(hostRootPath, "exported.txt")
	exportResponse, err := client.SandboxExport(context.Background(), controlapipkg.SandboxExportRequest{
		SandboxSourcePath:   "/loopgate/home/outputs/export-me.txt",
		HostDestinationPath: hostDestinationPath,
	})
	if err != nil {
		t.Fatalf("sandbox export: %v", err)
	}
	if exportResponse.Action != "export" {
		t.Fatalf("unexpected export response: %#v", exportResponse)
	}
	if exportResponse.SourceSandboxPath != "/loopgate/home/outputs/export-me.txt" {
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

	_, err := client.SandboxImport(context.Background(), controlapipkg.SandboxImportRequest{
		HostSourcePath:  hostSourcePath,
		DestinationName: "example.txt",
	})
	if err == nil {
		t.Fatal("expected sandbox import denial without operator mount binding")
	}
	if !strings.Contains(err.Error(), controlapipkg.DenialCodeControlSessionBindingInvalid) {
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
	if _, err := client.SandboxImport(context.Background(), controlapipkg.SandboxImportRequest{
		HostSourcePath:  hostSourcePath,
		DestinationName: "example.txt",
	}); err != nil {
		t.Fatalf("sandbox import: %v", err)
	}
	if _, err := client.SandboxStage(context.Background(), controlapipkg.SandboxStageRequest{
		SandboxSourcePath: "/loopgate/home/imports/example.txt",
		OutputName:        "export-me.txt",
	}); err != nil {
		t.Fatalf("sandbox stage: %v", err)
	}

	_, err := client.SandboxExport(context.Background(), controlapipkg.SandboxExportRequest{
		SandboxSourcePath:   "/loopgate/home/outputs/export-me.txt",
		HostDestinationPath: filepath.Join(hostRootPath, "exported.txt"),
	})
	if err == nil {
		t.Fatal("expected sandbox export denial without operator mount write grant")
	}
	if !strings.Contains(err.Error(), controlapipkg.DenialCodeApprovalRequired) {
		t.Fatalf("expected approval-required denial, got %v", err)
	}
}

// --- Security hardening tests ---
