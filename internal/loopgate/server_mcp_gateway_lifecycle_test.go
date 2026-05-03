package loopgate

import (
	"context"
	"errors"
	"loopgate/internal/ledger"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
