package config

import (
	"strings"
	"testing"
)

func TestApplyPolicyDefaults_RejectsUnknownClaudeCodeTool(t *testing.T) {
	policy := Policy{}
	policy.Tools.ClaudeCode.ToolPolicies = map[string]ClaudeCodeToolPolicy{
		"UnknownTool": {},
	}

	err := applyPolicyDefaults(&policy)
	if err == nil {
		t.Fatal("expected unsupported Claude Code tool to be rejected")
	}
	if !strings.Contains(err.Error(), "unsupported tool") {
		t.Fatalf("expected unsupported tool error, got %v", err)
	}
}

func TestPolicy_ClaudeCodeDenyUnknownToolsDefaultsTrue(t *testing.T) {
	policy := Policy{}

	if !policy.ClaudeCodeDenyUnknownTools() {
		t.Fatal("expected deny unknown tools to default true")
	}
}

func TestApplyPolicyDefaults_NormalizesClaudeCodeToolPolicy(t *testing.T) {
	policy := Policy{}
	policy.Tools.ClaudeCode.ToolPolicies = map[string]ClaudeCodeToolPolicy{
		"Bash": {
			AllowedCommandPrefixes: []string{" git status ", "git status"},
			DeniedCommandPrefixes:  []string{" rm ", "rm"},
			AllowedDomains:         []string{" Example.COM ", "example.com"},
		},
	}

	if err := applyPolicyDefaults(&policy); err != nil {
		t.Fatalf("apply defaults: %v", err)
	}

	toolPolicy := policy.Tools.ClaudeCode.ToolPolicies["Bash"]
	if len(toolPolicy.AllowedCommandPrefixes) != 1 || toolPolicy.AllowedCommandPrefixes[0] != "git status" {
		t.Fatalf("expected normalized allowed command prefixes, got %#v", toolPolicy.AllowedCommandPrefixes)
	}
	if len(toolPolicy.DeniedCommandPrefixes) != 1 || toolPolicy.DeniedCommandPrefixes[0] != "rm" {
		t.Fatalf("expected normalized denied command prefixes, got %#v", toolPolicy.DeniedCommandPrefixes)
	}
	if len(toolPolicy.AllowedDomains) != 1 || toolPolicy.AllowedDomains[0] != "example.com" {
		t.Fatalf("expected normalized allowed domains, got %#v", toolPolicy.AllowedDomains)
	}
}

func TestPolicy_HookAuditProjectionLevelDefaultsFull(t *testing.T) {
	policy := Policy{}

	if got := policy.HookAuditProjectionLevel(); got != "full" {
		t.Fatalf("expected default hook audit projection level full, got %q", got)
	}
	if !policy.HookAuditProjectionIncludesPreviews() {
		t.Fatal("expected default hook audit projection to include previews")
	}
}

func TestApplyPolicyDefaults_AcceptsMinimalHookAuditProjectionLevel(t *testing.T) {
	policy := Policy{}
	policy.Logging.AuditDetail.HookProjectionLevel = "minimal"

	if err := applyPolicyDefaults(&policy); err != nil {
		t.Fatalf("apply defaults: %v", err)
	}
	if got := policy.HookAuditProjectionLevel(); got != "minimal" {
		t.Fatalf("expected minimal hook audit projection level, got %q", got)
	}
	if policy.HookAuditProjectionIncludesPreviews() {
		t.Fatal("expected minimal hook audit projection level to omit previews")
	}
}

func TestApplyPolicyDefaults_RejectsUnknownHookAuditProjectionLevel(t *testing.T) {
	policy := Policy{}
	policy.Logging.AuditDetail.HookProjectionLevel = "verbose"

	err := applyPolicyDefaults(&policy)
	if err == nil {
		t.Fatal("expected invalid hook audit projection level to be rejected")
	}
	if !strings.Contains(err.Error(), "hook_projection_level") {
		t.Fatalf("expected hook_projection_level validation error, got %v", err)
	}
}
