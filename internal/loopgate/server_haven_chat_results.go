package loopgate

import (
	"encoding/json"
	"fmt"
	"strings"

	modelpkg "morph/internal/model"
	"morph/internal/orchestrator"
	"morph/internal/secrets"
	"morph/internal/threadstore"
)

func havenStructuredValidationErrorTurn(validationErrors []orchestrator.ToolCallValidationError) modelpkg.ConversationTurn {
	validationTurn := modelpkg.ConversationTurn{
		Role:      "user",
		Timestamp: threadstore.NowUTC(),
	}
	for _, validationError := range validationErrors {
		validationTurn.ToolResults = append(validationTurn.ToolResults, modelpkg.ToolResultBlock{
			ToolUseID: validationError.BlockID,
			ToolName:  validationError.BlockName,
			Content:   "Tool call rejected: " + validationError.Error() + ". Check the tool name and required arguments, then try again.",
			IsError:   true,
		})
	}
	return validationTurn
}

func havenStructuredToolResultTurn(toolResults []orchestrator.ToolResult) modelpkg.ConversationTurn {
	resultTurn := modelpkg.ConversationTurn{
		Role:      "user",
		Timestamp: threadstore.NowUTC(),
	}
	for _, toolResult := range toolResults {
		resultTurn.ToolResults = append(resultTurn.ToolResults, modelpkg.ToolResultBlock{
			ToolUseID: toolResult.CallID,
			ToolName:  toolResult.Capability,
			Content:   havenToolResultContent(toolResult),
			IsError:   toolResult.Status != orchestrator.StatusSuccess,
		})
	}
	return resultTurn
}

func havenToolResultFromCapabilityResponse(callID string, capabilityName string, capabilityResponse CapabilityResponse) (orchestrator.ToolResult, error) {
	if capabilityResponse.Status == ResponseStatusPendingApproval {
		return orchestrator.ToolResult{
			CallID:            callID,
			Capability:        capabilityName,
			Status:            orchestrator.StatusPendingApproval,
			Output:            havenPendingApprovalContent(capabilityResponse),
			Reason:            secrets.RedactText(capabilityResponse.DenialReason),
			ApprovalRequestID: strings.TrimSpace(capabilityResponse.ApprovalRequestID),
		}, nil
	}
	switch capabilityResponse.Status {
	case ResponseStatusSuccess:
		promptEligibleOutput, err := havenPromptEligibleOutput(capabilityResponse)
		if err != nil {
			return orchestrator.ToolResult{
				CallID:     callID,
				Capability: capabilityName,
				Status:     orchestrator.StatusError,
				Reason:     "invalid result classification from Loopgate",
			}, err
		}
		return orchestrator.ToolResult{
			CallID:     callID,
			Capability: capabilityName,
			Status:     orchestrator.StatusSuccess,
			Output:     promptEligibleOutput,
		}, nil
	case ResponseStatusDenied:
		return orchestrator.ToolResult{
			CallID:     callID,
			Capability: capabilityName,
			Status:     orchestrator.StatusDenied,
			Reason:     secrets.RedactText(capabilityResponse.DenialReason),
			DenialCode: strings.TrimSpace(capabilityResponse.DenialCode),
		}, nil
	case ResponseStatusError:
		return orchestrator.ToolResult{
			CallID:     callID,
			Capability: capabilityName,
			Status:     orchestrator.StatusError,
			Reason:     secrets.RedactText(capabilityResponse.DenialReason),
			DenialCode: strings.TrimSpace(capabilityResponse.DenialCode),
		}, nil
	default:
		return orchestrator.ToolResult{
			CallID:     callID,
			Capability: capabilityName,
			Status:     orchestrator.StatusError,
			Reason:     "unknown Loopgate response status",
		}, fmt.Errorf("unknown Loopgate response status %q", capabilityResponse.Status)
	}
}

func havenPendingApprovalContent(capabilityResponse CapabilityResponse) string {
	approvalReason := strings.TrimSpace(capabilityResponse.DenialReason)
	if approvalReason == "" {
		approvalReason = "Loopgate requires approval before this action can continue."
	}
	return approvalReason + " Open the Loopgate approval surface to allow or deny it."
}

// havenExtractOrganizePlanIDFromResults finds the plan_id from a successful host.organize.plan
// result so the loop can auto-apply without an extra model round-trip.
func havenExtractOrganizePlanIDFromResults(toolResults []orchestrator.ToolResult) string {
	for i := range toolResults {
		tr := &toolResults[i]
		if strings.TrimSpace(tr.Capability) != "host.organize.plan" || tr.Status != orchestrator.StatusSuccess {
			continue
		}
		var structured struct {
			PlanID string `json:"plan_id"`
		}
		if err := json.Unmarshal([]byte(tr.Output), &structured); err == nil {
			if id := strings.TrimSpace(structured.PlanID); id != "" {
				return id
			}
		}
	}
	return ""
}

func firstHavenPendingApprovalToolResult(toolResults []orchestrator.ToolResult) *orchestrator.ToolResult {
	for i := range toolResults {
		if toolResults[i].Status != orchestrator.StatusPendingApproval {
			continue
		}
		if strings.TrimSpace(toolResults[i].ApprovalRequestID) == "" {
			continue
		}
		tr := toolResults[i]
		return &tr
	}
	return nil
}

// havenApprovalWaitSuffix is the user-facing instruction appended when the loop
// pauses at an approval gate. Extracted as a constant so the SSE emitter can
// send just the suffix when the model already produced a prose prefix that was
// emitted as an earlier text_delta in the same iteration.
const havenApprovalWaitSuffix = `Approve the security prompt in Haven when it appears. After you approve, I’ll finish applying the plan. If you already approved, say "continue" and I’ll pick up from the tool result.`

func havenAssistantTextWaitingForLoopgate(modelAssistantPrefix string) string {
	trimmedPrefix := strings.TrimSpace(modelAssistantPrefix)
	if trimmedPrefix != "" {
		return trimmedPrefix + "\n\n" + havenApprovalWaitSuffix
	}
	return "This step needs your confirmation before any files move on your Mac.\n\n" + havenApprovalWaitSuffix
}

// havenSSEPreviewForToolResult returns a short, display-friendly status string
// for a tool_result SSE event. It must not include raw tool output, model-generated
// text, or any material that has not already been redacted by the capability pipeline.
// The preview is purely cosmetic; the authoritative result is in the tool output
// stored separately in the thread log.
func havenSSEPreviewForToolResult(tr orchestrator.ToolResult) string {
	switch tr.Status {
	case orchestrator.StatusPendingApproval:
		return "awaiting approval"
	case orchestrator.StatusSuccess:
		switch strings.TrimSpace(tr.Capability) {
		case "host.folder.list":
			return "listing ready"
		case "host.organize.plan":
			return "plan ready"
		case "host.plan.apply":
			return "applied"
		default:
			return "done"
		}
	case orchestrator.StatusDenied:
		if code := strings.TrimSpace(tr.DenialCode); code != "" {
			return "denied: " + code
		}
		return "denied"
	default:
		return string(tr.Status)
	}
}

func havenChatFallbackText(err error) string {
	if err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "rate limited") || strings.Contains(msg, "429") {
			return "The model API is rate-limiting this account right now. Try again in a minute, or add credits to your Anthropic account."
		}
	}
	return "I could not reach the model right now. Check your model connection in Settings."
}

func havenCapabilityNeedsHostOrganizeApprovalUX(capabilityName string) bool {
	switch strings.TrimSpace(capabilityName) {
	case "host.organize.plan", "host.plan.apply":
		return true
	default:
		return false
	}
}

func havenAccumulateUXSignal(signals *[]string, signal string) {
	if signals == nil || strings.TrimSpace(signal) == "" {
		return
	}
	for _, existing := range *signals {
		if existing == signal {
			return
		}
	}
	*signals = append(*signals, signal)
}

func havenToolResultContent(toolResult orchestrator.ToolResult) string {
	var rawContent string
	switch {
	case strings.TrimSpace(toolResult.Output) != "":
		rawContent = toolResult.Output
	case strings.TrimSpace(toolResult.Reason) != "":
		rawContent = toolResult.Reason
	default:
		rawContent = string(toolResult.Status)
	}
	if code := strings.TrimSpace(toolResult.DenialCode); code != "" &&
		(toolResult.Status == orchestrator.StatusDenied || toolResult.Status == orchestrator.StatusError) {
		rawContent = strings.TrimSpace(rawContent)
		if rawContent == "" {
			rawContent = "(no message)"
		}
		rawContent = rawContent + " (denial_code: " + code + ")"
	}
	return capHavenToolResultContentForModel(toolResult.Capability, rawContent)
}

func capHavenToolResultContentForModel(capabilityName string, content string) string {
	if content == "" {
		return content
	}
	maxRunes := defaultHavenToolResultMaxRunes
	if configuredMaxRunes, found := havenToolResultMaxRunesByCapability[strings.TrimSpace(capabilityName)]; found && configuredMaxRunes > 0 {
		maxRunes = configuredMaxRunes
	}
	contentRunes := []rune(content)
	if len(contentRunes) <= maxRunes {
		return content
	}
	truncatedContent := string(contentRunes[:maxRunes])
	return truncatedContent + fmt.Sprintf(
		"\n\n[Haven truncated tool output to %d Unicode code points for capability %q; use narrower reads or paging if you need the rest.]",
		maxRunes,
		capabilityName,
	)
}

func havenPromptEligibleOutput(capabilityResponse CapabilityResponse) (string, error) {
	if capabilityResponse.Status != ResponseStatusSuccess {
		return "", nil
	}
	resultClassification, err := capabilityResponse.ResultClassification()
	if err != nil {
		return "", err
	}
	if !resultClassification.PromptEligible() {
		return "", nil
	}
	promptEligibleStructuredResult := make(map[string]interface{})
	for fieldName, fieldValue := range capabilityResponse.StructuredResult {
		fieldMetadata, found := capabilityResponse.FieldsMeta[fieldName]
		if !found || !fieldMetadata.PromptEligible {
			continue
		}
		promptEligibleStructuredResult[fieldName] = fieldValue
	}
	if len(promptEligibleStructuredResult) == 0 {
		return "", nil
	}
	promptBytes, err := json.MarshalIndent(promptEligibleStructuredResult, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal prompt-eligible structured result: %w", err)
	}
	return string(promptBytes), nil
}

func havenPromptEligibleToolResults(toolResults []orchestrator.ToolResult) []orchestrator.ToolResult {
	filteredToolResults := make([]orchestrator.ToolResult, 0, len(toolResults))
	for _, toolResult := range toolResults {
		if toolResult.Status == orchestrator.StatusSuccess && strings.TrimSpace(toolResult.Output) == "" {
			continue
		}
		filteredToolResults = append(filteredToolResults, toolResult)
	}
	return filteredToolResults
}
