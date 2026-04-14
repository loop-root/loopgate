package loopgate

import (
	"morph/internal/config"
	modelpkg "morph/internal/model"
	modelruntime "morph/internal/modelruntime"
)

type havenChatToolState struct {
	availableToolDefs                  []modelpkg.ToolDefinition
	nativeToolDefs                     []modelpkg.NativeToolDef
	runtimeFacts                       []string
	hostFolderOrganizeToolkitAvailable bool
}

func (server *Server) buildHavenChatToolState(tokenClaims capabilityToken, req havenChatRequest, runtimeConfig modelruntime.Config) havenChatToolState {
	allowedCapabilitySummaries := filterHavenCapabilitySummaries(server.capabilitySummaries(), tokenClaims.AllowedCapabilities)
	policyRuntime := server.currentPolicyRuntime()
	if shellDevEnabled, err := config.IsShellDevModeEnabled(server.repoRoot); err == nil && !shellDevEnabled {
		allowedCapabilitySummaries = havenFilterOutCapability(allowedCapabilitySummaries, "shell_exec")
	}

	availableToolDefs := buildHavenToolDefinitions(allowedCapabilitySummaries)
	nativeToolDefs := modelpkg.BuildNativeToolDefsForAllowedNamesWithOptions(policyRuntime.registry, capabilityNamesFromSummaries(allowedCapabilitySummaries), modelpkg.NativeToolDefBuildOptions{
		HavenUserIntentGuards: true,
		CompactNativeTools:    useCompactHavenNativeTools,
	})
	if useCompactHavenNativeTools {
		availableToolDefs = buildCompactInvokeCapabilityToolDefinitions(capabilityNamesFromSummaries(allowedCapabilitySummaries))
	}

	runtimeFacts := server.buildHavenRuntimeFacts(allowedCapabilitySummaries, runtimeConfig.ProviderName, runtimeConfig.ModelName, req.ProjectPath, req.ProjectName, req.GitBranch, req.AdditionalPaths)
	allowedCapabilityNames := make(map[string]struct{}, len(allowedCapabilitySummaries))
	for _, summary := range allowedCapabilitySummaries {
		allowedCapabilityNames[summary.Name] = struct{}{}
	}

	return havenChatToolState{
		availableToolDefs:                  availableToolDefs,
		nativeToolDefs:                     nativeToolDefs,
		runtimeFacts:                       runtimeFacts,
		hostFolderOrganizeToolkitAvailable: hasAllHavenCapabilities(allowedCapabilityNames, "host.folder.list", "host.folder.read", "host.organize.plan", "host.plan.apply"),
	}
}
