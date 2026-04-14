package shell

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"loopgate/internal/config"
	"loopgate/internal/loopgate"
	modelruntime "loopgate/internal/modelruntime"
	"loopgate/internal/sandbox"
	"loopgate/internal/ui"
)

type CommandContext struct {
	RepoRoot             string
	Persona              config.Persona
	Policy               config.Policy
	CurrentRuntimeConfig modelruntime.Config
	LoopgateClient       loopgate.ControlPlaneClient
	LoopgateStatus       loopgate.StatusResponse
}

func buildHelpText() string {
	catalog := commandCatalog()
	entries := make([]ui.HelpCommandEntry, 0, len(catalog))
	for _, cmd := range catalog {
		args := cmd.ShortArgs
		if args == "" {
			args = cmd.Args
		}
		desc := cmd.ShortDesc
		if desc == "" {
			desc = cmd.Description
		}
		entries = append(entries, ui.HelpCommandEntry{
			Name: cmd.Name,
			Args: args,
			Desc: desc,
		})
	}
	return ui.HelpPanel(entries)
}

func summarizeAgent(commandContext CommandContext) string {
	toolNames := capabilityNames(commandContext.LoopgateStatus.Capabilities)
	return strings.Join([]string{
		fmt.Sprintf("agent: %s v%s", commandContext.Persona.Name, commandContext.Persona.Version),
		fmt.Sprintf("description: %s", commandContext.Persona.Description),
		"control_plane: loopgate",
		"approval_authority: loopgate",
		"policy_source: loopgate",
		fmt.Sprintf("model: %s / %s", commandContext.CurrentRuntimeConfig.ProviderName, commandContext.CurrentRuntimeConfig.ModelName),
		fmt.Sprintf("capabilities_registered: %d", len(toolNames)),
		fmt.Sprintf("capability_names: %s", formatList(toolNames, "none")),
		fmt.Sprintf("treat_model_output_as_untrusted: %t", commandContext.Persona.Trust.TreatModelOutputAsUntrusted),
		fmt.Sprintf("require_validation_before_use: %t", commandContext.Persona.Trust.RequireValidationBeforeUse),
		fmt.Sprintf("deny_by_default: %t", commandContext.Persona.RiskControls.DenyByDefault),
	}, "\n")
}

func summarizeModel(commandContext CommandContext) string {
	lines := []string{
		modelruntime.SummarizeConfig(commandContext.CurrentRuntimeConfig),
		fmt.Sprintf("config_path: %s", modelruntime.ConfigPath(commandContext.RepoRoot)),
		fmt.Sprintf("config_file_present: %t", fileExists(modelruntime.ConfigPath(commandContext.RepoRoot))),
		fmt.Sprintf("env_overrides_active: %s", formatList(activeModelEnvOverrides(), "none")),
	}
	return strings.Join(lines, "\n")
}

func summarizePersona(persona config.Persona) string {
	personalityTraits := []string{
		"helpfulness=" + persona.Personality.Helpfulness,
		"honesty=" + persona.Personality.Honesty,
		"safety_mindset=" + persona.Personality.SafetyMindset,
		"security_mindset=" + persona.Personality.SecurityMindset,
		"directness=" + persona.Personality.Directness,
		"pragmatism=" + persona.Personality.Pragmatism,
		"skepticism=" + persona.Personality.Skepticism,
	}

	lines := []string{
		fmt.Sprintf("name: %s", persona.Name),
		fmt.Sprintf("version: %s", persona.Version),
		fmt.Sprintf("tone: %s", persona.Communication.Tone),
		fmt.Sprintf("values: %s", formatList(persona.Values, "none")),
		fmt.Sprintf("traits: %s", strings.Join(personalityTraits, ", ")),
		fmt.Sprintf("state_unknowns_explicitly: %t", persona.Communication.StateUnknownsExplicitly),
		fmt.Sprintf("avoid_speculation: %t", persona.Communication.AvoidSpeculation),
		fmt.Sprintf("treat_tool_output_as_untrusted: %t", persona.Trust.TreatToolOutputAsUntrusted),
		fmt.Sprintf("risky_behavior_count: %d", len(persona.RiskControls.RiskyBehaviorDefinition)),
		fmt.Sprintf("escalation_trigger_count: %d", len(persona.RiskControls.EscalationTriggers)),
	}
	return strings.Join(lines, "\n")
}

func summarizeSettings(commandContext CommandContext) string {
	lines := []string{
		fmt.Sprintf("repo_root: %s", commandContext.RepoRoot),
		fmt.Sprintf("persona: %s v%s", commandContext.Persona.Name, commandContext.Persona.Version),
		"control_plane: loopgate",
		fmt.Sprintf("model_provider: %s", commandContext.CurrentRuntimeConfig.ProviderName),
		fmt.Sprintf("model_profile: %s", defaultSummaryValue(commandContext.CurrentRuntimeConfig.ProfileName, "default")),
		fmt.Sprintf("model_name: %s", commandContext.CurrentRuntimeConfig.ModelName),
		fmt.Sprintf("filesystem_reads_enabled: %t", commandContext.Policy.Tools.Filesystem.ReadEnabled),
		fmt.Sprintf("filesystem_writes_enabled: %t", commandContext.Policy.Tools.Filesystem.WriteEnabled),
		fmt.Sprintf("filesystem_write_requires_approval: %t", commandContext.Policy.Tools.Filesystem.WriteRequiresApproval),
		fmt.Sprintf("http_enabled: %t", commandContext.Policy.Tools.HTTP.Enabled),
		fmt.Sprintf("shell_enabled: %t", commandContext.Policy.Tools.Shell.Enabled),
		fmt.Sprintf("model_runtime_config_path: %s", modelruntime.ConfigPath(commandContext.RepoRoot)),
	}
	return strings.Join(lines, "\n")
}

func summarizeNetwork(commandContext CommandContext) string {
	modelUsesNetwork := commandContext.CurrentRuntimeConfig.ProviderName != "stub"
	lines := []string{
		"external_http_capabilities: loopgate-only",
		"raw_http_bodies_prompt_eligible: false",
		fmt.Sprintf("http_policy_enabled: %t", commandContext.Policy.Tools.HTTP.Enabled),
		fmt.Sprintf("http_requires_approval: %t", commandContext.Policy.Tools.HTTP.RequiresApproval),
		fmt.Sprintf("http_timeout_seconds: %d", commandContext.Policy.Tools.HTTP.TimeoutSeconds),
		fmt.Sprintf("allowed_domains: %s", formatList(commandContext.Policy.Tools.HTTP.AllowedDomains, "none configured")),
		fmt.Sprintf("shell_enabled: %t", commandContext.Policy.Tools.Shell.Enabled),
		fmt.Sprintf("model_uses_network: %t", modelUsesNetwork),
	}
	if modelUsesNetwork {
		lines = append(lines, fmt.Sprintf("model_base_url: %s", commandContext.CurrentRuntimeConfig.BaseURL))
		if commandContext.CurrentRuntimeConfig.ModelConnectionID != "" {
			lines = append(lines, fmt.Sprintf("model_connection_id: %s", commandContext.CurrentRuntimeConfig.ModelConnectionID))
		} else if commandContext.CurrentRuntimeConfig.APIKeyEnvVar != "" {
			lines = append(lines,
				fmt.Sprintf("legacy_model_api_key_env_var: %s", commandContext.CurrentRuntimeConfig.APIKeyEnvVar),
				"model_secret_storage: legacy runtime env reference (Loopgate remote inference denies this path)",
			)
		} else {
			lines = append(lines, "model_secret_storage: none (loopback no-auth model)")
		}
	}
	if len(capabilityNamesByCategory(commandContext.LoopgateStatus.Capabilities, "http")) == 0 {
		lines = append(lines, "http_tools_registered: none")
	}
	return strings.Join(lines, "\n")
}

func summarizeConnections(commandContext CommandContext) string {
	if len(commandContext.LoopgateStatus.Connections) == 0 {
		return "configured_connections: none"
	}

	lines := []string{fmt.Sprintf("configured_connections: %d", len(commandContext.LoopgateStatus.Connections))}
	for _, connectionStatus := range commandContext.LoopgateStatus.Connections {
		lines = append(lines, fmt.Sprintf(
			"%s/%s: grant_type=%s status=%s scopes=%s secret_ref=%s last_validated_at_utc=%s last_used_at_utc=%s last_rotated_at_utc=%s",
			connectionStatus.Provider,
			defaultSummaryValue(connectionStatus.Subject, "default"),
			connectionStatus.GrantType,
			connectionStatus.Status,
			formatList(connectionStatus.Scopes, "none"),
			connectionStatus.SecureStoreRefID,
			defaultSummaryValue(connectionStatus.LastValidatedAtUTC, "never"),
			defaultSummaryValue(connectionStatus.LastUsedAtUTC, "never"),
			defaultSummaryValue(connectionStatus.LastRotatedAtUTC, "never"),
		))
	}
	return strings.Join(lines, "\n")
}

func summarizeConfigPaths(commandContext CommandContext) string {
	type configPathRecord struct {
		label string
		path  string
	}

	configPaths := []configPathRecord{
		{label: "persona", path: filepath.Join(commandContext.RepoRoot, "persona", "default.yaml")},
		{label: "policy", path: filepath.Join(commandContext.RepoRoot, "core", "policy", "policy.yaml")},
		{label: "loopgate_connections", path: filepath.Join(commandContext.RepoRoot, "loopgate", "connections")},
		{label: "state", path: filepath.Join(commandContext.RepoRoot, "runtime", "state", "working_state.json")},
		{label: "model_runtime", path: modelruntime.ConfigPath(commandContext.RepoRoot)},
		{label: "audit", path: filepath.Join(commandContext.RepoRoot, "runtime", "state", "loopgate_events.jsonl")},
	}
	sandboxPaths := sandbox.PathsForRepo(commandContext.RepoRoot)

	lines := make([]string, 0, len(configPaths)+2)
	for _, configPath := range configPaths {
		lines = append(lines, fmt.Sprintf("%s: %s (%s)", configPath.label, configPath.path, existenceStatus(configPath.path)))
	}
	lines = append(lines, fmt.Sprintf("sandbox_root: %s (%s)", sandboxPaths.Root, existenceStatus(sandboxPaths.Root)))
	lines = append(lines, fmt.Sprintf("sandbox_home: %s (%s)", sandboxPaths.Home, existenceStatus(sandboxPaths.Home)))
	lines = append(lines, "control_plane_socket: loopgate")
	lines = append(lines, "model_provider_secret_storage: Loopgate-owned model connections are preferred; Morph stores only non-secret model runtime metadata locally")
	lines = append(lines, "integration_secret_storage: loopgate-owned refs and backend validation implemented; macOS Keychain backend is active")
	lines = append(lines, fmt.Sprintf("loopgate_socket: %s", filepath.Join(commandContext.RepoRoot, "runtime", "state", "loopgate.sock")))
	return strings.Join(lines, "\n")
}

func summarizeTools(commandContext CommandContext) string {
	if len(commandContext.LoopgateStatus.Capabilities) == 0 {
		return "registered_capabilities: none"
	}

	lines := []string{fmt.Sprintf("registered_capabilities: %d", len(commandContext.LoopgateStatus.Capabilities))}
	for _, capability := range commandContext.LoopgateStatus.Capabilities {
		lines = append(lines, fmt.Sprintf("%s: category=%s operation=%s description=%s", capability.Name, capability.Category, capability.Operation, capability.Description))
	}
	return strings.Join(lines, "\n")
}

func validateModelConfig(loopgateClient loopgate.ControlPlaneClient, runtimeConfig modelruntime.Config) string {
	if loopgateClient == nil {
		return "Denied: loopgate-backed model validation is unavailable."
	}
	validatedConfig, err := loopgateClient.ValidateModelConfig(context.Background(), runtimeConfig)
	if err != nil {
		return "Denied: " + err.Error()
	}
	return strings.Join([]string{
		"model config validated by Loopgate",
		modelruntime.SummarizeConfig(validatedConfig),
	}, "\n")
}

func capabilityNames(capabilities []loopgate.CapabilitySummary) []string {
	names := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		names = append(names, capability.Name)
	}
	return names
}

func capabilityNamesByCategory(capabilities []loopgate.CapabilitySummary, category string) []string {
	names := make([]string, 0)
	for _, capability := range capabilities {
		if capability.Category == category {
			names = append(names, capability.Name)
		}
	}
	return names
}

func activeModelEnvOverrides() []string {
	envNames := []string{
		"MORPH_MODEL_PROVIDER",
		"MORPH_MODEL_NAME",
		"MORPH_MODEL_BASE_URL",
		"MORPH_MODEL_API_KEY_ENV",
		"MORPH_MODEL_TEMPERATURE",
		"MORPH_MODEL_MAX_OUTPUT_TOKENS",
		"MORPH_MODEL_TIMEOUT_SECONDS",
	}
	activeNames := make([]string, 0, len(envNames))
	for _, envName := range envNames {
		if rawValue, found := os.LookupEnv(envName); found && strings.TrimSpace(rawValue) != "" {
			activeNames = append(activeNames, envName)
		}
	}
	sort.Strings(activeNames)
	return activeNames
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func existenceStatus(path string) string {
	if fileExists(path) {
		return "present"
	}
	return "not found"
}

func formatList(values []string, emptyValue string) string {
	if len(values) == 0 {
		return emptyValue
	}
	return strings.Join(values, ", ")
}

func defaultSummaryValue(rawValue string, fallback string) string {
	if strings.TrimSpace(rawValue) == "" {
		return fallback
	}
	return rawValue
}
