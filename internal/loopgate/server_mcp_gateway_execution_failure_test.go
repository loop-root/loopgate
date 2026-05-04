package loopgate

import (
	"context"
	"encoding/json"
	"fmt"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"strings"
	"testing"
	"time"
)

func TestClientExecuteMCPGatewayInvocation_FailsClosedOnNotificationFlood(t *testing.T) {
	repoRoot := t.TempDir()
	testExecutablePath, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	t.Setenv("LOOPGATE_MCP_GATEWAY_TEST_HELPER", "1")
	t.Setenv("LOOPGATE_MCP_GATEWAY_TEST_HELPER_MODE", "notification_flood")

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
          - LOOPGATE_MCP_GATEWAY_TEST_HELPER_MODE
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
	mcpClient.ConfigureSession("test-actor", "mcp-gateway-execute-notification-flood", []string{controlCapabilityMCPGatewayWrite})
	if _, err := mcpClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure mcp_gateway.write token: %v", err)
	}

	if _, err := mcpClient.EnsureMCPGatewayServerLaunched(context.Background(), controlapipkg.MCPGatewayEnsureLaunchRequest{ServerID: "helper_stdio"}); err != nil {
		t.Fatalf("ensure launched helper stdio server: %v", err)
	}

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

	if _, err := mcpClient.ExecuteMCPGatewayInvocation(context.Background(), controlapipkg.MCPGatewayExecutionRequest{
		ApprovalRequestID:      preparedApproval.ApprovalRequestID,
		ApprovalManifestSHA256: preparedApproval.ApprovalManifestSHA256,
		ServerID:               "helper_stdio",
		ToolName:               "search_repositories",
		Arguments: map[string]json.RawMessage{
			"owner": json.RawMessage(`"openai"`),
			"repo":  json.RawMessage(`"loopgate"`),
		},
	}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeExecutionFailed) {
		t.Fatalf("expected execution-failed denial on notification flood, got %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		server.mu.Lock()
		_, launchedFound := server.mcpGatewayLaunchedServers["helper_stdio"]
		storedApproval := server.mcpGatewayApprovalRequests[preparedApproval.ApprovalRequestID]
		server.mu.Unlock()
		if !launchedFound && storedApproval.State == approvalStateExecutionFailed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected notification-flooded server cleanup and failed approval, launched=%v approval=%#v", launchedFound, storedApproval)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestClientExecuteMCPGatewayInvocation_CancelsBlockedRoundTrip(t *testing.T) {
	repoRoot := t.TempDir()
	testExecutablePath, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	t.Setenv("LOOPGATE_MCP_GATEWAY_TEST_HELPER", "1")
	t.Setenv("LOOPGATE_MCP_GATEWAY_TEST_HELPER_MODE", "block_tools_call")

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
          - LOOPGATE_MCP_GATEWAY_TEST_HELPER_MODE
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
	mcpClient.ConfigureSession("test-actor", "mcp-gateway-execute-cancel", []string{controlCapabilityMCPGatewayWrite})
	if _, err := mcpClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure mcp_gateway.write token: %v", err)
	}

	if _, err := mcpClient.EnsureMCPGatewayServerLaunched(context.Background(), controlapipkg.MCPGatewayEnsureLaunchRequest{ServerID: "helper_stdio"}); err != nil {
		t.Fatalf("ensure launched helper stdio server: %v", err)
	}

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

	executionContext, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	_, err = mcpClient.ExecuteMCPGatewayInvocation(executionContext, controlapipkg.MCPGatewayExecutionRequest{
		ApprovalRequestID:      preparedApproval.ApprovalRequestID,
		ApprovalManifestSHA256: preparedApproval.ApprovalManifestSHA256,
		ServerID:               "helper_stdio",
		ToolName:               "search_repositories",
		Arguments: map[string]json.RawMessage{
			"owner": json.RawMessage(`"openai"`),
			"repo":  json.RawMessage(`"loopgate"`),
		},
	})
	if err == nil {
		t.Fatal("expected blocked MCP round-trip to fail after context cancellation")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") && !strings.Contains(err.Error(), controlapipkg.DenialCodeExecutionFailed) {
		t.Fatalf("expected cancellation-driven request failure, got %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		server.mu.Lock()
		_, launchedFound := server.mcpGatewayLaunchedServers["helper_stdio"]
		storedApproval := server.mcpGatewayApprovalRequests[preparedApproval.ApprovalRequestID]
		server.mu.Unlock()
		if !launchedFound && storedApproval.State == approvalStateExecutionFailed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected blocked round-trip cleanup after cancellation, launched=%v approval=%#v", launchedFound, storedApproval)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestClientExecuteMCPGatewayInvocation_RejectsUnlaunchedServer(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAMLWithMCPGateway(`
  mcp_gateway:
    deny_unknown_servers: true
    servers:
      helper_stdio:
        enabled: true
        transport: stdio
        launch:
          command: /bin/cat
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
	mcpClient.ConfigureSession("test-actor", "mcp-gateway-execute-unlaunched", []string{controlCapabilityMCPGatewayWrite})
	if _, err := mcpClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure mcp_gateway.write token: %v", err)
	}

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

	if _, err := mcpClient.ExecuteMCPGatewayInvocation(context.Background(), controlapipkg.MCPGatewayExecutionRequest{
		ApprovalRequestID:      preparedApproval.ApprovalRequestID,
		ApprovalManifestSHA256: preparedApproval.ApprovalManifestSHA256,
		ServerID:               "helper_stdio",
		ToolName:               "search_repositories",
		Arguments: map[string]json.RawMessage{
			"owner": json.RawMessage(`"openai"`),
			"repo":  json.RawMessage(`"loopgate"`),
		},
	}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeMCPGatewayServerNotLaunched) {
		t.Fatalf("expected unlaunched MCP server denial, got %v", err)
	}
}
