package loopgate

import (
	"context"
	"encoding/json"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"strings"
	"testing"
)

func TestDiagnosticRouteRequiresScopedCapability(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	pinTestProcessAsExpectedClient(t, server)

	deniedClient := NewClient(client.socketPath)
	deniedClient.ConfigureSession("test-actor", "diagnostic-read-denied", []string{controlCapabilityConnectionRead})
	if _, err := deniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied diagnostic.read token: %v", err)
	}
	var deniedReport map[string]interface{}
	if err := deniedClient.FetchDiagnosticReport(context.Background(), &deniedReport); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected diagnostic.read scope denial, got %v", err)
	}
	if _, err := deniedClient.CheckAuditExportTrust(context.Background()); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected diagnostic.read scope denial for audit export trust check, got %v", err)
	}
	if _, err := deniedClient.LoadMCPGatewayInventory(context.Background()); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected diagnostic.read scope denial for MCP gateway inventory, got %v", err)
	}
	if _, err := deniedClient.LoadMCPGatewayServerStatus(context.Background()); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected diagnostic.read scope denial for MCP gateway server status, got %v", err)
	}
	if _, err := deniedClient.CheckMCPGatewayDecision(context.Background(), controlapipkg.MCPGatewayDecisionRequest{
		ServerID: "github",
		ToolName: "search_repositories",
	}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected diagnostic.read scope denial for MCP gateway decision, got %v", err)
	}
	if _, err := deniedClient.ValidateMCPGatewayInvocation(context.Background(), controlapipkg.MCPGatewayInvocationRequest{
		ServerID: "github",
		ToolName: "search_repositories",
	}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected diagnostic.read scope denial for MCP gateway invocation validation, got %v", err)
	}
	if _, err := deniedClient.RequestMCPGatewayInvocationApproval(context.Background(), controlapipkg.MCPGatewayInvocationRequest{
		ServerID: "github",
		ToolName: "search_repositories",
		Arguments: map[string]json.RawMessage{
			"query": json.RawMessage(`"loopgate"`),
		},
	}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected mcp_gateway.write scope denial for MCP gateway approval preparation, got %v", err)
	}
	if _, err := deniedClient.DecideMCPGatewayInvocationApproval(context.Background(), controlapipkg.MCPGatewayApprovalDecisionRequest{
		ApprovalRequestID: "missing",
		Approved:          true,
		DecisionNonce:     "nonce",
	}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected mcp_gateway.write scope denial for MCP gateway approval decision, got %v", err)
	}
	if _, err := deniedClient.ValidateMCPGatewayExecution(context.Background(), controlapipkg.MCPGatewayExecutionRequest{
		ApprovalRequestID:      "missing",
		ApprovalManifestSHA256: "abcd",
		ServerID:               "github",
		ToolName:               "search_repositories",
		Arguments: map[string]json.RawMessage{
			"query": json.RawMessage(`"loopgate"`),
		},
	}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected mcp_gateway.write scope denial for MCP gateway execution validation, got %v", err)
	}
	if _, err := deniedClient.EnsureMCPGatewayServerLaunched(context.Background(), controlapipkg.MCPGatewayEnsureLaunchRequest{
		ServerID: "github",
	}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected mcp_gateway.write scope denial for MCP gateway server launch, got %v", err)
	}
	if _, err := deniedClient.StopMCPGatewayServer(context.Background(), controlapipkg.MCPGatewayStopRequest{
		ServerID: "github",
	}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected mcp_gateway.write scope denial for MCP gateway server stop, got %v", err)
	}
	if _, err := deniedClient.ExecuteMCPGatewayInvocation(context.Background(), controlapipkg.MCPGatewayExecutionRequest{
		ApprovalRequestID:      "missing",
		ApprovalManifestSHA256: "abcd",
		ServerID:               "github",
		ToolName:               "search_repositories",
		Arguments: map[string]json.RawMessage{
			"query": json.RawMessage(`"loopgate"`),
		},
	}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected mcp_gateway.write scope denial for MCP gateway execution, got %v", err)
	}

	allowedClient := NewClient(client.socketPath)
	allowedClient.ConfigureSession("test-actor", "diagnostic-read-allowed", []string{controlCapabilityDiagnosticRead})
	if _, err := allowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure diagnostic.read token: %v", err)
	}
	var allowedReport map[string]interface{}
	if err := allowedClient.FetchDiagnosticReport(context.Background(), &allowedReport); err != nil {
		t.Fatalf("fetch diagnostic report with diagnostic.read: %v", err)
	}
	if _, found := allowedReport["nonce_replay"]; !found {
		t.Fatalf("expected diagnostic report to include nonce_replay projection, got %#v", allowedReport)
	}
	if _, err := allowedClient.CheckAuditExportTrust(context.Background()); err != nil {
		t.Fatalf("check audit export trust with diagnostic.read: %v", err)
	}
	if _, err := allowedClient.LoadMCPGatewayInventory(context.Background()); err != nil {
		t.Fatalf("load MCP gateway inventory with diagnostic.read: %v", err)
	}
	if _, err := allowedClient.LoadMCPGatewayServerStatus(context.Background()); err != nil {
		t.Fatalf("load MCP gateway server status with diagnostic.read: %v", err)
	}
	decisionResponse, err := allowedClient.CheckMCPGatewayDecision(context.Background(), controlapipkg.MCPGatewayDecisionRequest{
		ServerID: "github",
		ToolName: "search_repositories",
	})
	if err != nil {
		t.Fatalf("check MCP gateway decision with diagnostic.read: %v", err)
	}
	if decisionResponse.Decision != "deny" || decisionResponse.DenialCode != controlapipkg.DenialCodeMCPGatewayServerNotFound {
		t.Fatalf("expected typed MCP gateway deny on unknown server with diagnostic.read, got %#v", decisionResponse)
	}
	if _, err := allowedClient.ValidateMCPGatewayInvocation(context.Background(), controlapipkg.MCPGatewayInvocationRequest{
		ServerID: "github",
		ToolName: "search_repositories",
	}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeMalformedRequest) {
		t.Fatalf("expected malformed invocation validation under diagnostic.read, got %v", err)
	}

	mcpWriteClient := NewClient(client.socketPath)
	mcpWriteClient.ConfigureSession("test-actor", "mcp-gateway-write-allowed", []string{controlCapabilityMCPGatewayWrite})
	if _, err := mcpWriteClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure mcp_gateway.write token: %v", err)
	}
	if _, err := mcpWriteClient.RequestMCPGatewayInvocationApproval(context.Background(), controlapipkg.MCPGatewayInvocationRequest{
		ServerID: "github",
		ToolName: "search_repositories",
	}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeMalformedRequest) {
		t.Fatalf("expected malformed approval preparation request under mcp_gateway.write, got %v", err)
	}
	if _, err := mcpWriteClient.DecideMCPGatewayInvocationApproval(context.Background(), controlapipkg.MCPGatewayApprovalDecisionRequest{}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeMalformedRequest) {
		t.Fatalf("expected malformed approval decision request under mcp_gateway.write, got %v", err)
	}
	if _, err := mcpWriteClient.ValidateMCPGatewayExecution(context.Background(), controlapipkg.MCPGatewayExecutionRequest{}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeMalformedRequest) {
		t.Fatalf("expected malformed MCP gateway execution validation request under mcp_gateway.write, got %v", err)
	}
	if _, err := mcpWriteClient.EnsureMCPGatewayServerLaunched(context.Background(), controlapipkg.MCPGatewayEnsureLaunchRequest{}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeMalformedRequest) {
		t.Fatalf("expected malformed MCP gateway ensure-launched request under mcp_gateway.write, got %v", err)
	}
	if _, err := mcpWriteClient.StopMCPGatewayServer(context.Background(), controlapipkg.MCPGatewayStopRequest{}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeMalformedRequest) {
		t.Fatalf("expected malformed MCP gateway stop request under mcp_gateway.write, got %v", err)
	}
	if _, err := mcpWriteClient.ExecuteMCPGatewayInvocation(context.Background(), controlapipkg.MCPGatewayExecutionRequest{}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeMalformedRequest) {
		t.Fatalf("expected malformed MCP gateway execute request under mcp_gateway.write, got %v", err)
	}
}
