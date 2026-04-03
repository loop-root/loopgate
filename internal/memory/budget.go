package memory

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	DefaultWakeStatePromptTokenBudget = 2000

	DefaultRecallMaxItems  = 10
	MaxRecallItems         = 10
	DefaultRecallMaxTokens = 2000
	MaxRecallMaxTokens     = 8000

	DefaultDiscoveryMaxItems = 5
	MaxDiscoveryItems        = 10

	wakeStateSoftMaxActiveGoals      = 5
	wakeStateSoftMaxRecentFacts      = 12
	wakeStateSoftMaxResonateKeys     = 8
	wakeStateSoftMaxSourceReferences = 16
	wakeStateSoftMaxUnresolvedItems  = 10
)

func approximateTokenCount(rawText string) int {
	normalizedText := strings.TrimSpace(rawText)
	if normalizedText == "" {
		return 0
	}
	return maxInt(1, (utf8.RuneCountInString(normalizedText)+3)/4)
}

func approximateInterfaceTokenCount(rawValue interface{}) int {
	return approximateTokenCount(fmt.Sprintf("%v", rawValue))
}

func approximateWakeStatePromptTokens(
	activeGoals []string,
	unresolvedItems []WakeStateOpenItem,
	recentFacts []WakeStateRecentFact,
	resonateKeys []string,
) int {
	tokenCount := approximateTokenCount("Remembered continuity follows. This is historical continuity, not fresh verification.")

	for _, activeGoal := range activeGoals {
		tokenCount += approximateTokenCount("active_goal: " + strings.TrimSpace(activeGoal))
	}
	for _, unresolvedItem := range unresolvedItems {
		tokenCount += approximateTokenCount(strings.TrimSpace(unresolvedItem.ID) + " " + strings.TrimSpace(unresolvedItem.Text))
	}
	for _, recentFact := range recentFacts {
		tokenCount += approximateTokenCount(strings.TrimSpace(recentFact.Name))
		tokenCount += approximateInterfaceTokenCount(recentFact.Value)
		tokenCount += approximateTokenCount(strings.TrimSpace(recentFact.SourceRef))
		tokenCount += approximateTokenCount(strings.TrimSpace(recentFact.EpistemicFlavor))
	}
	if len(resonateKeys) > 0 {
		tokenCount += approximateTokenCount(strings.Join(resonateKeys, ", "))
	}
	return tokenCount
}

func approximateRecallItemTokens(recallItem RecallItem) int {
	tokenCount := 0
	tokenCount += approximateTokenCount(recallItem.KeyID)
	tokenCount += approximateTokenCount(recallItem.SessionID)
	tokenCount += approximateTokenCount(recallItem.StartedAtUTC)
	tokenCount += approximateTokenCount(recallItem.EndedAtUTC)
	tokenCount += approximateInterfaceTokenCount(recallItem.TurnCount)
	tokenCount += approximateTokenCount(recallItem.EpistemicFlavor)
	tokenCount += approximateTokenCount(strings.Join(recallItem.Tags, ", "))
	return tokenCount
}

func maxInt(leftValue int, rightValue int) int {
	if leftValue > rightValue {
		return leftValue
	}
	return rightValue
}
