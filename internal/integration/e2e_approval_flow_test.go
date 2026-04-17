//go:build e2e

package integration_test

import (
	"context"
	"testing"

	"loopgate/internal/loopgate"
)

func TestE2EApprovalWriteAuditFlow(t *testing.T) {
	harness := newLoopgateHarness(t, integrationApprovalPolicyYAML())
	status := harness.waitForStatus(t)

	client := harness.newClient("e2e-actor", "e2e-approval-audit", capabilityNames(status.Capabilities))
	t.Cleanup(client.CloseIdleConnections)

	targetPath := "approved-e2e.txt"
	requestID := "req-e2e-approval-write"
	pendingResponse, err := client.ExecuteCapability(context.Background(), loopgate.CapabilityRequest{
		RequestID:  requestID,
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    targetPath,
			"content": "approved through governed e2e flow",
		},
	})
	if err != nil {
		t.Fatalf("execute pending approval request: %v", err)
	}
	if !pendingResponse.ApprovalRequired {
		t.Fatalf("expected approval required response, got %#v", pendingResponse)
	}
	if pendingResponse.DenialCode != loopgate.DenialCodeApprovalRequired {
		t.Fatalf("expected approval required denial code, got %#v", pendingResponse)
	}
	if pendingResponse.ApprovalRequestID == "" {
		t.Fatalf("expected approval request id, got %#v", pendingResponse)
	}
	if approvalClass, _ := pendingResponse.Metadata["approval_class"].(string); approvalClass != loopgate.ApprovalClassWriteSandboxPath {
		t.Fatalf("expected approval_class %q, got %#v", loopgate.ApprovalClassWriteSandboxPath, pendingResponse.Metadata)
	}

	approvedResponse, err := client.DecideApproval(context.Background(), pendingResponse.ApprovalRequestID, true)
	if err != nil {
		t.Fatalf("approve pending request: %v", err)
	}
	if approvedResponse.Status != loopgate.ResponseStatusSuccess {
		t.Fatalf("expected successful approval resolution, got %#v", approvedResponse)
	}

	readResponse, err := client.ExecuteCapability(context.Background(), loopgate.CapabilityRequest{
		RequestID:  "req-e2e-approval-readback",
		Capability: "fs_read",
		Arguments: map[string]string{
			"path": targetPath,
		},
	})
	if err != nil {
		t.Fatalf("read back approved file: %v", err)
	}
	if readResponse.Status != loopgate.ResponseStatusSuccess {
		t.Fatalf("expected successful readback response, got %#v", readResponse)
	}
	if readResponse.StructuredResult["content"] != "approved through governed e2e flow" {
		t.Fatalf("unexpected approved file contents: %#v", readResponse.StructuredResult)
	}

	harness.verifyAuditChain(t)
	events, _ := harness.readAuditEvents(t)
	approvalCreatedIndex := -1
	approvalGrantedIndex := -1
	capabilityExecutedIndex := -1
	for index, auditEvent := range events {
		switch auditEvent.Type {
		case "approval.created":
			if auditEvent.Data["approval_request_id"] == pendingResponse.ApprovalRequestID {
				if auditEvent.Data["approval_class"] != loopgate.ApprovalClassWriteSandboxPath {
					t.Fatalf("expected approval.created approval_class %q, got %#v", loopgate.ApprovalClassWriteSandboxPath, auditEvent.Data)
				}
				approvalCreatedIndex = index
			}
		case "approval.granted":
			if auditEvent.Data["approval_request_id"] == pendingResponse.ApprovalRequestID {
				if auditEvent.Data["approval_class"] != loopgate.ApprovalClassWriteSandboxPath {
					t.Fatalf("expected approval.granted approval_class %q, got %#v", loopgate.ApprovalClassWriteSandboxPath, auditEvent.Data)
				}
				approvalGrantedIndex = index
			}
		case "capability.executed":
			if auditEvent.Data["request_id"] == requestID {
				capabilityExecutedIndex = index
			}
		}
	}
	if approvalCreatedIndex == -1 {
		t.Fatalf("expected approval.created audit event for %q, got %#v", pendingResponse.ApprovalRequestID, events)
	}
	if approvalGrantedIndex == -1 {
		t.Fatalf("expected approval.granted audit event for %q, got %#v", pendingResponse.ApprovalRequestID, events)
	}
	if capabilityExecutedIndex == -1 {
		t.Fatalf("expected capability.executed audit event for %q, got %#v", requestID, events)
	}
	if !(approvalCreatedIndex < approvalGrantedIndex && approvalGrantedIndex < capabilityExecutedIndex) {
		t.Fatalf(
			"expected approval.created < approval.granted < capability.executed, got created=%d granted=%d executed=%d",
			approvalCreatedIndex,
			approvalGrantedIndex,
			capabilityExecutedIndex,
		)
	}
}

func integrationApprovalPolicyYAML() string {
	return `version: 0.1.0
tools:
  filesystem:
    allowed_roots:
      - "."
    denied_paths:
      - "core/policy"
    read_enabled: true
    write_enabled: true
    write_requires_approval: true
  http:
    enabled: false
    allowed_domains: []
    requires_approval: true
    timeout_seconds: 10
  shell:
    enabled: false
    allowed_commands: []
    requires_approval: true
logging:
  log_commands: true
  log_tool_calls: true
safety:
  allow_persona_modification: false
  allow_policy_modification: false
`
}
