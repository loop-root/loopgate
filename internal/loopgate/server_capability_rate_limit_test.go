package loopgate

import (
	"context"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
