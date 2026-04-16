package loopgate

import (
	"testing"
	"time"
)

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

func TestPruneExpiredLocked_PrunesReplayStateAfterSessionTTL(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	nowUTC := server.now().UTC()
	expiredSeenAtUTC := nowUTC.Add(-requestReplayWindow - time.Minute)
	retainedSeenAtUTC := nowUTC.Add(-30 * time.Minute)

	server.mu.Lock()
	server.expirySweepMaxInterval = 0
	server.seenRequests["expired-request"] = seenRequest{
		ControlSessionID: "session-expired",
		SeenAt:           expiredSeenAtUTC,
	}
	server.seenRequests["retained-request"] = seenRequest{
		ControlSessionID: "session-retained",
		SeenAt:           retainedSeenAtUTC,
	}
	server.seenAuthNonces["expired-nonce"] = seenRequest{
		ControlSessionID: "session-expired",
		SeenAt:           expiredSeenAtUTC,
	}
	server.seenAuthNonces["retained-nonce"] = seenRequest{
		ControlSessionID: "session-retained",
		SeenAt:           retainedSeenAtUTC,
	}
	server.usedTokens["expired-token"] = usedToken{
		TokenID:          "expired-token",
		ControlSessionID: "session-expired",
		ConsumedAt:       expiredSeenAtUTC,
	}
	server.usedTokens["retained-token"] = usedToken{
		TokenID:          "retained-token",
		ControlSessionID: "session-retained",
		ConsumedAt:       retainedSeenAtUTC,
	}
	server.pruneExpiredLocked()
	_, expiredRequestFound := server.seenRequests["expired-request"]
	_, retainedRequestFound := server.seenRequests["retained-request"]
	_, expiredNonceFound := server.seenAuthNonces["expired-nonce"]
	_, retainedNonceFound := server.seenAuthNonces["retained-nonce"]
	_, expiredTokenFound := server.usedTokens["expired-token"]
	_, retainedTokenFound := server.usedTokens["retained-token"]
	server.mu.Unlock()

	if expiredRequestFound || expiredNonceFound || expiredTokenFound {
		t.Fatalf("expected expired replay state to be pruned, got requests=%v nonces=%v tokens=%v", expiredRequestFound, expiredNonceFound, expiredTokenFound)
	}
	if !retainedRequestFound || !retainedNonceFound || !retainedTokenFound {
		t.Fatalf("expected retained replay state to remain, got requests=%v nonces=%v tokens=%v", retainedRequestFound, retainedNonceFound, retainedTokenFound)
	}
}

func TestPruneExpiredLocked_PrunesTerminalApprovalsAfterSessionTTL(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	nowUTC := server.now().UTC()
	expiredApprovalExpiryUTC := nowUTC.Add(-approvalTTL - requestReplayWindow - time.Minute)
	retainedApprovalExpiryUTC := nowUTC.Add(-approvalTTL - 30*time.Minute)

	server.mu.Lock()
	server.expirySweepMaxInterval = 0
	server.approvals["approval-expired"] = pendingApproval{
		ID:        "approval-expired",
		ExpiresAt: expiredApprovalExpiryUTC,
		State:     approvalStateGranted,
	}
	server.approvals["approval-retained"] = pendingApproval{
		ID:        "approval-retained",
		ExpiresAt: retainedApprovalExpiryUTC,
		State:     approvalStateGranted,
	}
	server.pruneExpiredLocked()
	_, expiredApprovalFound := server.approvals["approval-expired"]
	retainedApproval, retainedApprovalFound := server.approvals["approval-retained"]
	server.mu.Unlock()

	if expiredApprovalFound {
		t.Fatal("expected terminal approval older than session TTL replay window to be pruned")
	}
	if !retainedApprovalFound {
		t.Fatal("expected more recent terminal approval to remain")
	}
	if retainedApproval.State != approvalStateGranted {
		t.Fatalf("expected retained approval to stay granted, got %#v", retainedApproval)
	}
}
