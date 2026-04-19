package config

import (
	"fmt"
	"slices"
	"strings"
)

// PolicyTemplatePreset describes a starter policy profile operators can render
// or apply during first-time setup.
type PolicyTemplatePreset struct {
	Name         string
	Aliases      []string
	Summary      string
	TemplateYAML string
}

var policyTemplatePresets = []PolicyTemplatePreset{
	{
		Name:    "strict",
		Aliases: []string{"strict-mvp"},
		Summary: "Read-oriented starter profile. Writes require approval; shell and HTTP stay disabled.",
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
      WebSearch:
        enabled: false
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
		Name:    "balanced",
		Aliases: nil,
		Summary: "Approval-gated developer shell profile. Common inspection and test commands are available, but HTTP stays disabled.",
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
    enabled: false
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
      - "sed"
      - "grep"
      - "find"
      - "head"
      - "tail"
      - "wc"
      - "sort"
      - "uniq"
      - "tr"
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
		Summary: "Local development profile. Common shell tooling and HTTP are enabled, still behind approval.",
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

// ResolvePolicyTemplatePreset resolves a canonical preset name or alias.
func ResolvePolicyTemplatePreset(name string) (PolicyTemplatePreset, error) {
	trimmedName := strings.TrimSpace(strings.ToLower(name))
	if trimmedName == "" {
		return PolicyTemplatePreset{}, fmt.Errorf("starter policy profile must not be empty (supported: %s)", strings.Join(PolicyTemplatePresetNames(), ", "))
	}
	for _, preset := range policyTemplatePresets {
		if trimmedName == preset.Name {
			return preset, nil
		}
		for _, alias := range preset.Aliases {
			if trimmedName == strings.TrimSpace(strings.ToLower(alias)) {
				return preset, nil
			}
		}
	}
	return PolicyTemplatePreset{}, fmt.Errorf("unknown starter policy profile %q (supported: %s)", name, strings.Join(PolicyTemplatePresetNames(), ", "))
}
