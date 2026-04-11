package loopgate

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"morph/internal/config"
)

type continuityFactCandidate struct {
	Fact          continuityDistillateFact
	DistillateID  string
	CreatedAtUTC  string
	AuthorityLane int
}

const (
	memoryFactStateClassAuthoritative = "authoritative_state"
	memoryFactStateClassDerived       = "derived_context"
)

func memoryFactStateClassForAuthorityLane(authorityLane int) string {
	if authorityLane >= 2 {
		return memoryFactStateClassAuthoritative
	}
	return memoryFactStateClassDerived
}

func memoryFactStateClassForDistillate(distillateRecord continuityDistillateRecord) string {
	if isExplicitProfileFactDistillate(distillateRecord) {
		return memoryFactStateClassAuthoritative
	}
	return memoryFactStateClassDerived
}

func compareContinuityFactCandidates(existingCandidate continuityFactCandidate, candidate continuityFactCandidate) int {
	switch {
	case candidate.AuthorityLane != existingCandidate.AuthorityLane:
		if candidate.AuthorityLane > existingCandidate.AuthorityLane {
			return 1
		}
		return -1
	}
	existingCreatedAtUTC := parseTimeOrZero(existingCandidate.CreatedAtUTC)
	candidateCreatedAtUTC := parseTimeOrZero(candidate.CreatedAtUTC)
	switch {
	case candidateCreatedAtUTC.After(existingCreatedAtUTC):
		return 1
	case existingCreatedAtUTC.After(candidateCreatedAtUTC):
		return -1
	}
	switch {
	case candidate.Fact.CertaintyScore > existingCandidate.Fact.CertaintyScore:
		return 1
	case candidate.Fact.CertaintyScore < existingCandidate.Fact.CertaintyScore:
		return -1
	}
	if reflect.DeepEqual(candidate.Fact.Value, existingCandidate.Fact.Value) {
		if candidate.DistillateID < existingCandidate.DistillateID {
			return 1
		}
		return -1
	}
	return 0
}

func anchorTupleKey(anchorVersion string, anchorKey string) string {
	trimmedAnchorVersion := strings.TrimSpace(anchorVersion)
	trimmedAnchorKey := strings.TrimSpace(anchorKey)
	if trimmedAnchorVersion == "" || trimmedAnchorKey == "" {
		return ""
	}
	return trimmedAnchorVersion + ":" + trimmedAnchorKey
}

func continuityFactAnchorTuple(factRecord continuityDistillateFact) (string, string) {
	return semanticProjectionAnchorVersion(factRecord.SemanticProjection), semanticProjectionAnchorKey(factRecord.SemanticProjection)
}

func appendRecentFactCandidate(recentFactsBySlotKey map[string]MemoryWakeStateRecentFact, recentFactOrder *[]string, factCandidatesByAnchorTupleKey map[string]continuityFactCandidate, ambiguousAnchorTupleKeys map[string]struct{}, candidate continuityFactCandidate) {
	if candidate.Fact.CertaintyScore <= 0 {
		candidate.Fact.CertaintyScore = certaintyScoreForEpistemicFlavor(candidate.Fact.EpistemicFlavor)
	}
	factAnchorVersion, factAnchorKey := continuityFactAnchorTuple(candidate.Fact)
	factAnchorTupleKey := anchorTupleKey(factAnchorVersion, factAnchorKey)
	if factAnchorTupleKey == "" {
		slotKey := candidate.Fact.Name + ":" + candidate.Fact.SourceRef
		recentFactsBySlotKey[slotKey] = memoryWakeRecentFactFromDistillateFact(candidate.Fact, memoryFactStateClassForAuthorityLane(candidate.AuthorityLane))
		*recentFactOrder = appendWithoutDuplicate(*recentFactOrder, slotKey)
		return
	}
	if _, ambiguous := ambiguousAnchorTupleKeys[factAnchorTupleKey]; ambiguous {
		return
	}
	if existingCandidate, found := factCandidatesByAnchorTupleKey[factAnchorTupleKey]; found {
		switch compareContinuityFactCandidates(existingCandidate, candidate) {
		case 1:
			factCandidatesByAnchorTupleKey[factAnchorTupleKey] = candidate
			recentFactsBySlotKey[factAnchorTupleKey] = memoryWakeRecentFactFromDistillateFact(candidate.Fact, memoryFactStateClassForAuthorityLane(candidate.AuthorityLane))
		case -1:
			return
		default:
			delete(factCandidatesByAnchorTupleKey, factAnchorTupleKey)
			delete(recentFactsBySlotKey, factAnchorTupleKey)
			ambiguousAnchorTupleKeys[factAnchorTupleKey] = struct{}{}
			*recentFactOrder = removeStringValue(*recentFactOrder, factAnchorTupleKey)
			return
		}
		*recentFactOrder = appendWithoutDuplicate(*recentFactOrder, factAnchorTupleKey)
		return
	}
	factCandidatesByAnchorTupleKey[factAnchorTupleKey] = candidate
	recentFactsBySlotKey[factAnchorTupleKey] = memoryWakeRecentFactFromDistillateFact(candidate.Fact, memoryFactStateClassForAuthorityLane(candidate.AuthorityLane))
	*recentFactOrder = appendWithoutDuplicate(*recentFactOrder, factAnchorTupleKey)
}

func buildLoopgateWakeProducts(currentState continuityMemoryState, now time.Time, runtimeConfig config.RuntimeConfig) (MemoryWakeStateResponse, continuityDiagnosticWakeReport) {
	activeGoalsByID := map[string]string{}
	activeGoalOrder := make([]string, 0, 8)
	unresolvedItemsByID := map[string]MemoryWakeStateOpenItem{}
	unresolvedItemOrder := make([]string, 0, 8)
	recentFactsBySlotKey := map[string]MemoryWakeStateRecentFact{}
	recentFactOrder := make([]string, 0, 12)
	factCandidatesByAnchorTupleKey := map[string]continuityFactCandidate{}
	ambiguousAnchorTupleKeys := map[string]struct{}{}
	sourceRefSeen := map[string]MemoryWakeStateSourceRef{}
	sourceRefOrder := make([]string, 0, 16)
	resonateKeys := make([]string, 0, 8)
	includedEntries := make([]continuityDiagnosticWakeEntry, 0, 24)
	excludedEntries := make([]continuityDiagnosticWakeEntry, 0, 24)
	familyCounts := map[string]int{}
	timeBandCounts := map[string]int{}

	for _, distillateRecord := range activeLoopgateDistillates(currentState) {
		for _, sourceRef := range distillateRecord.SourceRefs {
			sourceRefKey := sourceRef.Kind + ":" + sourceRef.Ref
			sourceRefSeen[sourceRefKey] = MemoryWakeStateSourceRef{
				Kind: sourceRef.Kind,
				Ref:  sourceRef.Ref,
			}
			sourceRefOrder = appendWithoutDuplicate(sourceRefOrder, sourceRefKey)
		}
		if isExplicitProfileFactDistillate(distillateRecord) {
			for _, factRecord := range distillateRecord.Facts {
				appendRecentFactCandidate(recentFactsBySlotKey, &recentFactOrder, factCandidatesByAnchorTupleKey, ambiguousAnchorTupleKeys, continuityFactCandidate{
					Fact:          factRecord,
					DistillateID:  distillateRecord.DistillateID,
					CreatedAtUTC:  distillateRecord.CreatedAtUTC,
					AuthorityLane: 2,
				})
				includedEntries = append(includedEntries, continuityDiagnosticWakeEntry{
					ItemKind:         wakeEntryKindDistillate,
					ItemID:           distillateRecord.DistillateID,
					GoalFamilyID:     distillateRecord.GoalFamilyID,
					Scope:            distillateRecord.Scope,
					RetentionScore:   distillateRecord.RetentionScore,
					EffectiveHotness: distillateRecord.EffectiveHotness,
					Reason:           "hard_bound_explicit_profile_fact",
					PrecedenceSource: "explicit_profile_memory",
					RedactedSummary:  redactedWakeSummary(fmt.Sprintf("%s=%v", factRecord.Name, factRecord.Value)),
				})
			}
		}
		for _, goalOp := range distillateRecord.GoalOps {
			switch goalOp.Action {
			case "opened":
				activeGoalsByID[goalOp.GoalID] = goalOp.Text
				activeGoalOrder = appendWithoutDuplicate(activeGoalOrder, goalOp.GoalID)
				includedEntries = append(includedEntries, continuityDiagnosticWakeEntry{
					ItemKind:         wakeEntryKindGoal,
					ItemID:           goalOp.GoalID,
					GoalFamilyID:     distillateRecord.GoalFamilyID,
					Scope:            distillateRecord.Scope,
					RetentionScore:   distillateRecord.RetentionScore,
					EffectiveHotness: distillateRecord.EffectiveHotness,
					Reason:           "hard_bound_active_goal",
					PrecedenceSource: "active_goal_state",
					RedactedSummary:  redactedWakeSummary(goalOp.Text),
				})
			case "closed":
				delete(activeGoalsByID, goalOp.GoalID)
				activeGoalOrder = removeStringValue(activeGoalOrder, goalOp.GoalID)
			}
		}
		for _, itemOp := range distillateRecord.UnresolvedItemOps {
			switch itemOp.Action {
			case "opened":
				taskMetadata := explicitTodoTaskMetadataFromDistillate(distillateRecord)
				taskMetadata.ID = itemOp.ItemID
				taskMetadata.Text = itemOp.Text
				taskMetadata.Status = explicitTodoWorkflowStatusTodo
				if taskMetadata.CreatedAtUTC == "" {
					taskMetadata.CreatedAtUTC = distillateRecord.CreatedAtUTC
				}
				unresolvedItemsByID[itemOp.ItemID] = taskMetadata
				unresolvedItemOrder = appendWithoutDuplicate(unresolvedItemOrder, itemOp.ItemID)
				includedEntries = append(includedEntries, continuityDiagnosticWakeEntry{
					ItemKind:         wakeEntryKindTodo,
					ItemID:           itemOp.ItemID,
					GoalFamilyID:     distillateRecord.GoalFamilyID,
					Scope:            distillateRecord.Scope,
					RetentionScore:   distillateRecord.RetentionScore,
					EffectiveHotness: distillateRecord.EffectiveHotness,
					Reason:           "hard_bound_open_task",
					PrecedenceSource: "open_task_state",
					RedactedSummary:  redactedWakeSummary(itemOp.Text),
				})
			case "closed":
				delete(unresolvedItemsByID, itemOp.ItemID)
				unresolvedItemOrder = removeStringValue(unresolvedItemOrder, itemOp.ItemID)
			case todoItemOpStatusSet:
				if existingItem, ok := unresolvedItemsByID[itemOp.ItemID]; ok {
					if normalized := normalizeExplicitTodoWorkflowStatus(itemOp.Status); normalized != "" {
						existingItem.Status = normalized
						unresolvedItemsByID[itemOp.ItemID] = existingItem
					}
				}
			}
		}
	}

	activeResonateKeys := activeLoopgateResonateKeys(currentState)
	sort.Slice(activeResonateKeys, func(leftIndex int, rightIndex int) bool {
		leftKey := activeResonateKeys[leftIndex]
		rightKey := activeResonateKeys[rightIndex]
		switch {
		case leftKey.EffectiveHotness != rightKey.EffectiveHotness:
			return leftKey.EffectiveHotness > rightKey.EffectiveHotness
		case leftKey.RetentionScore != rightKey.RetentionScore:
			return leftKey.RetentionScore > rightKey.RetentionScore
		case leftKey.CreatedAtUTC != rightKey.CreatedAtUTC:
			return leftKey.CreatedAtUTC > rightKey.CreatedAtUTC
		default:
			return itemKindID(wakeEntryKindResonateKey, leftKey.KeyID) < itemKindID(wakeEntryKindResonateKey, rightKey.KeyID)
		}
	})
	for _, resonateKeyRecord := range activeResonateKeys {
		if distillateRecord, found := currentState.Distillates[resonateKeyRecord.DistillateID]; found && isExplicitProfileFactDistillate(distillateRecord) {
			continue
		}
		goalFamilyID := strings.TrimSpace(resonateKeyRecord.GoalFamilyID)
		timeBandKey := goalFamilyID + ":" + timeBandKeyFor(resonateKeyRecord.CreatedAtUTC)
		if goalFamilyID != "" && familyCounts[goalFamilyID] >= 2 {
			excludedEntries = append(excludedEntries, continuityDiagnosticWakeEntry{
				ItemKind:         wakeEntryKindResonateKey,
				ItemID:           resonateKeyRecord.KeyID,
				GoalFamilyID:     resonateKeyRecord.GoalFamilyID,
				Scope:            resonateKeyRecord.Scope,
				RetentionScore:   resonateKeyRecord.RetentionScore,
				EffectiveHotness: resonateKeyRecord.EffectiveHotness,
				TrimReason:       "duplicate_family_cap",
				PrecedenceSource: "optional_memory",
			})
			continue
		}
		if goalFamilyID != "" && timeBandCounts[timeBandKey] >= 2 {
			excludedEntries = append(excludedEntries, continuityDiagnosticWakeEntry{
				ItemKind:         wakeEntryKindResonateKey,
				ItemID:           resonateKeyRecord.KeyID,
				GoalFamilyID:     resonateKeyRecord.GoalFamilyID,
				Scope:            resonateKeyRecord.Scope,
				RetentionScore:   resonateKeyRecord.RetentionScore,
				EffectiveHotness: resonateKeyRecord.EffectiveHotness,
				TrimReason:       "duplicate_family_time_band_cap",
				PrecedenceSource: "optional_memory",
			})
			continue
		}
		resonateKeys = append(resonateKeys, resonateKeyRecord.KeyID)
		sourceRefKey := "resonate_key:" + resonateKeyRecord.KeyID
		sourceRefSeen[sourceRefKey] = MemoryWakeStateSourceRef{Kind: "resonate_key", Ref: resonateKeyRecord.KeyID}
		sourceRefOrder = appendWithoutDuplicate(sourceRefOrder, sourceRefKey)
		familyCounts[goalFamilyID]++
		timeBandCounts[timeBandKey]++
		includedEntries = append(includedEntries, continuityDiagnosticWakeEntry{
			ItemKind:         wakeEntryKindResonateKey,
			ItemID:           resonateKeyRecord.KeyID,
			GoalFamilyID:     resonateKeyRecord.GoalFamilyID,
			Scope:            resonateKeyRecord.Scope,
			RetentionScore:   resonateKeyRecord.RetentionScore,
			EffectiveHotness: resonateKeyRecord.EffectiveHotness,
			Reason:           "eligible_optional_resonate_key",
			PrecedenceSource: "remembered_context",
		})
	}

	distillates := activeLoopgateDistillates(currentState)
	sort.Slice(distillates, func(leftIndex int, rightIndex int) bool {
		leftDistillate := distillates[leftIndex]
		rightDistillate := distillates[rightIndex]
		switch {
		case leftDistillate.EffectiveHotness != rightDistillate.EffectiveHotness:
			return leftDistillate.EffectiveHotness > rightDistillate.EffectiveHotness
		case leftDistillate.RetentionScore != rightDistillate.RetentionScore:
			return leftDistillate.RetentionScore > rightDistillate.RetentionScore
		case leftDistillate.CreatedAtUTC != rightDistillate.CreatedAtUTC:
			return leftDistillate.CreatedAtUTC > rightDistillate.CreatedAtUTC
		default:
			return itemKindID(wakeEntryKindDistillate, leftDistillate.DistillateID) < itemKindID(wakeEntryKindDistillate, rightDistillate.DistillateID)
		}
	})
	for _, distillateRecord := range distillates {
		if isExplicitProfileFactDistillate(distillateRecord) {
			continue
		}
		goalFamilyID := strings.TrimSpace(distillateRecord.GoalFamilyID)
		timeBandKey := goalFamilyID + ":" + timeBandKeyFor(distillateRecord.CreatedAtUTC)
		if goalFamilyID != "" && familyCounts[goalFamilyID] >= 2 {
			excludedEntries = append(excludedEntries, continuityDiagnosticWakeEntry{
				ItemKind:         wakeEntryKindDistillate,
				ItemID:           distillateRecord.DistillateID,
				GoalFamilyID:     distillateRecord.GoalFamilyID,
				Scope:            distillateRecord.Scope,
				RetentionScore:   distillateRecord.RetentionScore,
				EffectiveHotness: distillateRecord.EffectiveHotness,
				TrimReason:       "duplicate_family_cap",
				PrecedenceSource: "optional_memory",
			})
			continue
		}
		if goalFamilyID != "" && timeBandCounts[timeBandKey] >= 2 {
			excludedEntries = append(excludedEntries, continuityDiagnosticWakeEntry{
				ItemKind:         wakeEntryKindDistillate,
				ItemID:           distillateRecord.DistillateID,
				GoalFamilyID:     distillateRecord.GoalFamilyID,
				Scope:            distillateRecord.Scope,
				RetentionScore:   distillateRecord.RetentionScore,
				EffectiveHotness: distillateRecord.EffectiveHotness,
				TrimReason:       "duplicate_family_time_band_cap",
				PrecedenceSource: "optional_memory",
			})
			continue
		}
		for _, factRecord := range distillateRecord.Facts {
			appendRecentFactCandidate(recentFactsBySlotKey, &recentFactOrder, factCandidatesByAnchorTupleKey, ambiguousAnchorTupleKeys, continuityFactCandidate{
				Fact:          factRecord,
				DistillateID:  distillateRecord.DistillateID,
				CreatedAtUTC:  distillateRecord.CreatedAtUTC,
				AuthorityLane: 1,
			})
		}
		familyCounts[goalFamilyID]++
		timeBandCounts[timeBandKey]++
		includedEntries = append(includedEntries, continuityDiagnosticWakeEntry{
			ItemKind:         wakeEntryKindDistillate,
			ItemID:           distillateRecord.DistillateID,
			GoalFamilyID:     distillateRecord.GoalFamilyID,
			Scope:            distillateRecord.Scope,
			RetentionScore:   distillateRecord.RetentionScore,
			EffectiveHotness: distillateRecord.EffectiveHotness,
			Reason:           "eligible_optional_distillate",
			PrecedenceSource: "remembered_context",
		})
	}

	trimToLimit(&activeGoalOrder, 5)
	trimToLimit(&unresolvedItemOrder, 10)
	trimToLimit(&recentFactOrder, 12)
	trimToLimit(&sourceRefOrder, 16)
	trimToLimit(&resonateKeys, 8)

	activeGoals := make([]string, 0, len(activeGoalOrder))
	for _, goalID := range activeGoalOrder {
		if goalText, found := activeGoalsByID[goalID]; found {
			activeGoals = append(activeGoals, goalText)
		}
	}
	unresolvedItems := make([]MemoryWakeStateOpenItem, 0, len(unresolvedItemOrder))
	for _, itemID := range unresolvedItemOrder {
		if unresolvedItem, found := unresolvedItemsByID[itemID]; found {
			unresolvedItems = append(unresolvedItems, unresolvedItem)
		}
	}
	recentFacts := make([]MemoryWakeStateRecentFact, 0, len(recentFactOrder))
	for _, factSlotKey := range recentFactOrder {
		if factRecord, found := recentFactsBySlotKey[factSlotKey]; found {
			recentFacts = append(recentFacts, factRecord)
		}
	}
	sourceRefs := make([]MemoryWakeStateSourceRef, 0, len(sourceRefOrder))
	for _, sourceRefKey := range sourceRefOrder {
		sourceRefs = append(sourceRefs, sourceRefSeen[sourceRefKey])
	}

	approxPromptTokens := approximateLoopgateWakeStateTokens(activeGoals, unresolvedItems, recentFacts, resonateKeys)
trimLoop:
	for approxPromptTokens > 2000 {
		switch {
		case len(resonateKeys) > 0:
			excludedEntries = append(excludedEntries, continuityDiagnosticWakeEntry{
				ItemKind:   wakeEntryKindResonateKey,
				ItemID:     resonateKeys[0],
				TrimReason: "token_budget",
			})
			resonateKeys = append([]string(nil), resonateKeys[1:]...)
		case len(recentFacts) > 0:
			excludedEntries = append(excludedEntries, continuityDiagnosticWakeEntry{
				ItemKind:   wakeEntryKindDistillate,
				ItemID:     recentFacts[0].Name,
				TrimReason: "token_budget",
			})
			recentFacts = append([]MemoryWakeStateRecentFact(nil), recentFacts[1:]...)
		case len(activeGoals) > 0:
			activeGoals = append([]string(nil), activeGoals[1:]...)
		case len(unresolvedItems) > 0:
			unresolvedItems = append([]MemoryWakeStateOpenItem(nil), unresolvedItems[1:]...)
		default:
			break trimLoop
		}
		approxPromptTokens = approximateLoopgateWakeStateTokens(activeGoals, unresolvedItems, recentFacts, resonateKeys)
	}

	sort.Slice(includedEntries, func(leftIndex int, rightIndex int) bool {
		return stableWakeEntryLess(includedEntries[leftIndex], includedEntries[rightIndex])
	})
	sort.Slice(excludedEntries, func(leftIndex int, rightIndex int) bool {
		return stableWakeEntryLess(excludedEntries[leftIndex], excludedEntries[rightIndex])
	})

	runtimeWakeState := MemoryWakeStateResponse{
		ID:                 "wake_loopgate_" + now.UTC().Format("20060102T150405Z"),
		Scope:              memoryScopeGlobal,
		CreatedAtUTC:       now.UTC().Format(time.RFC3339Nano),
		SourceRefs:         sourceRefs,
		ActiveGoals:        activeGoals,
		UnresolvedItems:    unresolvedItems,
		RecentFacts:        recentFacts,
		ResonateKeys:       resonateKeys,
		PromptTokenBudget:  2000,
		ApproxPromptTokens: approxPromptTokens,
	}
	diagnosticWake := continuityDiagnosticWakeReport{
		SchemaVersion:     continuityMemorySchemaVersion,
		ResolutionVersion: continuityResolutionVersion,
		ReportID:          newDiagnosticWakeReportID(now.UTC()),
		CreatedAtUTC:      now.UTC().Format(time.RFC3339Nano),
		RuntimeWakeID:     runtimeWakeState.ID,
		Entries:           includedEntries,
		ExcludedEntries:   excludedEntries,
	}
	return runtimeWakeState, diagnosticWake
}

func approximateLoopgateWakeStateTokens(activeGoals []string, unresolvedItems []MemoryWakeStateOpenItem, recentFacts []MemoryWakeStateRecentFact, resonateKeys []string) int {
	tokenCount := approximateLoopgateTokenCount("remembered continuity")
	for _, activeGoal := range activeGoals {
		tokenCount += approximateLoopgateTokenCount(activeGoal)
	}
	for _, unresolvedItem := range unresolvedItems {
		tokenCount += approximateLoopgateTokenCount(unresolvedItem.ID + " " + unresolvedItem.Text)
	}
	for _, factRecord := range recentFacts {
		tokenCount += approximateLoopgateTokenCount(factRecord.Name)
		tokenCount += approximateLoopgateTokenCount(fmt.Sprintf("%v", factRecord.Value))
		tokenCount += approximateLoopgateTokenCount(factRecord.SourceRef)
	}
	tokenCount += approximateLoopgateTokenCount(strings.Join(resonateKeys, ", "))
	return tokenCount
}

func trimToLimit(values *[]string, limit int) {
	if len(*values) <= limit {
		return
	}
	*values = append([]string(nil), (*values)[len(*values)-limit:]...)
}

func memoryWakeRecentFactFromDistillateFact(factRecord continuityDistillateFact, stateClass string) MemoryWakeStateRecentFact {
	conflictAnchorVersion, conflictAnchorKey := continuityFactAnchorTuple(factRecord)
	return MemoryWakeStateRecentFact{
		Name:               factRecord.Name,
		Value:              factRecord.Value,
		SourceRef:          factRecord.SourceRef,
		EpistemicFlavor:    factRecord.EpistemicFlavor,
		StateClass:         stateClass,
		ConflictKeyVersion: conflictAnchorVersion,
		ConflictKey:        conflictAnchorKey,
		CertaintyScore:     factRecord.CertaintyScore,
	}
}
