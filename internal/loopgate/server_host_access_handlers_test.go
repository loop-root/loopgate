package loopgate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"loopgate/internal/config"
	"loopgate/internal/ledger"
	policypkg "loopgate/internal/policy"
	"loopgate/internal/sandbox"
	toolspkg "loopgate/internal/tools"
)

func TestParseHostOrganizePlanJSON_ArrayAndWrappedString(t *testing.T) {
	arrayText := `[{"kind":"mkdir","path":"a"},{"kind":"move","from":"b","to":"c"}]`
	ops, err := parseHostOrganizePlanJSON(arrayText)
	if err != nil {
		t.Fatalf("array form: %v", err)
	}
	if len(ops) != 2 || ops[0].Kind != "mkdir" || ops[0].Path != "a" {
		t.Fatalf("ops: %#v", ops)
	}

	wrapped, err := json.Marshal(arrayText)
	if err != nil {
		t.Fatal(err)
	}
	ops2, err := parseHostOrganizePlanJSON(string(wrapped))
	if err != nil {
		t.Fatalf("JSON-string-wrapped form: %v", err)
	}
	if len(ops2) != 2 {
		t.Fatalf("wrapped: %#v", ops2)
	}
}

func TestExecuteHostPlanApply_UnknownPlanIDExplainsRecovery(t *testing.T) {
	repoRoot := t.TempDir()
	now := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	server := &Server{
		sandboxPaths: sandbox.PathsForRepo(repoRoot),
		hostAccessRuntime: hostAccessRuntimeState{
			plans:         make(map[string]*hostAccessStoredPlan),
			appliedPlanAt: make(map[string]time.Time),
		},
		now:              func() time.Time { return now },
		appendAuditEvent: func(string, ledger.Event) error { return nil },
		auditPath:        filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"),
	}

	tok := capabilityToken{ControlSessionID: "cs-1", ActorLabel: "operator", ClientSessionLabel: "cli-1"}
	req := CapabilityRequest{
		RequestID:  "r1",
		Capability: "host.plan.apply",
		Arguments:  map[string]string{"plan_id": "nonexistentplanid0000000000000000"},
	}

	resp := server.executeHostPlanApplyCapability(tok, req)
	if resp.Status != ResponseStatusError {
		t.Fatalf("expected error status, got %#v", resp)
	}
	if want := "no stored plan matches"; !strings.Contains(resp.DenialReason, want) {
		t.Fatalf("expected denial to contain %q, got %q", want, resp.DenialReason)
	}
	if strings.Contains(resp.DenialReason, "already used") {
		t.Fatalf("unexpected already-applied wording: %q", resp.DenialReason)
	}
}

func TestExecuteHostPlanApply_DuplicateApplyAfterSuccessHintsAlreadyUsed(t *testing.T) {
	repoRoot := t.TempDir()
	now := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	planID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	server := &Server{
		sandboxPaths: sandbox.PathsForRepo(repoRoot),
		hostAccessRuntime: hostAccessRuntimeState{
			plans:         make(map[string]*hostAccessStoredPlan),
			appliedPlanAt: map[string]time.Time{planID: now},
		},
		now:              func() time.Time { return now },
		appendAuditEvent: func(string, ledger.Event) error { return nil },
		auditPath:        filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"),
	}

	tok := capabilityToken{ControlSessionID: "cs-1", ActorLabel: "operator", ClientSessionLabel: "cli-1"}
	req := CapabilityRequest{
		RequestID:  "r2",
		Capability: "host.plan.apply",
		Arguments:  map[string]string{"plan_id": planID},
	}

	resp := server.executeHostPlanApplyCapability(tok, req)
	if resp.Status != ResponseStatusError {
		t.Fatalf("expected error status, got %#v", resp)
	}
	if want := "already used"; !strings.Contains(resp.DenialReason, want) {
		t.Fatalf("expected denial to contain %q, got %q", want, resp.DenialReason)
	}
	if want := "host.organize.plan"; !strings.Contains(resp.DenialReason, want) {
		t.Fatalf("expected denial to mention %q, got %q", want, resp.DenialReason)
	}
}

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
		CapabilityRequest{
			RequestID:  "req-downloads-allow",
			Capability: "host.plan.apply",
			Arguments:  map[string]string{"plan_id": planID},
		},
		true,
	)

	if resp.ApprovalRequired {
		t.Fatalf("expected low-risk host plan apply to bypass approval, got %#v", resp)
	}
	if resp.Status != ResponseStatusSuccess {
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
		CapabilityRequest{
			RequestID:  "req-downloads-approval",
			Capability: "host.plan.apply",
			Arguments:  map[string]string{"plan_id": planID},
		},
		true,
	)

	if !resp.ApprovalRequired {
		t.Fatalf("expected overwrite-risk host plan apply to require approval, got %#v", resp)
	}
	if resp.Status != ResponseStatusPendingApproval {
		t.Fatalf("expected pending approval, got %#v", resp)
	}
}

func newHostPlanApplyPolicyTestServer(t *testing.T, repoRoot string, homeDir string) *Server {
	t.Helper()

	now := time.Date(2026, 4, 12, 12, 0, 0, 0, time.UTC)
	var policy config.Policy
	policy.Tools.Filesystem.ReadEnabled = true
	policy.Tools.Filesystem.WriteEnabled = true
	policy.Tools.Filesystem.WriteRequiresApproval = true
	policy.Tools.Filesystem.AllowedRoots = []string{"."}

	server := &Server{
		sandboxPaths: sandbox.PathsForRepo(repoRoot),
		hostAccessRuntime: hostAccessRuntimeState{
			plans:         make(map[string]*hostAccessStoredPlan),
			appliedPlanAt: make(map[string]time.Time),
		},
		sessionState: sessionControlState{
			sessions:  make(map[string]controlSession),
			tokens:    make(map[string]capabilityToken),
			openByUID: make(map[uint32]time.Time),
		},
		approvalState: approvalControlState{
			records:    make(map[string]pendingApproval),
			tokenIndex: make(map[string]string),
		},
		replayState: replayControlState{
			seenRequests:   make(map[string]seenRequest),
			seenAuthNonces: make(map[string]seenRequest),
			usedTokens:     make(map[string]usedToken),
		},
		now:              func() time.Time { return now },
		appendAuditEvent: func(string, ledger.Event) error { return nil },
		auditPath:        filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"),
		configStateDir:   filepath.Join(repoRoot, "runtime", "state"),
		resolveUserHomeDir: func() (string, error) {
			return homeDir, nil
		},
		checker: policypkg.NewChecker(policy),
		providerRuntime: providerRuntimeState{
			configuredCapabilities: map[string]configuredCapability{},
		},
		maxTotalApprovalRecords:              128,
		maxPendingApprovalsPerControlSession: 64,
		maxSeenRequestReplayEntries:          defaultMaxSeenRequestReplayEntries,
		maxAuthNonceReplayEntries:            defaultMaxAuthNonceReplayEntries,
	}
	registry, err := toolspkg.NewDefaultRegistry(repoRoot, policy)
	if err != nil {
		t.Fatalf("new default registry: %v", err)
	}
	server.registry = registry
	return server
}

func seedGrantedDownloadsFolderAccess(t *testing.T, server *Server) {
	t.Helper()

	if err := os.MkdirAll(server.configStateDir, 0o700); err != nil {
		t.Fatalf("mkdir config state dir: %v", err)
	}
	if err := config.SaveJSONConfig(server.configStateDir, folderAccessConfigSection, folderAccessConfigFile{
		Version:    folderAccessConfigVersion,
		GrantedIDs: []string{folderAccessSharedID, folderAccessDownloadsID},
	}); err != nil {
		t.Fatalf("save granted downloads folder access: %v", err)
	}
}

func contextBackgroundWithOperatorMount(controlSessionID string) context.Context {
	return withOperatorMountControlSession(context.Background(), controlSessionID)
}
