package loopgate

import (
	"strings"

	modelpkg "morph/internal/model"
)

// havenIsNonUserFacingAssistantPlaceholder reports literals that some model stacks echo
// instead of true empty content. They must not reach the Haven UI as assistant text.
func havenIsNonUserFacingAssistantPlaceholder(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "(no text in model response)":
		return true
	default:
		return false
	}
}

// havenUserMessageLikelyHostFolderAction is a narrow client-agnostic heuristic for when
// the operator probably expects host.folder.* / host.organize.* tools rather than chat-only.
func havenUserMessageLikelyHostFolderAction(raw string) bool {
	trimmedText := strings.TrimSpace(strings.ToLower(raw))
	if trimmedText == "" {
		return false
	}
	wantsHostWork := strings.Contains(trimmedText, "organize") || strings.Contains(trimmedText, "organise") ||
		strings.Contains(trimmedText, "cleanup") || strings.Contains(trimmedText, "clean up") ||
		strings.Contains(trimmedText, "clear out") || strings.Contains(trimmedText, "tidy") ||
		(strings.Contains(trimmedText, "list") && (strings.Contains(trimmedText, "file") || strings.Contains(trimmedText, "folder") || strings.Contains(trimmedText, "download"))) ||
		strings.Contains(trimmedText, "sort my") || strings.Contains(trimmedText, "declutter")
	hostScope := strings.Contains(trimmedText, "download") || strings.Contains(trimmedText, "desktop") ||
		strings.Contains(trimmedText, "file") || strings.Contains(trimmedText, "folder") ||
		strings.Contains(trimmedText, "mac") || strings.Contains(trimmedText, "disk") || strings.Contains(trimmedText, "drive") ||
		strings.Contains(trimmedText, "finder")
	return wantsHostWork && hostScope
}

// havenThreadHasPriorAssistantWork returns true if the conversation contains at least one
// non-empty assistant turn — indicating the model has already done some work (e.g. listed the folder).
func havenThreadHasPriorAssistantWork(conversation []modelpkg.ConversationTurn) bool {
	for _, turn := range conversation {
		if turn.Role == "assistant" && strings.TrimSpace(turn.Content) != "" {
			return true
		}
	}
	return false
}

func havenThreadContainsHostFolderUserIntent(conversation []modelpkg.ConversationTurn) bool {
	for _, turn := range conversation {
		if turn.Role != "user" {
			continue
		}
		if havenUserMessageLikelyHostFolderAction(turn.Content) {
			return true
		}
	}
	return false
}

func havenIsShortAffirmation(raw string) bool {
	trimmedText := strings.TrimSpace(strings.ToLower(raw))
	if trimmedText == "" {
		return false
	}
	switch trimmedText {
	case "y", "yes", "yeah", "yep", "sure", "ok", "okay", "please", "please do", "go ahead", "do it",
		"sounds good", "sounds great", "confirm", "confirmed", "proceed", "mhm", "uh huh":
		return true
	}
	if (strings.HasPrefix(trimmedText, "yes ") || strings.HasPrefix(trimmedText, "ok ") || strings.HasPrefix(trimmedText, "sure ")) && len(trimmedText) < 40 {
		return true
	}
	return false
}

// havenHostFolderProseNudgeApplies decides whether to auto-continue when the model answered with
// prose only. Follow-ups like "yes" do not match havenUserMessageLikelyHostFolderAction alone, but
// still need tool pressure when the thread already asked to organize Downloads/Desktop.
func havenHostFolderProseNudgeApplies(initialUserMessage string, conversationWithCurrentUser []modelpkg.ConversationTurn) bool {
	if havenUserMessageLikelyHostFolderAction(initialUserMessage) {
		return true
	}
	if !havenThreadContainsHostFolderUserIntent(conversationWithCurrentUser) {
		return false
	}
	trimmedText := strings.TrimSpace(strings.ToLower(initialUserMessage))
	if len(trimmedText) > 160 {
		return false
	}
	if havenIsShortAffirmation(trimmedText) {
		return true
	}
	if len(trimmedText) < 120 && (strings.Contains(trimmedText, "nicer") || strings.Contains(trimmedText, "neater") || strings.Contains(trimmedText, "whatever") ||
		strings.Contains(trimmedText, "you decide") || strings.Contains(trimmedText, "your call") || strings.Contains(trimmedText, "up to you")) {
		return true
	}
	return false
}
