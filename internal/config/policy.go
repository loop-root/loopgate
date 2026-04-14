package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"morph/internal/identifiers"
	"morph/internal/secrets"

	"gopkg.in/yaml.v3"
)

// Policy represents Morph's capability governance configuration.
type Policy struct {
	Version string `yaml:"version" json:"version"`

	Tools struct {
		ClaudeCode ClaudeCodePolicy `yaml:"claude_code" json:"claude_code"`
		MCPGateway MCPGatewayPolicy `yaml:"mcp_gateway" json:"mcp_gateway"`

		Filesystem struct {
			ReadEnabled           bool     `yaml:"read_enabled" json:"read_enabled"`
			WriteEnabled          bool     `yaml:"write_enabled" json:"write_enabled"`
			WriteRequiresApproval bool     `yaml:"write_requires_approval" json:"write_requires_approval"`
			AllowedRoots          []string `yaml:"allowed_roots" json:"allowed_roots"`
			DeniedPaths           []string `yaml:"denied_paths" json:"denied_paths"`
		} `yaml:"filesystem" json:"filesystem"`

		HTTP struct {
			Enabled          bool     `yaml:"enabled" json:"enabled"`
			AllowedDomains   []string `yaml:"allowed_domains" json:"allowed_domains"`
			RequiresApproval bool     `yaml:"requires_approval" json:"requires_approval"`
			TimeoutSeconds   int      `yaml:"timeout_seconds" json:"timeout_seconds"`
		} `yaml:"http" json:"http"`

		Shell struct {
			Enabled          bool     `yaml:"enabled" json:"enabled"`
			AllowedCommands  []string `yaml:"allowed_commands" json:"allowed_commands"`
			RequiresApproval bool     `yaml:"requires_approval" json:"requires_approval"`
		} `yaml:"shell" json:"shell"`

		Morphlings struct {
			SpawnEnabled    bool `yaml:"spawn_enabled" json:"spawn_enabled"`
			MaxActive       int  `yaml:"max_active" json:"max_active"`
			RequireTemplate bool `yaml:"require_template" json:"require_template"`
		} `yaml:"morphlings" json:"morphlings"`
	} `yaml:"tools" json:"tools"`

	Logging struct {
		LogCommands         bool `yaml:"log_commands" json:"log_commands"`
		LogMemoryPromotions bool `yaml:"log_memory_promotions" json:"log_memory_promotions"`
		LogToolCalls        bool `yaml:"log_tool_calls" json:"log_tool_calls"`
		AuditDetail         struct {
			HookProjectionLevel string `yaml:"hook_projection_level" json:"hook_projection_level"`
		} `yaml:"audit_detail" json:"audit_detail"`
	} `yaml:"logging" json:"logging"`

	Memory struct {
		AutoDistillate                bool `yaml:"auto_distillate" json:"auto_distillate"`
		RequirePromotionApproval      bool `yaml:"require_promotion_approval" json:"require_promotion_approval"`
		ContinuityReviewRequired      bool `yaml:"continuity_review_required" json:"continuity_review_required"`
		SubmitPreviousMinEvents       int  `yaml:"submit_previous_min_events" json:"submit_previous_min_events"`
		SubmitPreviousMinPayloadBytes int  `yaml:"submit_previous_min_payload_bytes" json:"submit_previous_min_payload_bytes"`
		SubmitPreviousMinPromptTokens int  `yaml:"submit_previous_min_prompt_tokens" json:"submit_previous_min_prompt_tokens"`
	} `yaml:"memory" json:"memory"`

	Safety struct {
		AllowPersonaModification bool `yaml:"allow_persona_modification" json:"allow_persona_modification"`
		AllowPolicyModification  bool `yaml:"allow_policy_modification" json:"allow_policy_modification"`
	} `yaml:"safety" json:"safety"`
}

type ClaudeCodePolicy struct {
	DenyUnknownTools *bool                           `yaml:"deny_unknown_tools" json:"deny_unknown_tools"`
	ToolPolicies     map[string]ClaudeCodeToolPolicy `yaml:"tool_policies" json:"tool_policies"`
}

type ClaudeCodeToolPolicy struct {
	Enabled                *bool    `yaml:"enabled" json:"enabled"`
	RequiresApproval       *bool    `yaml:"requires_approval" json:"requires_approval"`
	AllowedRoots           []string `yaml:"allowed_roots" json:"allowed_roots"`
	DeniedPaths            []string `yaml:"denied_paths" json:"denied_paths"`
	AllowedDomains         []string `yaml:"allowed_domains" json:"allowed_domains"`
	AllowedCommandPrefixes []string `yaml:"allowed_command_prefixes" json:"allowed_command_prefixes"`
	DeniedCommandPrefixes  []string `yaml:"denied_command_prefixes" json:"denied_command_prefixes"`
}

type MCPGatewayPolicy struct {
	DenyUnknownServers *bool                             `yaml:"deny_unknown_servers" json:"deny_unknown_servers"`
	Servers            map[string]MCPGatewayServerPolicy `yaml:"servers" json:"servers"`
}

type MCPGatewayServerPolicy struct {
	Enabled            *bool                           `yaml:"enabled" json:"enabled"`
	RequiresApproval   *bool                           `yaml:"requires_approval" json:"requires_approval"`
	Transport          string                          `yaml:"transport" json:"transport"`
	Launch             MCPGatewayLaunchPolicy          `yaml:"launch" json:"launch"`
	WorkingDirectory   string                          `yaml:"working_directory" json:"working_directory"`
	AllowedEnvironment []string                        `yaml:"allowed_environment" json:"allowed_environment"`
	SecretEnvironment  map[string]secrets.SecretRef    `yaml:"secret_environment" json:"secret_environment"`
	ToolPolicies       map[string]MCPGatewayToolPolicy `yaml:"tool_policies" json:"tool_policies"`
}

type MCPGatewayLaunchPolicy struct {
	Command string   `yaml:"command" json:"command"`
	Args    []string `yaml:"args" json:"args"`
}

type MCPGatewayToolPolicy struct {
	Enabled            *bool             `yaml:"enabled" json:"enabled"`
	RequiresApproval   *bool             `yaml:"requires_approval" json:"requires_approval"`
	RequiredArguments  []string          `yaml:"required_arguments" json:"required_arguments"`
	AllowedArguments   []string          `yaml:"allowed_arguments" json:"allowed_arguments"`
	DeniedArguments    []string          `yaml:"denied_arguments" json:"denied_arguments"`
	ArgumentValueKinds map[string]string `yaml:"argument_value_kinds" json:"argument_value_kinds"`
}

var supportedClaudeCodeToolPolicyNames = map[string]struct{}{
	"Bash":      {},
	"Write":     {},
	"Edit":      {},
	"MultiEdit": {},
	"Read":      {},
	"Glob":      {},
	"Grep":      {},
	"WebFetch":  {},
	"WebSearch": {},
}

var supportedMCPGatewayTransportNames = map[string]struct{}{
	"stdio": {},
}

var supportedClaudeCodeToolPolicyNameList = []string{
	"Bash",
	"Write",
	"Edit",
	"MultiEdit",
	"Read",
	"Glob",
	"Grep",
	"WebFetch",
	"WebSearch",
}

var supportedMCPGatewayTransportNameList = []string{
	"stdio",
}

var supportedMCPGatewayArgumentValueKinds = map[string]struct{}{
	"string":  {},
	"number":  {},
	"boolean": {},
	"object":  {},
	"array":   {},
	"null":    {},
}

var supportedMCPGatewayArgumentValueKindList = []string{
	"array",
	"boolean",
	"null",
	"number",
	"object",
	"string",
}

var environmentVariablePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
var mcpGatewayArgumentNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,127}$`)

// PolicyLoadResult holds the parsed policy and its content hash.
type PolicyLoadResult struct {
	Policy        Policy
	ContentSHA256 string // hex-encoded SHA-256 of raw policy bytes
}

// LoadPolicy loads policy from core/policy/policy.yaml.
// The repository policy file is required; a missing file is a startup error.
func LoadPolicy(repoRoot string) (Policy, error) {
	result, err := LoadPolicyWithHash(repoRoot)
	if err != nil {
		return Policy{}, err
	}
	return result.Policy, nil
}

// LoadPolicyWithHash loads policy and returns both the parsed policy and
// the SHA-256 hash of the raw policy file contents.
func LoadPolicyWithHash(repoRoot string) (PolicyLoadResult, error) {
	path := filepath.Join(repoRoot, "core", "policy", "policy.yaml")

	rawBytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return PolicyLoadResult{}, fmt.Errorf("required policy file not found at %s", path)
		}
		return PolicyLoadResult{}, err
	}

	contentHash := sha256.Sum256(rawBytes)
	hashHex := hex.EncodeToString(contentHash[:])
	if err := verifyPolicySignature(repoRoot, rawBytes); err != nil {
		return PolicyLoadResult{}, err
	}

	pol, err := ParsePolicyDocument(rawBytes)
	if err != nil {
		return PolicyLoadResult{}, err
	}
	return PolicyLoadResult{Policy: pol, ContentSHA256: hashHex}, nil
}

// ParsePolicyDocument strictly decodes and normalizes a policy YAML document.
// It validates fields and applies secure defaults, but does not verify a
// detached signature. Callers that rely on signature verification must do so
// before trusting the returned policy.
func ParsePolicyDocument(rawBytes []byte) (Policy, error) {
	var pol Policy
	decoder := yaml.NewDecoder(bytes.NewReader(rawBytes))
	decoder.KnownFields(true)
	if err := decoder.Decode(&pol); err != nil {
		return Policy{}, err
	}
	if err := applyPolicyDefaults(&pol); err != nil {
		return Policy{}, err
	}
	return pol, nil
}

func applyPolicyDefaults(pol *Policy) error {
	if pol.Version == "" {
		pol.Version = "0.1.0"
	}
	if err := applyClaudeCodePolicyDefaults(&pol.Tools.ClaudeCode); err != nil {
		return err
	}
	if err := applyMCPGatewayPolicyDefaults(&pol.Tools.MCPGateway); err != nil {
		return err
	}
	pol.Tools.Filesystem.AllowedRoots = normalizeConfiguredPaths(pol.Tools.Filesystem.AllowedRoots)
	if (pol.Tools.Filesystem.ReadEnabled || pol.Tools.Filesystem.WriteEnabled) && len(pol.Tools.Filesystem.AllowedRoots) == 0 {
		return fmt.Errorf("filesystem allowed_roots must be explicitly configured when filesystem access is enabled")
	}
	pol.Tools.Filesystem.DeniedPaths = normalizeConfiguredPaths(pol.Tools.Filesystem.DeniedPaths)
	normalizedAllowedCommands, err := normalizeConfiguredCommandAllowlist(pol.Tools.Shell.AllowedCommands)
	if err != nil {
		return err
	}
	pol.Tools.Shell.AllowedCommands = normalizedAllowedCommands
	if pol.Tools.Shell.Enabled && len(pol.Tools.Shell.AllowedCommands) == 0 {
		return fmt.Errorf("shell allowed_commands must be explicitly configured when shell access is enabled")
	}
	if pol.Tools.HTTP.TimeoutSeconds <= 0 {
		pol.Tools.HTTP.TimeoutSeconds = 10
	}
	if pol.Tools.Morphlings.MaxActive <= 0 {
		pol.Tools.Morphlings.MaxActive = 5
	}
	if pol.Memory.SubmitPreviousMinEvents <= 0 {
		pol.Memory.SubmitPreviousMinEvents = 3
	}
	if pol.Memory.SubmitPreviousMinPayloadBytes <= 0 {
		pol.Memory.SubmitPreviousMinPayloadBytes = 512
	}
	if pol.Memory.SubmitPreviousMinPromptTokens <= 0 {
		pol.Memory.SubmitPreviousMinPromptTokens = 120
	}
	switch strings.TrimSpace(pol.Logging.AuditDetail.HookProjectionLevel) {
	case "", "full":
		pol.Logging.AuditDetail.HookProjectionLevel = "full"
	case "minimal":
		pol.Logging.AuditDetail.HookProjectionLevel = "minimal"
	default:
		return fmt.Errorf("logging.audit_detail.hook_projection_level must be one of: full, minimal")
	}
	return nil
}

func applyClaudeCodePolicyDefaults(policy *ClaudeCodePolicy) error {
	for toolName, toolPolicy := range policy.ToolPolicies {
		if _, supported := supportedClaudeCodeToolPolicyNames[toolName]; !supported {
			return fmt.Errorf("tools.claude_code.tool_policies contains unsupported tool %q", toolName)
		}
		toolPolicy.AllowedRoots = normalizeConfiguredPaths(toolPolicy.AllowedRoots)
		toolPolicy.DeniedPaths = normalizeConfiguredPaths(toolPolicy.DeniedPaths)
		toolPolicy.AllowedDomains = normalizeConfiguredDomains(toolPolicy.AllowedDomains)
		toolPolicy.AllowedCommandPrefixes = normalizeConfiguredPrefixes(toolPolicy.AllowedCommandPrefixes)
		toolPolicy.DeniedCommandPrefixes = normalizeConfiguredPrefixes(toolPolicy.DeniedCommandPrefixes)
		policy.ToolPolicies[toolName] = toolPolicy
	}
	return nil
}

func applyMCPGatewayPolicyDefaults(policy *MCPGatewayPolicy) error {
	for serverID, serverPolicy := range policy.Servers {
		if err := identifiers.ValidateSafeIdentifier("tools.mcp_gateway.servers key", serverID); err != nil {
			return err
		}
		trimmedTransport := strings.TrimSpace(serverPolicy.Transport)
		if trimmedTransport == "" {
			trimmedTransport = "stdio"
		}
		if _, supported := supportedMCPGatewayTransportNames[trimmedTransport]; !supported {
			return fmt.Errorf("tools.mcp_gateway.servers.%s.transport must be one of: %s", serverID, strings.Join(supportedMCPGatewayTransportNameList, ", "))
		}
		serverPolicy.Transport = trimmedTransport

		normalizedLaunchCommand, err := normalizeConfiguredCommandAllowlist([]string{serverPolicy.Launch.Command})
		if err != nil {
			return fmt.Errorf("tools.mcp_gateway.servers.%s.launch.command: %w", serverID, err)
		}
		if len(normalizedLaunchCommand) != 1 {
			return fmt.Errorf("tools.mcp_gateway.servers.%s.launch.command is required", serverID)
		}
		serverPolicy.Launch.Command = normalizedLaunchCommand[0]
		serverPolicy.Launch.Args = normalizeNonEmptyStrings(serverPolicy.Launch.Args)
		serverPolicy.WorkingDirectory = normalizeOptionalConfiguredPath(serverPolicy.WorkingDirectory)
		serverPolicy.AllowedEnvironment, err = normalizeConfiguredEnvironmentVariableNames(serverPolicy.AllowedEnvironment)
		if err != nil {
			return fmt.Errorf("tools.mcp_gateway.servers.%s.allowed_environment: %w", serverID, err)
		}
		if serverPolicy.SecretEnvironment == nil {
			serverPolicy.SecretEnvironment = map[string]secrets.SecretRef{}
		}
		validatedSecretEnvironment := make(map[string]secrets.SecretRef, len(serverPolicy.SecretEnvironment))
		for environmentVariableName, secretRef := range serverPolicy.SecretEnvironment {
			if err := validateEnvironmentVariableName(environmentVariableName); err != nil {
				return fmt.Errorf("tools.mcp_gateway.servers.%s.secret_environment.%s: %w", serverID, environmentVariableName, err)
			}
			if err := secretRef.Validate(); err != nil {
				return fmt.Errorf("tools.mcp_gateway.servers.%s.secret_environment.%s: %w", serverID, environmentVariableName, err)
			}
			validatedSecretEnvironment[strings.TrimSpace(environmentVariableName)] = secretRef
		}
		serverPolicy.SecretEnvironment = validatedSecretEnvironment

		validatedToolPolicies := make(map[string]MCPGatewayToolPolicy, len(serverPolicy.ToolPolicies))
		for toolName, toolPolicy := range serverPolicy.ToolPolicies {
			if err := identifiers.ValidateSafeIdentifier("mcp gateway tool", toolName); err != nil {
				return fmt.Errorf("tools.mcp_gateway.servers.%s.tool_policies.%s: %w", serverID, toolName, err)
			}
			normalizedRequiredArguments, err := normalizeConfiguredMCPGatewayArgumentNames(toolPolicy.RequiredArguments)
			if err != nil {
				return fmt.Errorf("tools.mcp_gateway.servers.%s.tool_policies.%s.required_arguments: %w", serverID, toolName, err)
			}
			normalizedAllowedArguments, err := normalizeConfiguredMCPGatewayArgumentNames(toolPolicy.AllowedArguments)
			if err != nil {
				return fmt.Errorf("tools.mcp_gateway.servers.%s.tool_policies.%s.allowed_arguments: %w", serverID, toolName, err)
			}
			normalizedDeniedArguments, err := normalizeConfiguredMCPGatewayArgumentNames(toolPolicy.DeniedArguments)
			if err != nil {
				return fmt.Errorf("tools.mcp_gateway.servers.%s.tool_policies.%s.denied_arguments: %w", serverID, toolName, err)
			}
			normalizedArgumentValueKinds, err := normalizeConfiguredMCPGatewayArgumentValueKinds(toolPolicy.ArgumentValueKinds)
			if err != nil {
				return fmt.Errorf("tools.mcp_gateway.servers.%s.tool_policies.%s.argument_value_kinds: %w", serverID, toolName, err)
			}
			toolPolicy.RequiredArguments = normalizedRequiredArguments
			toolPolicy.AllowedArguments = normalizedAllowedArguments
			toolPolicy.DeniedArguments = normalizedDeniedArguments
			toolPolicy.ArgumentValueKinds = normalizedArgumentValueKinds
			validatedToolPolicies[toolName] = toolPolicy
		}
		serverPolicy.ToolPolicies = validatedToolPolicies
		policy.Servers[serverID] = serverPolicy
	}
	return nil
}

func normalizeConfiguredPaths(rawPaths []string) []string {
	normalized := make([]string, 0, len(rawPaths))
	for _, rawPath := range rawPaths {
		trimmed := strings.TrimSpace(rawPath)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, expandHomePrefix(trimmed))
	}
	return normalized
}

func expandHomePrefix(pathValue string) string {
	if pathValue == "~" {
		if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" {
			return homeDir
		}
		return pathValue
	}

	prefix := "~" + string(os.PathSeparator)
	if strings.HasPrefix(pathValue, prefix) {
		if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" {
			return filepath.Join(homeDir, pathValue[len(prefix):])
		}
	}
	return pathValue
}

func normalizeConfiguredDomains(rawDomains []string) []string {
	normalizedDomains := make([]string, 0, len(rawDomains))
	seenDomains := make(map[string]struct{}, len(rawDomains))
	for _, rawDomain := range rawDomains {
		trimmedDomain := strings.ToLower(strings.TrimSpace(rawDomain))
		if trimmedDomain == "" {
			continue
		}
		if _, alreadySeen := seenDomains[trimmedDomain]; alreadySeen {
			continue
		}
		seenDomains[trimmedDomain] = struct{}{}
		normalizedDomains = append(normalizedDomains, trimmedDomain)
	}
	return normalizedDomains
}

func normalizeConfiguredPrefixes(rawPrefixes []string) []string {
	normalizedPrefixes := make([]string, 0, len(rawPrefixes))
	seenPrefixes := make(map[string]struct{}, len(rawPrefixes))
	for _, rawPrefix := range rawPrefixes {
		trimmedPrefix := strings.TrimSpace(rawPrefix)
		if trimmedPrefix == "" {
			continue
		}
		if _, alreadySeen := seenPrefixes[trimmedPrefix]; alreadySeen {
			continue
		}
		seenPrefixes[trimmedPrefix] = struct{}{}
		normalizedPrefixes = append(normalizedPrefixes, trimmedPrefix)
	}
	return normalizedPrefixes
}

func normalizeNonEmptyStrings(rawValues []string) []string {
	normalizedValues := make([]string, 0, len(rawValues))
	for _, rawValue := range rawValues {
		trimmedValue := strings.TrimSpace(rawValue)
		if trimmedValue == "" {
			continue
		}
		normalizedValues = append(normalizedValues, trimmedValue)
	}
	return normalizedValues
}

func normalizeOptionalConfiguredPath(rawPath string) string {
	trimmedPath := strings.TrimSpace(rawPath)
	if trimmedPath == "" {
		return ""
	}
	normalizedPaths := normalizeConfiguredPaths([]string{trimmedPath})
	if len(normalizedPaths) == 0 {
		return ""
	}
	return normalizedPaths[0]
}

func normalizeConfiguredCommandAllowlist(rawCommands []string) ([]string, error) {
	normalizedCommands := make([]string, 0, len(rawCommands))
	seenCommands := make(map[string]struct{}, len(rawCommands))
	for _, rawCommand := range rawCommands {
		trimmedCommand := strings.TrimSpace(rawCommand)
		if trimmedCommand == "" {
			continue
		}
		if strings.ContainsAny(trimmedCommand, " \t\r\n") {
			return nil, fmt.Errorf("shell allowed_commands entries must be single command names or exact paths")
		}
		cleanedCommand := filepath.Clean(trimmedCommand)
		if cleanedCommand == "." || cleanedCommand == ".." {
			return nil, fmt.Errorf("shell allowed_commands entry %q is invalid", rawCommand)
		}
		if _, alreadySeen := seenCommands[cleanedCommand]; alreadySeen {
			continue
		}
		seenCommands[cleanedCommand] = struct{}{}
		normalizedCommands = append(normalizedCommands, cleanedCommand)
	}
	return normalizedCommands, nil
}

func normalizeConfiguredEnvironmentVariableNames(rawNames []string) ([]string, error) {
	normalizedNames := make([]string, 0, len(rawNames))
	seenNames := make(map[string]struct{}, len(rawNames))
	for _, rawName := range rawNames {
		trimmedName := strings.TrimSpace(rawName)
		if trimmedName == "" {
			continue
		}
		if err := validateEnvironmentVariableName(trimmedName); err != nil {
			return nil, err
		}
		if _, alreadySeen := seenNames[trimmedName]; alreadySeen {
			continue
		}
		seenNames[trimmedName] = struct{}{}
		normalizedNames = append(normalizedNames, trimmedName)
	}
	return normalizedNames, nil
}

func validateEnvironmentVariableName(environmentVariableName string) error {
	trimmedName := strings.TrimSpace(environmentVariableName)
	if trimmedName == "" {
		return fmt.Errorf("environment variable name is required")
	}
	if !environmentVariablePattern.MatchString(trimmedName) {
		return fmt.Errorf("environment variable name must match %s", environmentVariablePattern.String())
	}
	return nil
}

func normalizeConfiguredMCPGatewayArgumentNames(argumentNames []string) ([]string, error) {
	normalizedArgumentNames := normalizeNonEmptyStrings(argumentNames)
	normalizedArgumentNames = slices.Compact(normalizedArgumentNames)
	for _, argumentName := range normalizedArgumentNames {
		if !mcpGatewayArgumentNamePattern.MatchString(argumentName) {
			return nil, fmt.Errorf("argument name %q is invalid", argumentName)
		}
	}
	return normalizedArgumentNames, nil
}

func normalizeConfiguredMCPGatewayArgumentValueKinds(argumentValueKinds map[string]string) (map[string]string, error) {
	if len(argumentValueKinds) == 0 {
		return map[string]string{}, nil
	}
	normalizedArgumentValueKinds := make(map[string]string, len(argumentValueKinds))
	for rawArgumentName, rawValueKind := range argumentValueKinds {
		argumentName := strings.TrimSpace(rawArgumentName)
		if !mcpGatewayArgumentNamePattern.MatchString(argumentName) {
			return nil, fmt.Errorf("argument name %q is invalid", rawArgumentName)
		}
		valueKind := strings.TrimSpace(rawValueKind)
		if _, supported := supportedMCPGatewayArgumentValueKinds[valueKind]; !supported {
			return nil, fmt.Errorf("argument value kind %q must be one of: %s", rawValueKind, strings.Join(supportedMCPGatewayArgumentValueKindList, ", "))
		}
		normalizedArgumentValueKinds[argumentName] = valueKind
	}
	return normalizedArgumentValueKinds, nil
}

func (p Policy) ClaudeCodeDenyUnknownTools() bool {
	if p.Tools.ClaudeCode.DenyUnknownTools == nil {
		return true
	}
	return *p.Tools.ClaudeCode.DenyUnknownTools
}

func (p Policy) ClaudeCodeToolPolicy(toolName string) (ClaudeCodeToolPolicy, bool) {
	toolPolicy, found := p.Tools.ClaudeCode.ToolPolicies[toolName]
	return toolPolicy, found
}

func (p Policy) MCPGatewayDenyUnknownServers() bool {
	if p.Tools.MCPGateway.DenyUnknownServers == nil {
		return true
	}
	return *p.Tools.MCPGateway.DenyUnknownServers
}

func (p Policy) MCPGatewayServerPolicy(serverID string) (MCPGatewayServerPolicy, bool) {
	serverPolicy, found := p.Tools.MCPGateway.Servers[serverID]
	return serverPolicy, found
}

// SupportedClaudeCodeToolPolicyNames returns the supported Claude Code tool
// names in a stable display order.
func SupportedClaudeCodeToolPolicyNames() []string {
	return append([]string(nil), supportedClaudeCodeToolPolicyNameList...)
}

// SupportedMCPGatewayTransportNames returns supported Phase 2 MCP transport
// names in a stable display order.
func SupportedMCPGatewayTransportNames() []string {
	return append([]string(nil), supportedMCPGatewayTransportNameList...)
}

func (p Policy) HookAuditProjectionLevel() string {
	level := strings.TrimSpace(p.Logging.AuditDetail.HookProjectionLevel)
	if level == "" {
		return "full"
	}
	return level
}

func (p Policy) HookAuditProjectionIncludesPreviews() bool {
	return p.HookAuditProjectionLevel() == "full"
}
