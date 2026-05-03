package loopgate

import (
	"context"
	"errors"
	"fmt"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
