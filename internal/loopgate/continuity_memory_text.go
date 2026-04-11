package loopgate

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type discoverSlotPreferenceRule struct {
	anchorTupleKey string
	requiredTags   []string
}

var discoverSlotPreferenceRules = []discoverSlotPreferenceRule{
	{anchorTupleKey: "v1:usr_profile:identity:fact:name", requiredTags: []string{"name"}},
	{anchorTupleKey: "v1:usr_profile:identity:fact:preferred_name", requiredTags: []string{"preferred", "name"}},
	{anchorTupleKey: "v1:usr_profile:settings:fact:timezone", requiredTags: []string{"timezone"}},
	{anchorTupleKey: "v1:usr_profile:settings:fact:locale", requiredTags: []string{"locale"}},
}

// Discover slot preference stays on a tiny allowlist of stable profile slots because broad
// anchor bias would distort general recall and make retrieval drift harder to detect in review.
func detectDiscoverSlotPreference(rawQuery string) string {
	queryTags := tokenizeLoopgateMemoryText(rawQuery)
	if len(queryTags) == 0 {
		return ""
	}
	queryTagSet := make(map[string]struct{}, len(queryTags))
	for _, queryTag := range queryTags {
		queryTagSet[queryTag] = struct{}{}
	}
	if containsAnyLoopgateMemoryTag(queryTagSet, "project", "task", "goal", "github", "history", "recent", "work", "context") {
		return ""
	}
	if containsAnyLoopgateMemoryTag(queryTagSet, "and", "both") {
		return ""
	}
	if !containsAnyLoopgateMemoryTag(queryTagSet, "user", "profile") {
		return ""
	}

	matchedAnchorTupleKeys := make([]string, 0, 1)
	for _, slotRule := range discoverSlotPreferenceRules {
		if hasAllLoopgateMemoryTags(queryTagSet, slotRule.requiredTags...) {
			matchedAnchorTupleKeys = append(matchedAnchorTupleKeys, slotRule.anchorTupleKey)
		}
	}
	if len(matchedAnchorTupleKeys) == 1 {
		if matchedAnchorTupleKeys[0] == "v1:usr_profile:identity:fact:name" &&
			containsAnyLoopgateMemoryTag(queryTagSet, "user", "profile", "identity") &&
			!containsAnyLoopgateMemoryTag(queryTagSet, "legal", "formal", "given", "full") {
			// In operator-facing profile queries, "name" normally means the display/current
			// identity slot, not a stricter legal-name field. Prefer `preferred_name` when the
			// query stays generic, then let exact-slot lookup fall back to `name` if needed.
			return "v1:usr_profile:identity:fact:preferred_name"
		}
		return matchedAnchorTupleKeys[0]
	}
	if len(matchedAnchorTupleKeys) == 2 &&
		containsStringValue(matchedAnchorTupleKeys, "v1:usr_profile:identity:fact:name") &&
		containsStringValue(matchedAnchorTupleKeys, "v1:usr_profile:identity:fact:preferred_name") {
		return "v1:usr_profile:identity:fact:preferred_name"
	}
	return ""
}

func containsAnyLoopgateMemoryTag(queryTagSet map[string]struct{}, wantedTags ...string) bool {
	for _, wantedTag := range wantedTags {
		if _, found := queryTagSet[wantedTag]; found {
			return true
		}
	}
	return false
}

func hasAllLoopgateMemoryTags(queryTagSet map[string]struct{}, wantedTags ...string) bool {
	for _, wantedTag := range wantedTags {
		if _, found := queryTagSet[wantedTag]; !found {
			return false
		}
	}
	return true
}

func containsStringValue(values []string, wantedValue string) bool {
	for _, value := range values {
		if value == wantedValue {
			return true
		}
	}
	return false
}

func actualContinuityPayloadBytes(events []ContinuityEventInput) int {
	totalBytes := 0
	for _, continuityEvent := range events {
		payloadBytes, _ := json.Marshal(continuityEvent)
		totalBytes += len(payloadBytes)
	}
	return totalBytes
}

func actualContinuityPromptTokens(events []ContinuityEventInput) int {
	tokenCount := 0
	for _, continuityEvent := range events {
		payloadBytes, _ := json.Marshal(continuityEvent)
		tokenCount += approximateLoopgateTokenCount(string(payloadBytes))
	}
	return tokenCount
}

func actualObservedContinuityPayloadBytes(events []continuityObservedEventRecord) int {
	totalBytes := 0
	for _, continuityEvent := range events {
		payloadBytes, _ := json.Marshal(continuityEvent)
		totalBytes += len(payloadBytes)
	}
	return totalBytes
}

func actualObservedContinuityPromptTokens(events []continuityObservedEventRecord) int {
	tokenCount := 0
	for _, continuityEvent := range events {
		payloadBytes, _ := json.Marshal(continuityEvent)
		tokenCount += approximateLoopgateTokenCount(string(payloadBytes))
	}
	return tokenCount
}

func approximateLoopgateRecallTokens(recalledItem MemoryRecallItem) int {
	tokenCount := approximateLoopgateTokenCount(recalledItem.KeyID + " " + recalledItem.ThreadID + " " + recalledItem.DistillateID)
	for _, activeGoal := range recalledItem.ActiveGoals {
		tokenCount += approximateLoopgateTokenCount(activeGoal)
	}
	for _, unresolvedItem := range recalledItem.UnresolvedItems {
		tokenCount += approximateLoopgateTokenCount(unresolvedItem.ID + " " + unresolvedItem.Text)
	}
	for _, factRecord := range recalledItem.Facts {
		tokenCount += approximateLoopgateTokenCount(factRecord.Name)
		tokenCount += approximateLoopgateTokenCount(fmt.Sprintf("%v", factRecord.Value))
		tokenCount += approximateLoopgateTokenCount(factRecord.SourceRef)
	}
	return tokenCount
}

func approximateLoopgateTokenCount(rawText string) int {
	normalizedText := strings.TrimSpace(rawText)
	if normalizedText == "" {
		return 0
	}
	return max(1, (len([]rune(normalizedText))+3)/4)
}

func normalizeLoopgateMemoryTags(rawTags []string) []string {
	tagSet := make(map[string]struct{}, len(rawTags))
	for _, rawTag := range rawTags {
		for _, normalizedTag := range tokenizeLoopgateMemoryText(rawTag) {
			tagSet[normalizedTag] = struct{}{}
		}
	}
	return normalizedLoopgateTagSet(tagSet)
}

func normalizedLoopgateTagSet(tagSet map[string]struct{}) []string {
	normalizedTags := make([]string, 0, len(tagSet))
	for normalizedTag := range tagSet {
		normalizedTags = append(normalizedTags, normalizedTag)
	}
	sort.Strings(normalizedTags)
	return normalizedTags
}

func recordLoopgateMemoryTags(tagSet map[string]struct{}, rawTexts ...string) {
	for _, rawText := range rawTexts {
		for _, normalizedTag := range tokenizeLoopgateMemoryText(rawText) {
			tagSet[normalizedTag] = struct{}{}
		}
	}
}

func tokenizeLoopgateMemoryText(rawText string) []string {
	normalizedText := strings.ToLower(strings.TrimSpace(rawText))
	if normalizedText == "" {
		return nil
	}
	tokenSet := map[string]struct{}{}
	currentToken := strings.Builder{}
	flushToken := func() {
		tokenValue := currentToken.String()
		currentToken.Reset()
		if len(tokenValue) < 3 || len(tokenValue) > 32 {
			return
		}
		if isAllDigits(tokenValue) {
			return
		}
		tokenSet[tokenValue] = struct{}{}
	}
	for _, currentRune := range normalizedText {
		switch {
		case currentRune >= 'a' && currentRune <= 'z':
			currentToken.WriteRune(currentRune)
		case currentRune >= '0' && currentRune <= '9':
			currentToken.WriteRune(currentRune)
		default:
			flushToken()
		}
	}
	flushToken()
	return normalizedLoopgateTagSet(tokenSet)
}

func isAllDigits(rawText string) bool {
	if rawText == "" {
		return false
	}
	for _, currentRune := range rawText {
		if currentRune < '0' || currentRune > '9' {
			return false
		}
	}
	return true
}

func appendWithoutDuplicate(values []string, newValue string) []string {
	for _, existingValue := range values {
		if existingValue == newValue {
			return values
		}
	}
	return append(values, newValue)
}

func removeStringValue(values []string, removedValue string) []string {
	filteredValues := values[:0]
	for _, existingValue := range values {
		if existingValue == removedValue {
			continue
		}
		filteredValues = append(filteredValues, existingValue)
	}
	return filteredValues
}
