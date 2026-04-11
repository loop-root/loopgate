package loopgate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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

func normalizeContinuityInspectRequest(rawRequest ContinuityInspectRequest) (ContinuityInspectRequest, error) {
	validatedRequest := rawRequest
	validatedRequest.Scope = strings.TrimSpace(validatedRequest.Scope)
	if validatedRequest.Scope == "" {
		validatedRequest.Scope = memoryScopeGlobal
	}
	validatedRequest.Tags = normalizeLoopgateMemoryTags(validatedRequest.Tags)
	for eventIndex, continuityEvent := range validatedRequest.Events {
		validatedRequest.Events[eventIndex].SessionID = strings.TrimSpace(continuityEvent.SessionID)
		validatedRequest.Events[eventIndex].ThreadID = strings.TrimSpace(continuityEvent.ThreadID)
		validatedRequest.Events[eventIndex].Scope = strings.TrimSpace(continuityEvent.Scope)
		validatedRequest.Events[eventIndex].Type = strings.TrimSpace(continuityEvent.Type)
		validatedRequest.Events[eventIndex].EpistemicFlavor = strings.TrimSpace(continuityEvent.EpistemicFlavor)
		validatedRequest.Events[eventIndex].EventHash = strings.TrimSpace(continuityEvent.EventHash)
		// Legacy synthetic continuity input is still used by tests and replay
		// compatibility. Caller-supplied source_refs are not authoritative
		// provenance, so drop them before the packet crosses into backend-owned
		// observed continuity state.
		validatedRequest.Events[eventIndex].SourceRefs = nil
	}
	if err := validatedRequest.Validate(); err != nil {
		return ContinuityInspectRequest{}, err
	}
	return validatedRequest, nil
}

func (server *Server) inspectObservedContinuity(tokenClaims capabilityToken, observedRequest ObservedContinuityInspectRequest) (ContinuityInspectResponse, error) {
	backend, err := server.memoryBackendForTenant(tokenClaims.TenantID)
	if err != nil {
		return ContinuityInspectResponse{}, err
	}
	return backend.InspectObservedContinuity(context.Background(), tokenClaims, observedRequest)
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

func (server *Server) memoryBackendForTenant(rawTenantID string) (MemoryBackend, error) {
	server.memoryMu.Lock()
	defer server.memoryMu.Unlock()

	partition, err := server.ensureMemoryPartitionLocked(rawTenantID)
	if err != nil {
		return nil, err
	}
	if partition.backend == nil {
		return nil, fmt.Errorf("memory backend is not configured")
	}
	return partition.backend, nil
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
	backend, err := server.memoryBackendForTenant(tenantID)
	if err != nil {
		return MemoryDiscoverResponse{}, err
	}
	return backend.Discover(context.Background(), validatedRequest)
}

func (server *Server) rememberMemoryFact(tokenClaims capabilityToken, rawRequest MemoryRememberRequest) (MemoryRememberResponse, error) {
	backend, err := server.memoryBackendForTenant(tokenClaims.TenantID)
	if err != nil {
		return MemoryRememberResponse{}, err
	}
	return backend.RememberFact(context.Background(), tokenClaims, rawRequest)
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
	backend, err := server.memoryBackendForTenant(tenantID)
	if err != nil {
		return MemoryRecallResponse{}, err
	}
	return backend.Recall(context.Background(), validatedRequest)
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
