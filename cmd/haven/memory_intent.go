package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"morph/internal/loopgate"
)

var (
	explicitRememberIntentPattern = regexp.MustCompile(`(?i)\b(remember|don't forget|do not forget|keep in mind|for future reference)\b`)
	rememberMyNamePattern         = regexp.MustCompile(`(?i)\b(?:please\s+)?remember(?:\s+that)?\s+my name is\s+([A-Za-z][A-Za-z .'\-]{0,40})`)
	myNamePattern                 = regexp.MustCompile(`(?i)\bmy name is\s+([A-Za-z][A-Za-z .'\-]{0,40})`)
	callMePattern                 = regexp.MustCompile(`(?i)\bcall me\s+([A-Za-z][A-Za-z .'\-]{0,40})`)
	rememberRoutinePattern        = regexp.MustCompile(`(?i)\b(?:please\s+)?remember(?:\s+that)?\s+every\s+(monday|tuesday|wednesday|thursday|friday|saturday|sunday)\b(.+)`)
	rememberFavoritePattern       = regexp.MustCompile(`(?i)\b(?:please\s+)?remember(?:\s+that)?\s+my favorite\s+([A-Za-z0-9 _-]{1,24})\s+is\s+(.+)`)
	rememberPreferencePattern     = regexp.MustCompile(`(?i)\b(?:please\s+)?remember(?:\s+that)?\s+i prefer\s+(.+)`)
)

type memoryTurnDirective struct {
	RuntimeFacts []string
}

func (app *HavenApp) buildMemoryTurnDirective(ctx context.Context, userMessage string) memoryTurnDirective {
	rememberRequest, explicitIntent := detectDeterministicRememberRequest(userMessage)
	if !app.hasResidentCapability("memory.remember") {
		if explicitIntent {
			return memoryTurnDirective{
				RuntimeFacts: []string{
					"The user sounds like they want something remembered, but memory.remember is not available in this session. Say that plainly instead of pretending it was stored.",
				},
			}
		}
		return memoryTurnDirective{}
	}
	if rememberRequest == nil {
		if explicitIntent {
			return memoryTurnDirective{
				RuntimeFacts: []string{
					"The user's latest message sounds like a memory request. Use memory.remember with a stable fact_key and concise fact_value when you are confident they want it in continuity (preferences, routines, profile, standing goals). Loopgate may reject unsafe or malformed candidates — be honest if the tool fails. If the fact is ambiguous, ask one brief clarifying question instead of storing.",
				},
			}
		}
		// No per-turn nudge to write memory; RUNTIME CONTRACT and deterministic patterns handle the rest.
		return memoryTurnDirective{}
	}

	rememberResponse, err := app.loopgateClient.RememberMemoryFact(ctx, *rememberRequest)
	if err != nil {
		return memoryTurnDirective{
			RuntimeFacts: []string{
				fmt.Sprintf("A deterministic memory write was attempted for this turn but failed: %s. Be honest about that and ask the user to try again.", loopgate.SafeMemoryRememberErrorText(err)),
			},
		}
	}
	app.RefreshWakeState()

	summaryLine := fmt.Sprintf("A durable memory was already stored for this turn: %s = %s.", rememberResponse.FactKey, rememberResponse.FactValue)
	if rememberResponse.UpdatedExisting {
		summaryLine = fmt.Sprintf("A durable memory was updated for this turn: %s = %s.", rememberResponse.FactKey, rememberResponse.FactValue)
	}
	return memoryTurnDirective{
		RuntimeFacts: []string{
			summaryLine + " Acknowledge it naturally and do not call memory.remember again unless you are correcting the stored fact.",
		},
	}
}

func detectDeterministicRememberRequest(userMessage string) (*loopgate.MemoryRememberRequest, bool) {
	trimmedMessage := strings.TrimSpace(userMessage)
	if trimmedMessage == "" {
		return nil, false
	}
	explicitIntent := explicitRememberIntentPattern.MatchString(trimmedMessage)

	if preferredName := firstCapturedValue(callMePattern, trimmedMessage); preferredName != "" {
		return &loopgate.MemoryRememberRequest{
			FactKey:         "preferred_name",
			FactValue:       preferredName,
			Reason:          "deterministic preferred name update from Haven conversation",
			SourceText:      trimmedMessage,
			CandidateSource: "explicit_fact",
			SourceChannel:   "user_input",
		}, true
	}
	if name := firstCapturedValue(rememberMyNamePattern, trimmedMessage); name != "" {
		return &loopgate.MemoryRememberRequest{
			FactKey:         "name",
			FactValue:       name,
			Reason:          "deterministic name update from explicit Haven memory request",
			SourceText:      trimmedMessage,
			CandidateSource: "explicit_fact",
			SourceChannel:   "user_input",
		}, true
	}
	if name := firstCapturedValue(myNamePattern, trimmedMessage); name != "" {
		return &loopgate.MemoryRememberRequest{
			FactKey:         "name",
			FactValue:       name,
			Reason:          "deterministic name update from Haven conversation",
			SourceText:      trimmedMessage,
			CandidateSource: "explicit_fact",
			SourceChannel:   "user_input",
		}, true
	}

	if !explicitIntent {
		return nil, false
	}

	if favoriteMatch := rememberFavoritePattern.FindStringSubmatch(trimmedMessage); len(favoriteMatch) == 3 {
		return &loopgate.MemoryRememberRequest{
			FactKey:         "preference.favorite_" + safeMemorySuffix(favoriteMatch[1]),
			FactValue:       normalizeMemoryValue(favoriteMatch[2]),
			Reason:          "deterministic favorite preference from explicit Haven memory request",
			SourceText:      trimmedMessage,
			CandidateSource: "explicit_fact",
			SourceChannel:   "user_input",
		}, true
	}
	if preferenceValue := firstCapturedValue(rememberPreferencePattern, trimmedMessage); preferenceValue != "" {
		return &loopgate.MemoryRememberRequest{
			FactKey:         "preference.stated_preference",
			FactValue:       normalizeMemoryValue(preferenceValue),
			Reason:          "deterministic preference from explicit Haven memory request",
			SourceText:      trimmedMessage,
			CandidateSource: "explicit_fact",
			SourceChannel:   "user_input",
		}, true
	}
	if routineMatch := rememberRoutinePattern.FindStringSubmatch(trimmedMessage); len(routineMatch) == 3 {
		day := strings.ToLower(strings.TrimSpace(routineMatch[1]))
		return &loopgate.MemoryRememberRequest{
			FactKey:         "routine." + safeMemorySuffix(day),
			FactValue:       normalizeMemoryValue("Every " + titleWord(day) + routineMatch[2]),
			Reason:          "deterministic routine from explicit Haven memory request",
			SourceText:      trimmedMessage,
			CandidateSource: "explicit_fact",
			SourceChannel:   "user_input",
		}, true
	}

	return nil, true
}

func firstCapturedValue(pattern *regexp.Regexp, rawText string) string {
	matches := pattern.FindStringSubmatch(rawText)
	if len(matches) < 2 {
		return ""
	}
	return normalizeMemoryValue(matches[1])
}

func normalizeMemoryValue(rawValue string) string {
	trimmedValue := strings.TrimSpace(rawValue)
	trimmedValue = strings.Trim(trimmedValue, ".,!?")
	trimmedValue = strings.Join(strings.Fields(trimmedValue), " ")
	return trimmedValue
}

func safeMemorySuffix(rawLabel string) string {
	lowerLabel := strings.ToLower(strings.TrimSpace(rawLabel))
	var builder strings.Builder
	lastUnderscore := false
	for _, runeValue := range lowerLabel {
		switch {
		case runeValue >= 'a' && runeValue <= 'z':
			builder.WriteRune(runeValue)
			lastUnderscore = false
		case runeValue >= '0' && runeValue <= '9':
			builder.WriteRune(runeValue)
			lastUnderscore = false
		default:
			if !lastUnderscore && builder.Len() > 0 {
				builder.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	result := strings.Trim(builder.String(), "_")
	if result == "" {
		return "detail"
	}
	return result
}

func titleWord(rawWord string) string {
	trimmedWord := strings.TrimSpace(rawWord)
	if trimmedWord == "" {
		return ""
	}
	return strings.ToUpper(trimmedWord[:1]) + trimmedWord[1:]
}

func (app *HavenApp) hasResidentCapability(capabilityName string) bool {
	for _, capability := range app.capabilities {
		if capability.Name == capabilityName {
			return true
		}
	}
	return false
}
