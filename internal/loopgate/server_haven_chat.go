package loopgate

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"morph/internal/threadstore"
)

func (server *Server) handleHavenChat(writer http.ResponseWriter, request *http.Request) {
	// Declared before defer so the panic recovery can reach the emitter.
	// After SSE headers are committed (200 OK), http.Error writes raw bytes into
	// the already-open stream body — the client skips them as non-data lines and
	// the turn silently disappears. Emit proper SSE events instead.
	var emitter *havenSSEEmitter
	var diagTenantID, diagUserID, diagControlSessionID string
	defer func() {
		if r := recover(); r != nil {
			// Panic recovery guarantees the control plane fails safely. We emit an SSE error
			// so the user knows the turn failed, rather than leaving the stream indefinitely open.
			// Recovery MUST NOT swallow failures that must fail closed for security — by
			// recovering here, we abort the HTTP request entirely ensure no permissive state or
			// capability token is leaked via a half-completed execution.
			if server.diagnostic != nil && server.diagnostic.Server != nil {
				args := []any{
					"panic", fmt.Sprintf("%v", r),
					"control_session_id", diagControlSessionID,
				}
				args = append(args, diagnosticSlogTenantUser(diagTenantID, diagUserID)...)
				server.diagnostic.Server.Error("haven_chat_panic", args...)
			}
			if emitter != nil {
				emitter.emit(havenSSEEvent{Type: "error", Error: "internal error"})
				emitter.emit(havenSSEEvent{Type: "turn_complete"})
			} else {
				http.Error(writer, "internal error", http.StatusInternalServerError)
			}
		}
	}()

	tokenClaims, ok := server.authenticateHavenChatRequest(writer, request)
	if !ok {
		return
	}
	diagControlSessionID = tokenClaims.ControlSessionID
	diagTenantID = tokenClaims.TenantID
	diagUserID = tokenClaims.UserID
	req, message, ok := server.decodeHavenChatRequest(writer, request, tokenClaims)
	if !ok {
		return
	}

	threadState, ok := server.prepareHavenChatThreadState(writer, tokenClaims, req, message)
	if !ok {
		return
	}

	runtimeState, ok := server.prepareHavenChatRuntimeState(writer, tokenClaims, req)
	if !ok {
		return
	}

	modelCtx, cancelModel := context.WithTimeout(request.Context(), runtimeState.timeoutWindow)
	defer cancelModel()

	// Commit SSE headers before entering the loop. From this point forward the
	// response is streamed; error paths use SSE events rather than JSON bodies.
	emitter = newHavenSSEEmitter(writer)
	if emitter == nil {
		// net/http's ResponseWriter always implements Flusher — this path is a
		// safety guard only (e.g. if a test proxy strips it).
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "streaming not supported by this transport",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}
	if server.diagnostic != nil && server.diagnostic.Server != nil {
		args := []any{
			"thread_id", threadState.threadID,
			"control_session_id", tokenClaims.ControlSessionID,
		}
		args = append(args, diagnosticSlogTenantUser(tokenClaims.TenantID, tokenClaims.UserID)...)
		server.diagnostic.Server.Debug("haven_chat_sse_stream_start", args...)
	}

	havenChatWallStart := time.Now()
	chatRuntime := newHavenChatRuntime(server)
	loopOutcome := chatRuntime.runToolLoop(
		modelCtx,
		runtimeState.modelClient,
		tokenClaims,
		runtimeState.persona,
		runtimeState.wakeText,
		threadState.windowedConversation,
		message,
		runtimeState.modelAttachments,
		runtimeState.availableToolDefs,
		runtimeState.nativeToolDefs,
		runtimeState.runtimeFacts,
		runtimeState.hostFolderOrganizeToolkitAvailable,
		emitter,
	)
	havenChatWallMs := time.Since(havenChatWallStart).Milliseconds()

	if loopOutcome.err != nil {
		_ = server.logEvent("haven.chat", tokenClaims.ControlSessionID, map[string]interface{}{
			"thread_id":          threadState.threadID,
			"workspace_id":       threadState.workspaceID,
			"control_session_id": tokenClaims.ControlSessionID,
			"haven_chat_wall_ms": havenChatWallMs,
			"error":              loopOutcome.err.Error(),
		})
		fallbackText := havenChatFallbackText(loopOutcome.err)
		_ = threadState.store.AppendEvent(threadState.threadID, threadstore.ConversationEvent{
			Type: threadstore.EventAssistantMessage,
			Data: map[string]interface{}{"text": fallbackText},
		})
		// The loop did not emit a text_delta for error paths — emit the fallback
		// text now so the client always receives a visible message.
		emitter.emit(havenSSEEvent{Type: "text_delta", Content: fallbackText})
		emitter.emit(havenSSEEvent{Type: "turn_complete", ThreadID: threadState.threadID})
		return
	}

	if err := threadState.store.AppendEvent(threadState.threadID, threadstore.ConversationEvent{
		Type: threadstore.EventAssistantMessage,
		Data: map[string]interface{}{"text": loopOutcome.assistantText},
	}); err != nil {
		// Persistence failed after the loop already emitted its events. Emit an
		// error event so the client knows the turn did not complete cleanly.
		emitter.emit(havenSSEEvent{
			Type:     "error",
			Error:    "cannot persist assistant message",
			ThreadID: threadState.threadID,
		})
		return
	}

	logPayload := map[string]interface{}{
		"thread_id":          threadState.threadID,
		"workspace_id":       threadState.workspaceID,
		"provider":           loopOutcome.modelResponse.ProviderName,
		"model":              loopOutcome.modelResponse.ModelName,
		"finish_reason":      loopOutcome.modelResponse.FinishReason,
		"input_tokens":       loopOutcome.modelResponse.Usage.InputTokens,
		"output_tokens":      loopOutcome.modelResponse.Usage.OutputTokens,
		"control_session_id": tokenClaims.ControlSessionID,
		"haven_chat_wall_ms": havenChatWallMs,
	}
	if strings.TrimSpace(loopOutcome.approvalStatus) != "" {
		logPayload["chat_status"] = loopOutcome.approvalStatus
	}
	_ = server.logEvent("haven.chat", tokenClaims.ControlSessionID, logPayload)

	// turn_complete closes the turn. The loop already emitted all text_delta,
	// tool_start, tool_result, and approval_needed events; this is the final
	// bookkeeping event the client uses to capture threadID, uxSignals, and
	// token counts.
	emitter.emit(havenSSEEvent{
		Type:         "turn_complete",
		ThreadID:     threadState.threadID,
		UXSignals:    loopOutcome.uxSignals,
		FinishReason: loopOutcome.modelResponse.FinishReason,
		InputTokens:  loopOutcome.modelResponse.Usage.InputTokens,
		OutputTokens: loopOutcome.modelResponse.Usage.OutputTokens,
		TotalTokens:  loopOutcome.modelResponse.Usage.TotalTokens,
		ProviderName: loopOutcome.modelResponse.ProviderName,
		ModelName:    loopOutcome.modelResponse.ModelName,
	})
	if server.diagnostic != nil && server.diagnostic.Server != nil {
		args := []any{
			"thread_id", threadState.threadID,
			"control_session_id", tokenClaims.ControlSessionID,
			"haven_chat_wall_ms", havenChatWallMs,
		}
		args = append(args, diagnosticSlogTenantUser(tokenClaims.TenantID, tokenClaims.UserID)...)
		server.diagnostic.Server.Debug("haven_chat_sse_stream_done", args...)
	}
}
