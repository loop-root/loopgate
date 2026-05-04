package loopgate

import (
	"context"
	"encoding/json"
	"fmt"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientExecuteMCPGatewayInvocation_OK(t *testing.T) {
	repoRoot := t.TempDir()
	testExecutablePath, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	t.Setenv("LOOPGATE_MCP_GATEWAY_TEST_HELPER", "1")

	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAMLWithMCPGateway(fmt.Sprintf(`
  mcp_gateway:
    deny_unknown_servers: true
    servers:
      helper_stdio:
        enabled: true
        transport: stdio
        launch:
          command: %s
          args:
            - -test.run=TestMCPGatewayFakeStdioServerHelper
        allowed_environment:
          - LOOPGATE_MCP_GATEWAY_TEST_HELPER
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
`, testExecutablePath)))

	mcpClient := NewClient(client.socketPath)
	mcpClient.ConfigureSession("test-actor", "mcp-gateway-execute", []string{controlCapabilityMCPGatewayWrite})
	if _, err := mcpClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure mcp_gateway.write token: %v", err)
	}

	launchResponse, err := mcpClient.EnsureMCPGatewayServerLaunched(context.Background(), controlapipkg.MCPGatewayEnsureLaunchRequest{ServerID: "helper_stdio"})
	if err != nil {
		t.Fatalf("ensure launched helper stdio server: %v", err)
	}
	t.Cleanup(func() {
		server.mu.Lock()
		launchedServer := server.mcpGatewayLaunchedServers["helper_stdio"]
		delete(server.mcpGatewayLaunchedServers, "helper_stdio")
		server.mu.Unlock()
		closeMCPGatewayLaunchedServerPipes(launchedServer)
		killMCPGatewayProcessByPID(launchResponse.PID)
	})

	preparedApproval, err := mcpClient.RequestMCPGatewayInvocationApproval(context.Background(), controlapipkg.MCPGatewayInvocationRequest{
		ServerID: "helper_stdio",
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

	executionResponse, err := mcpClient.ExecuteMCPGatewayInvocation(context.Background(), controlapipkg.MCPGatewayExecutionRequest{
		ApprovalRequestID:      preparedApproval.ApprovalRequestID,
		ApprovalManifestSHA256: preparedApproval.ApprovalManifestSHA256,
		ServerID:               "helper_stdio",
		ToolName:               "search_repositories",
		Arguments: map[string]json.RawMessage{
			"owner": json.RawMessage(`"openai"`),
			"repo":  json.RawMessage(`"loopgate"`),
		},
	})
	if err != nil {
		t.Fatalf("execute MCP gateway invocation: %v", err)
	}
	if executionResponse.ApprovalState != approvalStateConsumed || executionResponse.ProcessPID != launchResponse.PID {
		t.Fatalf("unexpected MCP gateway execute response: %#v", executionResponse)
	}
	if !strings.Contains(string(executionResponse.ToolResult), `"echo_name":"search_repositories"`) {
		t.Fatalf("expected echoed tool result, got %s", string(executionResponse.ToolResult))
	}

	server.mu.Lock()
	storedApproval := server.mcpGatewayApprovalRequests[preparedApproval.ApprovalRequestID]
	server.mu.Unlock()
	if storedApproval.State != approvalStateConsumed || storedApproval.ExecutedAt.IsZero() {
		t.Fatalf("expected consumed MCP approval after execution, got %#v", storedApproval)
	}

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	auditText := string(auditBytes)
	if !strings.Contains(auditText, "\"type\":\"mcp_gateway.execution_started\"") || !strings.Contains(auditText, "\"type\":\"mcp_gateway.execution_completed\"") {
		t.Fatalf("expected execution start and completion audit events, got %s", auditText)
	}
	if strings.Contains(auditText, `"echo_arguments"`) || strings.Contains(auditText, `"owner":"openai"`) || strings.Contains(auditText, `"repo":"loopgate"`) {
		t.Fatalf("raw argument values leaked into execution audit: %s", auditText)
	}
}
