package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"loopgate/internal/config"
)

type consolePolicyExplanation struct {
	Error            string
	DenyUnknownTools bool
	Filesystem       consoleFilesystemPolicySummary
	Shell            consoleShellPolicySummary
	HTTP             consoleHTTPPolicySummary
	Tools            []consoleClaudeToolPolicySummary
}

type consoleFilesystemPolicySummary struct {
	ReadEnabled           bool
	WriteEnabled          bool
	WriteRequiresApproval bool
	AllowedRootCount      int
	DeniedPathCount       int
}

type consoleShellPolicySummary struct {
	Enabled             bool
	RequiresApproval    bool
	AllowedCommandCount int
}

type consoleHTTPPolicySummary struct {
	Enabled            bool
	RequiresApproval   bool
	AllowedDomainCount int
	TimeoutSeconds     int
}

type consoleClaudeToolPolicySummary struct {
	ToolName                  string
	Configured                bool
	BaseDecision              string
	EffectiveDecision         string
	OperatorOverrideClass     string
	OperatorOverrideMaxScope  string
	AllowedRootCount          int
	DeniedPathCount           int
	AllowedDomainCount        int
	AllowedCommandPrefixCount int
	DeniedCommandPrefixCount  int
}

func collectConsolePolicyExplanation(repoRoot string) consolePolicyExplanation {
	loadResult, err := config.LoadPolicyWithHash(repoRoot)
	if err != nil {
		return consolePolicyExplanation{Error: err.Error()}
	}
	return summarizeConsolePolicyExplanation(loadResult.Policy)
}

func summarizeConsolePolicyExplanation(policy config.Policy) consolePolicyExplanation {
	explanation := consolePolicyExplanation{
		DenyUnknownTools: policy.ClaudeCodeDenyUnknownTools(),
		Filesystem: consoleFilesystemPolicySummary{
			ReadEnabled:           policy.Tools.Filesystem.ReadEnabled,
			WriteEnabled:          policy.Tools.Filesystem.WriteEnabled,
			WriteRequiresApproval: policy.Tools.Filesystem.WriteRequiresApproval,
			AllowedRootCount:      len(policy.Tools.Filesystem.AllowedRoots),
			DeniedPathCount:       len(policy.Tools.Filesystem.DeniedPaths),
		},
		Shell: consoleShellPolicySummary{
			Enabled:             policy.Tools.Shell.Enabled,
			RequiresApproval:    policy.Tools.Shell.RequiresApproval,
			AllowedCommandCount: len(policy.Tools.Shell.AllowedCommands),
		},
		HTTP: consoleHTTPPolicySummary{
			Enabled:            policy.Tools.HTTP.Enabled,
			RequiresApproval:   policy.Tools.HTTP.RequiresApproval,
			AllowedDomainCount: len(policy.Tools.HTTP.AllowedDomains),
			TimeoutSeconds:     policy.Tools.HTTP.TimeoutSeconds,
		},
	}

	for _, toolName := range config.SupportedClaudeCodeToolPolicyNames() {
		toolPolicy, configured := policy.ClaudeCodeToolPolicy(toolName)
		baseDecision, _ := consoleClaudeCodeToolBaseDecision(policy, toolName)
		toolSummary := consoleClaudeToolPolicySummary{
			ToolName:                  toolName,
			Configured:                configured,
			BaseDecision:              baseDecision,
			EffectiveDecision:         consoleClaudeCodeToolEffectiveDecision(baseDecision, toolPolicy, configured),
			OperatorOverrideMaxScope:  "none",
			AllowedRootCount:          len(toolPolicy.AllowedRoots),
			DeniedPathCount:           len(toolPolicy.DeniedPaths),
			AllowedDomainCount:        len(toolPolicy.AllowedDomains),
			AllowedCommandPrefixCount: len(toolPolicy.AllowedCommandPrefixes),
			DeniedCommandPrefixCount:  len(toolPolicy.DeniedCommandPrefixes),
		}
		if overrideClass, maxDelegation, hasOverrideClass := policy.ClaudeCodeToolOperatorOverride(toolName); hasOverrideClass {
			toolSummary.OperatorOverrideClass = overrideClass
			toolSummary.OperatorOverrideMaxScope = consoleOperatorGrantScopeLabel(maxDelegation)
		}
		explanation.Tools = append(explanation.Tools, toolSummary)
	}
	return explanation
}

func consoleClaudeCodeToolBaseDecision(policy config.Policy, toolName string) (string, string) {
	switch toolName {
	case "Bash":
		if !policy.Tools.Shell.Enabled {
			return "disabled", "tools.shell.enabled=false"
		}
		if policy.Tools.Shell.RequiresApproval {
			return "approval_required", "tools.shell.requires_approval=true"
		}
		return "allow", "tools.shell"
	case "Write", "Edit", "MultiEdit":
		if !policy.Tools.Filesystem.WriteEnabled {
			return "disabled", "tools.filesystem.write_enabled=false"
		}
		if policy.Tools.Filesystem.WriteRequiresApproval {
			return "approval_required", "tools.filesystem.write_requires_approval=true"
		}
		return "allow", "tools.filesystem write"
	case "Read", "Glob", "Grep":
		if !policy.Tools.Filesystem.ReadEnabled {
			return "disabled", "tools.filesystem.read_enabled=false"
		}
		return "allow", "tools.filesystem read"
	case "WebFetch", "WebSearch":
		if !policy.Tools.HTTP.Enabled {
			return "disabled", "tools.http.enabled=false"
		}
		if policy.Tools.HTTP.RequiresApproval {
			return "approval_required", "tools.http.requires_approval=true"
		}
		return "allow", "tools.http"
	default:
		return "unknown", "unsupported tool mapping"
	}
}

func consoleClaudeCodeToolEffectiveDecision(baseDecision string, toolPolicy config.ClaudeCodeToolPolicy, configured bool) string {
	decision := baseDecision
	if !configured {
		return decision
	}
	if toolPolicy.Enabled != nil && !*toolPolicy.Enabled {
		return "disabled"
	}
	if toolPolicy.RequiresApproval != nil {
		if *toolPolicy.RequiresApproval {
			decision = "approval_required"
		} else {
			decision = "allow"
		}
		return consoleDecisionWithLimitSuffix(decision, toolPolicy)
	}
	if toolPolicy.Enabled != nil && *toolPolicy.Enabled {
		decision = "allow"
	}
	return consoleDecisionWithLimitSuffix(decision, toolPolicy)
}

func consoleDecisionWithLimitSuffix(decision string, toolPolicy config.ClaudeCodeToolPolicy) string {
	if !consoleToolPolicyHasLimits(toolPolicy) {
		return decision
	}
	switch decision {
	case "allow":
		return "allow_if_limits_pass"
	case "approval_required":
		return "approval_required_if_limits_pass"
	default:
		return decision
	}
}

func consoleToolPolicyHasLimits(toolPolicy config.ClaudeCodeToolPolicy) bool {
	return len(toolPolicy.AllowedRoots) > 0 ||
		len(toolPolicy.DeniedPaths) > 0 ||
		len(toolPolicy.AllowedDomains) > 0 ||
		len(toolPolicy.AllowedCommandPrefixes) > 0 ||
		len(toolPolicy.DeniedCommandPrefixes) > 0
}

func consoleOperatorGrantScopeLabel(maxDelegation string) string {
	switch strings.TrimSpace(maxDelegation) {
	case config.OperatorOverrideDelegationPersistent:
		return "permanent"
	case config.OperatorOverrideDelegationSession:
		return "session"
	default:
		return "none"
	}
}

func printConsolePolicyExplanation(output io.Writer, explanation consolePolicyExplanation) {
	fmt.Fprintln(output, "Policy")
	if explanation.Error != "" {
		fmt.Fprintf(output, "  unavailable: %s\n\n", explanation.Error)
		return
	}
	fmt.Fprintf(output, "  claude_code.deny_unknown_tools: %t\n", explanation.DenyUnknownTools)
	fmt.Fprintf(output, "  filesystem: read=%s write=%s write_approval=%s allowed_roots=%d denied_paths=%d\n",
		consoleBoolLabel(explanation.Filesystem.ReadEnabled),
		consoleBoolLabel(explanation.Filesystem.WriteEnabled),
		consoleApprovalLabel(explanation.Filesystem.WriteEnabled, explanation.Filesystem.WriteRequiresApproval),
		explanation.Filesystem.AllowedRootCount,
		explanation.Filesystem.DeniedPathCount,
	)
	fmt.Fprintf(output, "  shell: enabled=%s approval=%s allowed_commands=%d\n",
		consoleBoolLabel(explanation.Shell.Enabled),
		consoleApprovalLabel(explanation.Shell.Enabled, explanation.Shell.RequiresApproval),
		explanation.Shell.AllowedCommandCount,
	)
	fmt.Fprintf(output, "  http: enabled=%s approval=%s allowed_domains=%d timeout_seconds=%d\n",
		consoleBoolLabel(explanation.HTTP.Enabled),
		consoleApprovalLabel(explanation.HTTP.Enabled, explanation.HTTP.RequiresApproval),
		explanation.HTTP.AllowedDomainCount,
		explanation.HTTP.TimeoutSeconds,
	)
	if len(explanation.Tools) == 0 {
		fmt.Fprintln(output)
		return
	}

	writer := tabwriter.NewWriter(output, 2, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "  TOOL\tBASE\tEFFECTIVE\tCONFIGURED\tMAX_GRANT\tLIMITS")
	for _, tool := range explanation.Tools {
		fmt.Fprintf(writer, "  %s\t%s\t%s\t%s\t%s\t%s\n",
			tool.ToolName,
			tool.BaseDecision,
			tool.EffectiveDecision,
			consoleBoolLabel(tool.Configured),
			tool.OperatorOverrideMaxScope,
			consoleToolLimitSummary(tool),
		)
	}
	_ = writer.Flush()
	fmt.Fprintln(output)
}

func consoleToolLimitSummary(tool consoleClaudeToolPolicySummary) string {
	parts := make([]string, 0, 5)
	if tool.AllowedRootCount > 0 {
		parts = append(parts, fmt.Sprintf("roots=%d", tool.AllowedRootCount))
	}
	if tool.DeniedPathCount > 0 {
		parts = append(parts, fmt.Sprintf("denied_paths=%d", tool.DeniedPathCount))
	}
	if tool.AllowedDomainCount > 0 {
		parts = append(parts, fmt.Sprintf("domains=%d", tool.AllowedDomainCount))
	}
	if tool.AllowedCommandPrefixCount > 0 {
		parts = append(parts, fmt.Sprintf("allowed_cmds=%d", tool.AllowedCommandPrefixCount))
	}
	if tool.DeniedCommandPrefixCount > 0 {
		parts = append(parts, fmt.Sprintf("denied_cmds=%d", tool.DeniedCommandPrefixCount))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " ")
}

func consoleBoolLabel(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func consoleApprovalLabel(enabled bool, requiresApproval bool) string {
	if !enabled {
		return "n/a"
	}
	return consoleBoolLabel(requiresApproval)
}
