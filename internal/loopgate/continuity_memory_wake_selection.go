package loopgate

import (
	"reflect"
	"strings"
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
