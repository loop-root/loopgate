package loopgate

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLogEventAddsTenantFromSession(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.mu.Lock()
	server.sessionState.sessions["sess-tenant"] = controlSession{
		ID:           "sess-tenant",
		TenantID:     "acme-corp",
		UserID:       "user-42",
		ExpiresAt:    server.now().UTC().Add(time.Hour),
		CreatedAt:    server.now().UTC(),
		PeerIdentity: peerIdentity{UID: 501, PID: 1000, EPID: 0},
	}
	server.mu.Unlock()

	if err := server.logEvent("test.tenant_audit", "sess-tenant", map[string]interface{}{
		"probe": "phase1",
	}); err != nil {
		t.Fatalf("logEvent: %v", err)
	}

	auditPath := filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl")
	raw, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if !bytes.Contains(raw, []byte(`acme-corp`)) || !bytes.Contains(raw, []byte(`user-42`)) {
		t.Fatalf("audit line missing tenant/user: %s", string(raw))
	}
}
