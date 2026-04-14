package config

import (
	"strings"
	"testing"

	"morph/internal/secrets"
)

func TestPolicy_MCPGatewayDenyUnknownServersDefaultsTrue(t *testing.T) {
	policy := Policy{}

	if !policy.MCPGatewayDenyUnknownServers() {
		t.Fatal("expected deny unknown MCP servers to default true")
	}
}

func TestApplyPolicyDefaults_NormalizesMCPGatewayServerPolicy(t *testing.T) {
	policy := Policy{}
	policy.Tools.MCPGateway.Servers = map[string]MCPGatewayServerPolicy{
		"github": {
			Transport:        " stdio ",
			WorkingDirectory: " ./mcp ",
			Launch: MCPGatewayLaunchPolicy{
				Command: " npx ",
				Args:    []string{" -y ", " ", " @modelcontextprotocol/server-github "},
			},
			AllowedEnvironment: []string{" HOME ", "HOME"},
			SecretEnvironment: map[string]secrets.SecretRef{
				"GITHUB_TOKEN": {
					ID:          "github_token",
					Backend:     "env",
					AccountName: "GITHUB_TOKEN",
					Scope:       "test",
				},
			},
			ToolPolicies: map[string]MCPGatewayToolPolicy{
				"search_repositories": {
					RequiredArguments: []string{" query ", "query"},
					AllowedArguments:  []string{" query ", " owner "},
					DeniedArguments:   []string{" token "},
					ArgumentValueKinds: map[string]string{
						"query": " string ",
						"owner": "string",
					},
				},
			},
		},
	}

	if err := applyPolicyDefaults(&policy); err != nil {
		t.Fatalf("apply defaults: %v", err)
	}

	serverPolicy, found := policy.MCPGatewayServerPolicy("github")
	if !found {
		t.Fatal("expected normalized github MCP server policy")
	}
	if serverPolicy.Transport != "stdio" {
		t.Fatalf("expected normalized stdio transport, got %q", serverPolicy.Transport)
	}
	if serverPolicy.Launch.Command != "npx" {
		t.Fatalf("expected normalized launch command, got %q", serverPolicy.Launch.Command)
	}
	if len(serverPolicy.Launch.Args) != 2 || serverPolicy.Launch.Args[0] != "-y" || serverPolicy.Launch.Args[1] != "@modelcontextprotocol/server-github" {
		t.Fatalf("expected normalized launch args, got %#v", serverPolicy.Launch.Args)
	}
	if serverPolicy.WorkingDirectory != "./mcp" {
		t.Fatalf("expected normalized working directory, got %q", serverPolicy.WorkingDirectory)
	}
	if len(serverPolicy.AllowedEnvironment) != 1 || serverPolicy.AllowedEnvironment[0] != "HOME" {
		t.Fatalf("expected normalized allowed environment, got %#v", serverPolicy.AllowedEnvironment)
	}
	toolPolicy := serverPolicy.ToolPolicies["search_repositories"]
	if len(toolPolicy.RequiredArguments) != 1 || toolPolicy.RequiredArguments[0] != "query" {
		t.Fatalf("expected normalized required arguments, got %#v", toolPolicy.RequiredArguments)
	}
	if len(toolPolicy.AllowedArguments) != 2 || toolPolicy.AllowedArguments[0] != "query" || toolPolicy.AllowedArguments[1] != "owner" {
		t.Fatalf("expected normalized allowed arguments, got %#v", toolPolicy.AllowedArguments)
	}
	if len(toolPolicy.DeniedArguments) != 1 || toolPolicy.DeniedArguments[0] != "token" {
		t.Fatalf("expected normalized denied arguments, got %#v", toolPolicy.DeniedArguments)
	}
	if len(toolPolicy.ArgumentValueKinds) != 2 || toolPolicy.ArgumentValueKinds["query"] != "string" || toolPolicy.ArgumentValueKinds["owner"] != "string" {
		t.Fatalf("expected normalized argument value kinds, got %#v", toolPolicy.ArgumentValueKinds)
	}
}

func TestApplyPolicyDefaults_RejectsUnknownMCPGatewayTransport(t *testing.T) {
	policy := Policy{}
	policy.Tools.MCPGateway.Servers = map[string]MCPGatewayServerPolicy{
		"github": {
			Transport: "sse",
			Launch: MCPGatewayLaunchPolicy{
				Command: "npx",
			},
		},
	}

	err := applyPolicyDefaults(&policy)
	if err == nil {
		t.Fatal("expected unsupported MCP gateway transport to be rejected")
	}
	if !strings.Contains(err.Error(), "transport") {
		t.Fatalf("expected transport validation error, got %v", err)
	}
}

func TestApplyPolicyDefaults_RejectsInvalidMCPGatewayEnvironmentVariableName(t *testing.T) {
	policy := Policy{}
	policy.Tools.MCPGateway.Servers = map[string]MCPGatewayServerPolicy{
		"github": {
			Launch: MCPGatewayLaunchPolicy{
				Command: "npx",
			},
			AllowedEnvironment: []string{"BAD-NAME"},
		},
	}

	err := applyPolicyDefaults(&policy)
	if err == nil {
		t.Fatal("expected invalid MCP gateway environment variable name to be rejected")
	}
	if !strings.Contains(err.Error(), "environment variable name") {
		t.Fatalf("expected environment variable validation error, got %v", err)
	}
}

func TestApplyPolicyDefaults_RejectsInvalidMCPGatewaySecretRef(t *testing.T) {
	policy := Policy{}
	policy.Tools.MCPGateway.Servers = map[string]MCPGatewayServerPolicy{
		"github": {
			Launch: MCPGatewayLaunchPolicy{
				Command: "npx",
			},
			SecretEnvironment: map[string]secrets.SecretRef{
				"GITHUB_TOKEN": {
					ID:          "",
					Backend:     "env",
					AccountName: "GITHUB_TOKEN",
					Scope:       "test",
				},
			},
		},
	}

	err := applyPolicyDefaults(&policy)
	if err == nil {
		t.Fatal("expected invalid MCP gateway secret ref to be rejected")
	}
	if !strings.Contains(err.Error(), "secret_environment") {
		t.Fatalf("expected secret_environment validation error, got %v", err)
	}
}

func TestApplyPolicyDefaults_RejectsInvalidMCPGatewayArgumentValueKind(t *testing.T) {
	policy := Policy{}
	policy.Tools.MCPGateway.Servers = map[string]MCPGatewayServerPolicy{
		"github": {
			Launch: MCPGatewayLaunchPolicy{
				Command: "npx",
			},
			ToolPolicies: map[string]MCPGatewayToolPolicy{
				"search_repositories": {
					ArgumentValueKinds: map[string]string{
						"query": "uuid",
					},
				},
			},
		},
	}

	err := applyPolicyDefaults(&policy)
	if err == nil {
		t.Fatal("expected invalid MCP gateway argument value kind to be rejected")
	}
	if !strings.Contains(err.Error(), "argument value kind") {
		t.Fatalf("expected argument value kind validation error, got %v", err)
	}
}
