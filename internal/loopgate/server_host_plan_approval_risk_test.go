package loopgate

import (
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExecuteCapabilityRequest_HostPlanApplyMoveOnlyWithinGrantedDownloadsBypassesApproval(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := filepath.Join(repoRoot, "home")
	downloadsDir := filepath.Join(homeDir, "Downloads")
	if err := os.MkdirAll(downloadsDir, 0o755); err != nil {
		t.Fatalf("mkdir downloads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(downloadsDir, "report.pdf"), []byte("pdf"), 0o644); err != nil {
		t.Fatalf("seed source file: %v", err)
	}

	server := newHostPlanApplyPolicyTestServer(t, repoRoot, homeDir)
	seedGrantedDownloadsFolderAccess(t, server)

	planID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	server.hostAccessRuntime.mu.Lock()
	server.hostAccessRuntime.plans[planID] = &hostAccessStoredPlan{
		ControlSessionID: "cs-downloads",
		FolderPresetID:   folderAccessDownloadsID,
		Operations: []hostOrganizePlanOp{
			{Kind: "mkdir", Path: "Archives"},
			{Kind: "move", From: "report.pdf", To: "Archives/report.pdf"},
		},
		CreatedAt: server.now().UTC(),
	}
	server.hostAccessRuntime.mu.Unlock()

	resp := server.executeCapabilityRequest(
		contextBackgroundWithOperatorMount("cs-downloads"),
		capabilityToken{
			TokenID:             "tok-downloads-allow",
			ControlSessionID:    "cs-downloads",
			ActorLabel:          "operator",
			ClientSessionLabel:  "operator-session",
			AllowedCapabilities: capabilitySet([]string{"host.plan.apply"}),
			ExpiresAt:           server.now().UTC().Add(time.Hour),
		},
		controlapipkg.CapabilityRequest{
			RequestID:  "req-downloads-allow",
			Capability: "host.plan.apply",
			Arguments:  map[string]string{"plan_id": planID},
		},
		true,
	)

	if resp.ApprovalRequired {
		t.Fatalf("expected low-risk host plan apply to bypass approval, got %#v", resp)
	}
	if resp.Status != controlapipkg.ResponseStatusSuccess {
		t.Fatalf("expected success, got %#v", resp)
	}
	if _, err := os.Stat(filepath.Join(downloadsDir, "Archives", "report.pdf")); err != nil {
		t.Fatalf("expected moved file in Archives, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(downloadsDir, "report.pdf")); !os.IsNotExist(err) {
		t.Fatalf("expected source file removed after move, stat err=%v", err)
	}
}

func TestExecuteCapabilityRequest_HostPlanApplyOverwriteRiskStillRequiresApproval(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := filepath.Join(repoRoot, "home")
	downloadsDir := filepath.Join(homeDir, "Downloads")
	if err := os.MkdirAll(filepath.Join(downloadsDir, "Archives"), 0o755); err != nil {
		t.Fatalf("mkdir downloads archives: %v", err)
	}
	if err := os.WriteFile(filepath.Join(downloadsDir, "report.pdf"), []byte("new"), 0o644); err != nil {
		t.Fatalf("seed source file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(downloadsDir, "Archives", "report.pdf"), []byte("existing"), 0o644); err != nil {
		t.Fatalf("seed conflicting destination file: %v", err)
	}

	server := newHostPlanApplyPolicyTestServer(t, repoRoot, homeDir)
	seedGrantedDownloadsFolderAccess(t, server)

	planID := "cccccccccccccccccccccccccccccccc"
	server.hostAccessRuntime.mu.Lock()
	server.hostAccessRuntime.plans[planID] = &hostAccessStoredPlan{
		ControlSessionID: "cs-downloads",
		FolderPresetID:   folderAccessDownloadsID,
		Operations: []hostOrganizePlanOp{
			{Kind: "move", From: "report.pdf", To: "Archives/report.pdf"},
		},
		CreatedAt: server.now().UTC(),
	}
	server.hostAccessRuntime.mu.Unlock()

	resp := server.executeCapabilityRequest(
		contextBackgroundWithOperatorMount("cs-downloads"),
		capabilityToken{
			TokenID:             "tok-downloads-approval",
			ControlSessionID:    "cs-downloads",
			ActorLabel:          "operator",
			ClientSessionLabel:  "operator-session",
			AllowedCapabilities: capabilitySet([]string{"host.plan.apply"}),
			ExpiresAt:           server.now().UTC().Add(time.Hour),
		},
		controlapipkg.CapabilityRequest{
			RequestID:  "req-downloads-approval",
			Capability: "host.plan.apply",
			Arguments:  map[string]string{"plan_id": planID},
		},
		true,
	)

	if !resp.ApprovalRequired {
		t.Fatalf("expected overwrite-risk host plan apply to require approval, got %#v", resp)
	}
	if resp.Status != controlapipkg.ResponseStatusPendingApproval {
		t.Fatalf("expected pending approval, got %#v", resp)
	}
}

func TestExecuteCapabilityRequest_HostPlanApplySymlinkDestinationStillRequiresApproval(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := filepath.Join(repoRoot, "home")
	downloadsDir := filepath.Join(homeDir, "Downloads")
	outsideDir := filepath.Join(repoRoot, "outside")
	if err := os.MkdirAll(downloadsDir, 0o755); err != nil {
		t.Fatalf("mkdir downloads: %v", err)
	}
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	if err := os.WriteFile(filepath.Join(downloadsDir, "report.pdf"), []byte("pdf"), 0o644); err != nil {
		t.Fatalf("seed source file: %v", err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(downloadsDir, "escape")); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	server := newHostPlanApplyPolicyTestServer(t, repoRoot, homeDir)
	seedGrantedDownloadsFolderAccess(t, server)

	planID := "dddddddddddddddddddddddddddddddd"
	server.hostAccessRuntime.mu.Lock()
	server.hostAccessRuntime.plans[planID] = &hostAccessStoredPlan{
		ControlSessionID: "cs-downloads",
		FolderPresetID:   folderAccessDownloadsID,
		Operations: []hostOrganizePlanOp{
			{Kind: "move", From: "report.pdf", To: "escape/report.pdf"},
		},
		CreatedAt: server.now().UTC(),
	}
	server.hostAccessRuntime.mu.Unlock()

	resp := server.executeCapabilityRequest(
		contextBackgroundWithOperatorMount("cs-downloads"),
		capabilityToken{
			TokenID:             "tok-downloads-symlink",
			ControlSessionID:    "cs-downloads",
			ActorLabel:          "operator",
			ClientSessionLabel:  "operator-session",
			AllowedCapabilities: capabilitySet([]string{"host.plan.apply"}),
			ExpiresAt:           server.now().UTC().Add(time.Hour),
		},
		controlapipkg.CapabilityRequest{
			RequestID:  "req-downloads-symlink",
			Capability: "host.plan.apply",
			Arguments:  map[string]string{"plan_id": planID},
		},
		true,
	)

	if !resp.ApprovalRequired {
		t.Fatalf("expected symlink destination host plan apply to require approval, got %#v", resp)
	}
	if resp.Status != controlapipkg.ResponseStatusPendingApproval {
		t.Fatalf("expected pending approval, got %#v", resp)
	}
}
