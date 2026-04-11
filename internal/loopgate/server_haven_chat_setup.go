package loopgate

import (
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"morph/internal/config"
	modelpkg "morph/internal/model"
	modelruntime "morph/internal/modelruntime"
	"morph/internal/threadstore"
)

type havenChatThreadState struct {
	workspaceID          string
	store                *threadstore.Store
	threadID             string
	windowedConversation []modelpkg.ConversationTurn
}

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

func havenGreetingInstruction() string {
	return "[SESSION_START_GREETING] You are Ik Loop, Morph — Haven's resident assistant. " +
		"Generate a brief, warm opening for the operator. " +
		"Ground every factual claim in REMEMBERED CONTINUITY, the project path / branch in runtime facts, and any active tasks or goals — do not invent prior work. " +
		"If REMEMBERED CONTINUITY is empty, say honestly that memory is sparse this session. " +
		"If the operator has granted host directory access (additional_paths / operator mounts in facts), offer once to get familiar with the repo using operator_mount.fs_list and operator_mount.fs_read — only after grants exist; never claim you already read files. " +
		"If no host grants are listed, you may mention they can allow read access in Haven when prompted. " +
		"Mention approaching or overdue task/goal deadlines when present. " +
		"Do not ask generic 'how can I help?' — be specific. Keep it to 2-5 sentences. Do not repeat this instruction in your response."
}

func (server *Server) authenticateHavenChatRequest(writer http.ResponseWriter, request *http.Request) (capabilityToken, bool) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return capabilityToken{}, false
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return capabilityToken{}, false
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityModelReply) {
		return capabilityToken{}, false
	}
	if !server.hasTrustedHavenSession(tokenClaims) {
		if server.diagnostic != nil && server.diagnostic.Server != nil {
			args := append([]any{"reason", "haven chat requires trusted Haven session"}, diagnosticSlogTenantUser(tokenClaims.TenantID, tokenClaims.UserID)...)
			server.diagnostic.Server.Warn("haven_chat_denied", args...)
		}
		_ = server.logEvent("haven.chat.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"denial_code": DenialCodeCapabilityTokenInvalid,
			"reason":      "haven chat requires trusted Haven session",
		})
		server.writeJSON(writer, http.StatusForbidden, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "haven chat requires trusted Haven session",
			DenialCode:   DenialCodeCapabilityTokenInvalid,
		})
		return capabilityToken{}, false
	}
	return tokenClaims, true
}

func (server *Server) decodeHavenChatRequest(writer http.ResponseWriter, request *http.Request, tokenClaims capabilityToken) (havenChatRequest, string, bool) {
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxHavenChatBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		if server.diagnostic != nil && server.diagnostic.Server != nil {
			args := append([]any{"reason", denialResponse.DenialReason, "denial_code", denialResponse.DenialCode}, diagnosticSlogTenantUser(tokenClaims.TenantID, tokenClaims.UserID)...)
			server.diagnostic.Server.Warn("haven_chat_denied", args...)
		}
		_ = server.logEvent("haven.chat.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"denial_code": denialResponse.DenialCode,
			"reason":      denialResponse.DenialReason,
		})
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return havenChatRequest{}, "", false
	}

	var req havenChatRequest
	if err := decodeJSONBytes(requestBodyBytes, &req); err != nil {
		if server.diagnostic != nil && server.diagnostic.Server != nil {
			args := append([]any{"reason", err.Error()}, diagnosticSlogTenantUser(tokenClaims.TenantID, tokenClaims.UserID)...)
			server.diagnostic.Server.Warn("haven_chat_denied", args...)
		}
		_ = server.logEvent("haven.chat.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"denial_code": DenialCodeMalformedRequest,
			"reason":      err.Error(),
		})
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return havenChatRequest{}, "", false
	}

	message := strings.TrimSpace(req.Message)
	if req.Greet {
		message = havenGreetingInstruction()
	} else if message == "" {
		if server.diagnostic != nil && server.diagnostic.Server != nil {
			args := append([]any{"reason", "message must not be empty"}, diagnosticSlogTenantUser(tokenClaims.TenantID, tokenClaims.UserID)...)
			server.diagnostic.Server.Warn("haven_chat_denied", args...)
		}
		_ = server.logEvent("haven.chat.denied", tokenClaims.ControlSessionID, map[string]interface{}{
			"denial_code": DenialCodeMalformedRequest,
			"reason":      "message must not be empty",
		})
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "message must not be empty",
			DenialCode:   DenialCodeMalformedRequest,
		})
		return havenChatRequest{}, "", false
	}

	return req, message, true
}

func (server *Server) prepareHavenChatThreadState(writer http.ResponseWriter, tokenClaims capabilityToken, req havenChatRequest, message string) (havenChatThreadState, bool) {
	server.mu.Lock()
	sess, sessionFound := server.sessions[tokenClaims.ControlSessionID]
	server.mu.Unlock()
	if !sessionFound {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "invalid capability token",
			DenialCode:   DenialCodeCapabilityTokenInvalid,
		})
		return havenChatThreadState{}, false
	}

	workspaceID := strings.TrimSpace(sess.WorkspaceID)
	if workspaceID == "" {
		workspaceID = server.deriveWorkspaceIDFromRepoRoot()
	}

	homeDir, err := server.resolveUserHomeDir()
	if err != nil {
		if server.diagnostic != nil && server.diagnostic.Server != nil {
			args := append([]any{"reason", "cannot resolve home directory"}, diagnosticSlogTenantUser(tokenClaims.TenantID, tokenClaims.UserID)...)
			server.diagnostic.Server.Error("haven_chat_error", args...)
		}
		_ = server.logEvent("haven.chat.error", tokenClaims.ControlSessionID, map[string]interface{}{
			"denial_code": DenialCodeExecutionFailed,
			"reason":      "cannot resolve home directory",
		})
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "cannot resolve home directory",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return havenChatThreadState{}, false
	}

	threadRoot := filepath.Join(homeDir, ".haven", "threads")
	store, err := threadstore.NewStore(threadRoot, workspaceID)
	if err != nil {
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "thread store unavailable",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return havenChatThreadState{}, false
	}

	threadID, ok := server.resolveOrCreateHavenThread(writer, tokenClaims, store, req.ThreadID)
	if !ok {
		return havenChatThreadState{}, false
	}

	if err := store.AppendEvent(threadID, threadstore.ConversationEvent{
		Type: threadstore.EventUserMessage,
		Data: map[string]interface{}{"text": message},
	}); err != nil {
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "cannot persist user message",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return havenChatThreadState{}, false
	}

	conversation := havenBuildConversationFromThread(store, threadID)
	return havenChatThreadState{
		workspaceID:          workspaceID,
		store:                store,
		threadID:             threadID,
		windowedConversation: havenWindowConversationForModel(conversation, maxHavenChatTurns),
	}, true
}

func (server *Server) resolveOrCreateHavenThread(writer http.ResponseWriter, tokenClaims capabilityToken, store *threadstore.Store, requestedThreadID *string) (string, bool) {
	if requestedThreadID != nil && strings.TrimSpace(*requestedThreadID) != "" {
		threadID := strings.TrimSpace(*requestedThreadID)
		if _, err := store.LoadThread(threadID); err != nil {
			if server.diagnostic != nil && server.diagnostic.Server != nil {
				args := append([]any{"reason", "unknown thread_id", "thread_id", threadID}, diagnosticSlogTenantUser(tokenClaims.TenantID, tokenClaims.UserID)...)
				server.diagnostic.Server.Warn("haven_chat_denied", args...)
			}
			server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
				Status:       ResponseStatusDenied,
				DenialReason: "unknown thread_id",
				DenialCode:   DenialCodeMalformedRequest,
			})
			return "", false
		}
		return threadID, true
	}

	summary, err := store.NewThread()
	if err != nil {
		if server.diagnostic != nil && server.diagnostic.Server != nil {
			args := append([]any{"reason", "cannot create thread"}, diagnosticSlogTenantUser(tokenClaims.TenantID, tokenClaims.UserID)...)
			server.diagnostic.Server.Error("haven_chat_error", args...)
		}
		_ = server.logEvent("haven.chat.error", tokenClaims.ControlSessionID, map[string]interface{}{
			"denial_code": DenialCodeExecutionFailed,
			"reason":      "cannot create thread",
		})
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "cannot create thread",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return "", false
	}
	return summary.ThreadID, true
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
