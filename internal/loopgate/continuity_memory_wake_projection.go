package loopgate

import (
	"fmt"
	"strings"
)

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
