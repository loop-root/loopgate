package main

import (
	"bytes"
	"strings"
	"testing"

	"loopgate/internal/config"
)

func TestSummarizeConsolePolicyExplanation_ReportsClaudeToolPosture(t *testing.T) {
	policy := mustParseConsolePolicy(t, []byte(`
version: 0.1.0
tools:
  filesystem:
    read_enabled: true
    write_enabled: true
    write_requires_approval: true
    allowed_roots:
      - "."
    denied_paths:
      - "runtime/state"
  http:
    enabled: true
    allowed_domains:
      - "example.com"
    requires_approval: true
    timeout_seconds: 10
  shell:
    enabled: true
    allowed_commands:
      - "git"
    requires_approval: true
  claude_code:
    deny_unknown_tools: true
    tool_policies:
      Read:
        requires_approval: false
        allowed_roots:
          - "."
        denied_paths:
          - "runtime/state"
      Bash:
        requires_approval: true
        allowed_command_prefixes:
          - "git status"
        denied_command_prefixes:
          - "rm"
operator_overrides:
  classes:
    repo_read_search:
      max_delegation: session
    repo_bash_safe:
      max_delegation: persistent
safety:
  allow_persona_modification: false
  allow_policy_modification: false
`))

	explanation := summarizeConsolePolicyExplanation(policy)
	if !explanation.DenyUnknownTools {
		t.Fatalf("expected deny_unknown_tools=true")
	}
	if explanation.Filesystem.AllowedRootCount != 1 || explanation.Filesystem.DeniedPathCount != 1 {
		t.Fatalf("expected filesystem path counts, got %#v", explanation.Filesystem)
	}

	readTool := findConsoleToolSummary(t, explanation, "Read")
	if readTool.BaseDecision != "allow" || readTool.EffectiveDecision != "allow_if_limits_pass" {
		t.Fatalf("expected Read allow posture, got %#v", readTool)
	}
	if readTool.OperatorOverrideMaxScope != "session" {
		t.Fatalf("expected Read session max grant scope, got %#v", readTool)
	}
	if got := consoleToolLimitSummary(readTool); got != "roots=1 denied_paths=1" {
		t.Fatalf("expected Read limit summary, got %q", got)
	}

	bashTool := findConsoleToolSummary(t, explanation, "Bash")
	if bashTool.BaseDecision != "approval_required" || bashTool.EffectiveDecision != "approval_required_if_limits_pass" {
		t.Fatalf("expected Bash approval posture, got %#v", bashTool)
	}
	if bashTool.OperatorOverrideMaxScope != "permanent" {
		t.Fatalf("expected Bash permanent max grant scope, got %#v", bashTool)
	}
	if got := consoleToolLimitSummary(bashTool); got != "allowed_cmds=1 denied_cmds=1" {
		t.Fatalf("expected Bash limit summary, got %q", got)
	}
}

func TestPrintConsolePolicyExplanation_RendersCompactPolicySection(t *testing.T) {
	explanation := consolePolicyExplanation{
		DenyUnknownTools: true,
		Filesystem: consoleFilesystemPolicySummary{
			ReadEnabled:           true,
			WriteEnabled:          true,
			WriteRequiresApproval: true,
			AllowedRootCount:      1,
			DeniedPathCount:       2,
		},
		Shell: consoleShellPolicySummary{
			Enabled:             true,
			RequiresApproval:    true,
			AllowedCommandCount: 3,
		},
		HTTP: consoleHTTPPolicySummary{
			Enabled:            true,
			RequiresApproval:   false,
			AllowedDomainCount: 4,
			TimeoutSeconds:     15,
		},
		Tools: []consoleClaudeToolPolicySummary{{
			ToolName:                  "Bash",
			Configured:                true,
			BaseDecision:              "approval_required",
			EffectiveDecision:         "approval_required",
			OperatorOverrideMaxScope:  "session",
			AllowedCommandPrefixCount: 2,
		}},
	}

	var output bytes.Buffer
	printConsolePolicyExplanation(&output, explanation)
	rendered := output.String()
	for _, expected := range []string{
		"Policy",
		"claude_code.deny_unknown_tools: true",
		"filesystem: read=yes write=yes write_approval=yes allowed_roots=1 denied_paths=2",
		"shell: enabled=yes approval=yes allowed_commands=3",
		"http: enabled=yes approval=no allowed_domains=4 timeout_seconds=15",
		"Bash",
		"approval_required",
		"session",
		"allowed_cmds=2",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected rendered policy to contain %q, got %q", expected, rendered)
		}
	}
}

func TestPrintConsolePolicyExplanation_RendersLoadError(t *testing.T) {
	var output bytes.Buffer
	printConsolePolicyExplanation(&output, consolePolicyExplanation{Error: "signature invalid"})
	rendered := output.String()
	if !strings.Contains(rendered, "unavailable: signature invalid") {
		t.Fatalf("expected policy load error, got %q", rendered)
	}
}

func mustParseConsolePolicy(t *testing.T, rawPolicy []byte) config.Policy {
	t.Helper()
	policy, err := config.ParsePolicyDocument(rawPolicy)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	return policy
}

func findConsoleToolSummary(t *testing.T, explanation consolePolicyExplanation, toolName string) consoleClaudeToolPolicySummary {
	t.Helper()
	for _, tool := range explanation.Tools {
		if tool.ToolName == toolName {
			return tool
		}
	}
	t.Fatalf("missing tool summary for %s in %#v", toolName, explanation.Tools)
	return consoleClaudeToolPolicySummary{}
}
