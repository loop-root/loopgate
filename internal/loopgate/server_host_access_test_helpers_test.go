package loopgate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"loopgate/internal/config"
	"loopgate/internal/ledger"
	policypkg "loopgate/internal/policy"
	"loopgate/internal/sandbox"
	toolspkg "loopgate/internal/tools"
)

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
