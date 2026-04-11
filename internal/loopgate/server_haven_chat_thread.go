package loopgate

import (
	"net/http"
	"path/filepath"
	"strings"

	modelpkg "morph/internal/model"
	"morph/internal/threadstore"
)

type havenChatThreadState struct {
	workspaceID          string
	store                *threadstore.Store
	threadID             string
	windowedConversation []modelpkg.ConversationTurn
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
