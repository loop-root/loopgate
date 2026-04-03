package memory

import "strings"

const (
	MemoryCandidateKey = "memory_candidate"
	ContinuityEventKey = "continuity_event"

	MemoryCandidateTypeProviderStructuredFact = "provider_structured_fact"
	MemoryCandidateTypeResonateKey            = "resonate_key"

	MemoryScopeGlobal = "global"

	EpistemicFlavorFreshlyChecked = "freshly_checked"
	EpistemicFlavorRemembered     = "remembered"

	ContinuityEventTypeProviderFactObserved = "provider_fact_observed"
	ContinuityEventTypeResonateKeyCreated   = "resonate_key_created"
	ContinuityEventTypeGoalOpened           = "goal_opened"
	ContinuityEventTypeGoalClosed           = "goal_closed"
	ContinuityEventTypeUnresolvedItemOpened = "unresolved_item_opened"
	ContinuityEventTypeUnresolvedItemClosed = "unresolved_item_resolved"
)

func AnnotateMemoryCandidate(
	ledgerEventData map[string]interface{},
	candidateType string,
	candidateScope string,
	epistemicFlavor string,
	candidatePayload map[string]interface{},
) map[string]interface{} {
	annotatedEventData := make(map[string]interface{}, len(ledgerEventData)+1)
	for eventDataKey, eventDataValue := range ledgerEventData {
		annotatedEventData[eventDataKey] = eventDataValue
	}
	annotatedEventData[MemoryCandidateKey] = map[string]interface{}{
		"type":             strings.TrimSpace(candidateType),
		"scope":            strings.TrimSpace(candidateScope),
		"epistemic_flavor": strings.TrimSpace(epistemicFlavor),
		"data":             cloneCandidatePayload(candidatePayload),
	}
	return annotatedEventData
}

func cloneCandidatePayload(candidatePayload map[string]interface{}) map[string]interface{} {
	clonedCandidatePayload := make(map[string]interface{}, len(candidatePayload))
	for payloadKey, payloadValue := range candidatePayload {
		clonedCandidatePayload[payloadKey] = payloadValue
	}
	return clonedCandidatePayload
}

func AnnotateContinuityEvent(
	ledgerEventData map[string]interface{},
	continuityEventType string,
	continuityScope string,
	epistemicFlavor string,
	sourceRefs []map[string]interface{},
	eventPayload map[string]interface{},
) map[string]interface{} {
	annotatedEventData := make(map[string]interface{}, len(ledgerEventData)+1)
	for eventDataKey, eventDataValue := range ledgerEventData {
		annotatedEventData[eventDataKey] = eventDataValue
	}
	annotatedEventData[ContinuityEventKey] = map[string]interface{}{
		"type":             strings.TrimSpace(continuityEventType),
		"scope":            strings.TrimSpace(continuityScope),
		"epistemic_flavor": strings.TrimSpace(epistemicFlavor),
		"source_refs":      cloneSourceRefs(sourceRefs),
		"payload":          cloneCandidatePayload(eventPayload),
	}
	return annotatedEventData
}

func BindContinuityThread(ledgerEventData map[string]interface{}, threadID string) map[string]interface{} {
	rawContinuityEvent, foundContinuityEvent := ledgerEventData[ContinuityEventKey].(map[string]interface{})
	if !foundContinuityEvent || len(rawContinuityEvent) == 0 {
		return ledgerEventData
	}

	boundEventData := make(map[string]interface{}, len(ledgerEventData))
	for eventDataKey, eventDataValue := range ledgerEventData {
		boundEventData[eventDataKey] = eventDataValue
	}

	boundContinuityEvent := make(map[string]interface{}, len(rawContinuityEvent)+1)
	for continuityKey, continuityValue := range rawContinuityEvent {
		boundContinuityEvent[continuityKey] = continuityValue
	}
	boundContinuityEvent["thread_id"] = strings.TrimSpace(threadID)
	boundEventData[ContinuityEventKey] = boundContinuityEvent
	return boundEventData
}

func cloneSourceRefs(sourceRefs []map[string]interface{}) []map[string]interface{} {
	clonedSourceRefs := make([]map[string]interface{}, 0, len(sourceRefs))
	for _, sourceRef := range sourceRefs {
		clonedSourceRefs = append(clonedSourceRefs, cloneCandidatePayload(sourceRef))
	}
	return clonedSourceRefs
}
