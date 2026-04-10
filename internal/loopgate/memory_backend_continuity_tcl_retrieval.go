package loopgate

import (
	"fmt"
	"sort"
	"strings"
)

func (backend *continuityTCLMemoryBackend) discoverFromBoundPartitionState(validatedRequest MemoryDiscoverRequest) (MemoryDiscoverResponse, error) {
	queryTags := tokenizeLoopgateMemoryText(validatedRequest.Query)
	slotPreferenceAnchorTupleKey := detectDiscoverSlotPreference(validatedRequest.Query)
	queryIntent := detectDiscoverQueryIntent(validatedRequest.Query)
	backend.server.memoryMu.Lock()
	defer backend.server.memoryMu.Unlock()

	type rankedDiscoverItem struct {
		item                MemoryDiscoverItem
		exactSlotAdmission  bool
		slotPreferenceBoost bool
		authoritativeState  bool
		previewFacet        bool
		otherEntityTagged   bool
	}
	discoveredItems := make([]rankedDiscoverItem, 0, len(backend.partition.state.ResonateKeys))
	exactSlotKeyID := ""
	if exactSlotItem, found := backend.discoverExactStableProfileSlotLocked(validatedRequest, queryTags, slotPreferenceAnchorTupleKey); found {
		exactSlotKeyID = exactSlotItem.KeyID
		discoveredItems = append(discoveredItems, rankedDiscoverItem{
			item:               exactSlotItem,
			exactSlotAdmission: true,
		})
	}
	for _, resonateKeyRecord := range activeLoopgateResonateKeys(backend.partition.state) {
		if resonateKeyRecord.Scope != validatedRequest.Scope {
			continue
		}
		if resonateKeyRecord.KeyID == exactSlotKeyID {
			continue
		}
		matchCount := 0
		matchCount = countLoopgateMemoryTagOverlap(queryTags, resonateKeyRecord.Tags)
		if matchCount == 0 {
			continue
		}
		slotPreferenceBoost := false
		authoritativeState := false
		if slotPreferenceAnchorTupleKey != "" {
			if distillateRecord, found := backend.partition.state.Distillates[resonateKeyRecord.DistillateID]; found {
				if explicitProfileFact, found := explicitProfileFactFromDistillate(backend.partition.state, distillateRecord); found && explicitProfileFact.AnchorTupleKey == slotPreferenceAnchorTupleKey {
					slotPreferenceBoost = true
				}
				authoritativeState = memoryFactStateClassForDistillate(distillateRecord) == memoryFactStateClassAuthoritative
			}
		} else if distillateRecord, found := backend.partition.state.Distillates[resonateKeyRecord.DistillateID]; found {
			authoritativeState = memoryFactStateClassForDistillate(distillateRecord) == memoryFactStateClassAuthoritative
		}
		discoveredItems = append(discoveredItems, rankedDiscoverItem{
			item: MemoryDiscoverItem{
				KeyID:        resonateKeyRecord.KeyID,
				ThreadID:     resonateKeyRecord.ThreadID,
				DistillateID: resonateKeyRecord.DistillateID,
				Scope:        resonateKeyRecord.Scope,
				CreatedAtUTC: resonateKeyRecord.CreatedAtUTC,
				Tags:         append([]string(nil), resonateKeyRecord.Tags...),
				MatchCount:   matchCount,
			},
			slotPreferenceBoost: slotPreferenceBoost,
			authoritativeState:  authoritativeState,
			previewFacet:        discoverTagsContainAny(resonateKeyRecord.Tags, "preview", "label", "card", "chip", "display", "badge"),
			otherEntityTagged:   discoverTagsContainAny(resonateKeyRecord.Tags, "teammate", "shadow", "operator", "pet", "cat"),
		})
	}
	sort.Slice(discoveredItems, func(leftIndex int, rightIndex int) bool {
		leftItem := discoveredItems[leftIndex]
		rightItem := discoveredItems[rightIndex]
		switch {
		// Exact slot admission is intentionally limited to the small stable-profile allowlist.
		// It keeps the current anchored value discoverable even if tag overlap is sparse or
		// older materialization paths emitted weaker tags than newer continuity entries.
		case leftItem.exactSlotAdmission != rightItem.exactSlotAdmission:
			return leftItem.exactSlotAdmission && !rightItem.exactSlotAdmission
		// Preview-targeted queries should keep preview facets eligible, but ordinary slot
		// queries should not let preview-label material outrank the canonical slot value.
		case queryIntent.previewTarget && leftItem.previewFacet != rightItem.previewFacet:
			return leftItem.previewFacet && !rightItem.previewFacet
		case queryIntent.slotSeeking && !queryIntent.previewTarget && leftItem.previewFacet != rightItem.previewFacet:
			return !leftItem.previewFacet && rightItem.previewFacet
		// User-scoped identity/profile queries should prefer same-entity material over
		// teammate or pet distractors once the candidates are already eligible for discover.
		case queryIntent.avoidOtherEntityDistractors && leftItem.otherEntityTagged != rightItem.otherEntityTagged:
			return !leftItem.otherEntityTagged && rightItem.otherEntityTagged
		// For slot-shaped queries, authoritative remembered state should beat softer derived
		// context when both candidates are otherwise admissible.
		case queryIntent.slotSeeking && leftItem.authoritativeState != rightItem.authoritativeState:
			return leftItem.authoritativeState && !rightItem.authoritativeState
		case leftItem.item.MatchCount != rightItem.item.MatchCount:
			return leftItem.item.MatchCount > rightItem.item.MatchCount
		// Slot preference only reorders already-eligible discover results. It is not an
		// admission path, and it stays bounded to a boolean tie-break rather than a score rewrite.
		case leftItem.slotPreferenceBoost != rightItem.slotPreferenceBoost:
			return leftItem.slotPreferenceBoost && !rightItem.slotPreferenceBoost
		case leftItem.item.CreatedAtUTC != rightItem.item.CreatedAtUTC:
			return leftItem.item.CreatedAtUTC > rightItem.item.CreatedAtUTC
		default:
			return leftItem.item.KeyID < rightItem.item.KeyID
		}
	})
	if queryIntent.avoidOtherEntityDistractors {
		hasSameEntityCandidate := false
		for _, discoveredItem := range discoveredItems {
			if !discoveredItem.otherEntityTagged {
				hasSameEntityCandidate = true
				break
			}
		}
		if hasSameEntityCandidate {
			filteredItems := make([]rankedDiscoverItem, 0, len(discoveredItems))
			for _, discoveredItem := range discoveredItems {
				if discoveredItem.otherEntityTagged {
					continue
				}
				filteredItems = append(filteredItems, discoveredItem)
			}
			discoveredItems = filteredItems
		}
	}
	if len(discoveredItems) > validatedRequest.MaxItems {
		discoveredItems = append([]rankedDiscoverItem(nil), discoveredItems[:validatedRequest.MaxItems]...)
	}
	responseItems := make([]MemoryDiscoverItem, 0, len(discoveredItems))
	for _, discoveredItem := range discoveredItems {
		responseItems = append(responseItems, discoveredItem.item)
	}
	return MemoryDiscoverResponse{
		Scope: validatedRequest.Scope,
		Query: validatedRequest.Query,
		Items: responseItems,
	}, nil
}

func countLoopgateMemoryTagOverlap(queryTags []string, candidateTags []string) int {
	matchCount := 0
	for _, queryTag := range queryTags {
		for _, candidateTag := range candidateTags {
			if candidateTag == queryTag {
				matchCount++
				break
			}
		}
	}
	return matchCount
}

type discoverQueryIntent struct {
	avoidOtherEntityDistractors bool
	slotSeeking                 bool
	previewTarget               bool
}

func detectDiscoverQueryIntent(rawQuery string) discoverQueryIntent {
	queryTags := tokenizeLoopgateMemoryText(rawQuery)
	if len(queryTags) == 0 {
		return discoverQueryIntent{}
	}
	queryTagSet := make(map[string]struct{}, len(queryTags))
	for _, queryTag := range queryTags {
		queryTagSet[queryTag] = struct{}{}
	}
	mentionsPreviewFacet := containsAnyLoopgateMemoryTag(queryTagSet, "preview", "label", "card", "chip", "display", "badge")
	mentionsPreviewExclusion := containsAnyLoopgateMemoryTag(queryTagSet, "not", "ignore", "exclude", "without")
	mentionsRememberedIdentity := containsAnyLoopgateMemoryTag(queryTagSet, "remembered", "current") &&
		containsAnyLoopgateMemoryTag(queryTagSet, "name", "preferred", "pronouns", "timezone", "locale")
	return discoverQueryIntent{
		avoidOtherEntityDistractors: containsAnyLoopgateMemoryTag(queryTagSet, "user", "profile") || mentionsRememberedIdentity,
		slotSeeking:                 containsAnyLoopgateMemoryTag(queryTagSet, "slot"),
		previewTarget:               mentionsPreviewFacet && !mentionsPreviewExclusion,
	}
}

func discoverTagsContainAny(candidateTags []string, wantedTags ...string) bool {
	if len(candidateTags) == 0 {
		return false
	}
	candidateTagSet := make(map[string]struct{}, len(candidateTags))
	for _, candidateTag := range candidateTags {
		candidateTagSet[candidateTag] = struct{}{}
	}
	return containsAnyLoopgateMemoryTag(candidateTagSet, wantedTags...)
}

func (backend *continuityTCLMemoryBackend) discoverExactStableProfileSlotLocked(validatedRequest MemoryDiscoverRequest, queryTags []string, slotPreferenceAnchorTupleKey string) (MemoryDiscoverItem, bool) {
	explicitProfileFact, found := activeExplicitProfileFactForDiscoverAnchorTupleKey(backend.partition.state, slotPreferenceAnchorTupleKey)
	if !found || explicitProfileFact.ResonateKeyID == "" {
		return MemoryDiscoverItem{}, false
	}
	resonateKeyRecord, found := backend.partition.state.ResonateKeys[explicitProfileFact.ResonateKeyID]
	if !found || resonateKeyRecord.Scope != validatedRequest.Scope {
		return MemoryDiscoverItem{}, false
	}
	return MemoryDiscoverItem{
		KeyID:        resonateKeyRecord.KeyID,
		ThreadID:     resonateKeyRecord.ThreadID,
		DistillateID: resonateKeyRecord.DistillateID,
		Scope:        resonateKeyRecord.Scope,
		CreatedAtUTC: resonateKeyRecord.CreatedAtUTC,
		Tags:         append([]string(nil), resonateKeyRecord.Tags...),
		MatchCount:   countLoopgateMemoryTagOverlap(queryTags, resonateKeyRecord.Tags),
	}, true
}

func activeExplicitProfileFactForDiscoverAnchorTupleKey(currentState continuityMemoryState, anchorTupleKey string) (explicitProfileFactRecord, bool) {
	explicitProfileFact, found := activeExplicitProfileFactByAnchorTupleKey(currentState, anchorTupleKey)
	if found {
		return explicitProfileFact, true
	}
	// Generic "profile name" queries should resolve to the preferred name when present,
	// but a bare legal-name slot is still a valid fallback if no preferred-name fact exists.
	if strings.TrimSpace(anchorTupleKey) == "v1:usr_profile:identity:fact:preferred_name" {
		return activeExplicitProfileFactByAnchorTupleKey(currentState, "v1:usr_profile:identity:fact:name")
	}
	return explicitProfileFactRecord{}, false
}

func (backend *continuityTCLMemoryBackend) recallFromBoundPartitionState(validatedRequest MemoryRecallRequest) (MemoryRecallResponse, error) {
	backend.server.memoryMu.Lock()
	defer backend.server.memoryMu.Unlock()

	recalledItems := make([]MemoryRecallItem, 0, len(validatedRequest.RequestedKeys))
	approxTokenCount := 0
	for _, requestedKeyID := range validatedRequest.RequestedKeys {
		resonateKeyRecord, distillateRecord, decision, err := resolveRecallMaterial(backend.partition.state, requestedKeyID)
		if err != nil {
			return MemoryRecallResponse{}, err
		}
		if resonateKeyRecord.Scope != validatedRequest.Scope {
			return MemoryRecallResponse{}, fmt.Errorf("resonate key %q is outside scope", requestedKeyID)
		}
		if !decision.Allowed {
			return MemoryRecallResponse{}, continuityGovernanceError{
				httpStatus:     403,
				responseStatus: ResponseStatusDenied,
				denialCode:     decision.DenialCode,
				reason:         fmt.Sprintf("resonate key %q is not eligible for recall", requestedKeyID),
			}
		}

		recalledFacts := make([]MemoryRecallFact, 0, len(distillateRecord.Facts))
		stateClass := memoryFactStateClassForDistillate(distillateRecord)
		for _, factRecord := range distillateRecord.Facts {
			recalledFacts = append(recalledFacts, memoryRecallFactFromDistillateFact(factRecord, stateClass))
		}
		activeGoals, unresolvedItems := loopgateRecallOpenItems(distillateRecord)
		recalledItem := MemoryRecallItem{
			KeyID:           resonateKeyRecord.KeyID,
			ThreadID:        resonateKeyRecord.ThreadID,
			DistillateID:    resonateKeyRecord.DistillateID,
			Scope:           resonateKeyRecord.Scope,
			CreatedAtUTC:    resonateKeyRecord.CreatedAtUTC,
			Tags:            append([]string(nil), resonateKeyRecord.Tags...),
			ActiveGoals:     activeGoals,
			UnresolvedItems: unresolvedItems,
			Facts:           recalledFacts,
			EpistemicFlavor: "remembered",
		}
		approxTokenCount += approximateLoopgateRecallTokens(recalledItem)
		recalledItems = append(recalledItems, recalledItem)
	}
	if approxTokenCount > validatedRequest.MaxTokens {
		return MemoryRecallResponse{}, fmt.Errorf("requested keys exceed max_tokens")
	}
	return MemoryRecallResponse{
		Scope:            validatedRequest.Scope,
		MaxItems:         validatedRequest.MaxItems,
		MaxTokens:        validatedRequest.MaxTokens,
		ApproxTokenCount: approxTokenCount,
		Items:            recalledItems,
	}, nil
}
