package tcl

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	tclMemoryIDPattern    = regexp.MustCompile(`^M[0-9]+$`)
	tclAnchorFieldPattern = regexp.MustCompile(`^[a-z0-9_]+$`)
	tclAnchorKeyPattern   = regexp.MustCompile(`^[a-z0-9_]+(:[a-z0-9_]+){3,4}$`)
	tclSignaturePattern   = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

func ValidateNode(node TCLNode) error {
	if strings.TrimSpace(node.ID) != "" && !tclMemoryIDPattern.MatchString(strings.TrimSpace(node.ID)) {
		return fmt.Errorf("invalid TCL node id %q", node.ID)
	}
	if !isValidAction(node.ACT) {
		return fmt.Errorf("invalid action %q", node.ACT)
	}
	if !isValidObject(node.OBJ) {
		return fmt.Errorf("invalid object %q", node.OBJ)
	}
	if !isValidState(node.STA) {
		return fmt.Errorf("invalid state %q", node.STA)
	}
	if node.OUT != "" && !isValidAction(node.OUT) {
		return fmt.Errorf("invalid output action %q", node.OUT)
	}
	if len(node.QUAL) > 3 {
		return fmt.Errorf("too many qualifiers: %d", len(node.QUAL))
	}
	seenQualifier := map[Qualifier]struct{}{}
	for _, qualifier := range node.QUAL {
		if !isValidQualifier(qualifier) {
			return fmt.Errorf("invalid qualifier %q", qualifier)
		}
		if _, exists := seenQualifier[qualifier]; exists {
			return fmt.Errorf("duplicate qualifier %q", qualifier)
		}
		seenQualifier[qualifier] = struct{}{}
	}
	for _, relation := range node.REL {
		if !isValidRelationType(relation.Type) {
			return fmt.Errorf("invalid relation type %q", relation.Type)
		}
		targetMID := strings.TrimSpace(relation.TargetMID)
		hasMIDTarget := targetMID != ""
		hasExprTarget := relation.TargetExpr != nil
		if hasMIDTarget == hasExprTarget {
			return fmt.Errorf("relation must target exactly one MID or expression")
		}
		if hasMIDTarget && !tclMemoryIDPattern.MatchString(targetMID) {
			return fmt.Errorf("invalid relation target %q", relation.TargetMID)
		}
		if hasExprTarget {
			if err := ValidateNode(*relation.TargetExpr); err != nil {
				return fmt.Errorf("invalid relation target expression: %w", err)
			}
		}
	}
	if node.META.CONF != 0 {
		if node.META.CONF < 0 || node.META.CONF > 9 {
			return fmt.Errorf("invalid confidence %d", node.META.CONF)
		}
	}
	if node.META.ACTOR != "" && !isValidObject(node.META.ACTOR) {
		return fmt.Errorf("invalid actor %q", node.META.ACTOR)
	}
	if node.META.TRUST != "" && !isValidTrust(node.META.TRUST) {
		return fmt.Errorf("invalid trust %q", node.META.TRUST)
	}
	if node.ANCHOR != nil {
		if err := validateConflictAnchor(*node.ANCHOR); err != nil {
			return fmt.Errorf("invalid conflict anchor: %w", err)
		}
	}
	if node.DECISION != nil && node.DECISION.DISP != "" && !isValidDisposition(node.DECISION.DISP) {
		return fmt.Errorf("invalid disposition %q", node.DECISION.DISP)
	}
	return nil
}

func ValidateSemanticProjection(semanticProjection SemanticProjection) error {
	anchorVersion := strings.TrimSpace(semanticProjection.AnchorVersion)
	anchorKey := strings.TrimSpace(semanticProjection.AnchorKey)
	switch {
	case anchorVersion == "" && anchorKey == "":
	case anchorVersion == "" || anchorKey == "":
		return fmt.Errorf("anchor version and anchor key must both be present or absent")
	default:
		if anchorVersion != "v1" {
			return fmt.Errorf("unsupported anchor version %q", semanticProjection.AnchorVersion)
		}
		if !tclAnchorKeyPattern.MatchString(anchorKey) {
			return fmt.Errorf("invalid anchor key %q", semanticProjection.AnchorKey)
		}
	}
	if err := validateSemanticSignature("exact_signature", semanticProjection.ExactSignature); err != nil {
		return err
	}
	if err := validateSemanticSignature("family_signature", semanticProjection.FamilySignature); err != nil {
		return err
	}
	for _, riskMotif := range semanticProjection.RiskMotifs {
		if !isValidRiskMotif(riskMotif) {
			return fmt.Errorf("invalid risk motif %q", riskMotif)
		}
	}
	return nil
}

func validateConflictAnchor(anchor ConflictAnchor) error {
	if strings.TrimSpace(anchor.Version) != "v1" {
		return fmt.Errorf("unsupported version %q", anchor.Version)
	}
	for fieldName, fieldValue := range map[string]string{
		"domain":    anchor.Domain,
		"entity":    anchor.Entity,
		"slot_kind": anchor.SlotKind,
		"slot_name": anchor.SlotName,
	} {
		if err := validateConflictAnchorField(fieldName, fieldValue, true); err != nil {
			return err
		}
	}
	if err := validateConflictAnchorField("facet", anchor.Facet, false); err != nil {
		return err
	}
	return nil
}

func validateConflictAnchorField(fieldName string, rawValue string, required bool) error {
	trimmedValue := strings.TrimSpace(rawValue)
	if trimmedValue == "" {
		if required {
			return fmt.Errorf("%s is required", fieldName)
		}
		return nil
	}
	if !tclAnchorFieldPattern.MatchString(trimmedValue) {
		return fmt.Errorf("%s %q must contain only lowercase ascii, digits, or underscore", fieldName, rawValue)
	}
	return nil
}

func isValidAction(action Action) bool {
	switch action {
	case ActionAsk, ActionRead, ActionWrite, ActionSearch, ActionAnalyze, ActionSummarize, ActionCompare, ActionPlan, ActionStore, ActionRecall, ActionApprove, ActionDeny:
		return true
	default:
		return false
	}
}

func isValidObject(object Object) bool {
	switch object {
	case ObjectUser, ObjectFile, ObjectRepository, ObjectMemory, ObjectTask, ObjectPolicy, ObjectImage, ObjectCode, ObjectNote, ObjectResult, ObjectSystem, ObjectUnknown:
		return true
	default:
		return false
	}
}

func isValidQualifier(qualifier Qualifier) bool {
	switch qualifier {
	case QualifierSecuritySensitive, QualifierUrgent, QualifierDetailed, QualifierConcise, QualifierPrivate, QualifierExternal, QualifierInternal, QualifierSpeculative, QualifierConfirmed:
		return true
	default:
		return false
	}
}

func isValidState(state State) bool {
	switch state {
	case StateActive, StatePending, StateDone, StateBlocked, StateReviewRequired, StateSuperseded, StateAmbiguous:
		return true
	default:
		return false
	}
}

func isValidRelationType(relationType RelationType) bool {
	switch relationType {
	case RelationSupports, RelationContradicts, RelationRelatedTo, RelationDerivedFrom, RelationDependsOn, RelationImportant:
		return true
	default:
		return false
	}
}

func isValidTrust(trust Trust) bool {
	switch trust {
	case TrustSystemDerived, TrustUserOriginated, TrustExternalDerived, TrustInferred:
		return true
	default:
		return false
	}
}

func isValidDisposition(disposition Disposition) bool {
	switch disposition {
	case DispositionKeep, DispositionDrop, DispositionFlag, DispositionQuarantine, DispositionReview:
		return true
	default:
		return false
	}
}

func isValidRiskMotif(riskMotif RiskMotif) bool {
	switch riskMotif {
	case RiskMotifPrivateExternalMemoryWrite:
		return true
	default:
		return false
	}
}

func validateSemanticSignature(fieldName string, rawSignature string) error {
	trimmedSignature := strings.TrimSpace(rawSignature)
	if trimmedSignature == "" {
		return nil
	}
	if !tclSignaturePattern.MatchString(trimmedSignature) {
		return fmt.Errorf("%s %q must be a sha256 signature", fieldName, rawSignature)
	}
	return nil
}
