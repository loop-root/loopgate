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
