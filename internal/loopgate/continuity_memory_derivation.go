package loopgate

import (
	"fmt"
	"strings"
	"time"

	tclpkg "morph/internal/tcl"
)

func deriveContinuityResonateKey(distillateRecord continuityDistillateRecord, now time.Time) continuityResonateKeyRecord {
	return continuityResonateKeyRecord{
		SchemaVersion:     continuityMemorySchemaVersion,
		DerivationVersion: continuityDerivationVersion,
		KeyID:             "rk_" + strings.TrimPrefix(distillateRecord.ThreadID, "thread_"),
		DistillateID:      distillateRecord.DistillateID,
		ThreadID:          distillateRecord.ThreadID,
		Scope:             distillateRecord.Scope,
		GoalType:          distillateRecord.GoalType,
		GoalFamilyID:      distillateRecord.GoalFamilyID,
		RetentionScore:    distillateRecord.RetentionScore,
		EffectiveHotness:  distillateRecord.EffectiveHotness,
		MemoryState:       distillateRecord.MemoryState,
		CreatedAtUTC:      now.UTC().Format(time.RFC3339Nano),
		Tags:              append([]string(nil), distillateRecord.Tags...),
	}
}

func deriveGoalOpSemanticProjection(action string, goalText string, sourceChannel string, trust tclpkg.Trust) *tclpkg.SemanticProjection {
	return deriveMemoryCandidateSemanticProjection(tclpkg.MemoryCandidate{
		Source:              tclpkg.CandidateSourceWorkflowStep,
		SourceChannel:       sourceChannel,
		NormalizedFactKey:   workflowTransitionCandidateKey("goal", action, ""),
		NormalizedFactValue: strings.TrimSpace(goalText),
		Trust:               trust,
		Actor:               tclpkg.ObjectSystem,
	})
}

func deriveUnresolvedItemOpSemanticProjection(action string, status string, itemText string, sourceChannel string, trust tclpkg.Trust) *tclpkg.SemanticProjection {
	return deriveMemoryCandidateSemanticProjection(tclpkg.MemoryCandidate{
		Source:              tclpkg.CandidateSourceWorkflowStep,
		SourceChannel:       sourceChannel,
		NormalizedFactKey:   workflowTransitionCandidateKey("task", action, status),
		NormalizedFactValue: strings.TrimSpace(itemText),
		Trust:               trust,
		Actor:               tclpkg.ObjectSystem,
	})
}

func workflowTransitionCandidateKey(entityKind string, action string, status string) string {
	normalizedEntityKind := strings.TrimSpace(entityKind)
	normalizedAction := strings.TrimSpace(action)
	if normalizedAction == todoItemOpStatusSet {
		return normalizedEntityKind + ".status." + normalizeExplicitTodoWorkflowStatus(status)
	}
	return normalizedEntityKind + "." + normalizedAction
}

func certaintyScoreForEpistemicFlavor(epistemicFlavor string) int {
	switch strings.TrimSpace(epistemicFlavor) {
	case "remembered":
		return 95
	case "freshly_checked":
		return 85
	case "confirmed", "validated":
		return 80
	case "inferred":
		return 60
	default:
		return 50
	}
}

func validateContinuityDistillateFact(factRecord continuityDistillateFact) error {
	if factRecord.SemanticProjection != nil {
		if err := tclpkg.ValidateSemanticProjection(*factRecord.SemanticProjection); err != nil {
			return fmt.Errorf("invalid semantic projection: %w", err)
		}
	}
	return nil
}

func validateContinuityGoalOp(goalOp continuityGoalOp) error {
	if strings.TrimSpace(goalOp.GoalID) == "" {
		return fmt.Errorf("goal_id is required")
	}
	switch strings.TrimSpace(goalOp.Action) {
	case "opened", "closed":
	default:
		return fmt.Errorf("action %q is invalid", goalOp.Action)
	}
	if goalOp.SemanticProjection != nil {
		if err := tclpkg.ValidateSemanticProjection(*goalOp.SemanticProjection); err != nil {
			return fmt.Errorf("semantic projection invalid: %w", err)
		}
	}
	return nil
}

func validateContinuityUnresolvedItemOp(itemOp continuityUnresolvedItemOp) error {
	if strings.TrimSpace(itemOp.ItemID) == "" {
		return fmt.Errorf("item_id is required")
	}
	switch strings.TrimSpace(itemOp.Action) {
	case "opened", "closed":
		if strings.TrimSpace(itemOp.Status) != "" {
			return fmt.Errorf("status must be empty when action is %q", itemOp.Action)
		}
	case todoItemOpStatusSet:
		if normalizeExplicitTodoWorkflowStatus(itemOp.Status) == "" {
			return fmt.Errorf("status %q is invalid for action %q", itemOp.Status, itemOp.Action)
		}
	default:
		return fmt.Errorf("action %q is invalid", itemOp.Action)
	}
	if itemOp.SemanticProjection != nil {
		if err := tclpkg.ValidateSemanticProjection(*itemOp.SemanticProjection); err != nil {
			return fmt.Errorf("semantic projection invalid: %w", err)
		}
	}
	return nil
}

func semanticProjectionAnchorVersion(semanticProjection *tclpkg.SemanticProjection) string {
	if semanticProjection == nil {
		return ""
	}
	return strings.TrimSpace(semanticProjection.AnchorVersion)
}

func semanticProjectionAnchorKey(semanticProjection *tclpkg.SemanticProjection) string {
	if semanticProjection == nil {
		return ""
	}
	return strings.TrimSpace(semanticProjection.AnchorKey)
}

func cloneSemanticProjection(semanticProjection *tclpkg.SemanticProjection) *tclpkg.SemanticProjection {
	if semanticProjection == nil {
		return nil
	}
	clonedProjection := *semanticProjection
	clonedProjection.AnchorVersion = strings.TrimSpace(clonedProjection.AnchorVersion)
	clonedProjection.AnchorKey = strings.TrimSpace(clonedProjection.AnchorKey)
	clonedProjection.ExactSignature = strings.TrimSpace(clonedProjection.ExactSignature)
	clonedProjection.FamilySignature = strings.TrimSpace(clonedProjection.FamilySignature)
	clonedProjection.RiskMotifs = append([]tclpkg.RiskMotif(nil), semanticProjection.RiskMotifs...)
	return &clonedProjection
}

func normalizeContinuityGoalOpForValidation(goalOp continuityGoalOp) continuityGoalOp {
	goalOp.SemanticProjection = cloneSemanticProjection(goalOp.SemanticProjection)
	return goalOp
}

func normalizeContinuityUnresolvedItemOpForValidation(itemOp continuityUnresolvedItemOp) continuityUnresolvedItemOp {
	itemOp.SemanticProjection = cloneSemanticProjection(itemOp.SemanticProjection)
	return itemOp
}

func normalizeContinuityDistillateFactForValidation(factRecord continuityDistillateFact) continuityDistillateFact {
	factRecord.SemanticProjection = cloneSemanticProjection(factRecord.SemanticProjection)
	return factRecord
}

func canonicalizeContinuityDistillateFact(factRecord continuityDistillateFact) continuityDistillateFact {
	return normalizeContinuityDistillateFactForValidation(factRecord)
}

func canonicalizeContinuityDistillateRecord(distillateRecord continuityDistillateRecord) continuityDistillateRecord {
	distillateRecord = cloneContinuityDistillateRecord(distillateRecord)
	for factIndex := range distillateRecord.Facts {
		distillateRecord.Facts[factIndex] = canonicalizeContinuityDistillateFact(distillateRecord.Facts[factIndex])
	}
	return distillateRecord
}

func firstDerivedGoalType(currentState continuityMemoryState, inspectionRecord continuityInspectionRecord) string {
	for _, distillateID := range inspectionRecord.DerivedDistillateIDs {
		if distillateRecord, found := currentState.Distillates[distillateID]; found {
			return distillateRecord.GoalType
		}
	}
	return ""
}

func firstDerivedGoalFamilyID(currentState continuityMemoryState, inspectionRecord continuityInspectionRecord) string {
	for _, distillateID := range inspectionRecord.DerivedDistillateIDs {
		if distillateRecord, found := currentState.Distillates[distillateID]; found {
			return distillateRecord.GoalFamilyID
		}
	}
	return ""
}
