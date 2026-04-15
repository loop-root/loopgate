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
