package loopgate

import modelpkg "morph/internal/model"

func (server *Server) buildHavenRuntimeFacts(capabilitySummaries []CapabilitySummary, providerName string, modelName string, projectPath string, projectName string, gitBranch string, additionalPaths []string) []string {
	currentTime := server.now()
	capabilitySummaryText := buildResidentCapabilitySummary(capabilitySummaries)
	runtimeFacts := buildHavenBaseRuntimeFacts(currentTime, providerName, modelName)
	runtimeFacts = append(runtimeFacts, buildHavenProjectContextFacts(projectPath, projectName, gitBranch, additionalPaths)...)
	if capabilitySummaryText != "" {
		runtimeFacts = append(runtimeFacts, "Describe your current built-in abilities in product language. Right now that includes: "+capabilitySummaryText+".")
	}
	runtimeFacts = append(runtimeFacts, buildResidentCapabilityFacts(capabilitySummaries)...)
	if useCompactHavenNativeTools {
		runtimeFacts = append(runtimeFacts,
			"Haven native tool-use API exposes only invoke_capability. Each call must include: (1) capability — exact registry id (e.g. host.folder.list, fs_read); (2) arguments_json — a string containing one JSON object whose keys match that tool's parameters. Example host.folder.list: arguments_json '{\"folder_name\":\"downloads\",\"path\":\".\"}'. Do not omit arguments_json.",
			modelpkg.HavenCompactNativeDispatchRuntimeFact,
		)
	}
	runtimeFacts = append(runtimeFacts, modelpkg.HavenConstrainedNativeToolsRuntimeFact)
	return runtimeFacts
}
