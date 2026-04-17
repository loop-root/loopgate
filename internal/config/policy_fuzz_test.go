package config

import (
	"strings"
	"testing"
)

func FuzzParsePolicyDocument(f *testing.F) {
	f.Add([]byte(`version: 0.1.0
tools:
  filesystem:
    allowed_roots:
      - "."
    denied_paths: []
    read_enabled: true
    write_enabled: false
    write_requires_approval: true
  http:
    enabled: false
    allowed_domains: []
    requires_approval: true
    timeout_seconds: 10
  shell:
    enabled: false
    allowed_commands: []
    requires_approval: true
logging:
  log_commands: true
  log_tool_calls: true
safety:
  allow_persona_modification: false
  allow_policy_modification: false
`))
	f.Add([]byte(`version: 0.1.0
tools:
  mcp_gateway:
    servers:
      test_stdio:
        transport: stdio
        launch:
          command: go
          args:
            - test
        allowed_environment:
          - PATH
        secret_environment:
          OPENAI_API_KEY:
            id: openai_api_key
            backend: env
            account_name: OPENAI_API_KEY
            scope: local
        tool_policies:
          inspect:
            required_arguments:
              - path
            argument_value_kinds:
              path: string
  filesystem:
    allowed_roots:
      - "."
    denied_paths: []
    read_enabled: true
    write_enabled: false
    write_requires_approval: true
  http:
    enabled: false
    allowed_domains: []
    requires_approval: true
    timeout_seconds: 10
  shell:
    enabled: false
    allowed_commands: []
    requires_approval: true
logging:
  log_commands: true
  log_tool_calls: true
  audit_detail:
    hook_projection_level: minimal
safety:
  allow_persona_modification: false
  allow_policy_modification: false
`))
	f.Add([]byte("version: 0.1.0\nunknown_section:\n  enabled: true\n"))

	f.Fuzz(func(t *testing.T, rawBytes []byte) {
		policy, err := ParsePolicyDocument(rawBytes)
		if err != nil {
			return
		}

		if strings.TrimSpace(policy.Version) == "" {
			t.Fatalf("accepted policy must set version: %#v", policy)
		}
		if policy.Tools.HTTP.TimeoutSeconds <= 0 {
			t.Fatalf("accepted policy must keep positive http timeout: %#v", policy.Tools.HTTP)
		}
		hookProjectionLevel := policy.HookAuditProjectionLevel()
		if hookProjectionLevel != "full" && hookProjectionLevel != "minimal" {
			t.Fatalf("accepted policy must normalize hook projection level, got %q", hookProjectionLevel)
		}
		if (policy.Tools.Filesystem.ReadEnabled || policy.Tools.Filesystem.WriteEnabled) && len(policy.Tools.Filesystem.AllowedRoots) == 0 {
			t.Fatalf("accepted filesystem policy must keep allowed_roots: %#v", policy.Tools.Filesystem)
		}
		if policy.Tools.Shell.Enabled && len(policy.Tools.Shell.AllowedCommands) == 0 {
			t.Fatalf("accepted shell policy must keep allowed_commands: %#v", policy.Tools.Shell)
		}
		for toolName := range policy.Tools.ClaudeCode.ToolPolicies {
			if _, supported := supportedClaudeCodeToolPolicyNames[toolName]; !supported {
				t.Fatalf("accepted unsupported Claude Code tool policy %q", toolName)
			}
		}
		for serverID, serverPolicy := range policy.Tools.MCPGateway.Servers {
			if _, supported := supportedMCPGatewayTransportNames[serverPolicy.Transport]; !supported {
				t.Fatalf("accepted unsupported MCP transport %q for %s", serverPolicy.Transport, serverID)
			}
			if strings.TrimSpace(serverPolicy.Launch.Command) == "" {
				t.Fatalf("accepted MCP server %s without launch command", serverID)
			}
			for _, environmentVariableName := range serverPolicy.AllowedEnvironment {
				if err := validateEnvironmentVariableName(environmentVariableName); err != nil {
					t.Fatalf("accepted invalid allowed_environment %q for %s: %v", environmentVariableName, serverID, err)
				}
			}
			for environmentVariableName, secretRef := range serverPolicy.SecretEnvironment {
				if err := validateEnvironmentVariableName(environmentVariableName); err != nil {
					t.Fatalf("accepted invalid secret_environment name %q for %s: %v", environmentVariableName, serverID, err)
				}
				if err := secretRef.Validate(); err != nil {
					t.Fatalf("accepted invalid secret_environment ref for %s/%s: %v", serverID, environmentVariableName, err)
				}
			}
			for toolName, toolPolicy := range serverPolicy.ToolPolicies {
				for _, argumentName := range toolPolicy.RequiredArguments {
					if !mcpGatewayArgumentNamePattern.MatchString(argumentName) {
						t.Fatalf("accepted invalid required argument %q for %s/%s", argumentName, serverID, toolName)
					}
				}
				for _, argumentName := range toolPolicy.AllowedArguments {
					if !mcpGatewayArgumentNamePattern.MatchString(argumentName) {
						t.Fatalf("accepted invalid allowed argument %q for %s/%s", argumentName, serverID, toolName)
					}
				}
				for _, argumentName := range toolPolicy.DeniedArguments {
					if !mcpGatewayArgumentNamePattern.MatchString(argumentName) {
						t.Fatalf("accepted invalid denied argument %q for %s/%s", argumentName, serverID, toolName)
					}
				}
				for argumentName, valueKind := range toolPolicy.ArgumentValueKinds {
					if !mcpGatewayArgumentNamePattern.MatchString(argumentName) {
						t.Fatalf("accepted invalid argument_value_kinds key %q for %s/%s", argumentName, serverID, toolName)
					}
					if _, supported := supportedMCPGatewayArgumentValueKinds[valueKind]; !supported {
						t.Fatalf("accepted invalid argument_value_kind %q for %s/%s", valueKind, serverID, toolName)
					}
				}
			}
		}
	})
}
