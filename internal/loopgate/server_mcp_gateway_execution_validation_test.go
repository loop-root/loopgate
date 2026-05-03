package loopgate

import (
	"context"
	"encoding/json"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientValidateMCPGatewayExecution_OK(t *testing.T) {
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

	mcpClient := NewClient(client.socketPath)
	mcpClient.ConfigureSession("test-actor", "mcp-gateway-validate-execution", []string{controlCapabilityMCPGatewayWrite})
	if _, err := mcpClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure mcp_gateway.write token: %v", err)
	}

	preparedApproval, err := mcpClient.RequestMCPGatewayInvocationApproval(context.Background(), controlapipkg.MCPGatewayInvocationRequest{
		ServerID: "github",
		ToolName: "search_repositories",
		Arguments: map[string]json.RawMessage{
			"owner": json.RawMessage(`"openai"`),
			"repo":  json.RawMessage(`"loopgate"`),
		},
	})
	if err != nil {
		t.Fatalf("prepare MCP gateway approval: %v", err)
	}
	if _, err := mcpClient.DecideMCPGatewayInvocationApproval(context.Background(), controlapipkg.MCPGatewayApprovalDecisionRequest{
		ApprovalRequestID:      preparedApproval.ApprovalRequestID,
		Approved:               true,
		DecisionNonce:          preparedApproval.ApprovalDecisionNonce,
		ApprovalManifestSHA256: preparedApproval.ApprovalManifestSHA256,
	}); err != nil {
		t.Fatalf("grant MCP gateway approval: %v", err)
	}

	executionValidation, err := mcpClient.ValidateMCPGatewayExecution(context.Background(), controlapipkg.MCPGatewayExecutionRequest{
		ApprovalRequestID:      preparedApproval.ApprovalRequestID,
		ApprovalManifestSHA256: preparedApproval.ApprovalManifestSHA256,
		ServerID:               "github",
		ToolName:               "search_repositories",
		Arguments: map[string]json.RawMessage{
			"owner": json.RawMessage(`"openai"`),
			"repo":  json.RawMessage(`"loopgate"`),
		},
	})
	if err != nil {
		t.Fatalf("validate MCP gateway execution: %v", err)
	}
	if !executionValidation.ExecutionAuthorized || executionValidation.ApprovalState != approvalStateGranted {
		t.Fatalf("unexpected execution validation response: %#v", executionValidation)
	}
	if executionValidation.ExecutionMethod != approvalExecutionMethodMCPGateway || executionValidation.ExecutionPath != approvalExecutionPathMCPGateway {
		t.Fatalf("unexpected execution contract projection: %#v", executionValidation)
	}

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	auditText := string(auditBytes)
	if !strings.Contains(auditText, "\"type\":\"mcp_gateway.execution_checked\"") {
		t.Fatalf("expected mcp_gateway.execution_checked audit event, got %s", auditText)
	}
	if !strings.Contains(auditText, "\"approval_request_id\":\""+preparedApproval.ApprovalRequestID+"\"") {
		t.Fatalf("expected approval id in execution audit, got %s", auditText)
	}
	if strings.Contains(auditText, "openai") || strings.Contains(auditText, "loopgate") {
		t.Fatalf("raw argument values leaked into execution audit: %s", auditText)
	}
}

func TestClientValidateMCPGatewayExecution_RejectsPendingApproval(t *testing.T) {
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

	mcpClient := NewClient(client.socketPath)
	mcpClient.ConfigureSession("test-actor", "mcp-gateway-validate-execution-pending", []string{controlCapabilityMCPGatewayWrite})
	if _, err := mcpClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure mcp_gateway.write token: %v", err)
	}

	preparedApproval, err := mcpClient.RequestMCPGatewayInvocationApproval(context.Background(), controlapipkg.MCPGatewayInvocationRequest{
		ServerID: "github",
		ToolName: "search_repositories",
		Arguments: map[string]json.RawMessage{
			"owner": json.RawMessage(`"openai"`),
			"repo":  json.RawMessage(`"loopgate"`),
		},
	})
	if err != nil {
		t.Fatalf("prepare MCP gateway approval: %v", err)
	}

	if _, err := mcpClient.ValidateMCPGatewayExecution(context.Background(), controlapipkg.MCPGatewayExecutionRequest{
		ApprovalRequestID:      preparedApproval.ApprovalRequestID,
		ApprovalManifestSHA256: preparedApproval.ApprovalManifestSHA256,
		ServerID:               "github",
		ToolName:               "search_repositories",
		Arguments: map[string]json.RawMessage{
			"owner": json.RawMessage(`"openai"`),
			"repo":  json.RawMessage(`"loopgate"`),
		},
	}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeApprovalStateInvalid) {
		t.Fatalf("expected approval state invalid for pending MCP approval, got %v", err)
	}
}

func TestClientValidateMCPGatewayExecution_RejectsBodyMismatch(t *testing.T) {
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
              - limit
            argument_value_kinds:
              owner: string
              repo: string
              limit: number
`))

	mcpClient := NewClient(client.socketPath)
	mcpClient.ConfigureSession("test-actor", "mcp-gateway-validate-execution-body-mismatch", []string{controlCapabilityMCPGatewayWrite})
	if _, err := mcpClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure mcp_gateway.write token: %v", err)
	}

	preparedApproval, err := mcpClient.RequestMCPGatewayInvocationApproval(context.Background(), controlapipkg.MCPGatewayInvocationRequest{
		ServerID: "github",
		ToolName: "search_repositories",
		Arguments: map[string]json.RawMessage{
			"owner": json.RawMessage(`"openai"`),
			"repo":  json.RawMessage(`"loopgate"`),
		},
	})
	if err != nil {
		t.Fatalf("prepare MCP gateway approval: %v", err)
	}
	if _, err := mcpClient.DecideMCPGatewayInvocationApproval(context.Background(), controlapipkg.MCPGatewayApprovalDecisionRequest{
		ApprovalRequestID:      preparedApproval.ApprovalRequestID,
		Approved:               true,
		DecisionNonce:          preparedApproval.ApprovalDecisionNonce,
		ApprovalManifestSHA256: preparedApproval.ApprovalManifestSHA256,
	}); err != nil {
		t.Fatalf("grant MCP gateway approval: %v", err)
	}

	if _, err := mcpClient.ValidateMCPGatewayExecution(context.Background(), controlapipkg.MCPGatewayExecutionRequest{
		ApprovalRequestID:      preparedApproval.ApprovalRequestID,
		ApprovalManifestSHA256: preparedApproval.ApprovalManifestSHA256,
		ServerID:               "github",
		ToolName:               "search_repositories",
		Arguments: map[string]json.RawMessage{
			"owner": json.RawMessage(`"openai"`),
			"repo":  json.RawMessage(`"loopgate"`),
			"limit": json.RawMessage(`10`),
		},
	}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeApprovalManifestMismatch) {
		t.Fatalf("expected approval manifest mismatch for altered execution body, got %v", err)
	}

	server.mu.Lock()
	storedApproval := server.mcpGatewayApprovalRequests[preparedApproval.ApprovalRequestID]
	server.mu.Unlock()
	if storedApproval.State != approvalStateGranted {
		t.Fatalf("expected granted MCP approval to remain unchanged after validation mismatch, got %#v", storedApproval)
	}
}
