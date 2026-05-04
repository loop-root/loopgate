package loopgate

import (
	"context"
	"encoding/json"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"loopgate/internal/ledger"
)

func TestClientExecuteCapability_RequiresApproval(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(true))

	response, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-approval",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "approved.txt",
			"content": "approved",
		},
	})
	if err != nil {
		t.Fatalf("execute pending approval: %v", err)
	}
	if !response.ApprovalRequired {
		t.Fatalf("expected approval required, got %#v", response)
	}
	if response.DenialCode != controlapipkg.DenialCodeApprovalRequired {
		t.Fatalf("expected approval-required denial code, got %#v", response)
	}
	if approvalClass, _ := response.Metadata["approval_class"].(string); approvalClass != ApprovalClassWriteSandboxPath {
		t.Fatalf("expected approval_class %q, got %#v", ApprovalClassWriteSandboxPath, response.Metadata)
	}

	resolvedResponse, err := client.DecideApproval(context.Background(), response.ApprovalRequestID, true)
	if err != nil {
		t.Fatalf("approve request: %v", err)
	}
	if resolvedResponse.Status != controlapipkg.ResponseStatusSuccess {
		t.Fatalf("unexpected resolved response: %#v", resolvedResponse)
	}

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read loopgate events: %v", err)
	}
	var foundApprovalCreated bool
	var foundApprovalGranted bool
	var foundCapabilityExecuted bool
	var grantedBeforeExecuted bool
	approvalID := response.ApprovalRequestID
	for _, line := range strings.Split(strings.TrimSpace(string(auditBytes)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var auditEvent ledger.Event
		if err := json.Unmarshal([]byte(line), &auditEvent); err != nil {
			t.Fatalf("decode audit event: %v", err)
		}
		switch auditEvent.Type {
		case "approval.created":
			if auditEvent.Data["approval_request_id"] != approvalID {
				continue
			}
			if auditEvent.Data["approval_class"] != ApprovalClassWriteSandboxPath {
				t.Fatalf("expected approval.created approval_class %q, got %#v", ApprovalClassWriteSandboxPath, auditEvent.Data)
			}
			foundApprovalCreated = true
		case "approval.granted":
			if auditEvent.Data["approval_request_id"] != approvalID {
				continue
			}
			if auditEvent.Data["approval_class"] != ApprovalClassWriteSandboxPath {
				t.Fatalf("expected approval.granted approval_class %q, got %#v", ApprovalClassWriteSandboxPath, auditEvent.Data)
			}
			foundApprovalGranted = true
			if !foundCapabilityExecuted {
				grantedBeforeExecuted = true
			}
		case "capability.executed":
			if auditEvent.Data["request_id"] != "req-approval" {
				continue
			}
			foundCapabilityExecuted = true
		}
	}
	if !foundApprovalCreated {
		t.Fatal("expected approval.created audit event for approved request")
	}
	if !foundApprovalGranted {
		t.Fatal("expected approval.granted audit event for approved request")
	}
	if !foundCapabilityExecuted {
		t.Fatal("expected capability.executed audit event after approval")
	}
	if !grantedBeforeExecuted {
		t.Fatal("expected approval.granted ledger line before capability.executed (audit must precede consumption-side execution record)")
	}
}
