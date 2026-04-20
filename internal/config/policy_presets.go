package config

import (
	"fmt"
	"slices"
	"strings"
)

// PolicyTemplatePreset describes a starter policy profile operators can render
// or apply during first-time setup.
type PolicyTemplatePreset struct {
	Name               string
	Aliases            []string
	Summary            string
	UseCase            string
	RecommendedInSetup bool
	AlwaysAllowed      []string
	ApprovalRequired   []string
	HardBlocks         []string
	TemplateYAML       string
}

var policyTemplatePresets = []PolicyTemplatePreset{
	{
		Name:               "strict",
		Aliases:            []string{"strict-mvp"},
		Summary:            "Higher-sensitivity starter. Read and search stay open; every edit needs approval; shell and web stay blocked.",
		UseCase:            "Best for initial rollout, sensitive repositories, and operators who want repo reads before broader automation.",
		RecommendedInSetup: false,
		AlwaysAllowed: []string{
			"Claude Read, Glob, and Grep inside the repo root",
			"Local audit logging and signed-policy enforcement",
		},
		ApprovalRequired: []string{
			"Claude Write, Edit, and MultiEdit inside the repo root",
		},
		HardBlocks: []string{
			"Bash, WebFetch, and WebSearch",
			"Policy, persona, runtime state, and .git internal paths",
		},
		TemplateYAML: `version: 0.1.0
tools:
  claude_code:
    deny_unknown_tools: true
    tool_policies:
      Bash:
        enabled: false
      Read:
        enabled: true
        allowed_roots:
          - "."
        denied_paths:
          - ".git"
          - "persona"
          - "runtime/state"
          - "core/policy"
      Glob:
        enabled: true
        allowed_roots:
          - "."
        denied_paths:
          - ".git"
          - "persona"
          - "runtime/state"
          - "core/policy"
      Grep:
        enabled: true
        allowed_roots:
          - "."
        denied_paths:
          - ".git"
          - "persona"
          - "runtime/state"
          - "core/policy"
      Write:
        enabled: true
        requires_approval: true
        allowed_roots:
          - "."
        denied_paths:
          - ".git"
          - "persona"
          - "core/policy"
          - "runtime/state"
          - ".claude/settings.json"
      Edit:
        enabled: true
        requires_approval: true
        allowed_roots:
          - "."
        denied_paths:
          - ".git"
          - "persona"
          - "core/policy"
          - "runtime/state"
          - ".claude/settings.json"
      MultiEdit:
        enabled: true
        requires_approval: true
        allowed_roots:
          - "."
        denied_paths:
          - ".git"
          - "persona"
          - "core/policy"
          - "runtime/state"
          - ".claude/settings.json"
      WebFetch:
        enabled: false
      WebSearch:
        enabled: false
  mcp_gateway:
    deny_unknown_servers: true
    servers: {}
  filesystem:
    allowed_roots:
      - "."
    denied_paths:
      - ".git"
      - "core/policy"
      - "persona"
      - "runtime/state"
      - ".claude/settings.json"
    read_enabled: true
    write_enabled: true
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
`,
	},
	{
		Name:               "balanced",
		Aliases:            nil,
		Summary:            "Recommended daily-driver. Repo reads and patch-style edits stay open; new-file writes and Bash need approval; web stays blocked.",
		UseCase:            "Best for normal day-to-day engineering in a trusted local repo where approvals should focus on riskier actions, not every patch.",
		RecommendedInSetup: true,
		AlwaysAllowed: []string{
			"Claude Read, Glob, and Grep inside the repo root",
			"Claude Edit and MultiEdit inside the repo root",
		},
		ApprovalRequired: []string{
			"Claude Write for new-file or full-file writes",
			"Claude Bash for the allowed inspection and test command set",
		},
		HardBlocks: []string{
			"WebFetch and WebSearch",
			"Policy, persona, runtime state, and .git internal paths",
		},
		TemplateYAML: `version: 0.1.0
tools:
  claude_code:
    deny_unknown_tools: true
    tool_policies:
      Bash:
        enabled: true
        requires_approval: true
        allowed_command_prefixes:
          - "pwd"
          - "ls"
          - "rg "
          - "cat "
          - "sed -n "
          - "head "
          - "tail "
          - "wc "
          - "git diff"
          - "git status"
          - "git log --oneline"
          - "git show"
          - "git branch --show-current"
          - "go test"
          - "go vet"
        denied_command_prefixes:
          - "curl"
          - "git clean"
          - "git commit"
          - "git push"
          - "git rebase"
          - "git reset"
          - "rm"
          - "sudo"
          - "wget"
      Read:
        enabled: true
        allowed_roots:
          - "."
        denied_paths:
          - ".git"
          - "persona"
          - "runtime/state"
          - "core/policy"
      Glob:
        enabled: true
        allowed_roots:
          - "."
        denied_paths:
          - ".git"
          - "persona"
          - "runtime/state"
          - "core/policy"
      Grep:
        enabled: true
        allowed_roots:
          - "."
        denied_paths:
          - ".git"
          - "persona"
          - "runtime/state"
          - "core/policy"
      Write:
        enabled: true
        requires_approval: true
        allowed_roots:
          - "."
        denied_paths:
          - ".git"
          - "persona"
          - "core/policy"
          - "runtime/state"
          - ".claude/settings.json"
      Edit:
        enabled: true
        requires_approval: false
        allowed_roots:
          - "."
        denied_paths:
          - ".git"
          - "persona"
          - "core/policy"
          - "runtime/state"
          - ".claude/settings.json"
      MultiEdit:
        enabled: true
        requires_approval: false
        allowed_roots:
          - "."
        denied_paths:
          - ".git"
          - "persona"
          - "core/policy"
          - "runtime/state"
          - ".claude/settings.json"
      WebFetch:
        enabled: false
      WebSearch:
        enabled: false
  mcp_gateway:
    deny_unknown_servers: true
    servers: {}
  filesystem:
    allowed_roots:
      - "."
    denied_paths:
      - ".git"
      - "core/policy"
      - "persona"
      - "runtime/state"
      - ".claude/settings.json"
    read_enabled: true
    write_enabled: true
    write_requires_approval: true
  http:
    enabled: false
    allowed_domains: []
    requires_approval: true
    timeout_seconds: 10
  shell:
    enabled: true
    allowed_commands:
      - "pwd"
      - "ls"
      - "rg"
      - "cat"
      - "sed"
      - "head"
      - "tail"
      - "wc"
      - "git"
      - "go"
    requires_approval: true
logging:
  log_commands: true
  log_tool_calls: true
safety:
  allow_persona_modification: false
  allow_policy_modification: false
`,
	},
	{
		Name:    "developer",
		Aliases: []string{"dev"},
		Summary: "Experimental escape hatch. Broader local development shell tooling and HTTP are enabled, still behind approval.",
		TemplateYAML: `version: 0.1.0
tools:
  claude_code:
    deny_unknown_tools: true
    tool_policies:
      Bash:
        enabled: true
        allowed_command_prefixes:
          - "ls"
          - "pwd"
          - "find "
          - "grep "
          - "cat "
          - "sed -n "
          - "head "
          - "tail "
          - "wc "
          - "sort "
          - "git status"
          - "git diff"
          - "go test"
          - "rg "
        denied_command_prefixes:
          - "rm "
          - "curl "
      Read:
        enabled: true
        allowed_roots:
          - "."
        denied_paths:
          - "runtime/state"
          - "core/policy"
      Glob:
        enabled: true
        allowed_roots:
          - "."
        denied_paths:
          - "runtime/state"
          - "core/policy"
      Grep:
        enabled: true
        allowed_roots:
          - "."
        denied_paths:
          - "runtime/state"
          - "core/policy"
      Write:
        enabled: true
        requires_approval: true
        allowed_roots:
          - "."
        denied_paths:
          - "core/policy"
          - ".claude/settings.json"
      Edit:
        enabled: true
        requires_approval: true
        allowed_roots:
          - "."
      MultiEdit:
        enabled: true
        requires_approval: true
        allowed_roots:
          - "."
      WebFetch:
        enabled: false
        allowed_domains: []
  mcp_gateway:
    deny_unknown_servers: true
    servers: {}
  filesystem:
    allowed_roots:
      - "."
    denied_paths:
      - "core/policy"
      - "persona"
    read_enabled: true
    write_enabled: true
    write_requires_approval: true
  http:
    enabled: true
    allowed_domains: []
    requires_approval: true
    timeout_seconds: 10
  shell:
    enabled: true
    allowed_commands:
      - "git"
      - "go"
      - "gofmt"
      - "rg"
      - "ls"
      - "cat"
      - "pwd"
      - "printf"
      - "mkdir"
      - "cp"
      - "mv"
      - "sed"
      - "grep"
      - "find"
      - "head"
      - "tail"
      - "wc"
      - "sort"
      - "uniq"
      - "tr"
      - "xargs"
      - "make"
      - "npm"
      - "pnpm"
      - "node"
      - "python3"
      - "uv"
      - "swift"
      - "xcodebuild"
    requires_approval: true
logging:
  log_commands: true
  log_tool_calls: true
safety:
  allow_persona_modification: false
  allow_policy_modification: false
`,
	},
}

var setupPolicyTemplatePresetNames = []string{
	"strict",
	"balanced",
}

// PolicyTemplatePresets returns the supported starter policy profiles in
// display order.
func PolicyTemplatePresets() []PolicyTemplatePreset {
	return slices.Clone(policyTemplatePresets)
}

// PolicyTemplatePresetNames returns the canonical starter policy profile names.
func PolicyTemplatePresetNames() []string {
	names := make([]string, 0, len(policyTemplatePresets))
	for _, preset := range policyTemplatePresets {
		names = append(names, preset.Name)
	}
	return names
}

// SetupPolicyTemplatePresets returns the supported starter policy profiles for
// the guided first-run path. Experimental templates may still be available
// through the policy-admin renderer without becoming part of the supported v1
// setup story.
func SetupPolicyTemplatePresets() []PolicyTemplatePreset {
	presets := make([]PolicyTemplatePreset, 0, len(setupPolicyTemplatePresetNames))
	for _, presetName := range setupPolicyTemplatePresetNames {
		preset, err := ResolvePolicyTemplatePreset(presetName)
		if err == nil {
			presets = append(presets, preset)
		}
	}
	return presets
}

// SetupPolicyTemplatePresetNames returns the canonical starter policy profile
// names exposed by the guided setup wizard.
func SetupPolicyTemplatePresetNames() []string {
	return append([]string(nil), setupPolicyTemplatePresetNames...)
}

// ResolvePolicyTemplatePreset resolves a canonical preset name or alias.
func ResolvePolicyTemplatePreset(name string) (PolicyTemplatePreset, error) {
	return resolvePolicyTemplatePreset(name, policyTemplatePresets, PolicyTemplatePresetNames())
}

// ResolveSetupPolicyTemplatePreset resolves the supported guided-setup starter
// policy profiles.
func ResolveSetupPolicyTemplatePreset(name string) (PolicyTemplatePreset, error) {
	return resolvePolicyTemplatePreset(name, SetupPolicyTemplatePresets(), SetupPolicyTemplatePresetNames())
}

func resolvePolicyTemplatePreset(name string, presets []PolicyTemplatePreset, supportedNames []string) (PolicyTemplatePreset, error) {
	trimmedName := strings.TrimSpace(strings.ToLower(name))
	if trimmedName == "" {
		return PolicyTemplatePreset{}, fmt.Errorf("starter policy profile must not be empty (supported: %s)", strings.Join(supportedNames, ", "))
	}
	for _, preset := range presets {
		if trimmedName == preset.Name {
			return preset, nil
		}
		for _, alias := range preset.Aliases {
			if trimmedName == strings.TrimSpace(strings.ToLower(alias)) {
				return preset, nil
			}
		}
	}
	return PolicyTemplatePreset{}, fmt.Errorf("unknown starter policy profile %q (supported: %s)", name, strings.Join(supportedNames, ", "))
}
