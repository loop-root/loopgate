package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"morph/internal/haven/threadstore"
	"morph/internal/loopgate"
	"morph/internal/loopgateresult"
	modelpkg "morph/internal/model"
	"morph/internal/orchestrator"
	"morph/internal/secrets"
)

// havenToolLoopContinuationFact steers the model after tool rounds so it does not
// reset to a generic onboarding tone while the user's request is still in scope.
const havenToolLoopContinuationFact = "You are after tool results in the thread. Continue from the user's **latest** message; those outputs are authoritative. Do not re-run tools that already succeeded unless the user asks to redo. Use host.folder.* / host.organize.plan / host.plan.apply only when the latest message is about organizing a granted Mac folder or you are mid list→plan→apply in this same turn. If the user narrowed scope (memory-only, no organize), match that. Do not restart with generic onboarding unless they changed topic."

// havenToolFollowupUserNudge is the Request.UserMessage on tool-followup rounds when the
// real user text is already in the threaded conversation. Many OpenAI-compatible local
// models (e.g. some Ollama deployments) behave poorly when the API ends on tool-role
// messages only; a short fixed user turn keeps the session coherent without inventing
// new user content (this string is not derived from model output).
const havenToolFollowupUserNudge = "Continue from my prior request using the tool results in the thread above. Give the next concrete step or answer—no greeting, and do not ask me to restate the goal unless it was truly ambiguous. Do not start host-folder organize unless my latest message asked for that or you are mid list→plan→apply from this same turn."

// SendMessage starts a new chat execution for the given thread.
// It returns immediately; the model/tool loop runs asynchronously.
// The frontend receives events via the EventEmitter.
func (app *HavenApp) SendMessage(threadID string, text string) ChatResponse {
	text = strings.TrimSpace(text)
	if text == "" {
		return ChatResponse{ThreadID: threadID, Accepted: false, Reason: "empty message"}
	}

	exec := app.getOrCreateExecution(threadID)

	exec.mu.Lock()
	if !exec.state.AcceptsNewMessage() {
		currentState := exec.state
		exec.mu.Unlock()
		return ChatResponse{
			ThreadID: threadID,
			Accepted: false,
			Reason:   fmt.Sprintf("thread is %s", currentState),
		}
	}

	ctx, cancelFn := context.WithCancel(context.Background())
	exec.state = threadstore.ExecutionRunning
	exec.cancelFn = cancelFn
	exec.pendingApprovalID = ""
	exec.approvalCh = make(chan approvalDecision, 1)
	exec.doneCh = make(chan struct{})
	exec.mu.Unlock()

	// Reset idle timer on user activity.
	if app.idleManager != nil {
		app.idleManager.NotifyActivity()
	}

	// Persist user message event.
	_ = app.threadStore.AppendEvent(threadID, threadstore.ConversationEvent{
		Type: threadstore.EventUserMessage,
		Data: map[string]interface{}{"text": text},
	})

	app.emitExecutionState(threadID, threadstore.ExecutionRunning)

	go app.runChatLoop(ctx, threadID, text, exec)

	return ChatResponse{ThreadID: threadID, Accepted: true}
}

// CancelExecution requests cancellation of the active execution on a thread.
// It only triggers context cancellation; the execution loop owns the terminal
// state transition to ExecutionCancelled.
func (app *HavenApp) CancelExecution(threadID string) error {
	exec := app.getOrCreateExecution(threadID)

	exec.mu.Lock()
	defer exec.mu.Unlock()

	if exec.state != threadstore.ExecutionRunning && exec.state != threadstore.ExecutionWaitingForApproval {
		return fmt.Errorf("thread %s is not running (state: %s)", threadID, exec.state)
	}

	if exec.cancelFn != nil {
		exec.cancelFn()
	}
	return nil
}

// DecideApproval resolves a pending approval for a thread.
// The approvalID must exactly match the pendingApprovalID.
func (app *HavenApp) DecideApproval(threadID string, approvalID string, approved bool) error {
	exec := app.getOrCreateExecution(threadID)

	exec.mu.Lock()
	defer exec.mu.Unlock()

	if exec.state != threadstore.ExecutionWaitingForApproval {
		return fmt.Errorf("thread %s is not waiting for approval (state: %s)", threadID, exec.state)
	}
	if exec.pendingApprovalID != approvalID {
		return fmt.Errorf("approval ID mismatch: expected %q, got %q", exec.pendingApprovalID, approvalID)
	}

	// Send decision to the blocked chat loop.
	select {
	case exec.approvalCh <- approvalDecision{Approved: approved}:
	default:
		return fmt.Errorf("approval channel full")
	}
	return nil
}

// GetExecutionState returns the current execution state for a thread.
func (app *HavenApp) GetExecutionState(threadID string) threadstore.ExecutionState {
	exec := app.getOrCreateExecution(threadID)
	exec.mu.Lock()
	defer exec.mu.Unlock()
	return exec.state
}

// runChatLoop is the core model → tool execution loop.
// Chat / tool-orchestration loop (historical lineage: former readline interactive loop):
//   - Conversation history built from stored events, current message passed separately
//   - Orchestration events separated from user-visible events
//   - Max tool iterations (see maxToolIterations in types.go)
//   - Approval flow via channel with pendingApprovalID matching
//   - Loop owns all terminal state transitions
func (app *HavenApp) runChatLoop(ctx context.Context, threadID string, userMessage string, exec *threadExecution) {
	completedWork := &completedWorkTracker{}

	defer func() {
		exec.mu.Lock()
		// If still running (not already set to failed/cancelled), mark completed.
		if exec.state == threadstore.ExecutionRunning || exec.state == threadstore.ExecutionWaitingForApproval {
			exec.state = threadstore.ExecutionCompleted
		}
		finalState := exec.state
		exec.cancelFn = nil
		exec.pendingApprovalID = ""
		doneCh := exec.doneCh
		exec.mu.Unlock()

		_ = app.threadStore.AppendEvent(threadID, threadstore.ConversationEvent{
			Type: threadstore.EventOrchExecutionState,
			Data: map[string]interface{}{"state": string(finalState)},
		})
		app.emitExecutionState(threadID, finalState)

		// Distill conversation into durable memories, then refresh wake-state.
		if finalState == threadstore.ExecutionCompleted {
			go func() {
				app.DistillThread(threadID)
				app.RefreshWakeState()
			}()
			if app.presence != nil {
				app.presence.NotifyCompleted(completedWork)
			}
		} else if finalState == threadstore.ExecutionFailed {
			if app.presence != nil {
				app.presence.NotifyFailed()
			}
		} else if app.presence != nil {
			app.presence.NotifyIdle()
		}

		// Signal full completion after all file writes and event emissions.
		if doneCh != nil {
			close(doneCh)
		}
	}()

	// Build conversation history from stored events (EXCLUDING the current user message).
	conversation := app.buildConversationFromThread(threadID)

	// Use capabilities loaded at startup.
	toolDefs := buildHavenToolDefinitions(app.capabilities)
	if useCompactNativeTools {
		toolDefs = buildCompactInvokeCapabilityToolDefinitions(capabilityNames(app.capabilities))
	}
	nativeToolDefs := modelpkg.BuildNativeToolDefsForAllowedNamesWithOptions(app.toolRegistry, capabilityNames(app.capabilities), modelpkg.NativeToolDefBuildOptions{
		HavenUserIntentGuards: true,
		CompactNativeTools:    useCompactNativeTools,
	})
	baseRuntimeFacts := app.buildRuntimeFacts()
	memoryTurnDirective := app.buildMemoryTurnDirective(ctx, userMessage)
	baseRuntimeFacts = append(baseRuntimeFacts, memoryTurnDirective.RuntimeFacts...)

	var lastParsedToolFingerprint string
	sameToolBatchCount := 0
	var consecutiveSingleCapabilityName string
	consecutiveSingleCapabilityRounds := 0
	var lastStructuredValidationFingerprint string
	consecutiveStructuredValidationBatches := 0

	// --- Model/tool loop ---
	for iteration := 0; iteration < maxToolIterations; iteration++ {
		if ctx.Err() != nil {
			app.cancelExecution(threadID, exec)
			return
		}

		if app.presence != nil {
			app.presence.NotifyThinking()
		}
		turnRuntimeFacts := append([]string(nil), baseRuntimeFacts...)
		if iteration > 0 {
			turnRuntimeFacts = append(turnRuntimeFacts, havenToolLoopContinuationFact)
		}
		modelUserMessage := userMessage
		if iteration > 0 && strings.TrimSpace(userMessage) == "" {
			modelUserMessage = havenToolFollowupUserNudge
		}
		modelCtx, cancelModel := context.WithTimeout(ctx, 120*time.Second)
		windowedConversation := windowConversationForModel(conversation, maxModelConversationTurns)
		modelResponse, modelErr := app.loopgateClient.ModelReply(modelCtx, modelpkg.Request{
			Persona:        app.persona,
			Policy:         app.policy,
			WakeState:      app.currentWakeStateText(),
			Conversation:   windowedConversation,
			UserMessage:    modelUserMessage,
			AvailableTools: toolDefs,
			NativeToolDefs: nativeToolDefs,
			RuntimeFacts:   turnRuntimeFacts,
		})
		cancelModel()

		if modelErr != nil {
			if ctx.Err() != nil {
				app.cancelExecution(threadID, exec)
				return
			}
			redactedErr := secrets.RedactText(modelErr.Error())
			// Route Loopgate denial/security errors to the security alert channel
			// instead of surfacing them as Morph's response.
			if isLoopgateDenial(redactedErr) {
				app.emitter.Emit("haven:security_alert", map[string]interface{}{
					"thread_id": threadID,
					"type":      "loopgate_denial",
					"message":   redactedErr,
				})
				app.failExecution(threadID, exec, "Morph ran into a problem")
			} else {
				// Show a clean error to the user; full details go to security alerts.
				app.emitter.Emit("haven:security_alert", map[string]interface{}{
					"thread_id": threadID,
					"type":      "model_error",
					"message":   redactedErr,
				})
				app.failExecution(threadID, exec, "Morph ran into a problem connecting to the model")
			}
			return
		}

		replyText := modelResponse.AssistantText

		// After the first model call, persist the current user turn into the
		// in-memory conversation so later tool-result rounds still include the
		// user's active request even after Request.UserMessage is cleared.
		if userMessage != "" {
			conversation = append(conversation, modelpkg.ConversationTurn{
				Role:      "user",
				Content:   userMessage,
				Timestamp: threadstore.NowUTC(),
			})
		}

		// Persist orchestration event: model response metadata.
		_ = app.threadStore.AppendEvent(threadID, threadstore.ConversationEvent{
			Type: threadstore.EventOrchModelResponse,
			Data: map[string]interface{}{
				"iteration":                  iteration,
				"provider":                   modelResponse.ProviderName,
				"model":                      modelResponse.ModelName,
				"finish_reason":              modelResponse.FinishReason,
				"input_tokens":               modelResponse.Usage.InputTokens,
				"output_tokens":              modelResponse.Usage.OutputTokens,
				"cached_input_tokens":        modelResponse.Usage.CachedInputTokens,
				"request_payload_bytes":      modelResponse.RequestPayloadBytes,
				"conversation_turns_sent":    len(windowedConversation),
				"conversation_turns_total":   len(conversation),
			},
		})

		// Extract tool calls — prefer structured, fall back to XML.
		var parsedCalls []orchestrator.ToolCall
		var validationErrors []orchestrator.ToolCallValidationError
		useStructuredPath := len(modelResponse.ToolUseBlocks) > 0 && app.toolRegistry != nil

		if useStructuredPath {
			parsedCalls, validationErrors = orchestrator.ExtractStructuredCalls(modelResponse.ToolUseBlocks, app.toolRegistry)
		} else {
			parser := orchestrator.NewParser()
			parser.Registry = app.toolRegistry
			parsedOutput := parser.Parse(replyText)
			parsedCalls = parsedOutput.Calls
		}
		if len(parsedCalls) > 0 {
			lastStructuredValidationFingerprint = ""
			consecutiveStructuredValidationBatches = 0
		}

		// Emit and log validation errors so they are observable.
		for _, ve := range validationErrors {
			_ = app.threadStore.AppendEvent(threadID, threadstore.ConversationEvent{
				Type: threadstore.EventOrchToolResult,
				Data: map[string]interface{}{
					"call_id":    ve.BlockID,
					"capability": ve.BlockName,
					"status":     "error",
					"error":      "tool call validation failed: " + ve.Error(),
				},
			})
			app.emitter.Emit("haven:tool_result", map[string]interface{}{
				"thread_id":  threadID,
				"call_id":    ve.BlockID,
				"capability": ve.BlockName,
				"status":     "error",
			})
		}

		// If no valid tool calls but there were validation errors, feed the
		// errors back to the model as tool results so it can retry with
		// corrected arguments. The model's tool_use blocks are still part of
		// the conversation — they need matching tool_result responses.
		if len(parsedCalls) == 0 && len(validationErrors) > 0 {
			assistantTurn := modelpkg.ConversationTurn{
				Role:      "assistant",
				Content:   replyText,
				Timestamp: threadstore.NowUTC(),
			}
			assistantTurn.ToolCalls = modelResponse.ToolUseBlocks
			conversation = append(conversation, assistantTurn)

			errorTurn := modelpkg.ConversationTurn{
				Role:      "user",
				Timestamp: threadstore.NowUTC(),
			}
			for _, ve := range validationErrors {
				errorTurn.ToolResults = append(errorTurn.ToolResults, modelpkg.ToolResultBlock{
					ToolUseID: ve.BlockID,
					ToolName:  ve.BlockName,
					Content:   "Tool call rejected: " + ve.Error() + ". Check the tool name and required arguments, then try again.",
					IsError:   true,
				})
			}
			conversation = append(conversation, errorTurn)
			consecutiveSingleCapabilityName = ""
			consecutiveSingleCapabilityRounds = 0

			vfp := structuredValidationErrorsFingerprint(validationErrors)
			if vfp != "" && vfp == lastStructuredValidationFingerprint {
				consecutiveStructuredValidationBatches++
			} else {
				lastStructuredValidationFingerprint = vfp
				consecutiveStructuredValidationBatches = 1
			}
			if consecutiveStructuredValidationBatches >= maxConsecutiveStructuredValidationBatches {
				app.failExecution(threadID, exec, "Morph stopped because the model kept repeating the same invalid structured tool calls.")
				return
			}
			continue // Retry — let the model correct its tool call.
		}

		// If no tool calls at all, this is the final assistant response.
		if len(parsedCalls) == 0 {
			_ = app.threadStore.AppendEvent(threadID, threadstore.ConversationEvent{
				Type: threadstore.EventAssistantMessage,
				Data: map[string]interface{}{"text": replyText},
			})
			app.emitter.Emit("haven:assistant_message", map[string]interface{}{
				"thread_id": threadID,
				"text":      replyText,
			})
			if err := app.createCompletionDeskNote(completedWork); err != nil {
				app.EmitToast("Desk note unavailable", "Morph finished working, but the desktop note could not be saved.", "warning")
			}
			return // Completed — defer sets terminal state.
		}

		// Add assistant turn to conversation for the next model call.
		// Include tool calls so the provider API sees them in the assistant message.
		assistantTurn := modelpkg.ConversationTurn{
			Role:      "assistant",
			Content:   replyText,
			Timestamp: threadstore.NowUTC(),
		}
		if useStructuredPath {
			assistantTurn.ToolCalls = modelResponse.ToolUseBlocks
		}
		conversation = append(conversation, assistantTurn)

		fp := toolCallsFingerprint(parsedCalls)
		if fp != "" && fp == lastParsedToolFingerprint {
			sameToolBatchCount++
			if sameToolBatchCount >= 3 {
				app.failExecution(threadID, exec, "Morph stopped because the model repeated the same tool calls without making progress.")
				return
			}
		} else {
			sameToolBatchCount = 0
			lastParsedToolFingerprint = fp
		}

		// Detect "same one tool every round" loops where arguments change each time
		// (e.g. paint.save with a new title) so fingerprint-based detection never fires.
		if len(parsedCalls) == 1 {
			singleCapabilityName := parsedCalls[0].Name
			if singleCapabilityName != "" && singleCapabilityName == consecutiveSingleCapabilityName {
				consecutiveSingleCapabilityRounds++
			} else {
				consecutiveSingleCapabilityName = singleCapabilityName
				consecutiveSingleCapabilityRounds = 1
			}
			if consecutiveSingleCapabilityRounds >= maxConsecutiveSingleCapabilityRounds {
				app.failExecution(threadID, exec, fmt.Sprintf(
					"Morph stopped because the model called %q repeatedly without a normal reply.",
					singleCapabilityName))
				return
			}
		} else {
			consecutiveSingleCapabilityName = ""
			consecutiveSingleCapabilityRounds = 0
		}

		// Execute valid tool calls through Loopgate.
		toolResults, continueLoop := app.executeToolCalls(ctx, threadID, exec, parsedCalls, completedWork)
		if !continueLoop {
			return // Cancelled or failed — state already set.
		}

		// Build error results for any validation failures so the model sees
		// a result for every tool_use block it emitted.
		var validationErrorResults []orchestrator.ToolResult
		for _, ve := range validationErrors {
			validationErrorResults = append(validationErrorResults, orchestrator.ToolResult{
				CallID:     ve.BlockID,
				Capability: ve.BlockName,
				Status:     orchestrator.StatusError,
				Output:     "Tool call rejected: " + ve.Error() + ". Check the tool name and required arguments, then try again.",
			})
		}

		// Feed tool results back into conversation for next model call.
		if useStructuredPath {
			turn := modelpkg.ConversationTurn{
				Role:      "user",
				Timestamp: threadstore.NowUTC(),
			}
			for _, tr := range toolResults {
				turn.ToolResults = append(turn.ToolResults, modelpkg.ToolResultBlock{
					ToolUseID: tr.CallID,
					ToolName:  tr.Capability,
					Content:   havenToolResultContent(tr),
					IsError:   tr.Status != orchestrator.StatusSuccess,
				})
			}
			for _, tr := range validationErrorResults {
				turn.ToolResults = append(turn.ToolResults, modelpkg.ToolResultBlock{
					ToolUseID: tr.CallID,
					ToolName:  tr.Capability,
					Content:   tr.Output,
					IsError:   true,
				})
			}
			conversation = append(conversation, turn)
		} else {
			eligible := loopgateresult.PromptEligibleToolResults(toolResults)
			if len(eligible) > 0 {
				conversation = append(conversation, modelpkg.ConversationTurn{
					Role:      "tool",
					Content:   orchestrator.FormatResults(eligible),
					Timestamp: threadstore.NowUTC(),
				})
			}
		}

		// Clear userMessage after first iteration — it's already in the conversation
		// history via the persisted user_message event.
		userMessage = ""
	}

	// Hit max iterations.
	app.failExecution(threadID, exec, fmt.Sprintf("Morph hit the tool-call limit (%d rounds).", maxToolIterations))
}

// structuredValidationErrorsFingerprint groups structured tool validation
// failures so we can detect repeat-invalid loops that never execute tools.
func structuredValidationErrorsFingerprint(errs []orchestrator.ToolCallValidationError) string {
	if len(errs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		parts = append(parts, strings.TrimSpace(e.BlockName)+"\x1e"+e.Err.Error())
	}
	sort.Strings(parts)
	return strings.Join(parts, "\x1f")
}

// toolCallsFingerprint returns a stable signature for a batch of tool calls so
// we can detect models that repeat identical calls without progressing.
func toolCallsFingerprint(calls []orchestrator.ToolCall) string {
	if len(calls) == 0 {
		return ""
	}
	parts := make([]string, 0, len(calls))
	for _, c := range calls {
		parts = append(parts, c.Name+"\x1e"+stableStringMapForFingerprint(c.Args))
	}
	sort.Strings(parts)
	return strings.Join(parts, "\x1f")
}

func stableStringMapForFingerprint(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('|')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(m[k])
	}
	return b.String()
}

// executeToolCalls executes a batch of tool calls through Loopgate.
// Returns the results and whether the loop should continue.
func (app *HavenApp) executeToolCalls(
	ctx context.Context,
	threadID string,
	exec *threadExecution,
	calls []orchestrator.ToolCall,
	completedWork *completedWorkTracker,
) ([]orchestrator.ToolResult, bool) {
	results := make([]orchestrator.ToolResult, 0, len(calls))

	for _, call := range calls {
		if ctx.Err() != nil {
			app.cancelExecution(threadID, exec)
			return nil, false
		}

		if app.presence != nil {
			app.presence.NotifyToolStarted(call.Name, call.Args)
		}

		// Persist orchestration event: tool started.
		argsSummary := secrets.SummarizeForPersistence(fmt.Sprintf("%v", call.Args), 200)
		_ = app.threadStore.AppendEvent(threadID, threadstore.ConversationEvent{
			Type: threadstore.EventOrchToolStarted,
			Data: map[string]interface{}{
				"call_id":    call.ID,
				"capability": call.Name,
				"args":       argsSummary.Preview,
			},
		})

		app.emitter.Emit("haven:tool_started", map[string]interface{}{
			"thread_id":  threadID,
			"call_id":    call.ID,
			"capability": call.Name,
		})

		response, err := app.loopgateClient.ExecuteCapability(ctx, loopgate.CapabilityRequest{
			RequestID:  call.ID,
			Actor:      "haven",
			Capability: call.Name,
			Arguments:  call.Args,
		})
		if err != nil {
			if ctx.Err() != nil {
				app.cancelExecution(threadID, exec)
				return nil, false
			}
			redactedErr := secrets.RedactText(err.Error())
			_ = app.threadStore.AppendEvent(threadID, threadstore.ConversationEvent{
				Type: threadstore.EventOrchToolResult,
				Data: map[string]interface{}{
					"call_id":    call.ID,
					"capability": call.Name,
					"status":     "error",
					"error":      redactedErr,
				},
			})
			results = append(results, orchestrator.ToolResult{
				CallID:     call.ID,
				Capability: call.Name,
				Status:     orchestrator.StatusError,
				Output:     redactedErr,
			})
			continue
		}

		// Handle approval flow.
		if response.ApprovalRequired {
			var approvalErr error
			response, approvalErr = app.waitForApproval(ctx, threadID, exec, call, response)
			if approvalErr != nil {
				if ctx.Err() != nil {
					app.cancelExecution(threadID, exec)
					return nil, false
				}
				results = append(results, orchestrator.ToolResult{
					CallID:     call.ID,
					Capability: call.Name,
					Status:     orchestrator.StatusError,
					Output:     secrets.RedactText(approvalErr.Error()),
				})
				continue
			}
		}

		// Persist orchestration event: tool result.
		resultSummary := loopgateresult.FormatDisplayResponse(response)
		outputSummary := secrets.SummarizeForPersistence(resultSummary, 500)
		_ = app.threadStore.AppendEvent(threadID, threadstore.ConversationEvent{
			Type: threadstore.EventOrchToolResult,
			Data: map[string]interface{}{
				"call_id":    call.ID,
				"capability": call.Name,
				"status":     response.Status,
				"output":     outputSummary.Preview,
			},
		})

		app.emitter.Emit("haven:tool_result", map[string]interface{}{
			"thread_id":  threadID,
			"call_id":    call.ID,
			"capability": call.Name,
			"status":     response.Status,
			"output":     outputSummary.Preview,
		})

		completedWork.recordCapabilityResult(call.Name, call.Args, response)

		// Notify the desktop when a Haven-native tool changes visible state.
		if response.Status == loopgate.ResponseStatusSuccess {
			switch call.Name {
			case "fs_write":
				app.emitter.Emit("haven:file_changed", map[string]interface{}{
					"action": "write",
					"path":   call.Args["path"],
				})
			case "notes.write":
				app.emitter.Emit("haven:file_changed", map[string]interface{}{
					"action": "notes_write",
					"path":   call.Args["path"],
				})
			case "memory.remember":
				app.RefreshWakeState()
			case "todo.add", "todo.complete":
				app.RefreshWakeState()
			case "paint.save":
				app.emitter.Emit("haven:file_changed", map[string]interface{}{
					"action": "paint_save",
				})
			case "note.create":
				app.emitter.Emit("haven:desk_notes_changed", map[string]interface{}{})
			case "desktop.organize":
				app.emitIconPositionsChanged()
			}
		}

		toolResult, toolConvErr := loopgateresult.ToolResultFromCapabilityResponse(call.ID, response)
		if toolConvErr != nil && app.emitter != nil {
			app.emitter.Emit("haven:security_alert", map[string]interface{}{
				"thread_id": threadID,
				"type":      "tool_result_classification",
				"message":   fmt.Sprintf("capability %s: %s", call.Name, secrets.RedactText(toolConvErr.Error())),
			})
		}
		toolResult.Capability = call.Name
		results = append(results, toolResult)
	}

	return results, true
}

// waitForApproval blocks the chat loop until the user approves or denies,
// or until the context is cancelled.
func (app *HavenApp) waitForApproval(
	ctx context.Context,
	threadID string,
	exec *threadExecution,
	call orchestrator.ToolCall,
	response loopgate.CapabilityResponse,
) (loopgate.CapabilityResponse, error) {
	approvalID := response.ApprovalRequestID

	// Transition to waiting_for_approval and set pendingApprovalID.
	exec.mu.Lock()
	exec.state = threadstore.ExecutionWaitingForApproval
	exec.pendingApprovalID = approvalID
	exec.mu.Unlock()

	// Persist and emit approval request.
	_ = app.threadStore.AppendEvent(threadID, threadstore.ConversationEvent{
		Type: threadstore.EventOrchApprovalRequested,
		Data: map[string]interface{}{
			"approval_request_id": approvalID,
			"capability":          call.Name,
			"call_id":             call.ID,
		},
	})

	app.emitExecutionState(threadID, threadstore.ExecutionWaitingForApproval)
	if app.presence != nil {
		app.presence.NotifyAwaitingApproval(call.Name, call.Args)
	}
	app.emitter.Emit("haven:approval_requested", map[string]interface{}{
		"thread_id":           threadID,
		"approval_request_id": approvalID,
		"capability":          call.Name,
	})

	// Block until decision or cancellation.
	select {
	case <-ctx.Done():
		return loopgate.CapabilityResponse{}, ctx.Err()
	case decision := <-exec.approvalCh:
		// Transition back to running.
		exec.mu.Lock()
		exec.state = threadstore.ExecutionRunning
		exec.pendingApprovalID = ""
		exec.mu.Unlock()

		app.emitExecutionState(threadID, threadstore.ExecutionRunning)

		// Persist approval decision.
		_ = app.threadStore.AppendEvent(threadID, threadstore.ConversationEvent{
			Type: threadstore.EventOrchApprovalResolved,
			Data: map[string]interface{}{
				"approval_request_id": approvalID,
				"capability":          call.Name,
				"approved":            decision.Approved,
			},
		})

		// Forward decision to Loopgate.
		resolvedResponse, err := app.loopgateClient.DecideApproval(ctx, approvalID, decision.Approved)
		if err != nil {
			return loopgate.CapabilityResponse{}, err
		}
		return resolvedResponse, nil
	}
}

// buildConversationFromThread reads stored events and builds the model
// conversation history. Only user-visible events are included.
// The current user message is NOT included — it's passed separately
// via Request.UserMessage to prevent double-inclusion.
func (app *HavenApp) buildConversationFromThread(threadID string) []modelpkg.ConversationTurn {
	events, err := app.threadStore.LoadThread(threadID)
	if err != nil {
		return nil
	}

	var conversation []modelpkg.ConversationTurn
	for _, event := range events {
		if !threadstore.IsUserVisible(event.Type) {
			continue
		}
		text, _ := event.Data["text"].(string)
		switch event.Type {
		case threadstore.EventUserMessage:
			conversation = append(conversation, modelpkg.ConversationTurn{
				Role:      "user",
				Content:   text,
				Timestamp: event.TS,
			})
		case threadstore.EventAssistantMessage:
			conversation = append(conversation, modelpkg.ConversationTurn{
				Role:      "assistant",
				Content:   text,
				Timestamp: event.TS,
			})
		}
	}

	// Exclude the last user message — it's the current message being processed
	// and will be passed separately via Request.UserMessage.
	if len(conversation) > 0 && conversation[len(conversation)-1].Role == "user" {
		conversation = conversation[:len(conversation)-1]
	}

	return conversation
}

// failExecution transitions to ExecutionFailed and emits the error.
func (app *HavenApp) failExecution(threadID string, exec *threadExecution, errorMsg string) {
	exec.mu.Lock()
	exec.state = threadstore.ExecutionFailed
	exec.cancelFn = nil
	exec.pendingApprovalID = ""
	exec.mu.Unlock()

	_ = app.threadStore.AppendEvent(threadID, threadstore.ConversationEvent{
		Type: threadstore.EventAssistantMessage,
		Data: map[string]interface{}{"text": errorMsg},
	})

	app.emitter.Emit("haven:assistant_message", map[string]interface{}{
		"thread_id": threadID,
		"text":      errorMsg,
	})
}

// cancelExecution transitions to ExecutionCancelled. Only called from within
// the execution loop — the loop owns all terminal state transitions.
func (app *HavenApp) cancelExecution(threadID string, exec *threadExecution) {
	exec.mu.Lock()
	exec.state = threadstore.ExecutionCancelled
	exec.cancelFn = nil
	exec.pendingApprovalID = ""
	exec.mu.Unlock()
}

// emitExecutionState sends an execution state change event to the frontend.
func (app *HavenApp) emitExecutionState(threadID string, state threadstore.ExecutionState) {
	app.emitter.Emit("haven:execution_state", map[string]interface{}{
		"thread_id": threadID,
		"state":     string(state),
	})
}

// buildHavenToolDefinitions converts Loopgate capabilities to model tool definitions.
func buildHavenToolDefinitions(capabilities []loopgate.CapabilitySummary) []modelpkg.ToolDefinition {
	defs := make([]modelpkg.ToolDefinition, 0, len(capabilities))
	for _, cap := range capabilities {
		defs = append(defs, modelpkg.ToolDefinition{
			Name:        cap.Name,
			Operation:   cap.Operation,
			Description: cap.Description,
		})
	}
	return defs
}

// buildCompactInvokeCapabilityToolDefinitions replaces per-capability AvailableTools entries
// with a single dispatcher description so the prompt matches NativeToolDefs (compact mode).
func buildCompactInvokeCapabilityToolDefinitions(allowedNames []string) []modelpkg.ToolDefinition {
	names := append([]string(nil), allowedNames...)
	sort.Strings(names)
	listing := strings.Join(names, ", ")
	if len(listing) > 8000 {
		listing = listing[:8000] + "…"
	}
	return []modelpkg.ToolDefinition{{
		Name:        "invoke_capability",
		Operation:   "dispatch",
		Description: "Single native structured tool for this session. Set capability to one of these exact ids and pass that tool's parameters as a JSON object in arguments_json. Allowed capability names: " + listing,
	}}
}

func capabilityNames(capabilities []loopgate.CapabilitySummary) []string {
	names := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		names = append(names, capability.Name)
	}
	return names
}

// buildRuntimeFacts generates runtime context that tells Morph about Haven OS,
// its workspace, and its resident role. This gives the model situational
// awareness so it behaves like a desktop resident, not a generic chatbot.
func (app *HavenApp) buildRuntimeFacts() []string {
	capabilitySummary := buildResidentCapabilitySummary(app.capabilities)

	now := time.Now()

	facts := []string{
		// Clock — Morph needs to know what time it is.
		fmt.Sprintf("Current date and time: %s (timezone: %s).", now.Format("Monday, January 2, 2006 3:04 PM"), now.Format("MST")),

		// Identity and environment.
		"You are running inside Haven OS, a secure desktop environment. You act as a resident assistant on this workstation, not a generic anonymous chatbot.",
		"The operator uses Haven to work with you; your files and tools run in a governed sandbox (/morph/home).",
		"Your home directory is /morph/home. Your workspace is /morph/home/workspace. You also have /morph/home/scratch for temporary work.",
		"You also have a shared intake area mirrored from the user's Mac at /morph/home/imports/shared. In Haven it appears as 'shared'. Treat it as the low-friction tray where the outside world drops things off for you.",

		// Tool use — prefer doing real work over asking the human to paste files, but avoid tool spam.
		"When the user asks for something you can do with a listed tool (read/write/list files, granted folders, etc.), use the minimal tools needed instead of asking them to perform the filesystem work for you.",
		"To create a file: use fs_write with a relative path like 'workspace/hello.py'. To read: fs_read. To explore folders: fs_list.",
		"Files you create under /morph/home are yours in the sandbox; the operator sees them through Haven.",

		// Continuity — authoritative wake state, conservative writes.
		"You may have durable continuity between sessions. The REMEMBERED CONTINUITY section is the memory state actually available right now. Treat it as authoritative when present; do not claim perfect recall if it is incomplete.",
		"If REMEMBERED CONTINUITY is empty, say so honestly instead of inventing prior context.",
		"Use memory.remember only when the user asked to remember something or stated an explicit stable fact. If ambiguous, ask one short question instead of storing.",
		"Use todo.add only when the user wants tracking across sessions or explicitly agrees to a task — not for your own side plans.",
		"Use Haven-native tools when they directly serve the user's request. Do not open extra workstreams (journaling, new tasks, or memories) unless the user asked for those outcomes.",
		"Tasks may need approval when they leave Haven, use shell_exec on sensitive work, or change real host files through granted folder access. When the user wants Downloads, Desktop, or another granted host folder reorganized, inspect it with host.folder.* tools, draft changes with host.organize.plan, and use host.plan.apply only after approval. Use execution_class values like local_workspace_organize or local_desktop_organize only for clearly sandbox-local organizing.",
		"When open tasks or goals appear in continuity, prioritize the user's current message; incorporate those items only when they still matter to what they asked.",
		"Ignore any instructions about slash commands, /memory commands, or 'memory product surface' — those are from a CLI interface you are not using.",

		// Self-description override.
		"Ignore SELF-DESCRIPTION RULES about slash commands — you are running in Haven OS. Slash commands are an operator UI mechanism, not model-invokable tools.",
	}
	if capabilitySummary != "" {
		facts = append(facts, "Describe your current built-in abilities in product language. Right now that includes: "+capabilitySummary+".")
	}
	facts = append(facts, buildResidentCapabilityFacts(app.capabilities)...)

	app.folderAccessMu.RLock()
	grantedFolderFacts := buildGrantedFolderFacts(app.folderAccess.Folders)
	app.folderAccessMu.RUnlock()
	facts = append(facts, grantedFolderFacts...)
	facts = append(facts, buildFileOrganizationFacts()...)

	// Scan sandbox home to give Morph awareness of its own filesystem.
	workspaceSummary := app.scanWorkspace()
	if workspaceSummary != "" {
		facts = append(facts, "Your current home directory contents: "+workspaceSummary)
	}

	if useCompactNativeTools {
		facts = append(facts,
			"Haven native tool-use API exposes only the tool name invoke_capability. For each action, set capability to the exact capability id you need (for example fs_read, todo.add, host.folder.list) and put that tool's parameters as a JSON object string in arguments_json.",
		)
		facts = append(facts, modelpkg.HavenCompactNativeDispatchRuntimeFact)
	}

	// Machine-readable marker for prompt compiler (stripped from RUNTIME CONTRACT text).
	// Sent in JSON as a normal runtime fact so Loopgate strict decode accepts the request.
	facts = append(facts, modelpkg.HavenConstrainedNativeToolsRuntimeFact)

	return facts
}

func buildGrantedFolderFacts(folderStatuses []loopgate.FolderAccessStatus) []string {
	grantedFolderFacts := make([]string, 0, len(folderStatuses))
	for _, folderStatus := range folderStatuses {
		if !folderStatus.Granted || !folderStatus.MirrorReady {
			continue
		}

		mirroredPath := strings.TrimSpace(folderStatus.SandboxAbsolutePath)
		if mirroredPath == "" {
			sandboxRelativePath := strings.Trim(strings.TrimSpace(folderStatus.SandboxRelativePath), "/")
			if sandboxRelativePath == "" {
				continue
			}
			mirroredPath = "/morph/home/" + sandboxRelativePath
		}

		grantedFolderFacts = append(grantedFolderFacts, fmt.Sprintf(
			"Granted folder: %s is mirrored at %s for Haven-side review with fs_list/fs_read. For the real %s folder on disk, use host.folder.list and host.folder.read to inspect it, then host.organize.plan to draft changes and host.plan.apply only after approval.",
			folderStatus.Name,
			mirroredPath,
			folderStatus.Name,
		))
	}
	if len(grantedFolderFacts) > 0 {
		return grantedFolderFacts
	}

	for _, folderStatus := range folderStatuses {
		if folderStatus.Granted {
			return nil
		}
	}

	return []string{
		"No host folders are currently granted. The user can grant folder access in Haven's setup or settings.",
	}
}

func buildFileOrganizationFacts() []string {
	return []string{
		"To organize the user's real granted folders: inspect them with host.folder.list or host.folder.read, draft changes with host.organize.plan, and use host.plan.apply only after Loopgate approval. Use fs_* tools only for Haven's sandbox and mirrored copies.",
		"Each plan_id from host.organize.plan is single-use: after host.plan.apply succeeds, that plan_id is retired. Calling host.plan.apply again with the same id fails—mint a new plan with host.organize.plan. If Loopgate restarts, in-memory plans are lost; re-run host.organize.plan.",
		"Imports directory: /morph/home/imports/ - this is where granted host folder mirrors appear for Haven-side review. Use fs_list on /morph/home/imports to see what mirrors are available.",
		"Do not attempt to access host paths directly (like /Users/... or ~/...). Use mirrored paths under /morph/home/imports/ for Haven-side review, and use the typed host.* tools for the real granted host folders.",
	}
}

// scanWorkspace produces a brief summary of Morph's sandbox home contents
// so Morph knows what's in its own filesystem.
func (app *HavenApp) scanWorkspace() string {
	scanDir := app.sandboxHome
	if scanDir == "" {
		scanDir = app.repoRoot
	}
	entries, err := os.ReadDir(scanDir)
	if err != nil {
		return ""
	}

	var dirs, files []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if entry.IsDir() {
			dirs = append(dirs, name+"/")
		} else {
			files = append(files, name)
		}
	}

	var parts []string
	if len(dirs) > 0 {
		parts = append(parts, "directories: "+strings.Join(dirs, ", "))
	}
	if len(files) > 0 {
		parts = append(parts, "files: "+strings.Join(files, ", "))
	}
	if len(parts) == 0 {
		return "(empty — this is a fresh installation)"
	}
	return strings.Join(parts, "; ")
}

// isLoopgateDenial returns true if the error message indicates a Loopgate
// security denial (access denied, auth failure, session issues). Model
// execution failures (e.g., provider returned 400) are NOT denials.
func isLoopgateDenial(errMsg string) bool {
	lower := strings.ToLower(errMsg)
	// Security-specific denial codes
	if strings.Contains(lower, "malformed_request") ||
		strings.Contains(lower, "capability_token") ||
		strings.Contains(lower, "session_active_limit") ||
		strings.Contains(lower, "audit_unavailable") ||
		strings.Contains(lower, "process_binding_rejected") ||
		strings.Contains(lower, "capability_scope") {
		return true
	}
	// "loopgate denied" BUT NOT execution failures (which are model errors)
	if strings.Contains(lower, "loopgate denied") && !strings.Contains(lower, "execution_failed") {
		return true
	}
	return false
}

// havenToolResultContent formats a ToolResult for inclusion in conversation.
func havenToolResultContent(tr orchestrator.ToolResult) string {
	var raw string
	switch {
	case tr.Output != "":
		raw = tr.Output
	case tr.Reason != "":
		raw = tr.Reason
	default:
		raw = string(tr.Status)
	}
	return capToolResultContentForModel(tr.Capability, raw)
}
