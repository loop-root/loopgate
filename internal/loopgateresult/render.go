package loopgateresult

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"loopgate/internal/loopgate"
	"loopgate/internal/secrets"
)

func SanitizedApprovalMetadata(rawMetadata map[string]interface{}) map[string]interface{} {
	sanitizedMetadata := make(map[string]interface{}, len(rawMetadata))
	for metadataKey, metadataValue := range rawMetadata {
		if metadataKey == "approval_decision_nonce" {
			continue
		}
		sanitizedMetadata[metadataKey] = metadataValue
	}
	return sanitizedMetadata
}

func StructuredDisplayText(structuredResult map[string]interface{}) string {
	return structuredText(structuredResult, true)
}

func StructuredPromptText(structuredResult map[string]interface{}) string {
	return structuredText(structuredResult, false)
}

func FormatDisplayResponse(capabilityResponse loopgate.CapabilityResponse) string {
	switch capabilityResponse.Status {
	case loopgate.ResponseStatusSuccess:
		resultClassification, err := capabilityResponse.ResultClassification()
		if err != nil {
			return "Error: invalid result classification from Loopgate."
		}
		if resultClassification.AuditOnly() {
			if resultClassification.Quarantined() {
				return "Result quarantined by Loopgate and recorded to audit only."
			}
			return "Result recorded to audit only."
		}
		displayText := StructuredDisplayText(capabilityResponse.StructuredResult)
		if resultClassification.Quarantined() {
			if strings.TrimSpace(displayText) != "" {
				return displayText + "\n\nSource remains quarantined in Loopgate."
			}
			return "Result quarantined by Loopgate."
		}
		return displayText
	case loopgate.ResponseStatusDenied:
		return "Denied: " + secrets.RedactText(capabilityResponse.DenialReason)
	case loopgate.ResponseStatusError:
		return "Error: " + secrets.RedactText(capabilityResponse.DenialReason)
	default:
		return StructuredDisplayText(SanitizedApprovalMetadata(capabilityResponse.Metadata))
	}
}

func PromptEligibleOutput(capabilityResponse loopgate.CapabilityResponse) (string, error) {
	if capabilityResponse.Status != loopgate.ResponseStatusSuccess {
		return "", nil
	}

	resultClassification, err := capabilityResponse.ResultClassification()
	if err != nil {
		return "", err
	}
	if !resultClassification.PromptEligible() {
		return "", nil
	}
	promptStructuredResult := promptEligibleStructuredResult(capabilityResponse)
	return StructuredPromptText(promptStructuredResult), nil
}

func ToolResultFromCapabilityResponse(callID string, capabilityResponse loopgate.CapabilityResponse) (ToolResult, error) {
	switch capabilityResponse.Status {
	case loopgate.ResponseStatusSuccess:
		promptOutput, err := PromptEligibleOutput(capabilityResponse)
		if err != nil {
			return ToolResult{
				CallID: callID,
				Status: StatusError,
				Reason: "invalid result classification from Loopgate",
			}, err
		}
		return ToolResult{
			CallID: callID,
			Status: StatusSuccess,
			Output: promptOutput,
		}, nil
	case loopgate.ResponseStatusDenied:
		return ToolResult{
			CallID:     callID,
			Status:     StatusDenied,
			Reason:     secrets.RedactText(capabilityResponse.DenialReason),
			DenialCode: strings.TrimSpace(capabilityResponse.DenialCode),
		}, nil
	case loopgate.ResponseStatusError:
		return ToolResult{
			CallID:     callID,
			Status:     StatusError,
			Reason:     secrets.RedactText(capabilityResponse.DenialReason),
			DenialCode: strings.TrimSpace(capabilityResponse.DenialCode),
		}, nil
	default:
		return ToolResult{
			CallID: callID,
			Status: StatusError,
			Reason: "unknown Loopgate response status",
		}, fmt.Errorf("unknown Loopgate response status %q", capabilityResponse.Status)
	}
}

func PromptEligibleToolResults(toolResults []ToolResult) []ToolResult {
	filteredResults := make([]ToolResult, 0, len(toolResults))
	for _, toolResult := range toolResults {
		if toolResult.Status == StatusSuccess && strings.TrimSpace(toolResult.Output) == "" {
			continue
		}
		filteredResults = append(filteredResults, toolResult)
	}
	return filteredResults
}

func SummarizeToolResults(toolCalls []ToolCall, toolResults []ToolResult) string {
	if len(toolResults) == 0 {
		return ""
	}

	callNameByID := make(map[string]string, len(toolCalls))
	callIDsInOrder := make([]string, 0, len(toolCalls))
	for _, toolCall := range toolCalls {
		callNameByID[toolCall.ID] = toolCall.Name
		callIDsInOrder = append(callIDsInOrder, toolCall.ID)
	}

	resultByCallID := make(map[string]ToolResult, len(toolResults))
	successCount := 0
	deniedCount := 0
	errorCount := 0
	for _, toolResult := range toolResults {
		resultByCallID[toolResult.CallID] = toolResult
		switch toolResult.Status {
		case StatusSuccess:
			successCount++
		case StatusDenied:
			deniedCount++
		case StatusError:
			errorCount++
		}
	}

	summaryLines := make([]string, 0, len(toolResults)+1)
	switch {
	case successCount == len(toolResults):
		summaryLines = append(summaryLines, fmt.Sprintf("Loopgate completed %d capability check(s) successfully.", len(toolResults)))
	case deniedCount == len(toolResults):
		summaryLines = append(summaryLines, fmt.Sprintf("Loopgate denied %d capability check(s).", len(toolResults)))
	case errorCount == len(toolResults):
		summaryLines = append(summaryLines, fmt.Sprintf("Loopgate encountered errors in %d capability check(s).", len(toolResults)))
	default:
		summaryLines = append(summaryLines, fmt.Sprintf("Loopgate completed this request partially: %d succeeded, %d denied, %d failed.", successCount, deniedCount, errorCount))
	}

	summarizedCallIDs := make(map[string]struct{}, len(toolResults))
	for _, callID := range callIDsInOrder {
		toolResult, found := resultByCallID[callID]
		if !found {
			continue
		}
		summarizedCallIDs[callID] = struct{}{}
		summaryLines = append(summaryLines, summarizeSingleToolResult(callNameByID[callID], toolResult))
	}

	remainingCallIDs := make([]string, 0, len(toolResults))
	for callID := range resultByCallID {
		if _, alreadySummarized := summarizedCallIDs[callID]; alreadySummarized {
			continue
		}
		remainingCallIDs = append(remainingCallIDs, callID)
	}
	sort.Strings(remainingCallIDs)
	for _, callID := range remainingCallIDs {
		summaryLines = append(summaryLines, summarizeSingleToolResult(callNameByID[callID], resultByCallID[callID]))
	}

	return strings.Join(summaryLines, "\n")
}

func summarizeSingleToolResult(capabilityName string, toolResult ToolResult) string {
	if capabilityName == "" {
		capabilityName = toolResult.CallID
	}
	switch toolResult.Status {
	case StatusSuccess:
		return fmt.Sprintf("- %s: ok", capabilityName)
	case StatusDenied:
		return fmt.Sprintf("- %s: denied (%s)", capabilityName, summarizeToolResultReason(toolResult))
	case StatusError:
		return fmt.Sprintf("- %s: error (%s)", capabilityName, summarizeToolResultReason(toolResult))
	default:
		return fmt.Sprintf("- %s: %s", capabilityName, toolResult.Status)
	}
}

func summarizeToolResultReason(toolResult ToolResult) string {
	trimmedReason := strings.TrimSpace(toolResult.Reason)
	if trimmedReason != "" {
		return trimmedReason
	}
	trimmedOutput := strings.TrimSpace(toolResult.Output)
	if trimmedOutput != "" {
		return trimmedOutput
	}
	return "no detail available"
}

func structuredText(structuredResult map[string]interface{}, prettyJSON bool) string {
	if len(structuredResult) == 0 {
		return ""
	}
	if contentValue, ok := structuredResult["content"].(string); ok {
		return contentValue
	}
	if entriesValue, ok := structuredResult["entries"].([]interface{}); ok {
		lines := make([]string, 0, len(entriesValue))
		for _, entryValue := range entriesValue {
			lines = append(lines, fmt.Sprint(entryValue))
		}
		return strings.Join(lines, "\n")
	}

	var encodedBytes []byte
	var err error
	if prettyJSON {
		encodedBytes, err = json.MarshalIndent(structuredResult, "", "  ")
	} else {
		encodedBytes, err = json.Marshal(structuredResult)
	}
	if err != nil {
		return fmt.Sprint(structuredResult)
	}
	return string(encodedBytes)
}

func promptEligibleStructuredResult(capabilityResponse loopgate.CapabilityResponse) map[string]interface{} {
	if len(capabilityResponse.StructuredResult) == 0 || len(capabilityResponse.FieldsMeta) == 0 {
		return map[string]interface{}{}
	}
	filteredStructuredResult := make(map[string]interface{})
	for fieldName, fieldValue := range capabilityResponse.StructuredResult {
		fieldMetadata, found := capabilityResponse.FieldsMeta[fieldName]
		if !found || !fieldMetadata.PromptEligible {
			continue
		}
		filteredStructuredResult[fieldName] = fieldValue
	}
	return filteredStructuredResult
}
