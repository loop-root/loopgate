package loopgate

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"loopgate/internal/ledger"
)

func TestMCPGatewayFakeStdioServerHelper(t *testing.T) {
	if os.Getenv("LOOPGATE_MCP_GATEWAY_TEST_HELPER") != "1" {
		return
	}
	helperMode := strings.TrimSpace(os.Getenv("LOOPGATE_MCP_GATEWAY_TEST_HELPER_MODE"))

	helperTransport := &mcpGatewayLaunchedServer{
		StdinWriter:          os.Stdout,
		StdoutBufferedReader: bufio.NewReader(os.Stdin),
	}

	type helperRequest struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id,omitempty"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params,omitempty"`
	}

	for {
		requestBodyBytes, err := readMCPGatewayJSONRPCFrame(helperTransport)
		if err != nil {
			if errors.Is(err, io.EOF) || strings.Contains(err.Error(), "EOF") {
				os.Exit(0)
			}
			_, _ = fmt.Fprintf(os.Stderr, "helper read frame: %v\n", err)
			os.Exit(2)
		}

		var request helperRequest
		if err := json.Unmarshal(requestBodyBytes, &request); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "helper decode request: %v\n", err)
			os.Exit(2)
		}

		switch request.Method {
		case "initialize":
			responseBytes, err := json.Marshal(map[string]interface{}{
				"jsonrpc": mcpGatewayJSONRPCVersion,
				"id":      json.RawMessage(request.ID),
				"result": map[string]interface{}{
					"protocolVersion": mcpGatewayProtocolVersion,
					"capabilities": map[string]interface{}{
						"tools": map[string]interface{}{},
					},
					"serverInfo": map[string]interface{}{
						"name":    "loopgate-test-helper",
						"version": "1.0.0",
					},
				},
			})
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "helper marshal initialize response: %v\n", err)
				os.Exit(2)
			}
			if err := writeMCPGatewayJSONRPCFrame(helperTransport, responseBytes); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "helper write initialize response: %v\n", err)
				os.Exit(2)
			}
		case "notifications/initialized":
			continue
		case "tools/call":
			switch helperMode {
			case "notification_flood":
				for notificationIndex := 0; notificationIndex <= mcpGatewayMaxNotificationFrames; notificationIndex++ {
					notificationBytes, err := json.Marshal(map[string]interface{}{
						"jsonrpc": mcpGatewayJSONRPCVersion,
						"method":  "notifications/tools/list_changed",
						"params": map[string]interface{}{
							"sequence": notificationIndex,
						},
					})
					if err != nil {
						_, _ = fmt.Fprintf(os.Stderr, "helper marshal notification flood frame: %v\n", err)
						os.Exit(2)
					}
					if err := writeMCPGatewayJSONRPCFrame(helperTransport, notificationBytes); err != nil {
						os.Exit(0)
					}
				}
				select {}
			case "block_tools_call":
				select {}
			}

			var params struct {
				Name      string                     `json:"name"`
				Arguments map[string]json.RawMessage `json:"arguments"`
			}
			if err := json.Unmarshal(request.Params, &params); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "helper decode tools/call params: %v\n", err)
				os.Exit(2)
			}
			responseBytes, err := json.Marshal(map[string]interface{}{
				"jsonrpc": mcpGatewayJSONRPCVersion,
				"id":      json.RawMessage(request.ID),
				"result": map[string]interface{}{
					"content": []map[string]interface{}{
						{
							"type": "text",
							"text": "ok",
						},
					},
					"structuredContent": map[string]interface{}{
						"echo_name":      params.Name,
						"echo_arguments": params.Arguments,
					},
				},
			})
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "helper marshal tools/call response: %v\n", err)
				os.Exit(2)
			}
			if err := writeMCPGatewayJSONRPCFrame(helperTransport, responseBytes); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "helper write tools/call response: %v\n", err)
				os.Exit(2)
			}
		default:
			responseBytes, err := json.Marshal(map[string]interface{}{
				"jsonrpc": mcpGatewayJSONRPCVersion,
				"id":      json.RawMessage(request.ID),
				"error": map[string]interface{}{
					"code":    -32601,
					"message": "method not found",
				},
			})
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "helper marshal error response: %v\n", err)
				os.Exit(2)
			}
			if err := writeMCPGatewayJSONRPCFrame(helperTransport, responseBytes); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "helper write error response: %v\n", err)
				os.Exit(2)
			}
		}
	}
}

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

func TestClientEnsureMCPGatewayServerLaunched_OK(t *testing.T) {
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

	mcpClient := NewClient(client.socketPath)
	mcpClient.ConfigureSession("test-actor", "mcp-gateway-ensure-launch", []string{controlCapabilityMCPGatewayWrite})
	if _, err := mcpClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure mcp_gateway.write token: %v", err)
	}

	launchResponse, err := mcpClient.EnsureMCPGatewayServerLaunched(context.Background(), controlapipkg.MCPGatewayEnsureLaunchRequest{ServerID: "cat_stdio"})
	if err != nil {
		t.Fatalf("ensure MCP gateway server launch: %v", err)
	}
	if launchResponse.LaunchState != mcpGatewayServerStateLaunched || launchResponse.PID <= 0 || launchResponse.Reused {
		t.Fatalf("unexpected launch response: %#v", launchResponse)
	}
	if launchResponse.CommandPath != "/bin/cat" {
		t.Fatalf("expected absolute command path projection, got %#v", launchResponse)
	}

	t.Cleanup(func() {
		server.mu.Lock()
		launchedServer := server.mcpGatewayLaunchedServers["cat_stdio"]
		delete(server.mcpGatewayLaunchedServers, "cat_stdio")
		server.mu.Unlock()
		closeMCPGatewayLaunchedServerPipes(launchedServer)
		killMCPGatewayProcessByPID(launchResponse.PID)
	})

	server.mu.Lock()
	launchedServer, found := server.mcpGatewayLaunchedServers["cat_stdio"]
	server.mu.Unlock()
	if !found {
		t.Fatal("expected launched MCP gateway server in authoritative state")
	}
	if launchedServer.PID != launchResponse.PID || launchedServer.LaunchState != mcpGatewayServerStateLaunched {
		t.Fatalf("unexpected authoritative launched server state: %#v", launchedServer)
	}

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	auditText := string(auditBytes)
	if !strings.Contains(auditText, "\"type\":\"mcp_gateway.server_launched\"") {
		t.Fatalf("expected mcp_gateway.server_launched audit event, got %s", auditText)
	}
	if !strings.Contains(auditText, "\"server_id\":\"cat_stdio\"") || !strings.Contains(auditText, "\"command_path\":\"/bin/cat\"") {
		t.Fatalf("expected launched server identity in audit, got %s", auditText)
	}
}

func TestClientEnsureMCPGatewayServerLaunched_ReusesRunningServer(t *testing.T) {
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

	mcpClient := NewClient(client.socketPath)
	mcpClient.ConfigureSession("test-actor", "mcp-gateway-ensure-launch-reuse", []string{controlCapabilityMCPGatewayWrite})
	if _, err := mcpClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure mcp_gateway.write token: %v", err)
	}

	firstLaunch, err := mcpClient.EnsureMCPGatewayServerLaunched(context.Background(), controlapipkg.MCPGatewayEnsureLaunchRequest{ServerID: "cat_stdio"})
	if err != nil {
		t.Fatalf("ensure first MCP gateway server launch: %v", err)
	}
	secondLaunch, err := mcpClient.EnsureMCPGatewayServerLaunched(context.Background(), controlapipkg.MCPGatewayEnsureLaunchRequest{ServerID: "cat_stdio"})
	if err != nil {
		t.Fatalf("ensure reused MCP gateway server launch: %v", err)
	}

	t.Cleanup(func() {
		server.mu.Lock()
		launchedServer := server.mcpGatewayLaunchedServers["cat_stdio"]
		delete(server.mcpGatewayLaunchedServers, "cat_stdio")
		server.mu.Unlock()
		closeMCPGatewayLaunchedServerPipes(launchedServer)
		killMCPGatewayProcessByPID(firstLaunch.PID)
	})

	if secondLaunch.PID != firstLaunch.PID || !secondLaunch.Reused || secondLaunch.LaunchState != mcpGatewayServerStateLaunched {
		t.Fatalf("expected reused launch response, got first=%#v second=%#v", firstLaunch, secondLaunch)
	}

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if launchedCount := strings.Count(string(auditBytes), "\"type\":\"mcp_gateway.server_launched\""); launchedCount != 1 {
		t.Fatalf("expected exactly one server_launched audit event for reused launch, got %d", launchedCount)
	}
}

func TestClientEnsureMCPGatewayServerLaunched_RollsBackOnAuditFailure(t *testing.T) {
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
	originalAppendAuditEvent := server.appendAuditEvent
	server.appendAuditEvent = func(path string, auditEvent ledger.Event) error {
		if auditEvent.Type == "mcp_gateway.server_launched" {
			return errors.New("audit unavailable")
		}
		return originalAppendAuditEvent(path, auditEvent)
	}

	mcpClient := NewClient(client.socketPath)
	mcpClient.ConfigureSession("test-actor", "mcp-gateway-ensure-launch-audit-fail", []string{controlCapabilityMCPGatewayWrite})
	if _, err := mcpClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure mcp_gateway.write token: %v", err)
	}

	if _, err := mcpClient.EnsureMCPGatewayServerLaunched(context.Background(), controlapipkg.MCPGatewayEnsureLaunchRequest{ServerID: "cat_stdio"}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeAuditUnavailable) {
		t.Fatalf("expected audit unavailable denial on MCP server launch, got %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	server.mu.Lock()
	_, found := server.mcpGatewayLaunchedServers["cat_stdio"]
	server.mu.Unlock()
	if found {
		t.Fatal("expected launched server state rollback after audit failure")
	}
}

func TestClientStopMCPGatewayServer_OK(t *testing.T) {
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

	mcpClient := NewClient(client.socketPath)
	mcpClient.ConfigureSession("test-actor", "mcp-gateway-stop", []string{controlCapabilityMCPGatewayWrite})
	if _, err := mcpClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure mcp_gateway.write token: %v", err)
	}

	launchResponse, err := mcpClient.EnsureMCPGatewayServerLaunched(context.Background(), controlapipkg.MCPGatewayEnsureLaunchRequest{ServerID: "cat_stdio"})
	if err != nil {
		t.Fatalf("ensure MCP gateway server launch: %v", err)
	}

	stopResponse, err := mcpClient.StopMCPGatewayServer(context.Background(), controlapipkg.MCPGatewayStopRequest{ServerID: "cat_stdio"})
	if err != nil {
		t.Fatalf("stop MCP gateway server: %v", err)
	}
	if !stopResponse.Stopped || stopResponse.ServerID != "cat_stdio" || stopResponse.PID != launchResponse.PID || stopResponse.PreviousLaunchState != mcpGatewayServerStateLaunched {
		t.Fatalf("unexpected stop response: %#v", stopResponse)
	}

	server.mu.Lock()
	_, found := server.mcpGatewayLaunchedServers["cat_stdio"]
	server.mu.Unlock()
	if found {
		t.Fatal("expected launched MCP gateway server to be removed after stop")
	}

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	auditText := string(auditBytes)
	if !strings.Contains(auditText, "\"type\":\"mcp_gateway.server_stopped\"") {
		t.Fatalf("expected mcp_gateway.server_stopped audit event, got %s", auditText)
	}
	if !strings.Contains(auditText, "\"server_id\":\"cat_stdio\"") || !strings.Contains(auditText, "\"previous_launch_state\":\"launched\"") {
		t.Fatalf("expected stopped server identity in audit, got %s", auditText)
	}
}

func TestClientStopMCPGatewayServer_NoOpWhenAbsent(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAMLWithMCPGateway(`
  mcp_gateway:
    deny_unknown_servers: true
    servers:
      cat_stdio:
        enabled: true
        transport: stdio
        launch:
          command: /bin/cat
`))

	mcpClient := NewClient(client.socketPath)
	mcpClient.ConfigureSession("test-actor", "mcp-gateway-stop-noop", []string{controlCapabilityMCPGatewayWrite})
	if _, err := mcpClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure mcp_gateway.write token: %v", err)
	}

	stopResponse, err := mcpClient.StopMCPGatewayServer(context.Background(), controlapipkg.MCPGatewayStopRequest{ServerID: "cat_stdio"})
	if err != nil {
		t.Fatalf("stop absent MCP gateway server: %v", err)
	}
	if stopResponse.Stopped || stopResponse.ServerID != "cat_stdio" {
		t.Fatalf("unexpected no-op stop response: %#v", stopResponse)
	}

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if strings.Contains(string(auditBytes), "\"type\":\"mcp_gateway.server_stopped\"") {
		t.Fatalf("expected no stop audit event for no-op stop, got %s", string(auditBytes))
	}
}

func TestClientStopMCPGatewayServer_ReturnsAuditUnavailableAfterStop(t *testing.T) {
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
	originalAppendAuditEvent := server.appendAuditEvent
	server.appendAuditEvent = func(path string, auditEvent ledger.Event) error {
		if auditEvent.Type == "mcp_gateway.server_stopped" {
			return errors.New("audit unavailable")
		}
		return originalAppendAuditEvent(path, auditEvent)
	}

	mcpClient := NewClient(client.socketPath)
	mcpClient.ConfigureSession("test-actor", "mcp-gateway-stop-audit-fail", []string{controlCapabilityMCPGatewayWrite})
	if _, err := mcpClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure mcp_gateway.write token: %v", err)
	}

	if _, err := mcpClient.EnsureMCPGatewayServerLaunched(context.Background(), controlapipkg.MCPGatewayEnsureLaunchRequest{ServerID: "cat_stdio"}); err != nil {
		t.Fatalf("ensure MCP gateway server launch: %v", err)
	}

	if _, err := mcpClient.StopMCPGatewayServer(context.Background(), controlapipkg.MCPGatewayStopRequest{ServerID: "cat_stdio"}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeAuditUnavailable) {
		t.Fatalf("expected audit unavailable denial on MCP server stop, got %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	server.mu.Lock()
	_, found := server.mcpGatewayLaunchedServers["cat_stdio"]
	server.mu.Unlock()
	if found {
		t.Fatal("expected stopped server state to remain absent after audit failure")
	}
}

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

func TestClientValidateMCPGatewayInvocation_OK(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAMLWithMCPGateway(`
  mcp_gateway:
    deny_unknown_servers: true
    servers:
      github:
        enabled: true
        requires_approval: false
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

	validateClient := NewClient(client.socketPath)
	validateClient.ConfigureSession("test-actor", "mcp-gateway-invocation-validate", []string{controlCapabilityDiagnosticRead})
	if _, err := validateClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure diagnostic.read token: %v", err)
	}

	response, err := validateClient.ValidateMCPGatewayInvocation(context.Background(), controlapipkg.MCPGatewayInvocationRequest{
		ServerID: "github",
		ToolName: "search_repositories",
		Arguments: map[string]json.RawMessage{
			"owner": json.RawMessage(`"openai"`),
			"repo":  json.RawMessage(`"loopgate"`),
		},
	})
	if err != nil {
		t.Fatalf("validate MCP gateway invocation: %v", err)
	}
	if response.Decision != "needs_approval" || !response.RequiresApproval {
		t.Fatalf("unexpected validation decision: %#v", response)
	}
	if response.ValidatedArgumentCount != 2 || len(response.ValidatedArgumentKeys) != 2 {
		t.Fatalf("unexpected validated argument projection: %#v", response)
	}
	if response.ValidatedArgumentKeys[0] != "owner" || response.ValidatedArgumentKeys[1] != "repo" {
		t.Fatalf("unexpected sorted validated argument keys: %#v", response.ValidatedArgumentKeys)
	}

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	auditText := string(auditBytes)
	if !strings.Contains(auditText, "\"type\":\"mcp_gateway.invocation_checked\"") {
		t.Fatalf("expected MCP gateway invocation audit event, got %s", auditText)
	}
	if !strings.Contains(auditText, "\"server_id\":\"github\"") || !strings.Contains(auditText, "\"tool_name\":\"search_repositories\"") {
		t.Fatalf("expected MCP gateway audit identity fields, got %s", auditText)
	}
	if !strings.Contains(auditText, "\"validated_argument_keys\":[\"owner\",\"repo\"]") {
		t.Fatalf("expected validated argument keys in audit, got %s", auditText)
	}
	if strings.Contains(auditText, "openai") || strings.Contains(auditText, "loopgate") {
		t.Fatalf("raw argument values leaked into audit: %s", auditText)
	}
}

func TestMCPGatewayInvocationValidateRoute_RejectsMalformedRequest(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	validateClient := NewClient(client.socketPath)
	validateClient.ConfigureSession("test-actor", "mcp-gateway-invocation-malformed", []string{controlCapabilityDiagnosticRead})
	if _, err := validateClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure diagnostic.read token: %v", err)
	}

	if _, err := validateClient.ValidateMCPGatewayInvocation(context.Background(), controlapipkg.MCPGatewayInvocationRequest{
		ServerID: "github",
		ToolName: "search_repositories",
	}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeMalformedRequest) {
		t.Fatalf("expected malformed invocation validation request denial, got %v", err)
	}
}

func TestClientValidateMCPGatewayInvocation_DeniesArgumentPolicyMismatch(t *testing.T) {
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
            required_arguments:
              - query
            allowed_arguments:
              - query
              - limit
            argument_value_kinds:
              query: string
              limit: number
`))

	validateClient := NewClient(client.socketPath)
	validateClient.ConfigureSession("test-actor", "mcp-gateway-invocation-denied", []string{controlCapabilityDiagnosticRead})
	if _, err := validateClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure diagnostic.read token: %v", err)
	}

	response, err := validateClient.ValidateMCPGatewayInvocation(context.Background(), controlapipkg.MCPGatewayInvocationRequest{
		ServerID: "github",
		ToolName: "search_repositories",
		Arguments: map[string]json.RawMessage{
			"query": json.RawMessage(`"loopgate"`),
			"limit": json.RawMessage(`"ten"`),
		},
	})
	if err != nil {
		t.Fatalf("validate MCP gateway invocation with wrong argument kind: %v", err)
	}
	if response.Decision != "deny" || response.DenialCode != controlapipkg.DenialCodeMCPGatewayArgumentsInvalid {
		t.Fatalf("expected typed argument-policy deny, got %#v", response)
	}

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	auditText := string(auditBytes)
	if !strings.Contains(auditText, "\"type\":\"mcp_gateway.invocation_checked\"") {
		t.Fatalf("expected MCP gateway invocation audit event, got %s", auditText)
	}
	if !strings.Contains(auditText, "\"decision\":\"deny\"") || !strings.Contains(auditText, "\"denial_code\":\""+controlapipkg.DenialCodeMCPGatewayArgumentsInvalid+"\"") {
		t.Fatalf("expected deny decision and denial code in audit, got %s", auditText)
	}
	if strings.Contains(auditText, "\"ten\"") {
		t.Fatalf("raw denied argument value leaked into audit: %s", auditText)
	}
}

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

func TestClientDecideMCPGatewayInvocationApproval_Approved(t *testing.T) {
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

	mcpClient := NewClient(client.socketPath)
	mcpClient.ConfigureSession("test-actor", "mcp-gateway-decide-approval", []string{controlCapabilityMCPGatewayWrite})
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

	resolutionResponse, err := mcpClient.DecideMCPGatewayInvocationApproval(context.Background(), controlapipkg.MCPGatewayApprovalDecisionRequest{
		ApprovalRequestID:      preparedApproval.ApprovalRequestID,
		Approved:               true,
		DecisionNonce:          preparedApproval.ApprovalDecisionNonce,
		ApprovalManifestSHA256: preparedApproval.ApprovalManifestSHA256,
	})
	if err != nil {
		t.Fatalf("approve MCP gateway approval: %v", err)
	}
	if !resolutionResponse.Approved || resolutionResponse.ApprovalState != approvalStateGranted {
		t.Fatalf("unexpected approval resolution response: %#v", resolutionResponse)
	}

	server.mu.Lock()
	storedApproval := server.mcpGatewayApprovalRequests[preparedApproval.ApprovalRequestID]
	server.mu.Unlock()
	if storedApproval.State != approvalStateGranted {
		t.Fatalf("expected granted MCP approval state, got %#v", storedApproval)
	}
	if storedApproval.DecisionNonce != "" || storedApproval.DecisionSubmittedAt.IsZero() {
		t.Fatalf("expected approval resolution metadata to be finalized, got %#v", storedApproval)
	}

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	auditText := string(auditBytes)
	if !strings.Contains(auditText, "\"type\":\"approval.granted\"") {
		t.Fatalf("expected approval.granted audit event, got %s", auditText)
	}
	if !strings.Contains(auditText, "\"approval_state\":\""+approvalStateGranted+"\"") {
		t.Fatalf("expected granted state in audit, got %s", auditText)
	}
	if strings.Contains(auditText, "openai") || strings.Contains(auditText, "loopgate") {
		t.Fatalf("raw argument values leaked into approval grant audit: %s", auditText)
	}
}

func TestClientDecideMCPGatewayInvocationApproval_Denied(t *testing.T) {
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

	mcpClient := NewClient(client.socketPath)
	mcpClient.ConfigureSession("test-actor", "mcp-gateway-decide-deny", []string{controlCapabilityMCPGatewayWrite})
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

	resolutionResponse, err := mcpClient.DecideMCPGatewayInvocationApproval(context.Background(), controlapipkg.MCPGatewayApprovalDecisionRequest{
		ApprovalRequestID: preparedApproval.ApprovalRequestID,
		Approved:          false,
		DecisionNonce:     preparedApproval.ApprovalDecisionNonce,
	})
	if err != nil {
		t.Fatalf("deny MCP gateway approval: %v", err)
	}
	if resolutionResponse.Approved || resolutionResponse.ApprovalState != approvalStateDenied {
		t.Fatalf("unexpected denied approval response: %#v", resolutionResponse)
	}

	server.mu.Lock()
	storedApproval := server.mcpGatewayApprovalRequests[preparedApproval.ApprovalRequestID]
	server.mu.Unlock()
	if storedApproval.State != approvalStateDenied {
		t.Fatalf("expected denied MCP approval state, got %#v", storedApproval)
	}
}

func TestClientDecideMCPGatewayInvocationApproval_RejectsManifestMismatch(t *testing.T) {
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

	mcpClient := NewClient(client.socketPath)
	mcpClient.ConfigureSession("test-actor", "mcp-gateway-decide-manifest-mismatch", []string{controlCapabilityMCPGatewayWrite})
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
		ApprovalManifestSHA256: strings.Repeat("0", len(preparedApproval.ApprovalManifestSHA256)),
	}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeApprovalManifestMismatch) {
		t.Fatalf("expected approval manifest mismatch denial, got %v", err)
	}

	server.mu.Lock()
	storedApproval := server.mcpGatewayApprovalRequests[preparedApproval.ApprovalRequestID]
	server.mu.Unlock()
	if storedApproval.State != approvalStatePending {
		t.Fatalf("expected pending MCP approval after manifest mismatch, got %#v", storedApproval)
	}
}

func TestClientDecideMCPGatewayInvocationApproval_RollsBackOnGrantAuditFailure(t *testing.T) {
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

	mcpClient := NewClient(client.socketPath)
	mcpClient.ConfigureSession("test-actor", "mcp-gateway-decide-audit-fail", []string{controlCapabilityMCPGatewayWrite})
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

	originalAppendAuditEvent := server.appendAuditEvent
	server.appendAuditEvent = func(path string, auditEvent ledger.Event) error {
		if auditEvent.Type == "approval.granted" {
			return errors.New("audit unavailable")
		}
		return originalAppendAuditEvent(path, auditEvent)
	}

	if _, err := mcpClient.DecideMCPGatewayInvocationApproval(context.Background(), controlapipkg.MCPGatewayApprovalDecisionRequest{
		ApprovalRequestID:      preparedApproval.ApprovalRequestID,
		Approved:               true,
		DecisionNonce:          preparedApproval.ApprovalDecisionNonce,
		ApprovalManifestSHA256: preparedApproval.ApprovalManifestSHA256,
	}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeAuditUnavailable) {
		t.Fatalf("expected audit unavailable denial on approval grant, got %v", err)
	}

	server.mu.Lock()
	storedApproval := server.mcpGatewayApprovalRequests[preparedApproval.ApprovalRequestID]
	server.mu.Unlock()
	if storedApproval.State != approvalStatePending || storedApproval.DecisionNonce == "" {
		t.Fatalf("expected pending MCP approval after grant audit failure, got %#v", storedApproval)
	}
}

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
