package main

import (
	"strings"

	modelpkg "morph/internal/model"
)

// maxModelConversationTurns caps how many ConversationTurn entries we send to the
// model per request. Older turns are dropped while preserving valid tool_use /
// tool_result pairing for the native API and XML tool protocol edges.
const maxModelConversationTurns = 48

// stripCurrentUserTurnForModel removes user turns whose content matches currentUserMessage.
// The same text is sent as Request.UserMessage, so leaving it in Conversation would duplicate
// the operator's latest message in the model prompt.
func stripCurrentUserTurnForModel(turns []modelpkg.ConversationTurn, currentUserMessage string) []modelpkg.ConversationTurn {
	needle := strings.TrimSpace(currentUserMessage)
	if needle == "" || len(turns) == 0 {
		return turns
	}
	out := make([]modelpkg.ConversationTurn, 0, len(turns))
	for _, t := range turns {
		if strings.EqualFold(strings.TrimSpace(t.Role), "user") && strings.TrimSpace(t.Content) == needle {
			continue
		}
		out = append(out, t)
	}
	return out
}

func windowConversationForModel(turns []modelpkg.ConversationTurn, maxTurns int) []modelpkg.ConversationTurn {
	if maxTurns <= 0 || len(turns) <= maxTurns {
		return turns
	}
	start := len(turns) - maxTurns
	if start < 0 {
		start = 0
	}
	start = fixConversationWindowStart(turns, start)
	return turns[start:]
}

func fixConversationWindowStart(turns []modelpkg.ConversationTurn, start int) int {
	if len(turns) == 0 {
		return 0
	}
	if start >= len(turns) {
		return len(turns) - 1
	}
	for start > 0 && conversationTurnNeedsPrecedingForNativeToolResults(turns[start]) {
		start--
	}
	for start > 0 && conversationTurnNeedsPrecedingForXMLToolMessage(turns[start]) {
		start--
	}
	for start < len(turns) && orphanedAssistantNativeToolTurn(turns, start) {
		start++
	}
	if start >= len(turns) {
		return len(turns) - 1
	}
	return start
}

func conversationTurnNeedsPrecedingForNativeToolResults(t modelpkg.ConversationTurn) bool {
	if len(t.ToolResults) == 0 {
		return false
	}
	role := strings.ToLower(strings.TrimSpace(t.Role))
	return role == "" || role == "user"
}

func conversationTurnNeedsPrecedingForXMLToolMessage(t modelpkg.ConversationTurn) bool {
	return strings.EqualFold(strings.TrimSpace(t.Role), "tool")
}

func orphanedAssistantNativeToolTurn(turns []modelpkg.ConversationTurn, start int) bool {
	t := turns[start]
	if !strings.EqualFold(strings.TrimSpace(t.Role), "assistant") || len(t.ToolCalls) == 0 {
		return false
	}
	if start+1 >= len(turns) {
		return true
	}
	next := turns[start+1]
	if strings.EqualFold(strings.TrimSpace(next.Role), "user") && len(next.ToolResults) > 0 {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(next.Role), "tool") {
		return false
	}
	return true
}
