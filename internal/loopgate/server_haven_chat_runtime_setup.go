package loopgate

import (
	"net/http"
	"strings"
	"time"

	"morph/internal/config"
	modelpkg "morph/internal/model"
	modelruntime "morph/internal/modelruntime"
)

type havenChatRuntimeState struct {
	persona                            config.Persona
	runtimeConfig                      modelruntime.Config
	modelClient                        *modelpkg.Client
	wakeText                           string
	modelAttachments                   []modelpkg.Attachment
	availableToolDefs                  []modelpkg.ToolDefinition
	nativeToolDefs                     []modelpkg.NativeToolDef
	runtimeFacts                       []string
	hostFolderOrganizeToolkitAvailable bool
	timeoutWindow                      time.Duration
}

func (server *Server) prepareHavenChatRuntimeState(writer http.ResponseWriter, tokenClaims capabilityToken, req havenChatRequest) (havenChatRuntimeState, bool) {
	persona, err := config.LoadPersona(server.repoRoot)
	if err != nil {
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "persona unavailable",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return havenChatRuntimeState{}, false
	}

	runtimeConfig, err := modelruntime.LoadConfig(server.repoRoot)
	if err != nil {
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "load model runtime config: " + err.Error(),
			DenialCode:   DenialCodeExecutionFailed,
		})
		return havenChatRuntimeState{}, false
	}

	modelClient, _, err := server.newModelClientFromConfig(runtimeConfig)
	if err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "initialize model runtime: " + err.Error(),
			DenialCode:   DenialCodeExecutionFailed,
		})
		return havenChatRuntimeState{}, false
	}

	wakeText, err := server.havenWakeStateSummaryText(tokenClaims.TenantID)
	if err != nil {
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "wake-state backend is unavailable",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return havenChatRuntimeState{}, false
	}

	allowedCapabilitySummaries := filterHavenCapabilitySummaries(server.capabilitySummaries(), tokenClaims.AllowedCapabilities)
	if shellDevEnabled, err := config.IsShellDevModeEnabled(server.repoRoot); err == nil && !shellDevEnabled {
		allowedCapabilitySummaries = havenFilterOutCapability(allowedCapabilitySummaries, "shell_exec")
	}

	availableToolDefs := buildHavenToolDefinitions(allowedCapabilitySummaries)
	nativeToolDefs := modelpkg.BuildNativeToolDefsForAllowedNamesWithOptions(server.registry, capabilityNamesFromSummaries(allowedCapabilitySummaries), modelpkg.NativeToolDefBuildOptions{
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

	return havenChatRuntimeState{
		persona:                            persona,
		runtimeConfig:                      runtimeConfig,
		modelClient:                        modelClient,
		wakeText:                           wakeText,
		modelAttachments:                   havenChatAttachmentsFromRequest(req.Attachments),
		availableToolDefs:                  availableToolDefs,
		nativeToolDefs:                     nativeToolDefs,
		runtimeFacts:                       runtimeFacts,
		hostFolderOrganizeToolkitAvailable: hasAllHavenCapabilities(allowedCapabilityNames, "host.folder.list", "host.folder.read", "host.organize.plan", "host.plan.apply"),
		timeoutWindow:                      havenChatTimeoutWindow(runtimeConfig),
	}, true
}

func havenChatAttachmentsFromRequest(attachments []havenChatAttachment) []modelpkg.Attachment {
	modelAttachments := make([]modelpkg.Attachment, 0, len(attachments))
	for _, attachment := range attachments {
		if strings.TrimSpace(attachment.Name) == "" || strings.TrimSpace(attachment.MimeType) == "" || strings.TrimSpace(attachment.Data) == "" {
			continue
		}
		modelAttachments = append(modelAttachments, modelpkg.Attachment{
			Name:     strings.TrimSpace(attachment.Name),
			MimeType: strings.ToLower(strings.TrimSpace(attachment.MimeType)),
			Data:     strings.TrimSpace(attachment.Data),
		})
	}
	return modelAttachments
}

func havenChatTimeoutWindow(runtimeConfig modelruntime.Config) time.Duration {
	timeoutWindow := 60 * time.Second
	if runtimeConfig.ProviderName == "openai_compatible" || modelruntime.IsLoopbackModelBaseURL(runtimeConfig.BaseURL) {
		timeoutWindow = 5 * time.Minute
	}
	return timeoutWindow
}
