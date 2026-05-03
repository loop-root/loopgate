package loopgate

import (
	"context"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"strings"
	"testing"
	"time"
)

func TestClientLoadMCPGatewayInventory_OK(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAMLWithMCPGateway(`
  mcp_gateway:
    deny_unknown_servers: true
    servers:
      github:
        enabled: true
        requires_approval: true
        transport: stdio
        launch:
          command: npx
          args:
            - -y
            - "@modelcontextprotocol/server-github"
        working_directory: .
        allowed_environment:
          - HOME
        secret_environment:
          GITHUB_TOKEN:
            id: github_token
            backend: env
            account_name: GITHUB_TOKEN
            scope: test
        tool_policies:
          search_repositories:
            enabled: true
            requires_approval: false
          list_issues:
            enabled: false
`))

	inventoryClient := NewClient(client.socketPath)
	inventoryClient.ConfigureSession("test-actor", "mcp-gateway-inventory", []string{controlCapabilityDiagnosticRead})
	if _, err := inventoryClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure diagnostic.read token: %v", err)
	}

	inventory, err := inventoryClient.LoadMCPGatewayInventory(context.Background())
	if err != nil {
		t.Fatalf("load MCP gateway inventory: %v", err)
	}
	if !inventory.DenyUnknownServers {
		t.Fatalf("expected deny_unknown_servers true, got %#v", inventory)
	}
	if len(inventory.Servers) != 1 {
		t.Fatalf("expected one declared server, got %#v", inventory.Servers)
	}

	serverView := inventory.Servers[0]
	if serverView.ServerID != "github" || serverView.EffectiveDecision != "needs_approval" {
		t.Fatalf("unexpected server view: %#v", serverView)
	}
	if len(serverView.SecretEnvironmentVariables) != 1 || serverView.SecretEnvironmentVariables[0] != "GITHUB_TOKEN" {
		t.Fatalf("unexpected secret environment projection: %#v", serverView)
	}
	if len(serverView.Tools) != 2 {
		t.Fatalf("expected two declared tools, got %#v", serverView.Tools)
	}
	if serverView.Tools[0].ToolName != "list_issues" || serverView.Tools[0].EffectiveDecision != "deny" {
		t.Fatalf("unexpected sorted disabled tool projection: %#v", serverView.Tools)
	}
	if serverView.Tools[1].ToolName != "search_repositories" || serverView.Tools[1].EffectiveDecision != "needs_approval" {
		t.Fatalf("unexpected approval-inherited tool projection: %#v", serverView.Tools)
	}
}

func TestClientLoadMCPGatewayServerStatus_OK(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAMLWithMCPGateway(`
  mcp_gateway:
    deny_unknown_servers: true
    servers:
      cat_stdio:
        enabled: true
        transport: stdio
        launch:
          command: /bin/cat
      helper_stdio:
        enabled: true
        transport: stdio
        launch:
          command: /bin/cat
`))

	mcpWriteClient := NewClient(client.socketPath)
	mcpWriteClient.ConfigureSession("test-actor", "mcp-gateway-status-launch", []string{controlCapabilityMCPGatewayWrite})
	if _, err := mcpWriteClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure mcp_gateway.write token: %v", err)
	}
	launchResponse, err := mcpWriteClient.EnsureMCPGatewayServerLaunched(context.Background(), controlapipkg.MCPGatewayEnsureLaunchRequest{ServerID: "cat_stdio"})
	if err != nil {
		t.Fatalf("ensure MCP gateway server launch: %v", err)
	}
	t.Cleanup(func() {
		server.mu.Lock()
		launchedServer := server.mcpGatewayLaunchedServers["cat_stdio"]
		delete(server.mcpGatewayLaunchedServers, "cat_stdio")
		server.mu.Unlock()
		closeMCPGatewayLaunchedServerPipes(launchedServer)
		killMCPGatewayProcessByPID(launchResponse.PID)
	})

	statusClient := NewClient(client.socketPath)
	statusClient.ConfigureSession("test-actor", "mcp-gateway-status-read", []string{controlCapabilityDiagnosticRead})
	if _, err := statusClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure diagnostic.read token: %v", err)
	}

	statusResponse, err := statusClient.LoadMCPGatewayServerStatus(context.Background())
	if err != nil {
		t.Fatalf("load MCP gateway server status: %v", err)
	}
	if len(statusResponse.Servers) != 2 {
		t.Fatalf("expected two declared MCP server status rows, got %#v", statusResponse.Servers)
	}

	if statusResponse.Servers[0].ServerID != "cat_stdio" || statusResponse.Servers[0].RuntimeState != mcpGatewayServerStateLaunched || statusResponse.Servers[0].PID != launchResponse.PID || statusResponse.Servers[0].CommandPath != "/bin/cat" {
		t.Fatalf("unexpected launched status row: %#v", statusResponse.Servers[0])
	}
	if statusResponse.Servers[1].ServerID != "helper_stdio" || statusResponse.Servers[1].RuntimeState != "absent" || statusResponse.Servers[1].PID != 0 {
		t.Fatalf("unexpected absent status row: %#v", statusResponse.Servers[1])
	}
}

func TestClientLoadMCPGatewayServerStatus_CleansUpDeadProcess(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAMLWithMCPGateway(`
  mcp_gateway:
    deny_unknown_servers: true
    servers:
      cat_stdio:
        enabled: true
        transport: stdio
        launch:
          command: /bin/cat
`))

	mcpWriteClient := NewClient(client.socketPath)
	mcpWriteClient.ConfigureSession("test-actor", "mcp-gateway-status-dead-launch", []string{controlCapabilityMCPGatewayWrite})
	if _, err := mcpWriteClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure mcp_gateway.write token: %v", err)
	}
	launchResponse, err := mcpWriteClient.EnsureMCPGatewayServerLaunched(context.Background(), controlapipkg.MCPGatewayEnsureLaunchRequest{ServerID: "cat_stdio"})
	if err != nil {
		t.Fatalf("ensure MCP gateway server launch: %v", err)
	}
	t.Cleanup(func() {
		server.mu.Lock()
		launchedServer := server.mcpGatewayLaunchedServers["cat_stdio"]
		delete(server.mcpGatewayLaunchedServers, "cat_stdio")
		server.mu.Unlock()
		closeMCPGatewayLaunchedServerPipes(launchedServer)
		killMCPGatewayProcessByPID(launchResponse.PID)
	})
	originalProcessExists := server.processExists
	server.processExists = func(pid int) (bool, error) {
		if pid == launchResponse.PID {
			return false, nil
		}
		return originalProcessExists(pid)
	}

	statusClient := NewClient(client.socketPath)
	statusClient.ConfigureSession("test-actor", "mcp-gateway-status-dead-read", []string{controlCapabilityDiagnosticRead})
	if _, err := statusClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure diagnostic.read token: %v", err)
	}

	var statusResponse controlapipkg.MCPGatewayServerStatusResponse
	deadProcessPruned := false
	for attempt := 0; attempt < 10; attempt++ {
		statusResponse, err = statusClient.LoadMCPGatewayServerStatus(context.Background())
		if err != nil {
			t.Fatalf("load MCP gateway server status after dead process: %v", err)
		}
		if len(statusResponse.Servers) == 1 && statusResponse.Servers[0].RuntimeState == "absent" {
			deadProcessPruned = true
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !deadProcessPruned {
		t.Fatalf("expected dead process cleanup to project absent runtime state, got %#v", statusResponse.Servers)
	}

	server.mu.Lock()
	_, found := server.mcpGatewayLaunchedServers["cat_stdio"]
	server.mu.Unlock()
	if found {
		t.Fatal("expected dead MCP gateway server to be pruned from authoritative launched state")
	}
}

func TestClientCheckMCPGatewayDecision_ReturnsTypedDecision(t *testing.T) {
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
`))

	decisionClient := NewClient(client.socketPath)
	decisionClient.ConfigureSession("test-actor", "mcp-gateway-decision", []string{controlCapabilityDiagnosticRead})
	if _, err := decisionClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure diagnostic.read token: %v", err)
	}

	allowedDecision, err := decisionClient.CheckMCPGatewayDecision(context.Background(), controlapipkg.MCPGatewayDecisionRequest{
		ServerID: "github",
		ToolName: "search_repositories",
	})
	if err != nil {
		t.Fatalf("check MCP gateway decision: %v", err)
	}
	if allowedDecision.Decision != "needs_approval" || !allowedDecision.RequiresApproval {
		t.Fatalf("unexpected approval decision: %#v", allowedDecision)
	}

	deniedDecision, err := decisionClient.CheckMCPGatewayDecision(context.Background(), controlapipkg.MCPGatewayDecisionRequest{
		ServerID: "github",
		ToolName: "unknown_tool",
	})
	if err != nil {
		t.Fatalf("check denied MCP gateway decision: %v", err)
	}
	if deniedDecision.Decision != "deny" || deniedDecision.DenialCode != controlapipkg.DenialCodeMCPGatewayToolNotFound {
		t.Fatalf("unexpected denied decision: %#v", deniedDecision)
	}
}

func TestMCPGatewayDecisionRoute_RejectsMalformedRequest(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	decisionClient := NewClient(client.socketPath)
	decisionClient.ConfigureSession("test-actor", "mcp-gateway-decision-malformed", []string{controlCapabilityDiagnosticRead})
	if _, err := decisionClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure diagnostic.read token: %v", err)
	}

	if _, err := decisionClient.CheckMCPGatewayDecision(context.Background(), controlapipkg.MCPGatewayDecisionRequest{}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeMalformedRequest) {
		t.Fatalf("expected malformed request denial, got %v", err)
	}
}
