package loopgate

import (
	"encoding/json"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"loopgate/internal/ledger"
	"loopgate/internal/sandbox"
)

func TestParseHostOrganizePlanJSON_ArrayAndWrappedString(t *testing.T) {
	arrayText := `[{"kind":"mkdir","path":"a"},{"kind":"move","from":"b","to":"c"}]`
	ops, err := parseHostOrganizePlanJSON(arrayText)
	if err != nil {
		t.Fatalf("array form: %v", err)
	}
	if len(ops) != 2 || ops[0].Kind != "mkdir" || ops[0].Path != "a" {
		t.Fatalf("ops: %#v", ops)
	}

	wrapped, err := json.Marshal(arrayText)
	if err != nil {
		t.Fatal(err)
	}
	ops2, err := parseHostOrganizePlanJSON(string(wrapped))
	if err != nil {
		t.Fatalf("JSON-string-wrapped form: %v", err)
	}
	if len(ops2) != 2 {
		t.Fatalf("wrapped: %#v", ops2)
	}
}

func TestExecuteHostPlanApply_UnknownPlanIDExplainsRecovery(t *testing.T) {
	repoRoot := t.TempDir()
	now := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	server := &Server{
		sandboxPaths: sandbox.PathsForRepo(repoRoot),
		hostAccessRuntime: hostAccessRuntimeState{
			plans:         make(map[string]*hostAccessStoredPlan),
			appliedPlanAt: make(map[string]time.Time),
		},
		now:              func() time.Time { return now },
		appendAuditEvent: func(string, ledger.Event) error { return nil },
		auditPath:        filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"),
	}

	tok := capabilityToken{ControlSessionID: "cs-1", ActorLabel: "operator", ClientSessionLabel: "cli-1"}
	req := controlapipkg.CapabilityRequest{
		RequestID:  "r1",
		Capability: "host.plan.apply",
		Arguments:  map[string]string{"plan_id": "nonexistentplanid0000000000000000"},
	}

	resp := server.executeHostPlanApplyCapability(tok, req)
	if resp.Status != controlapipkg.ResponseStatusError {
		t.Fatalf("expected error status, got %#v", resp)
	}
	if want := "no stored plan matches"; !strings.Contains(resp.DenialReason, want) {
		t.Fatalf("expected denial to contain %q, got %q", want, resp.DenialReason)
	}
	if strings.Contains(resp.DenialReason, "already used") {
		t.Fatalf("unexpected already-applied wording: %q", resp.DenialReason)
	}
}

func TestExecuteHostPlanApply_DuplicateApplyAfterSuccessHintsAlreadyUsed(t *testing.T) {
	repoRoot := t.TempDir()
	now := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	planID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	server := &Server{
		sandboxPaths: sandbox.PathsForRepo(repoRoot),
		hostAccessRuntime: hostAccessRuntimeState{
			plans:         make(map[string]*hostAccessStoredPlan),
			appliedPlanAt: map[string]time.Time{planID: now},
		},
		now:              func() time.Time { return now },
		appendAuditEvent: func(string, ledger.Event) error { return nil },
		auditPath:        filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"),
	}

	tok := capabilityToken{ControlSessionID: "cs-1", ActorLabel: "operator", ClientSessionLabel: "cli-1"}
	req := controlapipkg.CapabilityRequest{
		RequestID:  "r2",
		Capability: "host.plan.apply",
		Arguments:  map[string]string{"plan_id": planID},
	}

	resp := server.executeHostPlanApplyCapability(tok, req)
	if resp.Status != controlapipkg.ResponseStatusError {
		t.Fatalf("expected error status, got %#v", resp)
	}
	if want := "already used"; !strings.Contains(resp.DenialReason, want) {
		t.Fatalf("expected denial to contain %q, got %q", want, resp.DenialReason)
	}
	if want := "host.organize.plan"; !strings.Contains(resp.DenialReason, want) {
		t.Fatalf("expected denial to mention %q, got %q", want, resp.DenialReason)
	}
}
