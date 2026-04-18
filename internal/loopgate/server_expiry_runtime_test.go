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
	server.sessionState.tokens["expired-token"] = capabilityToken{
		TokenID:      "expired-token-id",
		Token:        "expired-token",
		ExpiresAt:    nowUTC.Add(-1 * time.Minute),
		PeerIdentity: peerIdentity{UID: 1234},
	}
	server.pruneExpiredLocked()
	_, found := server.sessionState.tokens["expired-token"]
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
	server.sessionState.tokens["live-token"] = capabilityToken{
		TokenID:      "live-token-id",
		Token:        "live-token",
		ExpiresAt:    tokenExpiryUTC,
		PeerIdentity: peerIdentity{UID: 1234},
	}
	server.sessionState.sessions["live-session"] = controlSession{
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
	server.replayState.seenRequests["expired-request"] = seenRequest{
		ControlSessionID: "session-expired",
		SeenAt:           expiredSeenAtUTC,
	}
	server.replayState.seenRequests["retained-request"] = seenRequest{
		ControlSessionID: "session-retained",
		SeenAt:           retainedSeenAtUTC,
	}
	server.replayState.seenAuthNonces["expired-nonce"] = seenRequest{
		ControlSessionID: "session-expired",
		SeenAt:           expiredSeenAtUTC,
	}
	server.replayState.seenAuthNonces["retained-nonce"] = seenRequest{
		ControlSessionID: "session-retained",
		SeenAt:           retainedSeenAtUTC,
	}
	server.replayState.usedTokens["expired-token"] = usedToken{
		TokenID:          "expired-token",
		ControlSessionID: "session-expired",
		ConsumedAt:       expiredSeenAtUTC,
	}
	server.replayState.usedTokens["retained-token"] = usedToken{
		TokenID:          "retained-token",
		ControlSessionID: "session-retained",
		ConsumedAt:       retainedSeenAtUTC,
	}
	server.pruneExpiredLocked()
	_, expiredRequestFound := server.replayState.seenRequests["expired-request"]
	_, retainedRequestFound := server.replayState.seenRequests["retained-request"]
	_, expiredNonceFound := server.replayState.seenAuthNonces["expired-nonce"]
	_, retainedNonceFound := server.replayState.seenAuthNonces["retained-nonce"]
	_, expiredTokenFound := server.replayState.usedTokens["expired-token"]
	_, retainedTokenFound := server.replayState.usedTokens["retained-token"]
	server.mu.Unlock()

	if expiredRequestFound || expiredNonceFound || expiredTokenFound {
		t.Fatalf("expected expired replay state to be pruned, got requests=%v nonces=%v tokens=%v", expiredRequestFound, expiredNonceFound, expiredTokenFound)
	}
	if !retainedRequestFound || !retainedNonceFound || !retainedTokenFound {
		t.Fatalf("expected retained replay state to remain, got requests=%v nonces=%v tokens=%v", retainedRequestFound, retainedNonceFound, retainedTokenFound)
	}
}

func TestPruneExpiredLocked_PrunesRateLimitBucketsOutsideTheirWindows(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	nowUTC := server.now().UTC()
	expiredFsReadTimestamp := nowUTC.Add(-fsReadRateWindow - time.Second)
	retainedFsReadTimestamp := nowUTC.Add(-30 * time.Second)
	expiredHookTimestamp := nowUTC.Add(-hookPreValidateRateWindow - time.Second)
	retainedHookTimestamp := nowUTC.Add(-30 * time.Second)

	server.mu.Lock()
	server.expirySweepMaxInterval = 0
	server.replayState.sessionReadCounts["session-expired"] = []time.Time{expiredFsReadTimestamp}
	server.replayState.sessionReadCounts["session-retained"] = []time.Time{retainedFsReadTimestamp}
	server.replayState.hookPreValidateCounts[1001] = []time.Time{expiredHookTimestamp}
	server.replayState.hookPreValidateCounts[2002] = []time.Time{retainedHookTimestamp}
	server.replayState.hookPeerAuthFailureCounts["missing:peer-a"] = []time.Time{expiredHookTimestamp}
	server.replayState.hookPeerAuthFailureCounts["missing:peer-b"] = []time.Time{retainedHookTimestamp}
	server.pruneExpiredLocked()

	_, expiredSessionReadsFound := server.replayState.sessionReadCounts["session-expired"]
	retainedSessionReads := server.replayState.sessionReadCounts["session-retained"]
	_, expiredHookReadsFound := server.replayState.hookPreValidateCounts[1001]
	retainedHookReads := server.replayState.hookPreValidateCounts[2002]
	_, expiredPeerAuthFailuresFound := server.replayState.hookPeerAuthFailureCounts["missing:peer-a"]
	retainedPeerAuthFailures := server.replayState.hookPeerAuthFailureCounts["missing:peer-b"]
	server.mu.Unlock()

	if expiredSessionReadsFound || expiredHookReadsFound || expiredPeerAuthFailuresFound {
		t.Fatalf("expected expired rate-limit buckets to be pruned, got session=%v hook=%v peer_auth=%v", expiredSessionReadsFound, expiredHookReadsFound, expiredPeerAuthFailuresFound)
	}
	if len(retainedSessionReads) != 1 || !retainedSessionReads[0].Equal(retainedFsReadTimestamp) {
		t.Fatalf("expected retained session fs_read bucket to survive, got %#v", retainedSessionReads)
	}
	if len(retainedHookReads) != 1 || !retainedHookReads[0].Equal(retainedHookTimestamp) {
		t.Fatalf("expected retained hook bucket to survive, got %#v", retainedHookReads)
	}
	if len(retainedPeerAuthFailures) != 1 || !retainedPeerAuthFailures[0].Equal(retainedHookTimestamp) {
		t.Fatalf("expected retained hook peer-auth bucket to survive, got %#v", retainedPeerAuthFailures)
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
	server.approvalState.records["approval-expired"] = pendingApproval{
		ID:        "approval-expired",
		ExpiresAt: expiredApprovalExpiryUTC,
		State:     approvalStateGranted,
	}
	server.approvalState.records["approval-retained"] = pendingApproval{
		ID:        "approval-retained",
		ExpiresAt: retainedApprovalExpiryUTC,
		State:     approvalStateGranted,
	}
	server.pruneExpiredLocked()
	_, expiredApprovalFound := server.approvalState.records["approval-expired"]
	retainedApproval, retainedApprovalFound := server.approvalState.records["approval-retained"]
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
