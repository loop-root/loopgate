package troubleshoot

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"loopgate/internal/config"
	"loopgate/internal/ledger"
)

func TestExplainHookBlock_FilteredBySessionAndToolUseID(t *testing.T) {
	repoRoot := t.TempDir()
	activeAuditPath := ActiveAuditPath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(activeAuditPath), 0o755); err != nil {
		t.Fatalf("mkdir runtime state: %v", err)
	}

	appendHookAuditEventForTest(t, activeAuditPath, "session-hook", "2026-04-19T20:00:00Z", "hook.pre_validate", 1, map[string]interface{}{
		"decision":           "allow",
		"hook_event_name":    "PreToolUse",
		"tool_use_id":        "toolu-old",
		"tool_name":          "Read",
		"hook_surface_class": "primary_authority",
		"reason":             "read allowed",
	})
	appendHookAuditEventForTest(t, activeAuditPath, "session-hook", "2026-04-19T20:00:01Z", "hook.pre_validate", 2, map[string]interface{}{
		"decision":           "block",
		"hook_event_name":    "PreToolUse",
		"tool_use_id":        "toolu-target",
		"tool_name":          "Bash",
		"hook_surface_class": "primary_authority",
		"hook_handling_mode": "policy_gate",
		"denial_code":        "policy_denied",
		"reason":             "tool not in governance map — denied by default",
	})
	appendHookAuditEventForTest(t, activeAuditPath, "session-hook", "2026-04-19T20:00:02Z", "hook.pre_validate", 3, map[string]interface{}{
		"decision":           "block",
		"hook_event_name":    "PermissionRequest",
		"tool_name":          "Bash",
		"hook_surface_class": "secondary_governance",
		"reason":             "other block",
	})

	explanation, err := ExplainHookBlock(repoRoot, config.DefaultRuntimeConfig(), HookBlockQuery{
		SessionID: "session-hook",
		ToolUseID: "toolu-target",
	})
	if err != nil {
		t.Fatalf("explain hook block: %v", err)
	}
	if explanation.HookEventName != "PreToolUse" {
		t.Fatalf("expected hook event name, got %#v", explanation)
	}
	if explanation.ToolUseID != "toolu-target" {
		t.Fatalf("expected tool use id, got %#v", explanation)
	}
	if explanation.ToolName != "Bash" {
		t.Fatalf("expected tool name, got %#v", explanation)
	}
	if explanation.DenialCode != "policy_denied" {
		t.Fatalf("expected denial code, got %#v", explanation)
	}
	if explanation.Reason != "tool not in governance map — denied by default" {
		t.Fatalf("expected reason, got %#v", explanation)
	}

	var rendered bytes.Buffer
	if err := WriteHookBlockExplanation(&rendered, explanation); err != nil {
		t.Fatalf("write hook explanation: %v", err)
	}
	if !strings.Contains(rendered.String(), "Tool use id: toolu-target") {
		t.Fatalf("expected tool use id in output, got %q", rendered.String())
	}
	if !strings.Contains(rendered.String(), "Denial code: policy_denied") {
		t.Fatalf("expected denial code in output, got %q", rendered.String())
	}
}

func TestExplainHookBlock_SessionOnlySelectsLatestBlockedEvent(t *testing.T) {
	repoRoot := t.TempDir()
	activeAuditPath := ActiveAuditPath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(activeAuditPath), 0o755); err != nil {
		t.Fatalf("mkdir runtime state: %v", err)
	}

	appendHookAuditEventForTest(t, activeAuditPath, "session-hook", "2026-04-19T20:10:00Z", "hook.pre_validate", 1, map[string]interface{}{
		"decision":           "block",
		"hook_event_name":    "ConfigChange",
		"hook_surface_class": "secondary_governance",
		"reason":             "older block",
	})
	appendHookAuditEventForTest(t, activeAuditPath, "session-hook", "2026-04-19T20:10:02Z", "hook.pre_validate", 2, map[string]interface{}{
		"decision":           "block",
		"hook_event_name":    "TaskCreated",
		"hook_surface_class": "secondary_governance",
		"reason":             "latest block",
	})

	explanation, err := ExplainHookBlock(repoRoot, config.DefaultRuntimeConfig(), HookBlockQuery{
		SessionID: "session-hook",
	})
	if err != nil {
		t.Fatalf("explain hook block by session: %v", err)
	}
	if !explanation.MatchedLatest {
		t.Fatalf("expected latest-match note, got %#v", explanation)
	}
	if explanation.HookEventName != "TaskCreated" {
		t.Fatalf("expected latest blocked hook event, got %#v", explanation)
	}
}

func TestExplainHookBlock_NotFound(t *testing.T) {
	repoRoot := t.TempDir()
	activeAuditPath := ActiveAuditPath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(activeAuditPath), 0o755); err != nil {
		t.Fatalf("mkdir runtime state: %v", err)
	}

	appendHookAuditEventForTest(t, activeAuditPath, "session-hook", "2026-04-19T20:15:00Z", "hook.pre_validate", 1, map[string]interface{}{
		"decision":           "allow",
		"hook_event_name":    "PreToolUse",
		"hook_surface_class": "primary_authority",
		"reason":             "allowed",
	})

	_, err := ExplainHookBlock(repoRoot, config.DefaultRuntimeConfig(), HookBlockQuery{
		SessionID: "session-hook",
	})
	if !errors.Is(err, ErrHookBlockNotFound) {
		t.Fatalf("expected hook-block not found, got %v", err)
	}
}

func appendHookAuditEventForTest(t *testing.T, activeAuditPath string, sessionID string, timestamp string, eventType string, auditSequence int64, data map[string]interface{}) {
	t.Helper()

	copied := map[string]interface{}{}
	for key, value := range data {
		copied[key] = value
	}
	copied["audit_sequence"] = auditSequence
	if err := ledger.Append(activeAuditPath, ledger.NewEvent(timestamp, eventType, sessionID, copied)); err != nil {
		t.Fatalf("append hook audit event: %v", err)
	}
}
