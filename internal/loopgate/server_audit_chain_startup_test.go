package loopgate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"loopgate/internal/ledger"
	policypkg "loopgate/internal/policy"
)

func TestNewServerLoadsLegacyHookAuditTailWithoutAuditSequence(t *testing.T) {
	repoRoot := t.TempDir()
	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if err := server.logEvent("test.audit", "session-a", map[string]interface{}{"step": "one"}); err != nil {
		t.Fatalf("log first audit event: %v", err)
	}
	if err := server.logEvent("test.audit", "session-a", map[string]interface{}{"step": "two"}); err != nil {
		t.Fatalf("log second audit event: %v", err)
	}

	auditPath := filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl")
	if err := ledger.Append(auditPath, ledger.Event{
		TS:      time.Now().UTC().Format(time.RFC3339Nano),
		Type:    "hook.pre_validate",
		Session: "session-a",
		Data: map[string]interface{}{
			"decision":  "block",
			"tool_name": "Bash",
			"category":  "shell",
			"operation": policypkg.OpExecute,
			"reason":    "shell commands require operator approval",
			"peer_uid":  uint32(os.Getuid()),
			"peer_pid":  4242,
		},
	}); err != nil {
		t.Fatalf("append legacy hook audit tail: %v", err)
	}

	restartedServer, err := NewServer(repoRoot, filepath.Join(t.TempDir(), "loopgate-restart.sock"))
	if err != nil {
		t.Fatalf("restart server with legacy hook audit tail: %v", err)
	}
	if restartedServer.audit.sequence != 3 {
		t.Fatalf("expected audit sequence 3 after legacy hook tail load, got %d", restartedServer.audit.sequence)
	}
	if strings.TrimSpace(restartedServer.audit.lastHash) == "" {
		t.Fatal("expected last audit hash after legacy hook tail load")
	}

	if err := restartedServer.logEvent("test.audit", "session-a", map[string]interface{}{"step": "three"}); err != nil {
		t.Fatalf("append audit event after legacy hook tail restart: %v", err)
	}
}

func TestNewServerLoadsRotatedAuditChainFromManifest(t *testing.T) {
	repoRoot := t.TempDir()
	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	if err := os.MkdirAll(filepath.Join(repoRoot, "config"), 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	runtimeConfigYAML := `version: "1"
logging:
  audit_ledger:
    max_event_bytes: 8192
    rotate_at_bytes: 550
    segment_dir: "runtime/state/loopgate_event_segments"
    manifest_path: "runtime/state/loopgate_event_segments/manifest.jsonl"
    verify_closed_segments_on_startup: true`
	if err := os.WriteFile(filepath.Join(repoRoot, "config", "runtime.yaml"), []byte(runtimeConfigYAML), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, "runtime", "state"), 0o700); err != nil {
		t.Fatalf("mkdir runtime state: %v", err)
	}

	for eventIndex := 0; eventIndex < 5; eventIndex++ {
		if err := server.logEvent("test.audit", "session-a", map[string]interface{}{
			"payload": strings.Repeat(string(rune('a'+eventIndex)), 140),
		}); err != nil {
			t.Fatalf("log rotated audit event %d: %v", eventIndex, err)
		}
	}

	rotatedAuditServer, err := NewServer(repoRoot, filepath.Join(t.TempDir(), "loopgate-restart.sock"))
	if err != nil {
		t.Fatalf("restart server with rotated audit chain: %v", err)
	}
	if rotatedAuditServer.audit.sequence != 5 {
		t.Fatalf("expected rotated audit sequence 5, got %d", rotatedAuditServer.audit.sequence)
	}
	if strings.TrimSpace(rotatedAuditServer.audit.lastHash) == "" {
		t.Fatal("expected rotated audit hash after restart")
	}

	if err := rotatedAuditServer.logEvent("test.audit", "session-a", map[string]interface{}{
		"payload": strings.Repeat("z", 140),
	}); err != nil {
		t.Fatalf("append audit event after rotated restart: %v", err)
	}

	lastSequence, _, err := ledger.ReadSegmentedChainState(
		filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"),
		"audit_sequence",
		rotatedAuditServer.auditLedgerRotationSettings(),
	)
	if err != nil {
		t.Fatalf("verify rotated segmented audit chain: %v", err)
	}
	if lastSequence != 6 {
		t.Fatalf("expected rotated audit sequence 6 after resumed append, got %d", lastSequence)
	}
}

func TestNewServerFailsClosedOnTamperedAuditChain(t *testing.T) {
	repoRoot := t.TempDir()
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))

	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, "runtime", "state"), 0o700); err != nil {
		t.Fatalf("mkdir audit dir: %v", err)
	}
	if err := server.logEvent("test.audit", "session-a", map[string]interface{}{"step": "one"}); err != nil {
		t.Fatalf("log first audit event: %v", err)
	}
	if err := server.logEvent("test.audit", "session-a", map[string]interface{}{"step": "two"}); err != nil {
		t.Fatalf("log second audit event: %v", err)
	}

	auditPath := filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl")
	content, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 audit lines, got %d", len(lines))
	}

	var tamperedEvent ledger.Event
	if err := json.Unmarshal([]byte(lines[0]), &tamperedEvent); err != nil {
		t.Fatalf("decode first audit event: %v", err)
	}
	tamperedEvent.Type = "test.audit.tampered"
	tamperedLineBytes, err := json.Marshal(tamperedEvent)
	if err != nil {
		t.Fatalf("marshal tampered audit event: %v", err)
	}
	lines[0] = string(tamperedLineBytes)
	if err := os.WriteFile(auditPath, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write tampered audit log: %v", err)
	}

	_, err = NewServer(repoRoot, filepath.Join(t.TempDir(), "loopgate-restart.sock"))
	if !errors.Is(err, ledger.ErrLedgerIntegrity) {
		t.Fatalf("expected ErrLedgerIntegrity, got %v", err)
	}
}
