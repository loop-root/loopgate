package loopgate

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMorphlingTenantMismatchDeniesStatus(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	now := server.now().UTC()
	server.morphlingsMu.Lock()
	server.morphlings["morphling-test"] = morphlingRecord{
		SchemaVersion:          "loopgate.morphling.v2",
		MorphlingID:            "morphling-test",
		TaskID:                 "task-test",
		RequestID:              "req_test",
		ParentControlSessionID: "control-session-1",
		TenantID:               "tenant-org-a",
		ActorLabel:             "test",
		ClientSessionLabel:     "cli",
		Class:                  "research",
		GoalText:               "goal",
		GoalHMAC:               "deadbeef",
		State:                  morphlingStateRunning,
		CreatedAtUTC:           now.Format(time.RFC3339Nano),
		LastEventAtUTC:         now.Format(time.RFC3339Nano),
	}
	server.morphlingsMu.Unlock()

	tokenWrongTenant := capabilityToken{
		ControlSessionID:   "control-session-1",
		TenantID:           "tenant-org-b",
		ActorLabel:         "test",
		ClientSessionLabel: "cli",
	}
	_, err := server.morphlingStatus(tokenWrongTenant, MorphlingStatusRequest{MorphlingID: "morphling-test"})
	if err != errMorphlingNotFound {
		t.Fatalf("expected errMorphlingNotFound for tenant mismatch, got %v", err)
	}

	tokenMatch := capabilityToken{
		ControlSessionID:   "control-session-1",
		TenantID:           "tenant-org-a",
		ActorLabel:         "test",
		ClientSessionLabel: "cli",
	}
	resp, err := server.morphlingStatus(tokenMatch, MorphlingStatusRequest{MorphlingID: "morphling-test"})
	if err != nil {
		t.Fatalf("morphlingStatus: %v", err)
	}
	if len(resp.Morphlings) != 1 || resp.Morphlings[0].MorphlingID != "morphling-test" {
		t.Fatalf("expected one morphling in response, got %#v", resp.Morphlings)
	}
}

func TestLogEventAddsTenantFromSession(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.mu.Lock()
	server.sessions["sess-tenant"] = controlSession{
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
