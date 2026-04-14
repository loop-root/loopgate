package loopgate

import (
	"context"
	"strings"

	"morph/internal/config"
	modelpkg "morph/internal/model"
	"morph/internal/orchestrator"
	toolspkg "morph/internal/tools"
)

type havenChatRuntime struct {
	policy                   config.Policy
	registry                 *toolspkg.Registry
	executeCapabilityRequest func(context.Context, capabilityToken, CapabilityRequest, bool) CapabilityResponse
}

func newHavenChatRuntime(server *Server) havenChatRuntime {
	policyRuntime := server.currentPolicyRuntime()
	return havenChatRuntime{
		policy:                   policyRuntime.policy,
		registry:                 policyRuntime.registry,
		executeCapabilityRequest: server.executeCapabilityRequest,
	}
}

// runToolLoop is Loopgate's supervised agent runtime for Haven turns.
//
// Runtime model (Loopgate terms):
//   - Morph (the resident assistant) operates within a bounded iteration budget
//     (maxHavenToolIterations). Loopgate owns the continuation decision; the model
//     only decides whether to call capabilities or return text within each iteration.
//   - Capability dispatch is policy-gated at every iteration via executeCapabilityRequest.
//     The model cannot bypass Loopgate authority by embedding capability names in text.
//   - Read-only capabilities may execute concurrently within a single iteration batch
//     (see executeToolCallsConcurrent). Write and execute capabilities are always
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
func (runtime havenChatRuntime) runToolLoop(
	ctx context.Context,
	modelClient *modelpkg.Client,
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
	loopState := newHavenChatLoopState(conversation, initialUserMessage)

	for iteration := 0; iteration < maxHavenToolIterations; iteration++ {
		if ctx.Err() != nil {
			return havenChatLoopOutcome{err: ctx.Err()}
		}

		turnRuntimeFacts := loopState.buildTurnRuntimeFacts(baseRuntimeFacts, hostFolderOrganizeToolkitAvailable, iteration)
		modelUserMessage := loopState.modelUserMessage(iteration)
		windowedConversation := havenWindowConversationForModel(loopState.conversation, maxHavenChatTurns)
		turnAttachments := havenChatTurnAttachments(iteration, initialAttachments)
		modelResponse, modelErr := modelClient.Reply(ctx, modelpkg.Request{
			Persona:        persona,
			Policy:         runtime.policy,
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
		loopState.lastModelResponse = modelResponse
		replyText := strings.TrimSpace(modelResponse.AssistantText)
		if havenIsNonUserFacingAssistantPlaceholder(replyText) {
			replyText = ""
		}

		if strings.EqualFold(strings.TrimSpace(modelResponse.FinishReason), "max_tokens") ||
			strings.EqualFold(strings.TrimSpace(modelResponse.FinishReason), "length") {
			if replyText == "" {
				replyText = "My response was cut off — the output limit was reached. Try a shorter or more specific request."
			}
			emitter.emit(havenSSEEvent{Type: "text_delta", Content: replyText})
			return havenChatLoopOutcome{modelResponse: loopState.lastModelResponse, assistantText: replyText, uxSignals: loopState.uxSignals}
		}

		loopState.appendUserTurnIfPresent()

		useStructuredPath := len(modelResponse.ToolUseBlocks) > 0 && runtime.registry != nil
		var parsedCalls []orchestrator.ToolCall
		var validationErrors []orchestrator.ToolCallValidationError
		if useStructuredPath {
			parsedCalls, validationErrors = orchestrator.ExtractStructuredCalls(modelResponse.ToolUseBlocks, runtime.registry)
		} else {
			parser := orchestrator.NewParser()
			parser.Registry = runtime.registry
			parsedCalls = parser.Parse(replyText).Calls
		}

		if len(parsedCalls) == 0 && len(validationErrors) == 0 {
			handled, outcome := loopState.handleNoToolResponse(replyText, initialUserMessage, hostFolderOrganizeToolkitAvailable, iteration, emitter)
			if handled {
				if outcome == nil {
					continue
				}
				return *outcome
			}
		}

		loopState.appendAssistantTurn(replyText, modelResponse.ToolUseBlocks, useStructuredPath)

		if len(parsedCalls) == 0 && len(validationErrors) > 0 {
			loopState.conversation = append(loopState.conversation, havenStructuredValidationErrorTurn(validationErrors))
			loopState.userMessage = ""
			continue
		}

		for _, call := range parsedCalls {
			emitter.emit(havenSSEEvent{Type: "tool_start", ToolCall: &havenSSEToolCall{
				CallID: call.ID,
				Name:   call.Name,
			}})
		}

		toolResults := runtime.executeToolCallsConcurrent(ctx, tokenClaims, parsedCalls, emitter)

		for _, validationError := range validationErrors {
			toolResults = append(toolResults, orchestrator.ToolResult{
				CallID:     validationError.BlockID,
				Capability: validationError.BlockName,
				Status:     orchestrator.StatusError,
				Output:     "Tool call rejected: " + validationError.Error() + ". Check the tool name and required arguments, then try again.",
			})
		}
		loopState.observeToolResults(parsedCalls, toolResults)

		if hostFolderOrganizeToolkitAvailable && loopState.awaitingHostPlanApply && ctx.Err() == nil {
			if planID := havenExtractOrganizePlanIDFromResults(toolResults); planID != "" {
				loopState.awaitingHostPlanApply = false
				autoCallID := "loopgate-auto-apply-" + planID
				emitter.emit(havenSSEEvent{Type: "tool_start", ToolCall: &havenSSEToolCall{
					CallID: autoCallID,
					Name:   "host.plan.apply",
				}})
				autoResults := runtime.executeToolCalls(ctx, tokenClaims, []orchestrator.ToolCall{{
					ID:   autoCallID,
					Name: "host.plan.apply",
					Args: map[string]string{"plan_id": planID},
				}})
				for _, toolResult := range autoResults {
					if toolResult.Status == orchestrator.StatusSuccess {
						havenAccumulateUXSignal(&loopState.uxSignals, havenUXSignalHostOrganizeApplied)
					}
					emitter.emit(havenSSEEvent{Type: "tool_result", ToolResult: &havenSSEToolResult{
						CallID:  toolResult.CallID,
						Preview: havenSSEPreviewForToolResult(toolResult),
						Status:  string(toolResult.Status),
					}})
				}
				toolResults = append(toolResults, autoResults...)
			}
		}

		if handled, outcome := loopState.pendingApprovalOutcome(replyText, toolResults, emitter); handled {
			return *outcome
		}
		if handled, outcome := loopState.completedHostPlanApplyOutcome(toolResults); handled {
			return *outcome
		}

		if toolResultTurn, ok := havenToolResultTurn(toolResults, useStructuredPath); ok {
			loopState.conversation = append(loopState.conversation, toolResultTurn)
		}

		loopState.userMessage = ""
	}

	timeoutText := "That took longer than expected and I had to stop mid-way. Try a smaller folder or ask again."
	emitter.emit(havenSSEEvent{Type: "text_delta", Content: timeoutText})
	return havenChatLoopOutcome{
		modelResponse: loopState.lastModelResponse,
		assistantText: timeoutText,
		uxSignals:     loopState.uxSignals,
	}
}
