package loopgate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	"morph/internal/config"
	"morph/internal/identifiers"
	"morph/internal/secrets"
	tclpkg "morph/internal/tcl"
)

const (
	continuityMemorySchemaVersion = "1"
	memoryScopeGlobal             = "global"
	explicitProfileFactSourceKind = "explicit_profile_fact"

	continuityInspectionOutcomeSkippedThreshold = "skipped_under_threshold"
	continuityInspectionOutcomeNoArtifacts      = "no_artifacts"
	continuityInspectionOutcomeDerived          = "derived"

	continuityReviewStatusPendingReview = "pending_review"
	continuityReviewStatusAccepted      = "accepted"
	continuityReviewStatusRejected      = "rejected"

	continuityReviewDecisionSourceAuto     = "auto"
	continuityReviewDecisionSourceOperator = "operator"

	continuityLineageStatusEligible   = "eligible"
	continuityLineageStatusTombstoned = "tombstoned"
	continuityLineageStatusPurged     = "purged"

	// Continuity inspect bounds sit under the HTTP body cap (maxCapabilityBodyBytes) but are
	// enforced at validation time so oversized inspect payloads fail before distillation work.
	maxContinuityEventsPerInspection       = 512
	maxContinuityInspectApproxPayloadBytes = 4 << 20 // 4 MiB declared or measured payload
)

type continuityInspectionRecord struct {
	InspectionID          string                      `json:"inspection_id"`
	ThreadID              string                      `json:"thread_id"`
	Scope                 string                      `json:"scope"`
	SubmittedAtUTC        string                      `json:"submitted_at_utc"`
	CompletedAtUTC        string                      `json:"completed_at_utc"`
	Outcome               string                      `json:"outcome,omitempty"`
	DerivationOutcome     string                      `json:"derivation_outcome,omitempty"`
	Review                continuityInspectionReview  `json:"review"`
	Lineage               continuityInspectionLineage `json:"lineage"`
	EventCount            int                         `json:"event_count"`
	ApproxPayloadBytes    int                         `json:"approx_payload_bytes"`
	ApproxPromptTokens    int                         `json:"approx_prompt_tokens"`
	DerivedDistillateIDs  []string                    `json:"derived_distillate_ids,omitempty"`
	DerivedResonateKeyIDs []string                    `json:"derived_resonate_key_ids,omitempty"`
}

type continuityInspectionReview struct {
	Status         string `json:"status"`
	DecisionSource string `json:"decision_source,omitempty"`
	ReviewedAtUTC  string `json:"reviewed_at_utc,omitempty"`
	Reason         string `json:"reason,omitempty"`
	OperationID    string `json:"operation_id,omitempty"`
}

type continuityInspectionLineage struct {
	Status                    string `json:"status"`
	ChangedAtUTC              string `json:"changed_at_utc,omitempty"`
	Reason                    string `json:"reason,omitempty"`
	OperationID               string `json:"operation_id,omitempty"`
	SupersededByInspectionID  string `json:"superseded_by_inspection_id,omitempty"`
	SupersededByDistillateID  string `json:"superseded_by_distillate_id,omitempty"`
	SupersededByResonateKeyID string `json:"superseded_by_resonate_key_id,omitempty"`
	SupersedesInspectionID    string `json:"supersedes_inspection_id,omitempty"`
}

type continuityEligibilityDecision struct {
	Allowed           bool
	DenialCode        string
	ReviewStatus      string
	LineageStatus     string
	DerivationOutcome string
}

type continuityGovernanceError struct {
	httpStatus     int
	responseStatus string
	denialCode     string
	reason         string
}

func (continuityError continuityGovernanceError) Error() string {
	return continuityError.reason
}

type continuityArtifactSourceRef struct {
	Kind   string `json:"kind"`
	Ref    string `json:"ref"`
	SHA256 string `json:"sha256,omitempty"`
}

type continuityDistillateFact struct {
	Name               string                     `json:"name"`
	Value              interface{}                `json:"value"`
	SourceRef          string                     `json:"source_ref"`
	EpistemicFlavor    string                     `json:"epistemic_flavor"`
	CertaintyScore     int                        `json:"certainty_score,omitempty"`
	SemanticProjection *tclpkg.SemanticProjection `json:"semantic_projection,omitempty"`
}

type rawContinuityDistillateFact struct {
	Name               string                     `json:"name"`
	Value              interface{}                `json:"value"`
	SourceRef          string                     `json:"source_ref"`
	EpistemicFlavor    string                     `json:"epistemic_flavor"`
	ConflictKeyVersion string                     `json:"conflict_key_version,omitempty"`
	ConflictKey        string                     `json:"conflict_key,omitempty"`
	CertaintyScore     int                        `json:"certainty_score,omitempty"`
	SemanticProjection *tclpkg.SemanticProjection `json:"semantic_projection,omitempty"`
}

func (factRecord *continuityDistillateFact) UnmarshalJSON(rawJSON []byte) error {
	var rawFact rawContinuityDistillateFact
	decoder := json.NewDecoder(bytes.NewReader(rawJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&rawFact); err != nil {
		return err
	}

	factRecord.Name = rawFact.Name
	factRecord.Value = rawFact.Value
	factRecord.SourceRef = rawFact.SourceRef
	factRecord.EpistemicFlavor = rawFact.EpistemicFlavor
	factRecord.CertaintyScore = rawFact.CertaintyScore
	factRecord.SemanticProjection = cloneSemanticProjection(rawFact.SemanticProjection)

	legacyAnchorVersion := strings.TrimSpace(rawFact.ConflictKeyVersion)
	legacyAnchorKey := strings.TrimSpace(rawFact.ConflictKey)
	if legacyAnchorVersion == "" && legacyAnchorKey == "" {
		return nil
	}
	if legacyAnchorVersion == "" || legacyAnchorKey == "" {
		return fmt.Errorf("legacy conflict key fields must both be present or absent")
	}
	if factRecord.SemanticProjection == nil {
		factRecord.SemanticProjection = &tclpkg.SemanticProjection{
			AnchorVersion: legacyAnchorVersion,
			AnchorKey:     legacyAnchorKey,
		}
		return nil
	}

	projectionAnchorVersion := semanticProjectionAnchorVersion(factRecord.SemanticProjection)
	projectionAnchorKey := semanticProjectionAnchorKey(factRecord.SemanticProjection)
	switch {
	case projectionAnchorVersion == "" && projectionAnchorKey == "":
		factRecord.SemanticProjection.AnchorVersion = legacyAnchorVersion
		factRecord.SemanticProjection.AnchorKey = legacyAnchorKey
	case projectionAnchorVersion != legacyAnchorVersion || projectionAnchorKey != legacyAnchorKey:
		return fmt.Errorf("legacy conflict key fields disagree with semantic projection anchor tuple")
	}
	return nil
}

type continuityGoalOp struct {
	GoalID             string                     `json:"goal_id"`
	Text               string                     `json:"text,omitempty"`
	Action             string                     `json:"action"`
	SemanticProjection *tclpkg.SemanticProjection `json:"semantic_projection,omitempty"`
}

type continuityUnresolvedItemOp struct {
	ItemID             string                     `json:"item_id"`
	Text               string                     `json:"text,omitempty"`
	Action             string                     `json:"action"`
	SemanticProjection *tclpkg.SemanticProjection `json:"semantic_projection,omitempty"`
	// Status is set when Action is todoItemOpStatusSet ("status_set"): "todo" or "in_progress".
	Status string `json:"status,omitempty"`
}

type continuityDistillateRecord struct {
	SchemaVersion        string                        `json:"schema_version,omitempty"`
	DerivationVersion    string                        `json:"derivation_version,omitempty"`
	DistillateID         string                        `json:"distillate_id"`
	InspectionID         string                        `json:"inspection_id"`
	ThreadID             string                        `json:"thread_id"`
	Scope                string                        `json:"scope"`
	GoalType             string                        `json:"goal_type,omitempty"`
	GoalFamilyID         string                        `json:"goal_family_id,omitempty"`
	NormalizationVersion string                        `json:"normalization_version,omitempty"`
	UserImportance       string                        `json:"user_importance,omitempty"`
	RetentionScore       int                           `json:"retention_score,omitempty"`
	EffectiveHotness     int                           `json:"effective_hotness,omitempty"`
	MemoryState          string                        `json:"memory_state,omitempty"`
	DerivationSignature  string                        `json:"derivation_signature,omitempty"`
	CreatedAtUTC         string                        `json:"created_at_utc"`
	SourceRefs           []continuityArtifactSourceRef `json:"source_refs,omitempty"`
	Tags                 []string                      `json:"tags,omitempty"`
	Facts                []continuityDistillateFact    `json:"facts,omitempty"`
	GoalOps              []continuityGoalOp            `json:"goal_ops,omitempty"`
	UnresolvedItemOps    []continuityUnresolvedItemOp  `json:"unresolved_item_ops,omitempty"`
	TombstonedAtUTC      string                        `json:"tombstoned_at_utc,omitempty"`
}

type continuityResonateKeyRecord struct {
	SchemaVersion     string   `json:"schema_version,omitempty"`
	DerivationVersion string   `json:"derivation_version,omitempty"`
	KeyID             string   `json:"key_id"`
	DistillateID      string   `json:"distillate_id"`
	ThreadID          string   `json:"thread_id"`
	Scope             string   `json:"scope"`
	GoalType          string   `json:"goal_type,omitempty"`
	GoalFamilyID      string   `json:"goal_family_id,omitempty"`
	RetentionScore    int      `json:"retention_score,omitempty"`
	EffectiveHotness  int      `json:"effective_hotness,omitempty"`
	MemoryState       string   `json:"memory_state,omitempty"`
	CreatedAtUTC      string   `json:"created_at_utc"`
	Tags              []string `json:"tags,omitempty"`
	TombstonedAtUTC   string   `json:"tombstoned_at_utc,omitempty"`
}

type continuityMemoryState struct {
	SchemaVersion  string
	Inspections    map[string]continuityInspectionRecord
	Distillates    map[string]continuityDistillateRecord
	ResonateKeys   map[string]continuityResonateKeyRecord
	WakeState      MemoryWakeStateResponse
	DiagnosticWake continuityDiagnosticWakeReport
}

type continuityMemoryStateFile struct {
	SchemaVersion  string                         `json:"schema_version"`
	Inspections    []continuityInspectionRecord   `json:"inspections,omitempty"`
	Distillates    []continuityDistillateRecord   `json:"distillates,omitempty"`
	ResonateKeys   []continuityResonateKeyRecord  `json:"resonate_keys,omitempty"`
	WakeState      MemoryWakeStateResponse        `json:"wake_state"`
	DiagnosticWake continuityDiagnosticWakeReport `json:"diagnostic_wake"`
}

func loadContinuityMemoryState(rootPath string, legacyStatePath string) (continuityMemoryState, error) {
	memoryPaths := newContinuityMemoryPaths(rootPath, legacyStatePath)
	_, continuityEventsErr := os.Stat(memoryPaths.ContinuityEventsPath)
	if continuityEventsErr == nil {
		return replayContinuityMemoryStateFromEvents(memoryPaths)
	}
	if !os.IsNotExist(continuityEventsErr) {
		return continuityMemoryState{}, continuityEventsErr
	}

	rawStateBytes, err := os.ReadFile(memoryPaths.CurrentStatePath)
	if err != nil {
		if os.IsNotExist(err) {
			if strings.TrimSpace(memoryPaths.LegacyStatePath) != "" {
				legacyState, legacyErr := loadLegacyContinuityMemoryState(memoryPaths.LegacyStatePath)
				if legacyErr == nil {
					return legacyState, nil
				}
				if !os.IsNotExist(legacyErr) {
					return continuityMemoryState{}, legacyErr
				}
			}
			return newEmptyContinuityMemoryState(), nil
		}
		return continuityMemoryState{}, err
	}

	var parsedStateFile continuityMemoryStateFile
	decoder := json.NewDecoder(bytes.NewReader(rawStateBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&parsedStateFile); err != nil {
		return continuityMemoryState{}, err
	}

	loadedState := continuityMemoryState{
		SchemaVersion:  strings.TrimSpace(parsedStateFile.SchemaVersion),
		Inspections:    make(map[string]continuityInspectionRecord, len(parsedStateFile.Inspections)),
		Distillates:    make(map[string]continuityDistillateRecord, len(parsedStateFile.Distillates)),
		ResonateKeys:   make(map[string]continuityResonateKeyRecord, len(parsedStateFile.ResonateKeys)),
		WakeState:      parsedStateFile.WakeState,
		DiagnosticWake: parsedStateFile.DiagnosticWake,
	}
	if loadedState.SchemaVersion == "" {
		loadedState.SchemaVersion = continuityMemorySchemaVersion
	}
	for _, inspectionRecord := range parsedStateFile.Inspections {
		normalizedInspectionRecord, err := normalizeContinuityInspectionRecord(inspectionRecord)
		if err != nil {
			return continuityMemoryState{}, fmt.Errorf("normalize inspection %q: %w", inspectionRecord.InspectionID, err)
		}
		loadedState.Inspections[normalizedInspectionRecord.InspectionID] = normalizedInspectionRecord
	}
	for _, distillateRecord := range parsedStateFile.Distillates {
		loadedState.Distillates[distillateRecord.DistillateID] = cloneContinuityDistillateRecord(distillateRecord)
	}
	for _, resonateKeyRecord := range parsedStateFile.ResonateKeys {
		loadedState.ResonateKeys[resonateKeyRecord.KeyID] = cloneContinuityResonateKeyRecord(resonateKeyRecord)
	}
	if err := loadedState.Validate(); err != nil {
		return continuityMemoryState{}, err
	}
	return canonicalizeContinuityMemoryState(loadedState), nil
}

func loadLegacyContinuityMemoryState(path string) (continuityMemoryState, error) {
	rawStateBytes, err := os.ReadFile(path)
	if err != nil {
		return continuityMemoryState{}, err
	}

	var parsedStateFile continuityMemoryStateFile
	decoder := json.NewDecoder(bytes.NewReader(rawStateBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&parsedStateFile); err != nil {
		return continuityMemoryState{}, err
	}

	loadedState := continuityMemoryState{
		SchemaVersion:  strings.TrimSpace(parsedStateFile.SchemaVersion),
		Inspections:    make(map[string]continuityInspectionRecord, len(parsedStateFile.Inspections)),
		Distillates:    make(map[string]continuityDistillateRecord, len(parsedStateFile.Distillates)),
		ResonateKeys:   make(map[string]continuityResonateKeyRecord, len(parsedStateFile.ResonateKeys)),
		WakeState:      parsedStateFile.WakeState,
		DiagnosticWake: parsedStateFile.DiagnosticWake,
	}
	if loadedState.SchemaVersion == "" {
		loadedState.SchemaVersion = continuityMemorySchemaVersion
	}
	for _, inspectionRecord := range parsedStateFile.Inspections {
		normalizedInspectionRecord, normalizeErr := normalizeContinuityInspectionRecord(inspectionRecord)
		if normalizeErr != nil {
			return continuityMemoryState{}, fmt.Errorf("normalize inspection %q: %w", inspectionRecord.InspectionID, normalizeErr)
		}
		loadedState.Inspections[normalizedInspectionRecord.InspectionID] = normalizedInspectionRecord
	}
	for _, distillateRecord := range parsedStateFile.Distillates {
		loadedState.Distillates[distillateRecord.DistillateID] = cloneContinuityDistillateRecord(distillateRecord)
	}
	for _, resonateKeyRecord := range parsedStateFile.ResonateKeys {
		loadedState.ResonateKeys[resonateKeyRecord.KeyID] = cloneContinuityResonateKeyRecord(resonateKeyRecord)
	}
	if err := loadedState.Validate(); err != nil {
		return continuityMemoryState{}, err
	}
	return canonicalizeContinuityMemoryState(loadedState), nil
}

func newEmptyContinuityMemoryState() continuityMemoryState {
	return continuityMemoryState{
		SchemaVersion: continuityMemorySchemaVersion,
		Inspections:   map[string]continuityInspectionRecord{},
		Distillates:   map[string]continuityDistillateRecord{},
		ResonateKeys:  map[string]continuityResonateKeyRecord{},
	}
}

func saveContinuityMemoryState(rootPath string, currentState continuityMemoryState, runtimeConfig config.RuntimeConfig, nowUTC time.Time) error {
	if err := currentState.Validate(); err != nil {
		return err
	}
	canonicalizedState := canonicalizeContinuityMemoryState(currentState)
	memoryPaths := newContinuityMemoryPaths(rootPath, "")
	stateFile := continuityMemoryStateFile{
		SchemaVersion:  canonicalizedState.SchemaVersion,
		WakeState:      canonicalizedState.WakeState,
		DiagnosticWake: canonicalizedState.DiagnosticWake,
	}

	inspectionIDs := make([]string, 0, len(canonicalizedState.Inspections))
	for inspectionID := range canonicalizedState.Inspections {
		inspectionIDs = append(inspectionIDs, inspectionID)
	}
	sort.Strings(inspectionIDs)
	for _, inspectionID := range inspectionIDs {
		stateFile.Inspections = append(stateFile.Inspections, cloneContinuityInspectionRecord(canonicalizedState.Inspections[inspectionID]))
	}

	distillateIDs := make([]string, 0, len(canonicalizedState.Distillates))
	for distillateID := range canonicalizedState.Distillates {
		distillateIDs = append(distillateIDs, distillateID)
	}
	sort.Strings(distillateIDs)
	for _, distillateID := range distillateIDs {
		stateFile.Distillates = append(stateFile.Distillates, cloneContinuityDistillateRecord(canonicalizedState.Distillates[distillateID]))
	}

	keyIDs := make([]string, 0, len(canonicalizedState.ResonateKeys))
	for keyID := range canonicalizedState.ResonateKeys {
		keyIDs = append(keyIDs, keyID)
	}
	sort.Strings(keyIDs)
	for _, keyID := range keyIDs {
		stateFile.ResonateKeys = append(stateFile.ResonateKeys, cloneContinuityResonateKeyRecord(canonicalizedState.ResonateKeys[keyID]))
	}

	stateBytes, err := json.MarshalIndent(stateFile, "", "  ")
	if err != nil {
		return err
	}
	if err := memoryPaths.ensure(); err != nil {
		return err
	}
	if err := memoryWritePrivateJSONAtomically(memoryPaths.CurrentStatePath, stateBytes); err != nil {
		return err
	}
	return writeContinuityArtifacts(memoryPaths, canonicalizedState, runtimeConfig, nowUTC)
}

func (currentState continuityMemoryState) Validate() error {
	if strings.TrimSpace(currentState.SchemaVersion) == "" {
		return fmt.Errorf("schema_version is required")
	}
	for inspectionID, inspectionRecord := range currentState.Inspections {
		if inspectionID != inspectionRecord.InspectionID {
			return fmt.Errorf("inspection key mismatch for %q", inspectionID)
		}
		if err := validateContinuityInspectionRecord(inspectionRecord); err != nil {
			return fmt.Errorf("inspection %q invalid: %w", inspectionID, err)
		}
	}
	for distillateID, distillateRecord := range currentState.Distillates {
		if distillateID != distillateRecord.DistillateID {
			return fmt.Errorf("distillate key mismatch for %q", distillateID)
		}
		if strings.TrimSpace(distillateRecord.InspectionID) == "" {
			return fmt.Errorf("distillate %q missing inspection_id", distillateID)
		}
		if _, found := currentState.Inspections[distillateRecord.InspectionID]; !found {
			return fmt.Errorf("distillate %q references unknown inspection %q", distillateID, distillateRecord.InspectionID)
		}
		for factIndex, factRecord := range distillateRecord.Facts {
			if err := validateContinuityDistillateFact(factRecord); err != nil {
				return fmt.Errorf("distillate %q fact %d invalid: %w", distillateID, factIndex, err)
			}
		}
		for goalOpIndex, goalOp := range distillateRecord.GoalOps {
			if err := validateContinuityGoalOp(goalOp); err != nil {
				return fmt.Errorf("distillate %q goal_op %d invalid: %w", distillateID, goalOpIndex, err)
			}
		}
		for itemOpIndex, itemOp := range distillateRecord.UnresolvedItemOps {
			if err := validateContinuityUnresolvedItemOp(itemOp); err != nil {
				return fmt.Errorf("distillate %q unresolved_item_op %d invalid: %w", distillateID, itemOpIndex, err)
			}
		}
	}
	for keyID, resonateKeyRecord := range currentState.ResonateKeys {
		if keyID != resonateKeyRecord.KeyID {
			return fmt.Errorf("resonate key mismatch for %q", keyID)
		}
		if strings.TrimSpace(resonateKeyRecord.DistillateID) == "" {
			return fmt.Errorf("resonate key %q missing distillate_id", keyID)
		}
		if _, found := currentState.Distillates[resonateKeyRecord.DistillateID]; !found {
			return fmt.Errorf("resonate key %q references unknown distillate %q", keyID, resonateKeyRecord.DistillateID)
		}
	}
	for inspectionID, inspectionRecord := range currentState.Inspections {
		for _, derivedDistillateID := range inspectionRecord.DerivedDistillateIDs {
			if _, found := currentState.Distillates[derivedDistillateID]; !found {
				return fmt.Errorf("inspection %q references unknown distillate %q", inspectionID, derivedDistillateID)
			}
		}
		for _, derivedKeyID := range inspectionRecord.DerivedResonateKeyIDs {
			if _, found := currentState.ResonateKeys[derivedKeyID]; !found {
				return fmt.Errorf("inspection %q references unknown resonate key %q", inspectionID, derivedKeyID)
			}
		}
	}
	return nil
}

func cloneContinuityMemoryState(currentState continuityMemoryState) continuityMemoryState {
	clonedState := continuityMemoryState{
		SchemaVersion:  currentState.SchemaVersion,
		Inspections:    make(map[string]continuityInspectionRecord, len(currentState.Inspections)),
		Distillates:    make(map[string]continuityDistillateRecord, len(currentState.Distillates)),
		ResonateKeys:   make(map[string]continuityResonateKeyRecord, len(currentState.ResonateKeys)),
		WakeState:      cloneMemoryWakeStateResponse(currentState.WakeState),
		DiagnosticWake: currentState.DiagnosticWake,
	}
	for inspectionID, inspectionRecord := range currentState.Inspections {
		clonedState.Inspections[inspectionID] = cloneContinuityInspectionRecord(inspectionRecord)
	}
	for distillateID, distillateRecord := range currentState.Distillates {
		clonedState.Distillates[distillateID] = cloneContinuityDistillateRecord(distillateRecord)
	}
	for keyID, resonateKeyRecord := range currentState.ResonateKeys {
		clonedState.ResonateKeys[keyID] = cloneContinuityResonateKeyRecord(resonateKeyRecord)
	}
	return clonedState
}

func normalizeContinuityInspectRequest(rawRequest ContinuityInspectRequest) (ContinuityInspectRequest, error) {
	validatedRequest := rawRequest
	validatedRequest.Scope = strings.TrimSpace(validatedRequest.Scope)
	if validatedRequest.Scope == "" {
		validatedRequest.Scope = memoryScopeGlobal
	}
	validatedRequest.Tags = normalizeLoopgateMemoryTags(validatedRequest.Tags)
	for eventIndex, continuityEvent := range validatedRequest.Events {
		validatedRequest.Events[eventIndex].ThreadID = strings.TrimSpace(continuityEvent.ThreadID)
		validatedRequest.Events[eventIndex].Scope = strings.TrimSpace(continuityEvent.Scope)
		validatedRequest.Events[eventIndex].Type = strings.TrimSpace(continuityEvent.Type)
		validatedRequest.Events[eventIndex].EpistemicFlavor = strings.TrimSpace(continuityEvent.EpistemicFlavor)
	}
	if err := validatedRequest.Validate(); err != nil {
		return ContinuityInspectRequest{}, err
	}
	return validatedRequest, nil
}

func (server *Server) inspectContinuityThread(tokenClaims capabilityToken, rawRequest ContinuityInspectRequest) (ContinuityInspectResponse, error) {
	validatedRequest, err := normalizeContinuityInspectRequest(rawRequest)
	if err != nil {
		return ContinuityInspectResponse{}, err
	}

	server.memoryMu.Lock()
	partition, partitionErr := server.ensureMemoryPartitionLocked(tokenClaims.TenantID)
	if partitionErr != nil {
		server.memoryMu.Unlock()
		return ContinuityInspectResponse{}, partitionErr
	}
	existingInspection, found := partition.state.Inspections[validatedRequest.InspectionID]
	if found {
		existingInspection = normalizeContinuityInspectionRecordMust(existingInspection)
		_ = server.inspectionLineageSelectionDecisionLocked(partition.state, existingInspection.InspectionID)
		server.memoryMu.Unlock()
		return buildContinuityInspectResponse(existingInspection), nil
	}
	server.memoryMu.Unlock()

	var inspectResponse ContinuityInspectResponse
	err = server.mutateContinuityMemory(tokenClaims.TenantID, tokenClaims.ControlSessionID, "memory.continuity.inspected", func(workingState continuityMemoryState, nowUTC time.Time) (continuityMemoryState, map[string]interface{}, continuityMutationEvents, error) {
		if existingInspection, found := workingState.Inspections[validatedRequest.InspectionID]; found {
			existingInspection = normalizeContinuityInspectionRecordMust(existingInspection)
			_ = server.inspectionLineageSelectionDecisionLocked(workingState, existingInspection.InspectionID)
			inspectResponse = buildContinuityInspectResponse(existingInspection)
			return workingState, nil, continuityMutationEvents{}, nil
		}

		inspectionRecord := continuityInspectionRecord{
			InspectionID:       validatedRequest.InspectionID,
			ThreadID:           validatedRequest.ThreadID,
			Scope:              validatedRequest.Scope,
			SubmittedAtUTC:     nowUTC.Format(time.RFC3339Nano),
			CompletedAtUTC:     nowUTC.Format(time.RFC3339Nano),
			EventCount:         len(validatedRequest.Events),
			ApproxPayloadBytes: actualContinuityPayloadBytes(validatedRequest.Events),
			ApproxPromptTokens: actualContinuityPromptTokens(validatedRequest.Events),
			Lineage: continuityInspectionLineage{
				Status:       continuityLineageStatusEligible,
				ChangedAtUTC: nowUTC.Format(time.RFC3339Nano),
				OperationID:  validatedRequest.InspectionID,
			},
		}
		inspectionRecord.DerivationOutcome = continuityInspectionOutcomeDerived
		if !server.continuityThresholdReached(inspectionRecord) {
			inspectionRecord.DerivationOutcome = continuityInspectionOutcomeSkippedThreshold
		}

		var derivedDistillate continuityDistillateRecord
		var derivedResonateKey continuityResonateKeyRecord
		var hasDerivedArtifacts bool
		if inspectionRecord.DerivationOutcome == continuityInspectionOutcomeDerived {
			derivedDistillate = deriveContinuityDistillate(validatedRequest, inspectionRecord, nowUTC, server.runtimeConfig, server.goalAliases)
			if len(derivedDistillate.Facts) == 0 && len(derivedDistillate.GoalOps) == 0 && len(derivedDistillate.UnresolvedItemOps) == 0 {
				inspectionRecord.DerivationOutcome = continuityInspectionOutcomeNoArtifacts
			} else {
				derivedResonateKey = deriveContinuityResonateKey(derivedDistillate, nowUTC)
				hasDerivedArtifacts = true
			}
		}

		switch inspectionRecord.DerivationOutcome {
		case continuityInspectionOutcomeDerived:
			if server.policy.Memory.ContinuityReviewRequired {
				inspectionRecord.Review = continuityInspectionReview{
					Status: continuityReviewStatusPendingReview,
				}
			} else {
				inspectionRecord.Review = continuityInspectionReview{
					Status:         continuityReviewStatusAccepted,
					DecisionSource: continuityReviewDecisionSourceAuto,
					ReviewedAtUTC:  nowUTC.Format(time.RFC3339Nano),
					OperationID:    validatedRequest.InspectionID,
				}
			}
		default:
			inspectionRecord.Review = continuityInspectionReview{
				Status:         continuityReviewStatusAccepted,
				DecisionSource: continuityReviewDecisionSourceAuto,
				ReviewedAtUTC:  nowUTC.Format(time.RFC3339Nano),
				OperationID:    validatedRequest.InspectionID,
			}
		}

		inspectionRecord.Outcome = inspectionRecord.DerivationOutcome
		if hasDerivedArtifacts {
			inspectionRecord.DerivedDistillateIDs = []string{derivedDistillate.DistillateID}
			inspectionRecord.DerivedResonateKeyIDs = []string{derivedResonateKey.KeyID}
			workingState.Distillates[derivedDistillate.DistillateID] = derivedDistillate
			workingState.ResonateKeys[derivedResonateKey.KeyID] = derivedResonateKey
		}
		workingState.Inspections[inspectionRecord.InspectionID] = inspectionRecord
		inspectResponse = buildContinuityInspectResponse(inspectionRecord)
		mutationEvents := continuityMutationEvents{
			Continuity: []continuityAuthoritativeEvent{{
				SchemaVersion: continuityMemorySchemaVersion,
				EventID:       "continuity_inspection_" + inspectionRecord.InspectionID,
				EventType:     "continuity_inspection_recorded",
				CreatedAtUTC:  nowUTC.Format(time.RFC3339Nano),
				Actor:         tokenClaims.ControlSessionID,
				Scope:         inspectionRecord.Scope,
				InspectionID:  inspectionRecord.InspectionID,
				ThreadID:      inspectionRecord.ThreadID,
				GoalType:      derivedDistillate.GoalType,
				GoalFamilyID:  derivedDistillate.GoalFamilyID,
				Request:       &validatedRequest,
				Inspection:    ptrContinuityInspectionRecord(cloneContinuityInspectionRecord(inspectionRecord)),
				Distillate:    ptrContinuityDistillateRecord(cloneContinuityDistillateRecord(derivedDistillate)),
				ResonateKey:   ptrContinuityResonateKeyRecord(cloneContinuityResonateKeyRecord(derivedResonateKey)),
			}},
		}
		if !hasDerivedArtifacts {
			mutationEvents.Continuity[0].Distillate = nil
			mutationEvents.Continuity[0].ResonateKey = nil
			mutationEvents.Continuity[0].GoalType = ""
			mutationEvents.Continuity[0].GoalFamilyID = ""
		}
		if hasDerivedArtifacts {
			mutationEvents.Goal = append(mutationEvents.Goal, continuityGoalEvent{
				SchemaVersion:      continuityMemorySchemaVersion,
				EventID:            "goal_projection_" + inspectionRecord.InspectionID,
				EventType:          "goal_projection_updated",
				CreatedAtUTC:       nowUTC.Format(time.RFC3339Nano),
				Actor:              tokenClaims.ControlSessionID,
				InspectionID:       inspectionRecord.InspectionID,
				ThreadID:           inspectionRecord.ThreadID,
				GoalType:           derivedDistillate.GoalType,
				GoalFamilyID:       derivedDistillate.GoalFamilyID,
				NeedsAliasCuration: strings.Contains(derivedDistillate.GoalFamilyID, ":fallback_"),
				GoalOps:            append([]continuityGoalOp(nil), derivedDistillate.GoalOps...),
				UnresolvedItemOps:  append([]continuityUnresolvedItemOp(nil), derivedDistillate.UnresolvedItemOps...),
			})
		}
		return workingState, map[string]interface{}{
			"inspection_id":          inspectionRecord.InspectionID,
			"thread_id":              inspectionRecord.ThreadID,
			"derivation_outcome":     inspectionRecord.DerivationOutcome,
			"review_status":          inspectionRecord.Review.Status,
			"lineage_status":         inspectionRecord.Lineage.Status,
			"event_count":            inspectionRecord.EventCount,
			"approx_payload_bytes":   inspectionRecord.ApproxPayloadBytes,
			"approx_prompt_tokens":   inspectionRecord.ApproxPromptTokens,
			"derived_distillate_ids": append([]string(nil), inspectionRecord.DerivedDistillateIDs...),
			"derived_resonate_keys":  append([]string(nil), inspectionRecord.DerivedResonateKeyIDs...),
		}, mutationEvents, nil
	})
	if err != nil {
		return ContinuityInspectResponse{}, err
	}
	return inspectResponse, nil
}

func buildContinuityInspectResponse(inspectionRecord continuityInspectionRecord) ContinuityInspectResponse {
	return ContinuityInspectResponse{
		InspectionID:          inspectionRecord.InspectionID,
		ThreadID:              inspectionRecord.ThreadID,
		Outcome:               inspectionRecord.DerivationOutcome,
		DerivationOutcome:     inspectionRecord.DerivationOutcome,
		ReviewStatus:          inspectionRecord.Review.Status,
		LineageStatus:         inspectionRecord.Lineage.Status,
		DerivedDistillateIDs:  append([]string(nil), inspectionRecord.DerivedDistillateIDs...),
		DerivedResonateKeyIDs: append([]string(nil), inspectionRecord.DerivedResonateKeyIDs...),
	}
}

func (server *Server) continuityThresholdReached(inspectionRecord continuityInspectionRecord) bool {
	if server.policy.Memory.SubmitPreviousMinEvents > 0 && inspectionRecord.EventCount >= server.policy.Memory.SubmitPreviousMinEvents {
		return true
	}
	if server.policy.Memory.SubmitPreviousMinPayloadBytes > 0 && inspectionRecord.ApproxPayloadBytes >= server.policy.Memory.SubmitPreviousMinPayloadBytes {
		return true
	}
	if server.policy.Memory.SubmitPreviousMinPromptTokens > 0 && inspectionRecord.ApproxPromptTokens >= server.policy.Memory.SubmitPreviousMinPromptTokens {
		return true
	}
	return false
}

func (server *Server) mutateContinuityMemory(tenantID string, controlSessionID string, auditEventType string, applyMutation func(continuityMemoryState, time.Time) (continuityMemoryState, map[string]interface{}, continuityMutationEvents, error)) error {
	server.memoryMu.Lock()
	defer server.memoryMu.Unlock()

	partition, err := server.ensureMemoryPartitionLocked(tenantID)
	if err != nil {
		return err
	}

	workingState := cloneContinuityMemoryState(partition.state)
	nowUTC := server.now().UTC()

	updatedState, auditData, mutationEvents, err := applyMutation(workingState, nowUTC)
	if err != nil {
		return err
	}
	updatedState.WakeState, updatedState.DiagnosticWake = buildLoopgateWakeProducts(updatedState, nowUTC, server.runtimeConfig)

	memoryPaths := newContinuityMemoryPaths(partition.rootPath, legacyContinuityPathForPartitionFromKey(server, partition.partitionKey))
	// Security ordering: append durable continuity JSONL only after the hash-chained audit
	// ledger records the mutation. Otherwise a failed audit leaves replayable continuity
	// events with no corresponding audit evidence (reviewers: CR S1, GR Finding 2, MR F10–F12).
	if auditData != nil {
		if err := server.logEvent(auditEventType, controlSessionID, auditData); err != nil {
			return err
		}
	}
	if err := appendContinuityMutationEvents(memoryPaths, mutationEvents); err != nil {
		return err
	}
	if err := server.saveMemoryState(partition.rootPath, updatedState, server.runtimeConfig); err != nil {
		return err
	}
	partition.state = canonicalizeContinuityMemoryState(updatedState)
	if partition.backend != nil {
		if syncErr := partition.backend.SyncAuthoritativeState(context.Background(), partition.state); syncErr != nil {
			return syncErr
		}
	}
	return nil
}

func deriveContinuityDistillate(validatedRequest ContinuityInspectRequest, inspectionRecord continuityInspectionRecord, now time.Time, runtimeConfig config.RuntimeConfig, goalAliases config.GoalAliases) continuityDistillateRecord {
	distillateID := "dist_" + strings.TrimPrefix(validatedRequest.ThreadID, "thread_")
	distillateRecord := continuityDistillateRecord{
		SchemaVersion:       continuityMemorySchemaVersion,
		DerivationVersion:   continuityDerivationVersion,
		DistillateID:        distillateID,
		InspectionID:        inspectionRecord.InspectionID,
		ThreadID:            validatedRequest.ThreadID,
		Scope:               validatedRequest.Scope,
		UserImportance:      "somewhat_important",
		CreatedAtUTC:        now.UTC().Format(time.RFC3339Nano),
		Tags:                append([]string(nil), validatedRequest.Tags...),
		DerivationSignature: buildDerivationSignature(validatedRequest),
	}

	discoveredTags := make(map[string]struct{}, len(validatedRequest.Tags))
	for _, initialTag := range validatedRequest.Tags {
		discoveredTags[initialTag] = struct{}{}
	}
	sourceRefSeen := map[string]struct{}{}
	for _, continuityEvent := range validatedRequest.Events {
		eventSourceRef := continuityArtifactSourceRef{
			Kind:   "morph_ledger_event",
			Ref:    fmt.Sprintf("ledger_sequence:%d", continuityEvent.LedgerSequence),
			SHA256: continuityEvent.EventHash,
		}
		if _, seen := sourceRefSeen[eventSourceRef.Ref]; !seen {
			sourceRefSeen[eventSourceRef.Ref] = struct{}{}
			distillateRecord.SourceRefs = append(distillateRecord.SourceRefs, eventSourceRef)
		}
		switch continuityEvent.Type {
		case "provider_fact_observed":
			candidateFacts, _ := continuityEvent.Payload["facts"].(map[string]interface{})
			factNames := make([]string, 0, len(candidateFacts))
			for factName := range candidateFacts {
				factNames = append(factNames, factName)
			}
			sort.Strings(factNames)
			for _, factName := range factNames {
				factValue := candidateFacts[factName]
				semanticProjection := deriveContinuityFactSemanticProjection(factName, factValue)
				distillateRecord.Facts = append(distillateRecord.Facts, continuityDistillateFact{
					Name:               factName,
					Value:              factValue,
					SourceRef:          eventSourceRef.Ref,
					EpistemicFlavor:    continuityEvent.EpistemicFlavor,
					CertaintyScore:     certaintyScoreForEpistemicFlavor(continuityEvent.EpistemicFlavor),
					SemanticProjection: semanticProjection,
				})
				recordLoopgateMemoryTags(discoveredTags, factName)
				if factValue, isString := factValue.(string); isString {
					recordLoopgateMemoryTags(discoveredTags, factValue)
				}
			}
		case "goal_opened":
			goalID, _ := continuityEvent.Payload["goal_id"].(string)
			goalText, _ := continuityEvent.Payload["text"].(string)
			if strings.TrimSpace(goalID) != "" {
				distillateRecord.GoalOps = append(distillateRecord.GoalOps, continuityGoalOp{
					GoalID:             strings.TrimSpace(goalID),
					Text:               strings.TrimSpace(goalText),
					Action:             "opened",
					SemanticProjection: deriveGoalOpSemanticProjection("opened", strings.TrimSpace(goalText), "continuity_inspection", tclpkg.TrustInferred),
				})
				if distillateRecord.GoalFamilyID == "" {
					goalNormalization := normalizeGoalFamily(goalText, goalAliases)
					distillateRecord.GoalType = goalNormalization.GoalType
					distillateRecord.GoalFamilyID = goalNormalization.GoalFamilyID
					distillateRecord.NormalizationVersion = goalNormalization.NormalizationVersion
				}
				recordLoopgateMemoryTags(discoveredTags, goalID, goalText)
			}
		case "goal_closed":
			goalID, _ := continuityEvent.Payload["goal_id"].(string)
			if strings.TrimSpace(goalID) != "" {
				distillateRecord.GoalOps = append(distillateRecord.GoalOps, continuityGoalOp{
					GoalID:             strings.TrimSpace(goalID),
					Action:             "closed",
					SemanticProjection: deriveGoalOpSemanticProjection("closed", "", "continuity_inspection", tclpkg.TrustInferred),
				})
				recordLoopgateMemoryTags(discoveredTags, goalID)
			}
		case "unresolved_item_opened":
			itemID, _ := continuityEvent.Payload["item_id"].(string)
			itemText, _ := continuityEvent.Payload["text"].(string)
			if strings.TrimSpace(itemID) != "" {
				distillateRecord.UnresolvedItemOps = append(distillateRecord.UnresolvedItemOps, continuityUnresolvedItemOp{
					ItemID:             strings.TrimSpace(itemID),
					Text:               strings.TrimSpace(itemText),
					Action:             "opened",
					SemanticProjection: deriveUnresolvedItemOpSemanticProjection("opened", "", strings.TrimSpace(itemText), "continuity_inspection", tclpkg.TrustInferred),
				})
				recordLoopgateMemoryTags(discoveredTags, itemID, itemText)
			}
		case "unresolved_item_resolved":
			itemID, _ := continuityEvent.Payload["item_id"].(string)
			if strings.TrimSpace(itemID) != "" {
				distillateRecord.UnresolvedItemOps = append(distillateRecord.UnresolvedItemOps, continuityUnresolvedItemOp{
					ItemID:             strings.TrimSpace(itemID),
					Action:             "closed",
					SemanticProjection: deriveUnresolvedItemOpSemanticProjection("closed", "", "", "continuity_inspection", tclpkg.TrustInferred),
				})
				recordLoopgateMemoryTags(discoveredTags, itemID)
			}
		}
	}

	sort.Slice(distillateRecord.Facts, func(leftIndex int, rightIndex int) bool {
		return distillateRecord.Facts[leftIndex].Name < distillateRecord.Facts[rightIndex].Name
	})
	distillateRecord.Tags = normalizeLoopgateMemoryTags(append([]string(nil), normalizedLoopgateTagSet(discoveredTags)...))
	if distillateRecord.GoalType == "" {
		goalNormalization := normalizeGoalFamily(strings.Join(distillateRecord.Tags, " "), goalAliases)
		distillateRecord.GoalType = goalNormalization.GoalType
		distillateRecord.GoalFamilyID = goalNormalization.GoalFamilyID
		distillateRecord.NormalizationVersion = goalNormalization.NormalizationVersion
	}
	distillateRecord.RetentionScore = importanceBase(runtimeConfig, distillateRecord.UserImportance) + runtimeConfig.Memory.Scoring.ApprovedGoalAnchor
	distillateRecord.EffectiveHotness = hotnessBase(runtimeConfig, distillateRecord.UserImportance)
	distillateRecord.MemoryState = deriveMemoryState(distillateRecord.EffectiveHotness, continuityLineageStatusEligible)
	return distillateRecord
}

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

type continuityFactCandidate struct {
	Fact          continuityDistillateFact
	DistillateID  string
	CreatedAtUTC  string
	AuthorityLane int
}

func deriveContinuityFactSemanticProjection(factName string, factValue interface{}) *tclpkg.SemanticProjection {
	normalizedFactName := strings.TrimSpace(factName)
	if normalizedFactName == "" {
		return nil
	}

	return deriveMemoryCandidateSemanticProjection(tclpkg.MemoryCandidate{
		Source:              tclpkg.CandidateSourceContinuity,
		SourceChannel:       "continuity_inspection",
		RawSourceText:       "",
		NormalizedFactKey:   normalizedFactName,
		NormalizedFactValue: strings.TrimSpace(fmt.Sprint(factValue)),
		Trust:               tclpkg.TrustInferred,
		Actor:               tclpkg.ObjectSystem,
	})
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

func appendRecentFactCandidate(recentFactsBySlotKey map[string]MemoryWakeStateRecentFact, recentFactOrder *[]string, factCandidatesByAnchorTupleKey map[string]continuityFactCandidate, ambiguousAnchorTupleKeys map[string]struct{}, candidate continuityFactCandidate) {
	if candidate.Fact.CertaintyScore <= 0 {
		candidate.Fact.CertaintyScore = certaintyScoreForEpistemicFlavor(candidate.Fact.EpistemicFlavor)
	}
	factAnchorVersion, factAnchorKey := continuityFactAnchorTuple(candidate.Fact)
	factAnchorTupleKey := anchorTupleKey(factAnchorVersion, factAnchorKey)
	if factAnchorTupleKey == "" {
		slotKey := candidate.Fact.Name + ":" + candidate.Fact.SourceRef
		recentFactsBySlotKey[slotKey] = memoryWakeRecentFactFromDistillateFact(candidate.Fact)
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
			recentFactsBySlotKey[factAnchorTupleKey] = memoryWakeRecentFactFromDistillateFact(candidate.Fact)
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
	recentFactsBySlotKey[factAnchorTupleKey] = memoryWakeRecentFactFromDistillateFact(candidate.Fact)
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

func (server *Server) loadMemoryWakeState(tenantID string) (MemoryWakeStateResponse, error) {
	server.memoryMu.Lock()
	partition, err := server.ensureMemoryPartitionLocked(tenantID)
	if err != nil {
		server.memoryMu.Unlock()
		return MemoryWakeStateResponse{}, err
	}
	backend := partition.backend
	server.memoryMu.Unlock()
	if backend == nil {
		return MemoryWakeStateResponse{}, fmt.Errorf("memory backend is not configured")
	}
	return backend.BuildWakeState(context.Background(), MemoryWakeStateRequest{Scope: memoryScopeGlobal})
}

func (server *Server) loadMemoryDiagnosticWake(tenantID string) MemoryDiagnosticWakeResponse {
	server.memoryMu.Lock()
	defer server.memoryMu.Unlock()
	partition, err := server.ensureMemoryPartitionLocked(tenantID)
	if err != nil {
		return MemoryDiagnosticWakeResponse{}
	}
	return cloneMemoryDiagnosticWakeResponse(memoryDiagnosticWakeResponseFromReport(partition.state.DiagnosticWake))
}

func (server *Server) discoverMemoryFromPartitionState(partition *memoryPartition, validatedRequest MemoryDiscoverRequest) (MemoryDiscoverResponse, error) {
	queryTags := tokenizeLoopgateMemoryText(validatedRequest.Query)
	server.memoryMu.Lock()
	defer server.memoryMu.Unlock()

	discoveredItems := make([]MemoryDiscoverItem, 0, len(partition.state.ResonateKeys))
	for _, resonateKeyRecord := range activeLoopgateResonateKeys(partition.state) {
		if resonateKeyRecord.Scope != validatedRequest.Scope {
			continue
		}
		matchCount := 0
		for _, queryTag := range queryTags {
			for _, keyTag := range resonateKeyRecord.Tags {
				if keyTag == queryTag {
					matchCount++
					break
				}
			}
		}
		if matchCount == 0 {
			continue
		}
		discoveredItems = append(discoveredItems, MemoryDiscoverItem{
			KeyID:        resonateKeyRecord.KeyID,
			ThreadID:     resonateKeyRecord.ThreadID,
			DistillateID: resonateKeyRecord.DistillateID,
			Scope:        resonateKeyRecord.Scope,
			CreatedAtUTC: resonateKeyRecord.CreatedAtUTC,
			Tags:         append([]string(nil), resonateKeyRecord.Tags...),
			MatchCount:   matchCount,
		})
	}
	sort.Slice(discoveredItems, func(leftIndex int, rightIndex int) bool {
		leftItem := discoveredItems[leftIndex]
		rightItem := discoveredItems[rightIndex]
		switch {
		case leftItem.MatchCount != rightItem.MatchCount:
			return leftItem.MatchCount > rightItem.MatchCount
		case leftItem.CreatedAtUTC != rightItem.CreatedAtUTC:
			return leftItem.CreatedAtUTC > rightItem.CreatedAtUTC
		default:
			return leftItem.KeyID < rightItem.KeyID
		}
	})
	if len(discoveredItems) > validatedRequest.MaxItems {
		discoveredItems = append([]MemoryDiscoverItem(nil), discoveredItems[:validatedRequest.MaxItems]...)
	}
	return MemoryDiscoverResponse{
		Scope: validatedRequest.Scope,
		Query: validatedRequest.Query,
		Items: discoveredItems,
	}, nil
}

func (server *Server) discoverMemory(tenantID string, rawRequest MemoryDiscoverRequest) (MemoryDiscoverResponse, error) {
	validatedRequest := rawRequest
	if validatedRequest.Scope == "" {
		validatedRequest.Scope = memoryScopeGlobal
	}
	if validatedRequest.MaxItems == 0 {
		validatedRequest.MaxItems = 5
	}
	if err := validatedRequest.Validate(); err != nil {
		return MemoryDiscoverResponse{}, err
	}
	server.memoryMu.Lock()
	partition, err := server.ensureMemoryPartitionLocked(tenantID)
	if err != nil {
		server.memoryMu.Unlock()
		return MemoryDiscoverResponse{}, err
	}
	backend := partition.backend
	server.memoryMu.Unlock()
	if backend == nil {
		return MemoryDiscoverResponse{}, fmt.Errorf("memory backend is not configured")
	}
	return backend.Discover(context.Background(), validatedRequest)
}

func (server *Server) rememberMemoryFact(tokenClaims capabilityToken, rawRequest MemoryRememberRequest) (MemoryRememberResponse, error) {
	validatedRequest, err := server.normalizeMemoryRememberRequest(rawRequest)
	if err != nil {
		return MemoryRememberResponse{}, err
	}
	tclAnalysis, err := server.analyzeExplicitMemoryCandidate(validatedRequest)
	if err != nil {
		if auditErr := server.logDeniedMemoryRememberCandidate(tokenClaims.ControlSessionID, validatedRequest, DenialCodeMemoryCandidateInvalid, DenialCodeMemoryCandidateInvalid, map[string]interface{}{
			"tcl_source_channel":   validatedRequest.SourceChannel,
			"tcl_candidate_source": validatedRequest.CandidateSource,
			"tcl_reason_code":      DenialCodeMemoryCandidateInvalid,
		}); auditErr != nil {
			return MemoryRememberResponse{}, continuityGovernanceError{
				httpStatus:     503,
				responseStatus: ResponseStatusError,
				denialCode:     DenialCodeAuditUnavailable,
				reason:         "control-plane audit is unavailable",
			}
		}
		return MemoryRememberResponse{}, continuityGovernanceError{
			httpStatus:     400,
			responseStatus: ResponseStatusDenied,
			denialCode:     DenialCodeMemoryCandidateInvalid,
			reason:         "explicit memory write could not be analyzed safely and was not stored",
		}
	}
	denialCode, safeReason, shouldPersist := memoryRememberGovernanceDecision(tclAnalysis.PolicyDecision)
	if !shouldPersist {
		if auditErr := server.logDeniedMemoryRememberCandidate(tokenClaims.ControlSessionID, validatedRequest, denialCode, tclAnalysis.PolicyDecision.REASON, tclAnalysis.AuditSummary); auditErr != nil {
			return MemoryRememberResponse{}, continuityGovernanceError{
				httpStatus:     503,
				responseStatus: ResponseStatusError,
				denialCode:     DenialCodeAuditUnavailable,
				reason:         "control-plane audit is unavailable",
			}
		}
		return MemoryRememberResponse{}, continuityGovernanceError{
			httpStatus:     403,
			responseStatus: ResponseStatusDenied,
			denialCode:     denialCode,
			reason:         safeReason,
		}
	}

	rememberedAnchorVersion, rememberedAnchorKey := memoryAnchorTupleForNode(tclAnalysis.Node)
	server.memoryMu.Lock()
	partition, partitionErr := server.ensureMemoryPartitionLocked(tokenClaims.TenantID)
	if partitionErr != nil {
		server.memoryMu.Unlock()
		return MemoryRememberResponse{}, partitionErr
	}
	existingFact, foundExisting := activeExplicitProfileFactByAnchorTuple(partition.state, rememberedAnchorVersion, rememberedAnchorKey)
	server.memoryMu.Unlock()
	if foundExisting && existingFact.FactValue == validatedRequest.FactValue {
		return MemoryRememberResponse{
			Scope:           validatedRequest.Scope,
			FactKey:         validatedRequest.FactKey,
			FactValue:       existingFact.FactValue,
			InspectionID:    existingFact.InspectionID,
			DistillateID:    existingFact.DistillateID,
			ResonateKeyID:   existingFact.ResonateKeyID,
			RememberedAtUTC: existingFact.CreatedAtUTC,
			UpdatedExisting: false,
		}, nil
	}

	var rememberResponse MemoryRememberResponse
	err = server.mutateContinuityMemory(tokenClaims.TenantID, tokenClaims.ControlSessionID, "memory.fact.remembered", func(workingState continuityMemoryState, nowUTC time.Time) (continuityMemoryState, map[string]interface{}, continuityMutationEvents, error) {
		if err := server.consumeMemoryFactWriteBudgetLocked(tokenClaims.ControlSessionID, tokenClaims.PeerIdentity.UID, nowUTC); err != nil {
			return workingState, nil, continuityMutationEvents{}, err
		}

		existingFact, foundExisting := activeExplicitProfileFactByAnchorTuple(workingState, rememberedAnchorVersion, rememberedAnchorKey)
		var existingInspection continuityInspectionRecord
		var foundInspection bool
		if foundExisting {
			existingInspection, foundInspection = workingState.Inspections[existingFact.InspectionID]
			if !foundInspection {
				return workingState, nil, continuityMutationEvents{}, fmt.Errorf("existing remembered fact inspection %q not found", existingFact.InspectionID)
			}
		}

		rememberedThreadSuffix := makeEventPayloadID("memfact", struct {
			FactKey   string `json:"fact_key"`
			FactValue string `json:"fact_value"`
			NowUTC    string `json:"now_utc"`
		}{
			FactKey:   validatedRequest.FactKey,
			FactValue: validatedRequest.FactValue,
			NowUTC:    nowUTC.Format(time.RFC3339Nano),
		})
		threadID := "thread_" + rememberedThreadSuffix
		inspectionID := "inspect_" + rememberedThreadSuffix
		distillateID := "dist_" + rememberedThreadSuffix
		resonateKeyID := "rk_" + rememberedThreadSuffix
		if foundExisting {
			existingInspection.Lineage = continuityInspectionLineage{
				Status:       continuityLineageStatusTombstoned,
				ChangedAtUTC: nowUTC.Format(time.RFC3339Nano),
				Reason:       "superseded_by_newer_explicit_profile_fact",
				OperationID: "remember_" + makeEventPayloadID("supersede", struct {
					FactKey string `json:"fact_key"`
					NowUTC  string `json:"now_utc"`
				}{
					FactKey: validatedRequest.FactKey,
					NowUTC:  nowUTC.Format(time.RFC3339Nano),
				}),
				SupersededByInspectionID:  inspectionID,
				SupersededByDistillateID:  distillateID,
				SupersededByResonateKeyID: resonateKeyID,
			}
			stampContinuityDerivedArtifactsExcluded(&workingState, existingInspection, nowUTC)
			workingState.Inspections[existingFact.InspectionID] = existingInspection
		}
		sourceRef := continuityArtifactSourceRef{
			Kind: explicitProfileFactSourceKind,
			Ref:  validatedRequest.FactKey,
		}
		userImportance := "somewhat_important"
		retentionScore := importanceBase(server.runtimeConfig, userImportance) + server.runtimeConfig.Memory.Scoring.ExplicitUserBonus
		effectiveHotness := hotnessBase(server.runtimeConfig, userImportance)
		inspectionRecord := continuityInspectionRecord{
			InspectionID:      inspectionID,
			ThreadID:          threadID,
			Scope:             validatedRequest.Scope,
			SubmittedAtUTC:    nowUTC.Format(time.RFC3339Nano),
			CompletedAtUTC:    nowUTC.Format(time.RFC3339Nano),
			Outcome:           continuityInspectionOutcomeDerived,
			DerivationOutcome: continuityInspectionOutcomeDerived,
			Review: continuityInspectionReview{
				Status:         continuityReviewStatusAccepted,
				DecisionSource: continuityReviewDecisionSourceOperator,
				ReviewedAtUTC:  nowUTC.Format(time.RFC3339Nano),
				Reason:         "explicit_profile_fact_write",
				OperationID:    "remember_" + inspectionID,
			},
			Lineage: continuityInspectionLineage{
				Status:                 continuityLineageStatusEligible,
				ChangedAtUTC:           nowUTC.Format(time.RFC3339Nano),
				Reason:                 "explicit_profile_fact_write",
				OperationID:            "remember_" + inspectionID,
				SupersedesInspectionID: strings.TrimSpace(existingFact.InspectionID),
			},
			EventCount:            1,
			ApproxPayloadBytes:    len([]byte(validatedRequest.FactValue)),
			ApproxPromptTokens:    approximateLoopgateTokenCount(validatedRequest.FactValue),
			DerivedDistillateIDs:  []string{distillateID},
			DerivedResonateKeyIDs: []string{resonateKeyID},
		}
		distillateRecord := continuityDistillateRecord{
			SchemaVersion:        continuityMemorySchemaVersion,
			DerivationVersion:    continuityDerivationVersion,
			DistillateID:         distillateID,
			InspectionID:         inspectionID,
			ThreadID:             threadID,
			Scope:                validatedRequest.Scope,
			GoalType:             goalTypePreferenceOrConfigUpdate,
			GoalFamilyID:         goalTypePreferenceOrConfigUpdate + ":preference_change",
			NormalizationVersion: continuityNormalizationVersion,
			UserImportance:       userImportance,
			RetentionScore:       retentionScore,
			EffectiveHotness:     effectiveHotness,
			MemoryState:          deriveMemoryState(effectiveHotness, continuityLineageStatusEligible),
			DerivationSignature: makeEventPayloadID("remember_signature", struct {
				FactKey   string `json:"fact_key"`
				FactValue string `json:"fact_value"`
			}{
				FactKey:   validatedRequest.FactKey,
				FactValue: validatedRequest.FactValue,
			}),
			CreatedAtUTC: nowUTC.Format(time.RFC3339Nano),
			SourceRefs:   []continuityArtifactSourceRef{sourceRef},
			Tags:         normalizeLoopgateMemoryTags([]string{validatedRequest.FactKey, validatedRequest.FactValue}),
			Facts: []continuityDistillateFact{{
				Name:               validatedRequest.FactKey,
				Value:              validatedRequest.FactValue,
				SourceRef:          sourceRef.Kind + ":" + sourceRef.Ref,
				EpistemicFlavor:    "remembered",
				CertaintyScore:     certaintyScoreForEpistemicFlavor("remembered"),
				SemanticProjection: cloneSemanticProjection(&tclAnalysis.Projection),
			}},
		}
		resonateKeyRecord := continuityResonateKeyRecord{
			SchemaVersion:     continuityMemorySchemaVersion,
			DerivationVersion: continuityDerivationVersion,
			KeyID:             resonateKeyID,
			DistillateID:      distillateID,
			ThreadID:          threadID,
			Scope:             validatedRequest.Scope,
			GoalType:          distillateRecord.GoalType,
			GoalFamilyID:      distillateRecord.GoalFamilyID,
			RetentionScore:    distillateRecord.RetentionScore,
			EffectiveHotness:  distillateRecord.EffectiveHotness,
			MemoryState:       distillateRecord.MemoryState,
			CreatedAtUTC:      nowUTC.Format(time.RFC3339Nano),
			Tags:              append([]string(nil), distillateRecord.Tags...),
		}

		workingState.Inspections[inspectionID] = inspectionRecord
		workingState.Distillates[distillateID] = distillateRecord
		workingState.ResonateKeys[resonateKeyID] = resonateKeyRecord

		rememberResponse = MemoryRememberResponse{
			Scope:           validatedRequest.Scope,
			FactKey:         validatedRequest.FactKey,
			FactValue:       validatedRequest.FactValue,
			InspectionID:    inspectionID,
			DistillateID:    distillateID,
			ResonateKeyID:   resonateKeyID,
			RememberedAtUTC: nowUTC.Format(time.RFC3339Nano),
			UpdatedExisting: foundExisting,
		}
		if foundExisting {
			rememberResponse.SupersededFactValue = existingFact.FactValue
		}

		mutationEvents := continuityMutationEvents{
			Continuity: []continuityAuthoritativeEvent{{
				SchemaVersion: continuityMemorySchemaVersion,
				EventID:       "memory_fact_" + inspectionID,
				EventType:     "memory_fact_remembered",
				CreatedAtUTC:  nowUTC.Format(time.RFC3339Nano),
				Actor:         tokenClaims.ControlSessionID,
				Scope:         validatedRequest.Scope,
				InspectionID:  inspectionID,
				ThreadID:      threadID,
				GoalType:      distillateRecord.GoalType,
				GoalFamilyID:  distillateRecord.GoalFamilyID,
				Inspection:    ptrContinuityInspectionRecord(cloneContinuityInspectionRecord(inspectionRecord)),
				Distillate:    ptrContinuityDistillateRecord(cloneContinuityDistillateRecord(distillateRecord)),
				ResonateKey:   ptrContinuityResonateKeyRecord(cloneContinuityResonateKeyRecord(resonateKeyRecord)),
			}},
		}
		if foundExisting {
			existingInspection := workingState.Inspections[existingFact.InspectionID]
			mutationEvents.Continuity = append(mutationEvents.Continuity, continuityAuthoritativeEvent{
				SchemaVersion: continuityMemorySchemaVersion,
				EventID:       "memory_fact_supersede_" + existingFact.InspectionID,
				EventType:     "continuity_inspection_lineage_updated",
				CreatedAtUTC:  nowUTC.Format(time.RFC3339Nano),
				Actor:         tokenClaims.ControlSessionID,
				Scope:         existingInspection.Scope,
				InspectionID:  existingInspection.InspectionID,
				ThreadID:      existingInspection.ThreadID,
				GoalType:      distillateRecord.GoalType,
				GoalFamilyID:  distillateRecord.GoalFamilyID,
				Lineage:       ptrContinuityInspectionLineage(existingInspection.Lineage),
			})
		}
		return workingState, mergeMemoryTCLAuditSummary(map[string]interface{}{
			"fact_key":         validatedRequest.FactKey,
			"inspection_id":    inspectionID,
			"distillate_id":    distillateID,
			"resonate_key_id":  resonateKeyID,
			"updated_existing": foundExisting,
			"scope":            validatedRequest.Scope,
		}, tclAnalysis.AuditSummary), mutationEvents, nil
	})
	if err != nil {
		return MemoryRememberResponse{}, err
	}
	return rememberResponse, nil
}

func (server *Server) recallMemoryFromPartitionState(partition *memoryPartition, validatedRequest MemoryRecallRequest) (MemoryRecallResponse, error) {
	server.memoryMu.Lock()
	defer server.memoryMu.Unlock()

	recalledItems := make([]MemoryRecallItem, 0, len(validatedRequest.RequestedKeys))
	approxTokenCount := 0
	for _, requestedKeyID := range validatedRequest.RequestedKeys {
		resonateKeyRecord, distillateRecord, decision, err := resolveRecallMaterial(partition.state, requestedKeyID)
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
		for _, factRecord := range distillateRecord.Facts {
			recalledFacts = append(recalledFacts, memoryRecallFactFromDistillateFact(factRecord))
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

func (server *Server) recallMemory(tenantID string, rawRequest MemoryRecallRequest) (MemoryRecallResponse, error) {
	validatedRequest := rawRequest
	if validatedRequest.Scope == "" {
		validatedRequest.Scope = memoryScopeGlobal
	}
	if validatedRequest.MaxItems == 0 {
		validatedRequest.MaxItems = 10
	}
	if validatedRequest.MaxTokens == 0 {
		validatedRequest.MaxTokens = 2000
	}
	if err := validatedRequest.Validate(); err != nil {
		return MemoryRecallResponse{}, err
	}
	server.memoryMu.Lock()
	partition, err := server.ensureMemoryPartitionLocked(tenantID)
	if err != nil {
		server.memoryMu.Unlock()
		return MemoryRecallResponse{}, err
	}
	backend := partition.backend
	server.memoryMu.Unlock()
	if backend == nil {
		return MemoryRecallResponse{}, fmt.Errorf("memory backend is not configured")
	}
	return backend.Recall(context.Background(), validatedRequest)
}

type explicitProfileFactRecord struct {
	InspectionID   string
	DistillateID   string
	ResonateKeyID  string
	FactKey        string
	FactValue      string
	AnchorTupleKey string
	CreatedAtUTC   string
}

func (server *Server) normalizeMemoryRememberRequest(rawRequest MemoryRememberRequest) (MemoryRememberRequest, error) {
	validatedRequest := rawRequest
	validatedRequest.Scope = strings.TrimSpace(validatedRequest.Scope)
	if validatedRequest.Scope == "" {
		validatedRequest.Scope = memoryScopeGlobal
	}
	validatedRequest.FactKey = tclpkg.CanonicalizeExplicitMemoryFactKey(validatedRequest.FactKey)
	validatedRequest.FactValue = strings.TrimSpace(validatedRequest.FactValue)
	validatedRequest.Reason = strings.TrimSpace(validatedRequest.Reason)
	validatedRequest.SourceText = strings.TrimSpace(validatedRequest.SourceText)
	validatedRequest.CandidateSource = strings.TrimSpace(validatedRequest.CandidateSource)
	validatedRequest.SourceChannel = strings.TrimSpace(validatedRequest.SourceChannel)
	if err := validatedRequest.Validate(); err != nil {
		return MemoryRememberRequest{}, err
	}
	if validatedRequest.Scope != memoryScopeGlobal {
		return MemoryRememberRequest{}, fmt.Errorf("scope must be %q", memoryScopeGlobal)
	}
	if validatedRequest.FactKey == "" {
		return MemoryRememberRequest{}, fmt.Errorf("fact_key is unsupported")
	}
	if err := identifiers.ValidateSafeIdentifier("fact_key", validatedRequest.FactKey); err != nil {
		return MemoryRememberRequest{}, err
	}
	if len([]byte(validatedRequest.FactValue)) > server.runtimeConfig.Memory.ExplicitFactWrites.MaxValueBytes {
		return MemoryRememberRequest{}, fmt.Errorf("fact_value exceeds max_value_bytes")
	}
	return validatedRequest, nil
}

func (server *Server) consumeMemoryFactWriteBudgetLocked(controlSessionID string, peerUID uint32, nowUTC time.Time) error {
	windowSeconds := server.runtimeConfig.Memory.ExplicitFactWrites.WindowSeconds
	if windowSeconds <= 0 {
		windowSeconds = 60
	}
	windowStartUTC := nowUTC.Add(-time.Duration(windowSeconds) * time.Second)
	sessionWrites := pruneOldMemoryFactWrites(server.memoryFactWritesBySession[controlSessionID], windowStartUTC)
	peerWrites := pruneOldMemoryFactWrites(server.memoryFactWritesByUID[peerUID], windowStartUTC)

	if len(sessionWrites) >= server.runtimeConfig.Memory.ExplicitFactWrites.MaxWritesPerSession {
		server.memoryFactWritesBySession[controlSessionID] = sessionWrites
		server.memoryFactWritesByUID[peerUID] = peerWrites
		return continuityGovernanceError{
			httpStatus:     429,
			responseStatus: ResponseStatusDenied,
			denialCode:     DenialCodeMemoryFactWriteRateLimited,
			reason:         fmt.Sprintf("explicit memory fact write rate limit exceeded for control session; retry after %ds", windowSeconds),
		}
	}
	if len(peerWrites) >= server.runtimeConfig.Memory.ExplicitFactWrites.MaxWritesPerPeerUID {
		server.memoryFactWritesBySession[controlSessionID] = sessionWrites
		server.memoryFactWritesByUID[peerUID] = peerWrites
		return continuityGovernanceError{
			httpStatus:     429,
			responseStatus: ResponseStatusDenied,
			denialCode:     DenialCodeMemoryFactWriteRateLimited,
			reason:         fmt.Sprintf("explicit memory fact write rate limit exceeded for local peer identity; retry after %ds", windowSeconds),
		}
	}

	sessionWrites = append(sessionWrites, nowUTC)
	peerWrites = append(peerWrites, nowUTC)
	server.memoryFactWritesBySession[controlSessionID] = sessionWrites
	server.memoryFactWritesByUID[peerUID] = peerWrites
	return nil
}

func pruneOldMemoryFactWrites(writeTimes []time.Time, windowStartUTC time.Time) []time.Time {
	if len(writeTimes) == 0 {
		return nil
	}
	filteredWriteTimes := writeTimes[:0]
	for _, writeTime := range writeTimes {
		if writeTime.Before(windowStartUTC) {
			continue
		}
		filteredWriteTimes = append(filteredWriteTimes, writeTime)
	}
	return filteredWriteTimes
}

func activeExplicitProfileFactByAnchorTuple(currentState continuityMemoryState, anchorVersion string, anchorKey string) (explicitProfileFactRecord, bool) {
	wantedAnchorTupleKey := anchorTupleKey(anchorVersion, anchorKey)
	if wantedAnchorTupleKey == "" {
		return explicitProfileFactRecord{}, false
	}
	for _, distillateRecord := range activeLoopgateDistillates(currentState) {
		explicitProfileFact, found := explicitProfileFactFromDistillate(currentState, distillateRecord)
		if found && explicitProfileFact.AnchorTupleKey == wantedAnchorTupleKey {
			return explicitProfileFact, true
		}
	}
	return explicitProfileFactRecord{}, false
}

func explicitProfileFactFromDistillate(currentState continuityMemoryState, distillateRecord continuityDistillateRecord) (explicitProfileFactRecord, bool) {
	if !isExplicitProfileFactDistillate(distillateRecord) || len(distillateRecord.Facts) != 1 {
		return explicitProfileFactRecord{}, false
	}
	var resonateKeyID string
	for _, resonateKeyRecord := range currentState.ResonateKeys {
		if resonateKeyRecord.DistillateID == distillateRecord.DistillateID {
			resonateKeyID = resonateKeyRecord.KeyID
			break
		}
	}
	factRecord := distillateRecord.Facts[0]
	factValue, isString := factRecord.Value.(string)
	if !isString {
		return explicitProfileFactRecord{}, false
	}
	anchorVersion, anchorKey := continuityFactAnchorTuple(factRecord)
	return explicitProfileFactRecord{
		InspectionID:   distillateRecord.InspectionID,
		DistillateID:   distillateRecord.DistillateID,
		ResonateKeyID:  resonateKeyID,
		FactKey:        factRecord.Name,
		FactValue:      factValue,
		AnchorTupleKey: anchorTupleKey(anchorVersion, anchorKey),
		CreatedAtUTC:   distillateRecord.CreatedAtUTC,
	}, true
}

func isExplicitProfileFactDistillate(distillateRecord continuityDistillateRecord) bool {
	for _, sourceRef := range distillateRecord.SourceRefs {
		if sourceRef.Kind == explicitProfileFactSourceKind {
			return true
		}
	}
	return false
}

func loopgateRecallOpenItems(distillateRecord continuityDistillateRecord) ([]string, []MemoryWakeStateOpenItem) {
	activeGoalsByID := map[string]string{}
	activeGoalOrder := make([]string, 0, len(distillateRecord.GoalOps))
	for _, goalOp := range distillateRecord.GoalOps {
		switch goalOp.Action {
		case "opened":
			activeGoalsByID[goalOp.GoalID] = goalOp.Text
			activeGoalOrder = appendWithoutDuplicate(activeGoalOrder, goalOp.GoalID)
		case "closed":
			delete(activeGoalsByID, goalOp.GoalID)
			activeGoalOrder = removeStringValue(activeGoalOrder, goalOp.GoalID)
		}
	}
	activeGoals := make([]string, 0, len(activeGoalOrder))
	for _, goalID := range activeGoalOrder {
		if goalText, found := activeGoalsByID[goalID]; found {
			activeGoals = append(activeGoals, goalText)
		}
	}

	unresolvedItemsByID := map[string]MemoryWakeStateOpenItem{}
	unresolvedItemOrder := make([]string, 0, len(distillateRecord.UnresolvedItemOps))
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
	unresolvedItems := make([]MemoryWakeStateOpenItem, 0, len(unresolvedItemOrder))
	for _, itemID := range unresolvedItemOrder {
		if unresolvedItem, found := unresolvedItemsByID[itemID]; found {
			unresolvedItems = append(unresolvedItems, unresolvedItem)
		}
	}
	return activeGoals, unresolvedItems
}

func activeLoopgateDistillates(currentState continuityMemoryState) []continuityDistillateRecord {
	distillates := make([]continuityDistillateRecord, 0, len(currentState.Distillates))
	for _, distillateRecord := range currentState.Distillates {
		decision := inspectionLineageSelectionDecision(currentState, distillateRecord.InspectionID)
		if !decision.Allowed {
			continue
		}
		distillates = append(distillates, cloneContinuityDistillateRecord(distillateRecord))
	}
	sort.Slice(distillates, func(leftIndex int, rightIndex int) bool {
		if distillates[leftIndex].CreatedAtUTC != distillates[rightIndex].CreatedAtUTC {
			return distillates[leftIndex].CreatedAtUTC < distillates[rightIndex].CreatedAtUTC
		}
		return distillates[leftIndex].DistillateID < distillates[rightIndex].DistillateID
	})
	return distillates
}

func activeLoopgateResonateKeys(currentState continuityMemoryState) []continuityResonateKeyRecord {
	resonateKeys := make([]continuityResonateKeyRecord, 0, len(currentState.ResonateKeys))
	for _, resonateKeyRecord := range currentState.ResonateKeys {
		_, _, decision, err := resolveRecallMaterial(currentState, resonateKeyRecord.KeyID)
		if err != nil || !decision.Allowed {
			continue
		}
		resonateKeys = append(resonateKeys, cloneContinuityResonateKeyRecord(resonateKeyRecord))
	}
	sort.Slice(resonateKeys, func(leftIndex int, rightIndex int) bool {
		if resonateKeys[leftIndex].CreatedAtUTC != resonateKeys[rightIndex].CreatedAtUTC {
			return resonateKeys[leftIndex].CreatedAtUTC < resonateKeys[rightIndex].CreatedAtUTC
		}
		return resonateKeys[leftIndex].KeyID < resonateKeys[rightIndex].KeyID
	})
	return resonateKeys
}

func resolveRecallMaterial(currentState continuityMemoryState, requestedKeyID string) (continuityResonateKeyRecord, continuityDistillateRecord, continuityEligibilityDecision, error) {
	resonateKeyRecord, found := currentState.ResonateKeys[requestedKeyID]
	if !found {
		return continuityResonateKeyRecord{}, continuityDistillateRecord{}, continuityEligibilityDecision{}, continuityGovernanceError{
			httpStatus:     404,
			responseStatus: ResponseStatusDenied,
			denialCode:     DenialCodeContinuityInspectionNotFound,
			reason:         fmt.Sprintf("resonate key %q not found", requestedKeyID),
		}
	}
	distillateRecord, found := currentState.Distillates[resonateKeyRecord.DistillateID]
	if !found {
		return continuityResonateKeyRecord{}, continuityDistillateRecord{}, continuityEligibilityDecision{}, continuityGovernanceError{
			httpStatus:     404,
			responseStatus: ResponseStatusDenied,
			denialCode:     DenialCodeContinuityInspectionNotFound,
			reason:         fmt.Sprintf("distillate for key %q not found", requestedKeyID),
		}
	}
	decision := inspectionLineageSelectionDecision(currentState, distillateRecord.InspectionID)
	if !decision.Allowed {
		return continuityResonateKeyRecord{}, continuityDistillateRecord{}, decision, continuityGovernanceError{
			httpStatus:     403,
			responseStatus: ResponseStatusDenied,
			denialCode:     decision.DenialCode,
			reason:         fmt.Sprintf("resonate key %q is not eligible", requestedKeyID),
		}
	}
	return cloneContinuityResonateKeyRecord(resonateKeyRecord), cloneContinuityDistillateRecord(distillateRecord), decision, nil
}

func inspectionLineageSelectionDecision(currentState continuityMemoryState, inspectionID string) continuityEligibilityDecision {
	inspectionRecord, found := currentState.Inspections[inspectionID]
	if !found {
		return continuityEligibilityDecision{
			Allowed:    false,
			DenialCode: DenialCodeContinuityInspectionNotFound,
		}
	}
	inspectionRecord = normalizeContinuityInspectionRecordMust(inspectionRecord)
	if inspectionRecord.DerivationOutcome != continuityInspectionOutcomeDerived {
		return continuityEligibilityDecision{
			Allowed:           false,
			DenialCode:        DenialCodeContinuityLineageIneligible,
			ReviewStatus:      inspectionRecord.Review.Status,
			LineageStatus:     inspectionRecord.Lineage.Status,
			DerivationOutcome: inspectionRecord.DerivationOutcome,
		}
	}
	if inspectionRecord.Review.Status != continuityReviewStatusAccepted {
		return continuityEligibilityDecision{
			Allowed:           false,
			DenialCode:        DenialCodeContinuityLineageIneligible,
			ReviewStatus:      inspectionRecord.Review.Status,
			LineageStatus:     inspectionRecord.Lineage.Status,
			DerivationOutcome: inspectionRecord.DerivationOutcome,
		}
	}
	if inspectionRecord.Lineage.Status != continuityLineageStatusEligible {
		return continuityEligibilityDecision{
			Allowed:           false,
			DenialCode:        DenialCodeContinuityLineageIneligible,
			ReviewStatus:      inspectionRecord.Review.Status,
			LineageStatus:     inspectionRecord.Lineage.Status,
			DerivationOutcome: inspectionRecord.DerivationOutcome,
		}
	}
	return continuityEligibilityDecision{
		Allowed:           true,
		ReviewStatus:      inspectionRecord.Review.Status,
		LineageStatus:     inspectionRecord.Lineage.Status,
		DerivationOutcome: inspectionRecord.DerivationOutcome,
	}
}

func (server *Server) inspectionLineageSelectionDecisionLocked(currentState continuityMemoryState, inspectionID string) continuityEligibilityDecision {
	return inspectionLineageSelectionDecision(currentState, inspectionID)
}

func buildMemoryInspectionGovernanceResponse(inspectionRecord continuityInspectionRecord) MemoryInspectionGovernanceResponse {
	return MemoryInspectionGovernanceResponse{
		InspectionID:          inspectionRecord.InspectionID,
		ThreadID:              inspectionRecord.ThreadID,
		DerivationOutcome:     inspectionRecord.DerivationOutcome,
		ReviewStatus:          inspectionRecord.Review.Status,
		LineageStatus:         inspectionRecord.Lineage.Status,
		DerivedDistillateIDs:  append([]string(nil), inspectionRecord.DerivedDistillateIDs...),
		DerivedResonateKeyIDs: append([]string(nil), inspectionRecord.DerivedResonateKeyIDs...),
	}
}

func (server *Server) reviewContinuityInspection(tokenClaims capabilityToken, inspectionID string, rawRequest MemoryInspectionReviewRequest) (MemoryInspectionGovernanceResponse, error) {
	validatedRequest := rawRequest
	if err := validatedRequest.Validate(); err != nil {
		return MemoryInspectionGovernanceResponse{}, err
	}

	var governanceResponse MemoryInspectionGovernanceResponse
	err := server.mutateContinuityMemory(tokenClaims.TenantID, tokenClaims.ControlSessionID, "memory.continuity.reviewed", func(workingState continuityMemoryState, nowUTC time.Time) (continuityMemoryState, map[string]interface{}, continuityMutationEvents, error) {
		inspectionRecord, found := workingState.Inspections[inspectionID]
		if !found {
			return workingState, nil, continuityMutationEvents{}, continuityGovernanceError{
				httpStatus:     404,
				responseStatus: ResponseStatusDenied,
				denialCode:     DenialCodeContinuityInspectionNotFound,
				reason:         "continuity inspection not found",
			}
		}
		inspectionRecord = normalizeContinuityInspectionRecordMust(inspectionRecord)
		targetStatus := strings.TrimSpace(validatedRequest.Decision)
		if inspectionRecord.Review.Status == targetStatus {
			governanceResponse = buildMemoryInspectionGovernanceResponse(inspectionRecord)
			return workingState, nil, continuityMutationEvents{}, nil
		}
		if inspectionRecord.Review.Status != continuityReviewStatusPendingReview {
			return workingState, nil, continuityMutationEvents{}, continuityGovernanceError{
				httpStatus:     409,
				responseStatus: ResponseStatusDenied,
				denialCode:     DenialCodeContinuityGovernanceStateConflict,
				reason:         "continuity review is already terminal",
			}
		}
		inspectionRecord.Review = continuityInspectionReview{
			Status:         targetStatus,
			DecisionSource: continuityReviewDecisionSourceOperator,
			ReviewedAtUTC:  nowUTC.Format(time.RFC3339Nano),
			Reason:         secrets.RedactText(strings.TrimSpace(validatedRequest.Reason)),
			OperationID:    validatedRequest.OperationID,
		}
		inspectionRecord.Outcome = inspectionRecord.DerivationOutcome
		workingState.Inspections[inspectionID] = inspectionRecord
		governanceResponse = buildMemoryInspectionGovernanceResponse(inspectionRecord)
		mutationEvents := continuityMutationEvents{
			Continuity: []continuityAuthoritativeEvent{{
				SchemaVersion: continuityMemorySchemaVersion,
				EventID:       "continuity_review_" + validatedRequest.OperationID,
				EventType:     "continuity_inspection_reviewed",
				CreatedAtUTC:  nowUTC.Format(time.RFC3339Nano),
				Actor:         tokenClaims.ControlSessionID,
				Scope:         inspectionRecord.Scope,
				InspectionID:  inspectionRecord.InspectionID,
				ThreadID:      inspectionRecord.ThreadID,
				Review:        ptrContinuityInspectionReview(inspectionRecord.Review),
			}},
			Profile: []continuityProfileEvent{{
				SchemaVersion: continuityMemorySchemaVersion,
				EventID:       "profile_review_" + validatedRequest.OperationID,
				EventType:     "continuity_review_recorded",
				CreatedAtUTC:  nowUTC.Format(time.RFC3339Nano),
				Actor:         tokenClaims.ControlSessionID,
				InspectionID:  inspectionRecord.InspectionID,
				ThreadID:      inspectionRecord.ThreadID,
				GoalType:      firstDerivedGoalType(workingState, inspectionRecord),
				GoalFamilyID:  firstDerivedGoalFamilyID(workingState, inspectionRecord),
				Review:        ptrContinuityInspectionReview(inspectionRecord.Review),
			}},
		}
		return workingState, map[string]interface{}{
			"inspection_id":      inspectionRecord.InspectionID,
			"thread_id":          inspectionRecord.ThreadID,
			"review_status":      inspectionRecord.Review.Status,
			"lineage_status":     inspectionRecord.Lineage.Status,
			"derivation_outcome": inspectionRecord.DerivationOutcome,
			"operation_id":       validatedRequest.OperationID,
			"reason":             secrets.RedactText(strings.TrimSpace(validatedRequest.Reason)),
		}, mutationEvents, nil
	})
	if err != nil {
		return MemoryInspectionGovernanceResponse{}, err
	}
	return governanceResponse, nil
}

func (server *Server) tombstoneContinuityInspection(tokenClaims capabilityToken, inspectionID string, rawRequest MemoryInspectionLineageRequest) (MemoryInspectionGovernanceResponse, error) {
	return server.updateContinuityLineageStatus(tokenClaims, inspectionID, continuityLineageStatusTombstoned, rawRequest, "memory.continuity.tombstoned")
}

func (server *Server) purgeContinuityInspection(tokenClaims capabilityToken, inspectionID string, rawRequest MemoryInspectionLineageRequest) (MemoryInspectionGovernanceResponse, error) {
	return server.updateContinuityLineageStatus(tokenClaims, inspectionID, continuityLineageStatusPurged, rawRequest, "memory.continuity.purged")
}

func continuitySupersessionRetentionActive(currentLineage continuityInspectionLineage, nowUTC time.Time) bool {
	if strings.TrimSpace(currentLineage.SupersededByInspectionID) == "" {
		return false
	}
	changedAtUTC := strings.TrimSpace(currentLineage.ChangedAtUTC)
	if changedAtUTC == "" {
		return true
	}
	supersededAtUTC, err := time.Parse(time.RFC3339Nano, changedAtUTC)
	if err != nil {
		return true
	}
	return nowUTC.Before(supersededAtUTC.Add(config.DefaultSupersededLineageRetentionWindow))
}

func (server *Server) updateContinuityLineageStatus(tokenClaims capabilityToken, inspectionID string, targetStatus string, rawRequest MemoryInspectionLineageRequest, auditEventType string) (MemoryInspectionGovernanceResponse, error) {
	validatedRequest := rawRequest
	if err := validatedRequest.Validate(); err != nil {
		return MemoryInspectionGovernanceResponse{}, err
	}

	var governanceResponse MemoryInspectionGovernanceResponse
	err := server.mutateContinuityMemory(tokenClaims.TenantID, tokenClaims.ControlSessionID, auditEventType, func(workingState continuityMemoryState, nowUTC time.Time) (continuityMemoryState, map[string]interface{}, continuityMutationEvents, error) {
		inspectionRecord, found := workingState.Inspections[inspectionID]
		if !found {
			return workingState, nil, continuityMutationEvents{}, continuityGovernanceError{
				httpStatus:     404,
				responseStatus: ResponseStatusDenied,
				denialCode:     DenialCodeContinuityInspectionNotFound,
				reason:         "continuity inspection not found",
			}
		}
		inspectionRecord = normalizeContinuityInspectionRecordMust(inspectionRecord)
		if inspectionRecord.Lineage.Status == targetStatus {
			governanceResponse = buildMemoryInspectionGovernanceResponse(inspectionRecord)
			return workingState, nil, continuityMutationEvents{}, nil
		}
		if inspectionRecord.Lineage.Status == continuityLineageStatusPurged {
			return workingState, nil, continuityMutationEvents{}, continuityGovernanceError{
				httpStatus:     409,
				responseStatus: ResponseStatusDenied,
				denialCode:     DenialCodeContinuityGovernanceStateConflict,
				reason:         "purged continuity lineage is terminal",
			}
		}
		if inspectionRecord.Lineage.Status == continuityLineageStatusTombstoned && targetStatus == continuityLineageStatusEligible {
			return workingState, nil, continuityMutationEvents{}, continuityGovernanceError{
				httpStatus:     409,
				responseStatus: ResponseStatusDenied,
				denialCode:     DenialCodeContinuityGovernanceStateConflict,
				reason:         "tombstoned continuity lineage cannot become eligible",
			}
		}
		if targetStatus == continuityLineageStatusPurged && inspectionRecord.Lineage.Status == continuityLineageStatusTombstoned && continuitySupersessionRetentionActive(inspectionRecord.Lineage, nowUTC) {
			return workingState, nil, continuityMutationEvents{}, continuityGovernanceError{
				httpStatus:     409,
				responseStatus: ResponseStatusDenied,
				denialCode:     DenialCodeContinuityRetentionWindowActive,
				reason:         "superseded continuity lineage remains under retention and cannot be purged yet",
			}
		}
		updatedLineage := inspectionRecord.Lineage
		updatedLineage.Status = targetStatus
		updatedLineage.ChangedAtUTC = nowUTC.Format(time.RFC3339Nano)
		updatedLineage.Reason = secrets.RedactText(strings.TrimSpace(validatedRequest.Reason))
		updatedLineage.OperationID = validatedRequest.OperationID
		inspectionRecord.Lineage = updatedLineage
		inspectionRecord.Outcome = inspectionRecord.DerivationOutcome
		stampContinuityDerivedArtifactsExcluded(&workingState, inspectionRecord, nowUTC)
		workingState.Inspections[inspectionID] = inspectionRecord
		governanceResponse = buildMemoryInspectionGovernanceResponse(inspectionRecord)
		mutationEvents := continuityMutationEvents{
			Continuity: []continuityAuthoritativeEvent{{
				SchemaVersion: continuityMemorySchemaVersion,
				EventID:       "continuity_lineage_" + validatedRequest.OperationID,
				EventType:     "continuity_inspection_lineage_updated",
				CreatedAtUTC:  nowUTC.Format(time.RFC3339Nano),
				Actor:         tokenClaims.ControlSessionID,
				Scope:         inspectionRecord.Scope,
				InspectionID:  inspectionRecord.InspectionID,
				ThreadID:      inspectionRecord.ThreadID,
				Lineage:       ptrContinuityInspectionLineage(inspectionRecord.Lineage),
			}},
			Profile: []continuityProfileEvent{{
				SchemaVersion: continuityMemorySchemaVersion,
				EventID:       "profile_lineage_" + validatedRequest.OperationID,
				EventType:     "continuity_lineage_recorded",
				CreatedAtUTC:  nowUTC.Format(time.RFC3339Nano),
				Actor:         tokenClaims.ControlSessionID,
				InspectionID:  inspectionRecord.InspectionID,
				ThreadID:      inspectionRecord.ThreadID,
				GoalType:      firstDerivedGoalType(workingState, inspectionRecord),
				GoalFamilyID:  firstDerivedGoalFamilyID(workingState, inspectionRecord),
				Lineage:       ptrContinuityInspectionLineage(inspectionRecord.Lineage),
			}},
		}
		return workingState, map[string]interface{}{
			"inspection_id":      inspectionRecord.InspectionID,
			"thread_id":          inspectionRecord.ThreadID,
			"review_status":      inspectionRecord.Review.Status,
			"lineage_status":     inspectionRecord.Lineage.Status,
			"derivation_outcome": inspectionRecord.DerivationOutcome,
			"operation_id":       validatedRequest.OperationID,
			"reason":             secrets.RedactText(strings.TrimSpace(validatedRequest.Reason)),
		}, mutationEvents, nil
	})
	if err != nil {
		return MemoryInspectionGovernanceResponse{}, err
	}
	return governanceResponse, nil
}

func stampContinuityDerivedArtifactsExcluded(workingState *continuityMemoryState, inspectionRecord continuityInspectionRecord, changedAt time.Time) {
	stampedAtUTC := changedAt.UTC().Format(time.RFC3339Nano)
	for _, distillateID := range inspectionRecord.DerivedDistillateIDs {
		distillateRecord, found := workingState.Distillates[distillateID]
		if !found || strings.TrimSpace(distillateRecord.TombstonedAtUTC) != "" {
			continue
		}
		distillateRecord.TombstonedAtUTC = stampedAtUTC
		workingState.Distillates[distillateID] = distillateRecord
	}
	for _, keyID := range inspectionRecord.DerivedResonateKeyIDs {
		resonateKeyRecord, found := workingState.ResonateKeys[keyID]
		if !found || strings.TrimSpace(resonateKeyRecord.TombstonedAtUTC) != "" {
			continue
		}
		resonateKeyRecord.TombstonedAtUTC = stampedAtUTC
		workingState.ResonateKeys[keyID] = resonateKeyRecord
	}
}

func continuityMemoryStatesEqual(leftState continuityMemoryState, rightState continuityMemoryState) bool {
	leftBytes, leftErr := json.Marshal(leftState)
	rightBytes, rightErr := json.Marshal(rightState)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return bytes.Equal(leftBytes, rightBytes)
}

func normalizeContinuityInspectionRecord(inspectionRecord continuityInspectionRecord) (continuityInspectionRecord, error) {
	normalizedRecord := cloneContinuityInspectionRecord(inspectionRecord)
	if strings.TrimSpace(normalizedRecord.DerivationOutcome) == "" {
		normalizedRecord.DerivationOutcome = strings.TrimSpace(normalizedRecord.Outcome)
	}
	if strings.TrimSpace(normalizedRecord.DerivationOutcome) == "" {
		normalizedRecord.DerivationOutcome = continuityInspectionOutcomeNoArtifacts
	}
	if strings.TrimSpace(normalizedRecord.Review.Status) == "" {
		normalizedRecord.Review.Status = continuityReviewStatusAccepted
	}
	if strings.TrimSpace(normalizedRecord.Lineage.Status) == "" {
		normalizedRecord.Lineage.Status = continuityLineageStatusEligible
	}
	normalizedRecord.Outcome = normalizedRecord.DerivationOutcome
	if err := validateContinuityInspectionRecord(normalizedRecord); err != nil {
		return continuityInspectionRecord{}, err
	}
	return normalizedRecord, nil
}

func normalizeContinuityInspectionRecordMust(inspectionRecord continuityInspectionRecord) continuityInspectionRecord {
	normalizedRecord, err := normalizeContinuityInspectionRecord(inspectionRecord)
	if err != nil {
		return inspectionRecord
	}
	return normalizedRecord
}

func validateContinuityInspectionRecord(inspectionRecord continuityInspectionRecord) error {
	switch inspectionRecord.DerivationOutcome {
	case continuityInspectionOutcomeSkippedThreshold, continuityInspectionOutcomeNoArtifacts, continuityInspectionOutcomeDerived:
	default:
		return fmt.Errorf("invalid derivation_outcome %q", inspectionRecord.DerivationOutcome)
	}
	switch inspectionRecord.Review.Status {
	case continuityReviewStatusPendingReview, continuityReviewStatusAccepted, continuityReviewStatusRejected:
	default:
		return fmt.Errorf("invalid review status %q", inspectionRecord.Review.Status)
	}
	switch inspectionRecord.Lineage.Status {
	case continuityLineageStatusEligible, continuityLineageStatusTombstoned, continuityLineageStatusPurged:
	default:
		return fmt.Errorf("invalid lineage status %q", inspectionRecord.Lineage.Status)
	}
	if strings.TrimSpace(inspectionRecord.Review.ReviewedAtUTC) != "" {
		if _, err := time.Parse(time.RFC3339Nano, inspectionRecord.Review.ReviewedAtUTC); err != nil {
			return fmt.Errorf("reviewed_at_utc is invalid: %w", err)
		}
	}
	if strings.TrimSpace(inspectionRecord.Lineage.ChangedAtUTC) != "" {
		if _, err := time.Parse(time.RFC3339Nano, inspectionRecord.Lineage.ChangedAtUTC); err != nil {
			return fmt.Errorf("lineage changed_at_utc is invalid: %w", err)
		}
	}
	if strings.TrimSpace(inspectionRecord.Review.OperationID) != "" {
		if err := identifiers.ValidateSafeIdentifier("review operation_id", inspectionRecord.Review.OperationID); err != nil {
			return err
		}
	}
	if strings.TrimSpace(inspectionRecord.Lineage.OperationID) != "" {
		if err := identifiers.ValidateSafeIdentifier("lineage operation_id", inspectionRecord.Lineage.OperationID); err != nil {
			return err
		}
	}
	if inspectionRecord.Review.Status == continuityReviewStatusPendingReview && strings.TrimSpace(inspectionRecord.Review.ReviewedAtUTC) != "" {
		return fmt.Errorf("pending_review must not set reviewed_at_utc")
	}
	return nil
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

func trimToLimit(values *[]string, limit int) {
	if len(*values) <= limit {
		return
	}
	*values = append([]string(nil), (*values)[len(*values)-limit:]...)
}

func cloneContinuityInspectionRecord(inspectionRecord continuityInspectionRecord) continuityInspectionRecord {
	inspectionRecord.Outcome = inspectionRecord.DerivationOutcome
	inspectionRecord.DerivedDistillateIDs = append([]string(nil), inspectionRecord.DerivedDistillateIDs...)
	inspectionRecord.DerivedResonateKeyIDs = append([]string(nil), inspectionRecord.DerivedResonateKeyIDs...)
	return inspectionRecord
}

func ptrContinuityInspectionRecord(inspectionRecord continuityInspectionRecord) *continuityInspectionRecord {
	return &inspectionRecord
}

func ptrContinuityDistillateRecord(distillateRecord continuityDistillateRecord) *continuityDistillateRecord {
	return &distillateRecord
}

func ptrContinuityResonateKeyRecord(resonateKeyRecord continuityResonateKeyRecord) *continuityResonateKeyRecord {
	return &resonateKeyRecord
}

func ptrContinuityInspectionReview(review continuityInspectionReview) *continuityInspectionReview {
	return &review
}

func ptrContinuityInspectionLineage(lineage continuityInspectionLineage) *continuityInspectionLineage {
	return &lineage
}

func memoryWakeRecentFactFromDistillateFact(factRecord continuityDistillateFact) MemoryWakeStateRecentFact {
	conflictAnchorVersion, conflictAnchorKey := continuityFactAnchorTuple(factRecord)
	return MemoryWakeStateRecentFact{
		Name:               factRecord.Name,
		Value:              factRecord.Value,
		SourceRef:          factRecord.SourceRef,
		EpistemicFlavor:    factRecord.EpistemicFlavor,
		ConflictKeyVersion: conflictAnchorVersion,
		ConflictKey:        conflictAnchorKey,
		CertaintyScore:     factRecord.CertaintyScore,
	}
}

func memoryRecallFactFromDistillateFact(factRecord continuityDistillateFact) MemoryRecallFact {
	conflictAnchorVersion, conflictAnchorKey := continuityFactAnchorTuple(factRecord)
	return MemoryRecallFact{
		Name:               factRecord.Name,
		Value:              factRecord.Value,
		SourceRef:          factRecord.SourceRef,
		EpistemicFlavor:    factRecord.EpistemicFlavor,
		ConflictKeyVersion: conflictAnchorVersion,
		ConflictKey:        conflictAnchorKey,
		CertaintyScore:     factRecord.CertaintyScore,
	}
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

func canonicalizeContinuityMemoryState(currentState continuityMemoryState) continuityMemoryState {
	canonicalizedState := cloneContinuityMemoryState(currentState)
	for distillateID, distillateRecord := range canonicalizedState.Distillates {
		canonicalizedState.Distillates[distillateID] = canonicalizeContinuityDistillateRecord(distillateRecord)
	}
	return canonicalizedState
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

func cloneContinuityDistillateRecord(distillateRecord continuityDistillateRecord) continuityDistillateRecord {
	distillateRecord.SourceRefs = append([]continuityArtifactSourceRef(nil), distillateRecord.SourceRefs...)
	distillateRecord.Tags = append([]string(nil), distillateRecord.Tags...)
	distillateRecord.Facts = append([]continuityDistillateFact(nil), distillateRecord.Facts...)
	for factIndex := range distillateRecord.Facts {
		distillateRecord.Facts[factIndex] = normalizeContinuityDistillateFactForValidation(distillateRecord.Facts[factIndex])
	}
	distillateRecord.GoalOps = append([]continuityGoalOp(nil), distillateRecord.GoalOps...)
	for goalOpIndex := range distillateRecord.GoalOps {
		distillateRecord.GoalOps[goalOpIndex] = normalizeContinuityGoalOpForValidation(distillateRecord.GoalOps[goalOpIndex])
	}
	distillateRecord.UnresolvedItemOps = append([]continuityUnresolvedItemOp(nil), distillateRecord.UnresolvedItemOps...)
	for itemOpIndex := range distillateRecord.UnresolvedItemOps {
		distillateRecord.UnresolvedItemOps[itemOpIndex] = normalizeContinuityUnresolvedItemOpForValidation(distillateRecord.UnresolvedItemOps[itemOpIndex])
	}
	return distillateRecord
}

func cloneContinuityResonateKeyRecord(resonateKeyRecord continuityResonateKeyRecord) continuityResonateKeyRecord {
	resonateKeyRecord.Tags = append([]string(nil), resonateKeyRecord.Tags...)
	return resonateKeyRecord
}

func cloneMemoryWakeStateResponse(wakeStateResponse MemoryWakeStateResponse) MemoryWakeStateResponse {
	wakeStateResponse.SourceRefs = append([]MemoryWakeStateSourceRef(nil), wakeStateResponse.SourceRefs...)
	wakeStateResponse.ActiveGoals = append([]string(nil), wakeStateResponse.ActiveGoals...)
	wakeStateResponse.UnresolvedItems = append([]MemoryWakeStateOpenItem(nil), wakeStateResponse.UnresolvedItems...)
	wakeStateResponse.RecentFacts = append([]MemoryWakeStateRecentFact(nil), wakeStateResponse.RecentFacts...)
	wakeStateResponse.ResonateKeys = append([]string(nil), wakeStateResponse.ResonateKeys...)
	return wakeStateResponse
}

func memoryDiagnosticWakeResponseFromReport(diagnosticReport continuityDiagnosticWakeReport) MemoryDiagnosticWakeResponse {
	response := MemoryDiagnosticWakeResponse{
		SchemaVersion:     diagnosticReport.SchemaVersion,
		ResolutionVersion: diagnosticReport.ResolutionVersion,
		ReportID:          diagnosticReport.ReportID,
		CreatedAtUTC:      diagnosticReport.CreatedAtUTC,
		RuntimeWakeID:     diagnosticReport.RuntimeWakeID,
		IncludedCount:     len(diagnosticReport.Entries),
		ExcludedCount:     len(diagnosticReport.ExcludedEntries),
		Entries:           make([]MemoryDiagnosticWakeEntry, 0, len(diagnosticReport.Entries)),
		ExcludedEntries:   make([]MemoryDiagnosticWakeEntry, 0, len(diagnosticReport.ExcludedEntries)),
	}
	for _, reportEntry := range diagnosticReport.Entries {
		response.Entries = append(response.Entries, memoryDiagnosticWakeEntryFromContinuity(reportEntry))
	}
	for _, reportEntry := range diagnosticReport.ExcludedEntries {
		response.ExcludedEntries = append(response.ExcludedEntries, memoryDiagnosticWakeEntryFromContinuity(reportEntry))
	}
	return response
}

func memoryDiagnosticWakeEntryFromContinuity(reportEntry continuityDiagnosticWakeEntry) MemoryDiagnosticWakeEntry {
	return MemoryDiagnosticWakeEntry{
		ItemKind:         reportEntry.ItemKind,
		GoalFamilyID:     reportEntry.GoalFamilyID,
		Scope:            reportEntry.Scope,
		RetentionScore:   reportEntry.RetentionScore,
		EffectiveHotness: reportEntry.EffectiveHotness,
		Reason:           reportEntry.Reason,
		TrimReason:       reportEntry.TrimReason,
		PrecedenceSource: reportEntry.PrecedenceSource,
		ScoreTrace:       append([]string(nil), reportEntry.ScoreTrace...),
		RedactedSummary:  reportEntry.RedactedSummary,
	}
}

func cloneMemoryDiagnosticWakeResponse(diagnosticResponse MemoryDiagnosticWakeResponse) MemoryDiagnosticWakeResponse {
	diagnosticResponse.Entries = append([]MemoryDiagnosticWakeEntry(nil), diagnosticResponse.Entries...)
	diagnosticResponse.ExcludedEntries = append([]MemoryDiagnosticWakeEntry(nil), diagnosticResponse.ExcludedEntries...)
	for entryIndex := range diagnosticResponse.Entries {
		diagnosticResponse.Entries[entryIndex].ScoreTrace = append([]string(nil), diagnosticResponse.Entries[entryIndex].ScoreTrace...)
	}
	for entryIndex := range diagnosticResponse.ExcludedEntries {
		diagnosticResponse.ExcludedEntries[entryIndex].ScoreTrace = append([]string(nil), diagnosticResponse.ExcludedEntries[entryIndex].ScoreTrace...)
	}
	return diagnosticResponse
}
