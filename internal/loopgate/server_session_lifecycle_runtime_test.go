package loopgate

import (
	"context"
	"errors"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"strings"
	"testing"
	"time"

	"loopgate/internal/ledger"
)

func TestOpenSessionRejectsEmptyCapabilityScope(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	client.ConfigureSession("test-actor", "test-session", nil)

	_, err := client.ensureCapabilityToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityScopeRequired) {
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
	if _, err := client.ensureCapabilityToken(context.Background()); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeControlSessionBindingInvalid) {
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
	if _, err := client.ensureCapabilityToken(context.Background()); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeControlSessionBindingInvalid) {
		t.Fatalf("expected operator mount binding denial without expected client pin, got %v", err)
	}
}

func TestOpenSessionRejectsPinnedClientWhenExecutableResolverUnavailable(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServerWithRuntime(t, repoRoot, loopgatePolicyYAML(false), nil, false)
	server.expectedClientPath = "/Applications/Loopgate.app/Contents/MacOS/Loopgate"
	server.resolveExePath = nil

	client.ConfigureSession("safe-actor", "safe-session", []string{"fs_list"})
	if _, err := client.ensureCapabilityToken(context.Background()); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeProcessBindingRejected) {
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
	if err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeSessionOpenRateLimited) {
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
	if err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeSessionActiveLimitReached) {
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
	if len(server.sessionState.sessions) != 0 {
		server.mu.Unlock()
		t.Fatalf("expected no active sessions after close, got %d", len(server.sessionState.sessions))
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
	server.approvalState.records["approval-pending-close"] = pendingApproval{
		ID:               "approval-pending-close",
		ControlSessionID: client.controlSessionID,
		CreatedAt:        server.now().UTC(),
		ExpiresAt:        server.now().UTC().Add(time.Minute),
		State:            approvalStatePending,
	}
	server.mu.Unlock()

	err := client.CloseSession(context.Background())
	if err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeSessionCloseBlocked) {
		t.Fatalf("expected session-close-blocked denial, got %v", err)
	}

	server.mu.Lock()
	_, sessionStillPresent := server.sessionState.sessions[client.controlSessionID]
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
	if _, err := replacingClient.ensureCapabilityToken(context.Background()); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeAuditUnavailable) {
		t.Fatalf("expected session open audit failure, got %v", err)
	}

	server.mu.Lock()
	_, originalSessionStillPresent := server.sessionState.sessions[originalControlSessionID]
	_, originalTokenStillPresent := server.sessionState.tokens[originalCapabilityToken]
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
	if _, err := replacingClient.ensureCapabilityToken(context.Background()); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeSessionOpenRateLimited) {
		t.Fatalf("expected session open rate-limit denial, got %v", err)
	}

	server.mu.Lock()
	_, originalSessionStillPresent := server.sessionState.sessions[originalControlSessionID]
	_, originalTokenStillPresent := server.sessionState.tokens[originalCapabilityToken]
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
