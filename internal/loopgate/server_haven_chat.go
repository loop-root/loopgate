package loopgate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"morph/internal/config"
	modelpkg "morph/internal/model"
	"morph/internal/orchestrator"
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
	loopOutcome := server.runHavenChatToolLoop(
		modelCtx,
		runtimeState.modelClient,
		threadState.store,
		threadState.threadID,
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

func (server *Server) deriveWorkspaceIDFromRepoRoot() string {
	abs, err := filepath.Abs(server.repoRoot)
	if err != nil {
		abs = server.repoRoot
	}
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:])
}

func (server *Server) havenWakeStateSummaryText(tenantID string) (string, error) {
	wake, err := server.loadMemoryWakeState(tenantID)
	if err != nil {
		return "", err
	}
	if len(wake.RecentFacts) == 0 && len(wake.ActiveGoals) == 0 && len(wake.UnresolvedItems) == 0 {
		return "", nil
	}

	var sb strings.Builder

	// Remembered facts (operator preferences, profile, routines).
	var factParts []string
	for _, f := range wake.RecentFacts {
		if len(factParts) >= 12 {
			break
		}
		factParts = append(factParts, fmt.Sprintf("  %s: %v", f.Name, f.Value))
	}
	if len(factParts) > 0 {
		sb.WriteString("REMEMBERED CONTINUITY (authoritative):\n")
		sb.WriteString(strings.Join(factParts, "\n"))
	} else {
		sb.WriteString("Wake-state is loaded with continuity metadata.")
	}

	// Active goals — persist across sessions.
	if len(wake.ActiveGoals) > 0 {
		sb.WriteString("\n\nACTIVE GOALS:\n")
		for _, g := range wake.ActiveGoals {
			sb.WriteString(fmt.Sprintf("  - %s\n", g))
		}
	}

	// Open tasks — the live task board the operator can track.
	if len(wake.UnresolvedItems) > 0 {
		sb.WriteString("\n\nOPEN TASKS (task board — use todo.list to get IDs, todo.complete to close):\n")
		for _, item := range wake.UnresolvedItems {
			line := fmt.Sprintf("  [%s] %s", item.ID, item.Text)
			if item.ScheduledForUTC != "" {
				line += fmt.Sprintf(" (due: %s)", item.ScheduledForUTC)
			}
			if item.NextStep != "" {
				line += fmt.Sprintf(" — next: %s", item.NextStep)
			}
			sb.WriteString(line + "\n")
		}
	}

	return sb.String(), nil
}

// runHavenChatToolLoop is Loopgate's supervised agent runtime for Haven turns.
//
// Runtime model (Loopgate terms):
//   - Morph (the resident assistant) operates within a bounded iteration budget
//     (maxHavenToolIterations). Loopgate owns the continuation decision; the model
//     only decides whether to call capabilities or return text within each iteration.
//   - Capability dispatch is policy-gated at every iteration via executeCapabilityRequest.
//     The model cannot bypass Loopgate authority by embedding capability names in text.
//   - Read-only capabilities may execute concurrently within a single iteration batch
//     (see executeHavenToolCallsConcurrent). Write and execute capabilities are always
//     dispatched serially to preserve observable ordering of side effects.
//   - All side-effect capabilities that require approval are held at the approval
//     boundary; the loop does not continue past a pending_approval result.
//
// Checkpoint gap (known limitation):
//
//	The conversation accumulates in memory across iterations. If the HTTP handler is
//	cancelled mid-loop (ctx cancellation, client disconnect, server restart) all
//	in-flight tool results are lost. A durable checkpoint would write each completed
//	tool-result turn to the threadstore before advancing to the next model call.
//	This is not yet implemented; recovery requires re-sending the user message.
//
// TODO(checkpoint): write completed tool-result turns to store after each iteration
// so that a cancelled or crashed request can be resumed without data loss.
func (server *Server) runHavenChatToolLoop(
	ctx context.Context,
	modelClient *modelpkg.Client,
	store *threadstore.Store,
	threadID string,
	tokenClaims capabilityToken,
	persona config.Persona,
	wakeState string,
	conversation []modelpkg.ConversationTurn,
	initialUserMessage string,
	initialAttachments []modelpkg.Attachment,
	availableTools []modelpkg.ToolDefinition,
	nativeToolDefs []modelpkg.NativeToolDef,
	baseRuntimeFacts []string,
	hostFolderOrganizeToolkitAvailable bool,
	emitter *havenSSEEmitter,
) havenChatLoopOutcome {
	userMessage := initialUserMessage
	var lastModelResponse modelpkg.Response
	var uxSignals []string
	proseOnlyHostFolderNudges := 0
	hostPlanApplyNudgeCount := 0
	sawHostFolderToolRound := false
	awaitingHostPlanApply := false

	for iteration := 0; iteration < maxHavenToolIterations; iteration++ {
		if ctx.Err() != nil {
			return havenChatLoopOutcome{err: ctx.Err()}
		}

		// On iteration 0 send the full capability catalog so the model knows what tools exist.
		// On subsequent iterations send only the lightweight set — the model already has tool
		// results in context and re-sending 2 000+ tokens of descriptions bloats input tokens
		// and adds measurable latency on every follow-up model call.
		var turnRuntimeFacts []string
		if iteration == 0 {
			turnRuntimeFacts = append([]string(nil), baseRuntimeFacts...)
		} else {
			turnRuntimeFacts = []string{
				havenToolLoopContinuationFact,
				modelpkg.HavenConstrainedNativeToolsRuntimeFact,
			}
			// Slim organize nudge is aggressive; inject it only while this request is already
			// executing host-folder tools or waiting on plan→apply — not whenever the toolkit exists.
			if hostFolderOrganizeToolkitAvailable && (sawHostFolderToolRound || awaitingHostPlanApply) {
				turnRuntimeFacts = append(turnRuntimeFacts, havenToolLoopSlimOrganizeFact)
			}
		}

		modelUserMessage := userMessage
		if iteration > 0 && strings.TrimSpace(modelUserMessage) == "" {
			modelUserMessage = havenToolFollowupUserNudge
		}

		windowedConversation := havenWindowConversationForModel(conversation, maxHavenChatTurns)
		// Attachments only apply to the first iteration — subsequent tool-loop iterations
		// are model↔tool exchanges that do not include the original user payload.
		var turnAttachments []modelpkg.Attachment
		if iteration == 0 {
			turnAttachments = initialAttachments
		}
		modelResponse, modelErr := modelClient.Reply(ctx, modelpkg.Request{
			Persona:        persona,
			Policy:         server.policy,
			SessionID:      tokenClaims.ControlSessionID,
			WakeState:      wakeState,
			Conversation:   windowedConversation,
			UserMessage:    modelUserMessage,
			Attachments:    turnAttachments,
			AvailableTools: availableTools,
			NativeToolDefs: nativeToolDefs,
			RuntimeFacts:   turnRuntimeFacts,
		})
		if modelErr != nil {
			return havenChatLoopOutcome{err: modelErr}
		}
		lastModelResponse = modelResponse
		replyText := strings.TrimSpace(modelResponse.AssistantText)
		if havenIsNonUserFacingAssistantPlaceholder(replyText) {
			replyText = ""
		}

		// If the model hit its output token limit the response is truncated — any
		// tool call JSON or plan_json will be malformed. Retrying with the same
		// (now longer) context only makes things worse and burns 40–50 s per
		// iteration. Return whatever text we have and let the user know.
		if strings.EqualFold(strings.TrimSpace(modelResponse.FinishReason), "max_tokens") ||
			strings.EqualFold(strings.TrimSpace(modelResponse.FinishReason), "length") {
			if replyText == "" {
				replyText = "My response was cut off — the output limit was reached. Try a shorter or more specific request."
			}
			emitter.emit(havenSSEEvent{Type: "text_delta", Content: replyText})
			return havenChatLoopOutcome{modelResponse: lastModelResponse, assistantText: replyText, uxSignals: uxSignals}
		}

		if userMessage != "" {
			conversation = append(conversation, modelpkg.ConversationTurn{
				Role:      "user",
				Content:   userMessage,
				Timestamp: threadstore.NowUTC(),
			})
		}

		useStructuredPath := len(modelResponse.ToolUseBlocks) > 0 && server.registry != nil
		var parsedCalls []orchestrator.ToolCall
		var validationErrors []orchestrator.ToolCallValidationError
		if useStructuredPath {
			parsedCalls, validationErrors = orchestrator.ExtractStructuredCalls(modelResponse.ToolUseBlocks, server.registry)
		} else {
			parser := orchestrator.NewParser()
			parser.Registry = server.registry
			parsedOutput := parser.Parse(replyText)
			parsedCalls = parsedOutput.Calls
		}

		if len(parsedCalls) == 0 && len(validationErrors) == 0 {
			if replyText == "" {
				replyText = "I didn't get a clear reply from the model. Try again in a moment."
			}
			// host.organize.plan does not enqueue Loopgate approval; host.plan.apply does. After any
			// host.* tool, sawHostFolderToolRound blocks the generic prose nudge — so without this,
			// the model can stop after organize.plan with "waiting for Loopgate" and Haven never
			// receives approval_required.
			if awaitingHostPlanApply &&
				hostFolderOrganizeToolkitAvailable &&
				hostPlanApplyNudgeCount < maxHavenHostPlanApplyNudges &&
				iteration+1 < maxHavenToolIterations {
				hostPlanApplyNudgeCount++
				conversation = append(conversation, modelpkg.ConversationTurn{
					Role:      "assistant",
					Content:   replyText,
					Timestamp: threadstore.NowUTC(),
				})
				userMessage = havenHostPlanApplyActNowNudge
				continue
			}
			if hostFolderOrganizeToolkitAvailable &&
				!sawHostFolderToolRound &&
				havenHostFolderProseNudgeApplies(initialUserMessage, conversation) &&
				proseOnlyHostFolderNudges < maxHavenHostFolderProseOnlyNudges &&
				iteration+1 < maxHavenToolIterations {
				proseOnlyHostFolderNudges++
				conversation = append(conversation, modelpkg.ConversationTurn{
					Role:      "assistant",
					Content:   replyText,
					Timestamp: threadstore.NowUTC(),
				})
				// If the thread already has a prior assistant turn, the folder has
				// been listed — push toward organize.plan. Otherwise push toward list.
				if havenThreadHasPriorAssistantWork(conversation) {
					userMessage = havenHostFolderPlanNowNudge
				} else {
					userMessage = havenHostFolderActNowNudge
				}
				continue
			}
			// Final return from this iteration — emit text now that we know no tool
			// calls follow (nudge continuations above would have called `continue`).
			emitter.emit(havenSSEEvent{Type: "text_delta", Content: replyText})
			return havenChatLoopOutcome{modelResponse: lastModelResponse, assistantText: replyText, uxSignals: uxSignals}
		}

		assistantTurn := modelpkg.ConversationTurn{
			Role:      "assistant",
			Content:   replyText,
			Timestamp: threadstore.NowUTC(),
		}
		if useStructuredPath {
			assistantTurn.ToolCalls = modelResponse.ToolUseBlocks
		}
		conversation = append(conversation, assistantTurn)

		if len(parsedCalls) == 0 && len(validationErrors) > 0 {
			conversation = append(conversation, havenStructuredValidationErrorTurn(validationErrors))
			userMessage = ""
			continue
		}

		// Emit tool_start for each valid call before execution so the client can
		// show progress immediately. Validation-error calls are not included here
		// because they were never dispatched — their tool_result would be orphaned.
		for _, call := range parsedCalls {
			emitter.emit(havenSSEEvent{Type: "tool_start", ToolCall: &havenSSEToolCall{
				CallID: call.ID,
				Name:   call.Name,
			}})
		}

		// executeHavenToolCallsConcurrent fans out read-only tools in parallel
		// and runs write/unknown tools serially. It also emits tool_result SSE
		// events inline as each tool finishes, so the operator sees live progress
		// rather than a single batch after all tools complete.
		toolResults := server.executeHavenToolCallsConcurrent(ctx, tokenClaims, parsedCalls, emitter)

		for _, validationError := range validationErrors {
			toolResults = append(toolResults, orchestrator.ToolResult{
				CallID:     validationError.BlockID,
				Capability: validationError.BlockName,
				Status:     orchestrator.StatusError,
				Output:     "Tool call rejected: " + validationError.Error() + ". Check the tool name and required arguments, then try again.",
			})
		}
		for _, parsedCall := range parsedCalls {
			if strings.HasPrefix(strings.TrimSpace(parsedCall.Name), "host.") {
				sawHostFolderToolRound = true
				break
			}
		}

		for _, tr := range toolResults {
			if tr.Status == orchestrator.StatusSuccess && tr.Capability == "host.plan.apply" {
				havenAccumulateUXSignal(&uxSignals, havenUXSignalHostOrganizeApplied)
			}
		}

		for _, tr := range toolResults {
			capName := strings.TrimSpace(tr.Capability)
			if capName == "host.organize.plan" && tr.Status == orchestrator.StatusSuccess {
				awaitingHostPlanApply = true
			}
			if capName == "host.plan.apply" {
				awaitingHostPlanApply = false
			}
		}

		// Auto-apply: when host.organize.plan just returned a plan_id, fire host.plan.apply
		// immediately without a model round-trip. This saves one full sequential model call
		// (~2–5 s) every time the organize flow completes successfully.
		// The pending-approval check below handles the resulting approval_required response
		// exactly as it would if the model had called host.plan.apply itself.
		if hostFolderOrganizeToolkitAvailable && awaitingHostPlanApply && ctx.Err() == nil {
			if planID := havenExtractOrganizePlanIDFromResults(toolResults); planID != "" {
				awaitingHostPlanApply = false
				autoCallID := "loopgate-auto-apply-" + planID
				emitter.emit(havenSSEEvent{Type: "tool_start", ToolCall: &havenSSEToolCall{
					CallID: autoCallID,
					Name:   "host.plan.apply",
				}})
				autoResults := server.executeHavenToolCalls(ctx, tokenClaims, []orchestrator.ToolCall{{
					ID:   autoCallID,
					Name: "host.plan.apply",
					Args: map[string]string{"plan_id": planID},
				}})
				for _, tr := range autoResults {
					if tr.Status == orchestrator.StatusSuccess {
						havenAccumulateUXSignal(&uxSignals, havenUXSignalHostOrganizeApplied)
					}
					emitter.emit(havenSSEEvent{Type: "tool_result", ToolResult: &havenSSEToolResult{
						CallID:  tr.CallID,
						Preview: havenSSEPreviewForToolResult(tr),
						Status:  string(tr.Status),
					}})
				}
				toolResults = append(toolResults, autoResults...)
			}
		}

		if pending := firstHavenPendingApprovalToolResult(toolResults); pending != nil {
			if havenCapabilityNeedsHostOrganizeApprovalUX(pending.Capability) {
				havenAccumulateUXSignal(&uxSignals, havenUXSignalHostOrganizeApprovalPending)
			}
			assistantText := havenAssistantTextWaitingForLoopgate(replyText)
			// Emit the approval preamble as text_delta. If the model produced prose
			// this iteration (replyText non-empty) it was NOT emitted earlier — tool
			// iterations hold text until the outcome is known. Emit the full combined
			// assistantText here so the client never receives a partial message.
			emitter.emit(havenSSEEvent{Type: "text_delta", Content: assistantText})
			emitter.emit(havenSSEEvent{Type: "approval_needed", ApprovalNeeded: &havenSSEApproval{
				ApprovalID: pending.ApprovalRequestID,
				Capability: pending.Capability,
			}})
			return havenChatLoopOutcome{
				modelResponse:      lastModelResponse,
				assistantText:      assistantText,
				approvalStatus:     "approval_required",
				approvalID:         pending.ApprovalRequestID,
				approvalCapability: pending.Capability,
				uxSignals:          uxSignals,
			}
		}

		if toolResultTurn, ok := havenToolResultTurn(toolResults, useStructuredPath); ok {
			conversation = append(conversation, toolResultTurn)
		}

		userMessage = ""
	}

	timeoutText := "That took longer than expected and I had to stop mid-way. Try a smaller folder or ask again."
	emitter.emit(havenSSEEvent{Type: "text_delta", Content: timeoutText})
	return havenChatLoopOutcome{
		modelResponse: lastModelResponse,
		assistantText: timeoutText,
		uxSignals:     uxSignals,
	}
}
