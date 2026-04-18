package loopgate

import (
	"encoding/json"
	"errors"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"strings"
	"testing"
)

func TestServerStartup_LoadsMCPGatewayManifestTable(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAMLWithMCPGateway(`
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
            requires_approval: true
`))

	manifest, err := server.resolveMCPGatewayServerManifest("github")
	if err != nil {
		t.Fatalf("resolve MCP gateway manifest: %v", err)
	}
	if manifest.ServerID != "github" {
		t.Fatalf("unexpected manifest server id: %#v", manifest)
	}
	if manifest.Transport != "stdio" {
		t.Fatalf("unexpected transport: %#v", manifest)
	}
	if manifest.LaunchCommand != "npx" {
		t.Fatalf("unexpected launch command: %#v", manifest)
	}
	if len(manifest.LaunchArgs) != 2 || manifest.LaunchArgs[0] != "-y" || manifest.LaunchArgs[1] != "@modelcontextprotocol/server-github" {
		t.Fatalf("unexpected launch args: %#v", manifest)
	}
	if !manifest.RequiresApproval {
		t.Fatalf("expected server approval requirement, got %#v", manifest)
	}
	toolManifest, found := manifest.ToolManifests["search_repositories"]
	if !found {
		t.Fatalf("expected tool manifest in manifest, got %#v", manifest.ToolManifests)
	}
	if !toolManifest.Enabled || !toolManifest.RequiresApproval || !toolManifest.DeclaredByPolicy {
		t.Fatalf("unexpected tool manifest: %#v", toolManifest)
	}
	if len(toolManifest.RequiredArguments) != 0 || len(toolManifest.ArgumentValueKinds) != 0 {
		t.Fatalf("unexpected default argument constraints: %#v", toolManifest)
	}
}

func TestResolveMCPGatewayServerManifest_RejectsUnknownServer(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	_, err := server.resolveMCPGatewayServerManifest("unknown_server")
	if !errors.Is(err, errMCPGatewayServerNotFound) {
		t.Fatalf("expected unknown MCP gateway server error, got %v", err)
	}
}

func TestResolveMCPGatewayServerManifest_RejectsDisabledServer(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAMLWithMCPGateway(`
  mcp_gateway:
    deny_unknown_servers: true
    servers:
      github:
        enabled: false
        transport: stdio
        launch:
          command: npx
`))

	_, err := server.resolveMCPGatewayServerManifest("github")
	if !errors.Is(err, errMCPGatewayServerDisabled) {
		t.Fatalf("expected disabled MCP gateway server error, got %v", err)
	}
}

func TestResolveMCPGatewayToolManifest_RejectsUnknownTool(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAMLWithMCPGateway(`
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
`))

	_, _, err := server.resolveMCPGatewayToolManifest("github", "unknown_tool")
	if !errors.Is(err, errMCPGatewayToolNotFound) {
		t.Fatalf("expected unknown MCP gateway tool error, got %v", err)
	}
}

func TestResolveMCPGatewayToolManifest_RejectsDisabledTool(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAMLWithMCPGateway(`
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
            enabled: false
`))

	_, _, err := server.resolveMCPGatewayToolManifest("github", "search_repositories")
	if !errors.Is(err, errMCPGatewayToolDisabled) {
		t.Fatalf("expected disabled MCP gateway tool error, got %v", err)
	}
}

func TestEvaluateMCPGatewayInvocationPolicy_InheritsApprovalFromServerOrTool(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAMLWithMCPGateway(`
  mcp_gateway:
    deny_unknown_servers: true
    servers:
      github:
        enabled: true
        requires_approval: true
        transport: stdio
        launch:
          command: npx
        tool_policies:
          search_repositories:
            enabled: true
            requires_approval: false
          list_issues:
            enabled: true
            requires_approval: true
`))

	searchDecision, err := server.evaluateMCPGatewayInvocationPolicy("github", "search_repositories")
	if err != nil {
		t.Fatalf("evaluate server approval inheritance: %v", err)
	}
	if !searchDecision.RequiresApproval {
		t.Fatalf("expected server-level approval inheritance, got %#v", searchDecision)
	}
	if searchDecision.ToolManifest.ToolName != "search_repositories" {
		t.Fatalf("unexpected tool manifest in decision: %#v", searchDecision)
	}

	listDecision, err := server.evaluateMCPGatewayInvocationPolicy("github", "list_issues")
	if err != nil {
		t.Fatalf("evaluate tool approval inheritance: %v", err)
	}
	if !listDecision.RequiresApproval {
		t.Fatalf("expected tool-level approval inheritance, got %#v", listDecision)
	}
}

func TestValidateMCPGatewayToolArguments_EnforcesDeclaredArgumentPolicy(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAMLWithMCPGateway(`
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
            denied_arguments:
              - token
            argument_value_kinds:
              query: string
              limit: number
`))

	serverManifest, toolManifest, err := server.resolveMCPGatewayToolManifest("github", "search_repositories")
	if err != nil {
		t.Fatalf("resolve tool manifest: %v", err)
	}
	if serverManifest.ServerID != "github" {
		t.Fatalf("unexpected server manifest: %#v", serverManifest)
	}

	validRequest, err := controlapipkg.ValidateMCPGatewayInvocationRequest(controlapipkg.MCPGatewayInvocationRequest{
		ServerID: "github",
		ToolName: "search_repositories",
		Arguments: map[string]json.RawMessage{
			"query": json.RawMessage(`"loopgate"`),
			"limit": json.RawMessage(`10`),
		},
	})
	if err != nil {
		t.Fatalf("validate invocation request: %v", err)
	}
	if err := validateMCPGatewayToolArguments(toolManifest, validRequest); err != nil {
		t.Fatalf("expected valid tool arguments: %v", err)
	}

	missingRequiredRequest, err := controlapipkg.ValidateMCPGatewayInvocationRequest(controlapipkg.MCPGatewayInvocationRequest{
		ServerID: "github",
		ToolName: "search_repositories",
		Arguments: map[string]json.RawMessage{
			"limit": json.RawMessage(`10`),
		},
	})
	if err != nil {
		t.Fatalf("validate missing-required invocation request: %v", err)
	}
	if err := validateMCPGatewayToolArguments(toolManifest, missingRequiredRequest); err == nil || !strings.Contains(err.Error(), "required argument") {
		t.Fatalf("expected required argument denial, got %v", err)
	}

	deniedArgumentRequest, err := controlapipkg.ValidateMCPGatewayInvocationRequest(controlapipkg.MCPGatewayInvocationRequest{
		ServerID: "github",
		ToolName: "search_repositories",
		Arguments: map[string]json.RawMessage{
			"query": json.RawMessage(`"loopgate"`),
			"token": json.RawMessage(`"secret"`),
		},
	})
	if err != nil {
		t.Fatalf("validate denied-argument invocation request: %v", err)
	}
	if err := validateMCPGatewayToolArguments(toolManifest, deniedArgumentRequest); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected allowed-arguments denial, got %v", err)
	}

	wrongKindRequest, err := controlapipkg.ValidateMCPGatewayInvocationRequest(controlapipkg.MCPGatewayInvocationRequest{
		ServerID: "github",
		ToolName: "search_repositories",
		Arguments: map[string]json.RawMessage{
			"query": json.RawMessage(`"loopgate"`),
			"limit": json.RawMessage(`"ten"`),
		},
	})
	if err != nil {
		t.Fatalf("validate wrong-kind invocation request: %v", err)
	}
	if err := validateMCPGatewayToolArguments(toolManifest, wrongKindRequest); err == nil || !strings.Contains(err.Error(), "must be number") {
		t.Fatalf("expected argument kind denial, got %v", err)
	}
}

func loopgatePolicyYAMLWithMCPGateway(mcpGatewaySection string) string {
	return "version: 0.1.0\n\n" +
		"tools:\n" +
		strings.TrimRight(mcpGatewaySection, "\n") + "\n" +
		"  filesystem:\n" +
		"    allowed_roots:\n" +
		"      - \".\"\n" +
		"    denied_paths: []\n" +
		"    read_enabled: true\n" +
		"    write_enabled: true\n" +
		"    write_requires_approval: false\n" +
		"  http:\n" +
		"    enabled: false\n" +
		"    allowed_domains: []\n" +
		"    requires_approval: true\n" +
		"    timeout_seconds: 10\n" +
		"  shell:\n" +
		"    enabled: false\n" +
		"    allowed_commands: []\n" +
		"    requires_approval: true\n" +
		"logging:\n" +
		"  log_commands: true\n" +
		"  log_tool_calls: true\n" +
		"safety:\n" +
		"  allow_persona_modification: false\n" +
		"  allow_policy_modification: false\n"
}
