package loopgate

import (
	"context"
	"encoding/json"
	"errors"
	"loopgate/internal/ledger"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientRequestMCPGatewayInvocationApproval_OK(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAMLWithMCPGateway(`
  mcp_gateway:
    deny_unknown_servers: true
    servers:
      github:
        enabled: true
        transport: stdio
        launch:
          command: npx
        tool_policies:
          search_repositories:
            enabled: true
            requires_approval: true
            required_arguments:
              - owner
              - repo
            allowed_arguments:
              - owner
              - repo
            argument_value_kinds:
              owner: string
              repo: string
`))

	approvalClient := NewClient(client.socketPath)
	approvalClient.ConfigureSession("test-actor", "mcp-gateway-request-approval", []string{controlCapabilityMCPGatewayWrite})
	if _, err := approvalClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure mcp_gateway.write token: %v", err)
	}

	response, err := approvalClient.RequestMCPGatewayInvocationApproval(context.Background(), controlapipkg.MCPGatewayInvocationRequest{
		ServerID: "github",
		ToolName: "search_repositories",
		Arguments: map[string]json.RawMessage{
			"owner": json.RawMessage(`"openai"`),
			"repo":  json.RawMessage(`"loopgate"`),
		},
	})
	if err != nil {
		t.Fatalf("request MCP gateway invocation approval: %v", err)
	}
	if response.Decision != "needs_approval" || !response.RequiresApproval || !response.ApprovalPrepared {
		t.Fatalf("unexpected approval preparation response: %#v", response)
	}
	if response.ApprovalRequestID == "" || response.ApprovalDecisionNonce == "" || response.ApprovalManifestSHA256 == "" || response.ApprovalExpiresAtUTC == "" {
		t.Fatalf("expected prepared approval metadata, got %#v", response)
	}

	server.mu.Lock()
	preparedApproval, found := server.mcpGatewayApprovalRequests[response.ApprovalRequestID]
	server.mu.Unlock()
	if !found {
		t.Fatalf("expected prepared approval %q in authoritative server state", response.ApprovalRequestID)
	}
	if preparedApproval.ServerID != "github" || preparedApproval.ToolName != "search_repositories" {
		t.Fatalf("unexpected prepared approval identity: %#v", preparedApproval)
	}

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	auditText := string(auditBytes)
	if !strings.Contains(auditText, "\"type\":\"approval.created\"") {
		t.Fatalf("expected approval.created audit event, got %s", auditText)
	}
	if !strings.Contains(auditText, "\"approval_class\":\""+ApprovalClassMCPGatewayInvoke+"\"") {
		t.Fatalf("expected MCP gateway approval class in audit, got %s", auditText)
	}
	if !strings.Contains(auditText, "\"server_id\":\"github\"") || !strings.Contains(auditText, "\"tool_name\":\"search_repositories\"") {
		t.Fatalf("expected MCP gateway identity in approval audit, got %s", auditText)
	}
	if !strings.Contains(auditText, "\"validated_argument_keys\":[\"owner\",\"repo\"]") {
		t.Fatalf("expected validated argument keys in approval audit, got %s", auditText)
	}
	if strings.Contains(auditText, "openai") || strings.Contains(auditText, "loopgate") {
		t.Fatalf("raw argument values leaked into approval audit: %s", auditText)
	}
}

func TestClientRequestMCPGatewayInvocationApproval_ReusesPendingApproval(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAMLWithMCPGateway(`
  mcp_gateway:
    deny_unknown_servers: true
    servers:
      github:
        enabled: true
        transport: stdio
        launch:
          command: npx
        tool_policies:
          search_repositories:
            enabled: true
            requires_approval: true
            required_arguments:
              - owner
              - repo
            allowed_arguments:
              - owner
              - repo
            argument_value_kinds:
              owner: string
              repo: string
`))

	approvalClient := NewClient(client.socketPath)
	approvalClient.ConfigureSession("test-actor", "mcp-gateway-request-approval-reuse", []string{controlCapabilityMCPGatewayWrite})
	if _, err := approvalClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure mcp_gateway.write token: %v", err)
	}

	invocationRequest := controlapipkg.MCPGatewayInvocationRequest{
		ServerID: "github",
		ToolName: "search_repositories",
		Arguments: map[string]json.RawMessage{
			"owner": json.RawMessage(`"openai"`),
			"repo":  json.RawMessage(`"loopgate"`),
		},
	}
	firstResponse, err := approvalClient.RequestMCPGatewayInvocationApproval(context.Background(), invocationRequest)
	if err != nil {
		t.Fatalf("request first MCP gateway invocation approval: %v", err)
	}
	secondResponse, err := approvalClient.RequestMCPGatewayInvocationApproval(context.Background(), invocationRequest)
	if err != nil {
		t.Fatalf("request second MCP gateway invocation approval: %v", err)
	}
	if firstResponse.ApprovalRequestID == "" || secondResponse.ApprovalRequestID != firstResponse.ApprovalRequestID {
		t.Fatalf("expected approval request reuse, got first=%#v second=%#v", firstResponse, secondResponse)
	}

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if approvalCreatedCount := strings.Count(string(auditBytes), "\"type\":\"approval.created\""); approvalCreatedCount != 1 {
		t.Fatalf("expected exactly one approval.created audit event for reused approval, got %d", approvalCreatedCount)
	}
}

func TestClientRequestMCPGatewayInvocationApproval_RollsBackOnAuditFailure(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAMLWithMCPGateway(`
  mcp_gateway:
    deny_unknown_servers: true
    servers:
      github:
        enabled: true
        transport: stdio
        launch:
          command: npx
        tool_policies:
          search_repositories:
            enabled: true
            requires_approval: true
            required_arguments:
              - owner
              - repo
            allowed_arguments:
              - owner
              - repo
            argument_value_kinds:
              owner: string
              repo: string
`))
	originalAppendAuditEvent := server.appendAuditEvent
	server.appendAuditEvent = func(path string, auditEvent ledger.Event) error {
		if auditEvent.Type == "approval.created" {
			return errors.New("audit unavailable")
		}
		return originalAppendAuditEvent(path, auditEvent)
	}

	approvalClient := NewClient(client.socketPath)
	approvalClient.ConfigureSession("test-actor", "mcp-gateway-request-approval-audit-fail", []string{controlCapabilityMCPGatewayWrite})
	if _, err := approvalClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure mcp_gateway.write token: %v", err)
	}

	if _, err := approvalClient.RequestMCPGatewayInvocationApproval(context.Background(), controlapipkg.MCPGatewayInvocationRequest{
		ServerID: "github",
		ToolName: "search_repositories",
		Arguments: map[string]json.RawMessage{
			"owner": json.RawMessage(`"openai"`),
			"repo":  json.RawMessage(`"loopgate"`),
		},
	}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeAuditUnavailable) {
		t.Fatalf("expected audit unavailable denial, got %v", err)
	}

	server.mu.Lock()
	pendingApprovalCount := len(server.mcpGatewayApprovalRequests)
	server.mu.Unlock()
	if pendingApprovalCount != 0 {
		t.Fatalf("expected MCP gateway approval rollback on audit failure, got %d pending approvals", pendingApprovalCount)
	}
}
