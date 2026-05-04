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
