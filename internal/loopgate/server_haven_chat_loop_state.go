package loopgate

import (
	"strings"

	modelpkg "morph/internal/model"
	"morph/internal/orchestrator"
	"morph/internal/threadstore"
)

type havenChatLoopState struct {
	conversation              []modelpkg.ConversationTurn
	userMessage               string
	lastModelResponse         modelpkg.Response
	uxSignals                 []string
	proseOnlyHostFolderNudges int
	hostPlanApplyNudgeCount   int
	sawHostFolderToolRound    bool
	awaitingHostPlanApply     bool
}

func newHavenChatLoopState(conversation []modelpkg.ConversationTurn, initialUserMessage string) havenChatLoopState {
	return havenChatLoopState{
		conversation: append([]modelpkg.ConversationTurn(nil), conversation...),
		userMessage:  initialUserMessage,
	}
}

func (state *havenChatLoopState) buildTurnRuntimeFacts(baseRuntimeFacts []string, hostFolderOrganizeToolkitAvailable bool, iteration int) []string {
	if iteration == 0 {
		return append([]string(nil), baseRuntimeFacts...)
	}

	turnRuntimeFacts := []string{
		havenToolLoopContinuationFact,
		modelpkg.HavenConstrainedNativeToolsRuntimeFact,
	}
	if hostFolderOrganizeToolkitAvailable && (state.sawHostFolderToolRound || state.awaitingHostPlanApply) {
		turnRuntimeFacts = append(turnRuntimeFacts, havenToolLoopSlimOrganizeFact)
	}
	return turnRuntimeFacts
}

func (state *havenChatLoopState) modelUserMessage(iteration int) string {
	modelUserMessage := state.userMessage
	if iteration > 0 && strings.TrimSpace(modelUserMessage) == "" {
		modelUserMessage = havenToolFollowupUserNudge
	}
	return modelUserMessage
}

func havenChatTurnAttachments(iteration int, initialAttachments []modelpkg.Attachment) []modelpkg.Attachment {
	if iteration == 0 {
		return initialAttachments
	}
	return nil
}

func (state *havenChatLoopState) appendUserTurnIfPresent() {
	if state.userMessage == "" {
		return
	}
	state.conversation = append(state.conversation, modelpkg.ConversationTurn{
		Role:      "user",
		Content:   state.userMessage,
		Timestamp: threadstore.NowUTC(),
	})
}

func (state *havenChatLoopState) appendAssistantTurn(replyText string, toolUseBlocks []modelpkg.ToolUseBlock, useStructuredPath bool) {
	assistantTurn := modelpkg.ConversationTurn{
		Role:      "assistant",
		Content:   replyText,
		Timestamp: threadstore.NowUTC(),
	}
	if useStructuredPath {
		assistantTurn.ToolCalls = toolUseBlocks
	}
	state.conversation = append(state.conversation, assistantTurn)
}

func (state *havenChatLoopState) handleNoToolResponse(replyText string, initialUserMessage string, hostFolderOrganizeToolkitAvailable bool, iteration int, emitter *havenSSEEmitter) (bool, *havenChatLoopOutcome) {
	if replyText == "" {
		replyText = "I didn't get a clear reply from the model. Try again in a moment."
	}

	if state.awaitingHostPlanApply &&
		hostFolderOrganizeToolkitAvailable &&
		state.hostPlanApplyNudgeCount < maxHavenHostPlanApplyNudges &&
		iteration+1 < maxHavenToolIterations {
		state.hostPlanApplyNudgeCount++
		state.conversation = append(state.conversation, modelpkg.ConversationTurn{
			Role:      "assistant",
			Content:   replyText,
			Timestamp: threadstore.NowUTC(),
		})
		state.userMessage = havenHostPlanApplyActNowNudge
		return true, nil
	}

	if hostFolderOrganizeToolkitAvailable &&
		!state.sawHostFolderToolRound &&
		havenHostFolderProseNudgeApplies(initialUserMessage, state.conversation) &&
		state.proseOnlyHostFolderNudges < maxHavenHostFolderProseOnlyNudges &&
		iteration+1 < maxHavenToolIterations {
		state.proseOnlyHostFolderNudges++
		state.conversation = append(state.conversation, modelpkg.ConversationTurn{
			Role:      "assistant",
			Content:   replyText,
			Timestamp: threadstore.NowUTC(),
		})
		if havenThreadHasPriorAssistantWork(state.conversation) {
			state.userMessage = havenHostFolderPlanNowNudge
		} else {
			state.userMessage = havenHostFolderActNowNudge
		}
		return true, nil
	}

	emitter.emit(havenSSEEvent{Type: "text_delta", Content: replyText})
	return true, &havenChatLoopOutcome{
		modelResponse: state.lastModelResponse,
		assistantText: replyText,
		uxSignals:     state.uxSignals,
	}
}

func (state *havenChatLoopState) observeToolResults(parsedCalls []orchestrator.ToolCall, toolResults []orchestrator.ToolResult) {
	for _, parsedCall := range parsedCalls {
		if strings.HasPrefix(strings.TrimSpace(parsedCall.Name), "host.") {
			state.sawHostFolderToolRound = true
			break
		}
	}

	for _, toolResult := range toolResults {
		if toolResult.Status == orchestrator.StatusSuccess && toolResult.Capability == "host.plan.apply" {
			havenAccumulateUXSignal(&state.uxSignals, havenUXSignalHostOrganizeApplied)
		}
	}

	for _, toolResult := range toolResults {
		capabilityName := strings.TrimSpace(toolResult.Capability)
		if capabilityName == "host.organize.plan" && toolResult.Status == orchestrator.StatusSuccess {
			state.awaitingHostPlanApply = true
		}
		if capabilityName == "host.plan.apply" {
			state.awaitingHostPlanApply = false
		}
	}
}

func (state *havenChatLoopState) pendingApprovalOutcome(replyText string, toolResults []orchestrator.ToolResult, emitter *havenSSEEmitter) (bool, *havenChatLoopOutcome) {
	pendingResult := firstHavenPendingApprovalToolResult(toolResults)
	if pendingResult == nil {
		return false, nil
	}

	if havenCapabilityNeedsHostOrganizeApprovalUX(pendingResult.Capability) {
		havenAccumulateUXSignal(&state.uxSignals, havenUXSignalHostOrganizeApprovalPending)
	}
	assistantText := havenAssistantTextWaitingForLoopgate(replyText)
	emitter.emit(havenSSEEvent{Type: "text_delta", Content: assistantText})
	emitter.emit(havenSSEEvent{Type: "approval_needed", ApprovalNeeded: &havenSSEApproval{
		ApprovalID: pendingResult.ApprovalRequestID,
		Capability: pendingResult.Capability,
	}})
	return true, &havenChatLoopOutcome{
		modelResponse:      state.lastModelResponse,
		assistantText:      assistantText,
		approvalStatus:     "approval_required",
		approvalID:         pendingResult.ApprovalRequestID,
		approvalCapability: pendingResult.Capability,
		uxSignals:          state.uxSignals,
	}
}
