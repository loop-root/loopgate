package tcl

import (
	"fmt"
	"strings"
	"time"
)

func NormalizeMemoryCandidate(candidate MemoryCandidateInput) (TCLNode, error) {
	switch candidate.Source {
	case CandidateSourceExplicitFact:
		return normalizeExplicitFactCandidate(candidate)
	case CandidateSourceContinuity:
		return normalizeContinuityFactCandidate(candidate)
	case CandidateSourceTaskMetadata:
		return normalizeTaskMetadataCandidate(candidate)
	case CandidateSourceWorkflowStep:
		return normalizeWorkflowTransitionCandidate(candidate)
	default:
		return TCLNode{}, fmt.Errorf("unsupported memory candidate source %q", candidate.Source)
	}
}

func normalizeExplicitFactCandidate(candidate MemoryCandidateInput) (TCLNode, error) {
	if strings.TrimSpace(candidate.NormalizedFactKey) == "" {
		return TCLNode{}, fmt.Errorf("normalized fact key is required")
	}
	if strings.TrimSpace(candidate.NormalizedFactValue) == "" {
		return TCLNode{}, fmt.Errorf("normalized fact value is required")
	}

	canonicalFactKey := CanonicalizeExplicitMemoryFactKey(candidate.NormalizedFactKey)
	if canonicalFactKey == "" {
		return TCLNode{}, fmt.Errorf("normalized fact key is required")
	}

	trust := candidate.Trust
	if trust == "" {
		trust = TrustUserOriginated
	}
	actor := candidate.Actor
	if actor == "" {
		actor = ObjectUser
	}
	sourceChannel := strings.TrimSpace(candidate.SourceChannel)
	if sourceChannel == "" {
		sourceChannel = "unknown"
	}
	qualifiers := []Qualifier{QualifierPrivate}
	state := StateActive
	outputAction := Action("")
	confidence := 8
	if isDangerousCandidate(candidate) {
		qualifiers = []Qualifier{QualifierPrivate, QualifierExternal}
		state = StateReviewRequired
		outputAction = ActionWrite
		confidence = 9
	}

	normalizedNode := TCLNode{
		ACT:  ActionStore,
		OBJ:  ObjectMemory,
		QUAL: qualifiers,
		OUT:  outputAction,
		STA:  state,
		ANCHOR: DeriveExplicitMemoryConflictAnchor(
			canonicalFactKey,
			candidate.NormalizedFactValue,
		),
		META: TCLMeta{
			ACTOR:  actor,
			TRUST:  trust,
			CONF:   confidence,
			TS:     time.Now().UTC().Unix(),
			SOURCE: sourceChannel,
		},
	}
	if err := ValidateNode(normalizedNode); err != nil {
		return TCLNode{}, err
	}
	return normalizedNode, nil
}

func normalizeContinuityFactCandidate(candidate MemoryCandidateInput) (TCLNode, error) {
	if strings.TrimSpace(candidate.NormalizedFactKey) == "" {
		return TCLNode{}, fmt.Errorf("normalized fact key is required")
	}

	trust := candidate.Trust
	if trust == "" {
		trust = TrustInferred
	}
	actor := candidate.Actor
	if actor == "" {
		actor = ObjectSystem
	}
	sourceChannel := strings.TrimSpace(candidate.SourceChannel)
	if sourceChannel == "" {
		sourceChannel = "unknown"
	}
	qualifiers := []Qualifier{QualifierInternal}
	state := StateActive
	outputAction := Action("")
	confidence := 6
	if isDangerousCandidate(candidate) {
		qualifiers = []Qualifier{QualifierPrivate, QualifierExternal}
		state = StateReviewRequired
		outputAction = ActionWrite
		confidence = 7
	}

	canonicalFactKey := CanonicalizeExplicitMemoryFactKey(candidate.NormalizedFactKey)
	var conflictAnchor *ConflictAnchor
	if canonicalFactKey != "" {
		conflictAnchor = DeriveExplicitMemoryConflictAnchor(canonicalFactKey, candidate.NormalizedFactValue)
	}

	normalizedNode := TCLNode{
		ACT:    ActionStore,
		OBJ:    ObjectMemory,
		QUAL:   qualifiers,
		OUT:    outputAction,
		STA:    state,
		ANCHOR: conflictAnchor,
		META: TCLMeta{
			ACTOR:  actor,
			TRUST:  trust,
			CONF:   confidence,
			TS:     time.Now().UTC().Unix(),
			SOURCE: sourceChannel,
		},
	}
	if err := ValidateNode(normalizedNode); err != nil {
		return TCLNode{}, err
	}
	return normalizedNode, nil
}

func normalizeTaskMetadataCandidate(candidate MemoryCandidateInput) (TCLNode, error) {
	if strings.TrimSpace(candidate.NormalizedFactKey) == "" {
		return TCLNode{}, fmt.Errorf("normalized fact key is required")
	}
	if strings.TrimSpace(candidate.NormalizedFactValue) == "" {
		return TCLNode{}, fmt.Errorf("normalized fact value is required")
	}

	trust := candidate.Trust
	if trust == "" {
		trust = TrustSystemDerived
	}
	actor := candidate.Actor
	if actor == "" {
		actor = ObjectSystem
	}
	sourceChannel := strings.TrimSpace(candidate.SourceChannel)
	if sourceChannel == "" {
		sourceChannel = "unknown"
	}

	normalizedNode := TCLNode{
		ACT:  ActionStore,
		OBJ:  ObjectTask,
		QUAL: []Qualifier{QualifierInternal, QualifierConfirmed},
		STA:  StateActive,
		META: TCLMeta{
			ACTOR:  actor,
			TRUST:  trust,
			CONF:   8,
			TS:     time.Now().UTC().Unix(),
			SOURCE: sourceChannel,
		},
	}
	if err := ValidateNode(normalizedNode); err != nil {
		return TCLNode{}, err
	}
	return normalizedNode, nil
}

func normalizeWorkflowTransitionCandidate(candidate MemoryCandidateInput) (TCLNode, error) {
	normalizedTransitionKey := strings.TrimSpace(candidate.NormalizedFactKey)
	if normalizedTransitionKey == "" {
		return TCLNode{}, fmt.Errorf("normalized fact key is required")
	}

	trust := candidate.Trust
	if trust == "" {
		trust = TrustSystemDerived
	}
	actor := candidate.Actor
	if actor == "" {
		actor = ObjectSystem
	}
	sourceChannel := strings.TrimSpace(candidate.SourceChannel)
	if sourceChannel == "" {
		sourceChannel = "unknown"
	}

	state, err := workflowTransitionState(normalizedTransitionKey)
	if err != nil {
		return TCLNode{}, err
	}
	normalizedNode := TCLNode{
		ACT:  ActionPlan,
		OBJ:  ObjectTask,
		QUAL: []Qualifier{QualifierInternal, QualifierConfirmed},
		STA:  state,
		META: TCLMeta{
			ACTOR:  actor,
			TRUST:  trust,
			CONF:   8,
			TS:     time.Now().UTC().Unix(),
			SOURCE: sourceChannel,
		},
	}
	if err := ValidateNode(normalizedNode); err != nil {
		return TCLNode{}, err
	}
	return normalizedNode, nil
}

func workflowTransitionState(normalizedTransitionKey string) (State, error) {
	switch normalizedTransitionKey {
	case "goal.opened", "task.opened", "task.status.todo":
		return StateActive, nil
	case "task.status.in_progress":
		return StatePending, nil
	case "goal.closed", "task.closed":
		return StateDone, nil
	default:
		return "", fmt.Errorf("unsupported workflow transition key %q", normalizedTransitionKey)
	}
}
