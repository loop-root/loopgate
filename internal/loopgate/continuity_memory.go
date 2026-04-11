package loopgate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
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

type discoverSlotPreferenceRule struct {
	anchorTupleKey string
	requiredTags   []string
}

var discoverSlotPreferenceRules = []discoverSlotPreferenceRule{
	{anchorTupleKey: "v1:usr_profile:identity:fact:name", requiredTags: []string{"name"}},
	{anchorTupleKey: "v1:usr_profile:identity:fact:preferred_name", requiredTags: []string{"preferred", "name"}},
	{anchorTupleKey: "v1:usr_profile:settings:fact:timezone", requiredTags: []string{"timezone"}},
	{anchorTupleKey: "v1:usr_profile:settings:fact:locale", requiredTags: []string{"locale"}},
}

// Discover slot preference stays on a tiny allowlist of stable profile slots because broad
// anchor bias would distort general recall and make retrieval drift harder to detect in review.
func detectDiscoverSlotPreference(rawQuery string) string {
	queryTags := tokenizeLoopgateMemoryText(rawQuery)
	if len(queryTags) == 0 {
		return ""
	}
	queryTagSet := make(map[string]struct{}, len(queryTags))
	for _, queryTag := range queryTags {
		queryTagSet[queryTag] = struct{}{}
	}
	if containsAnyLoopgateMemoryTag(queryTagSet, "project", "task", "goal", "github", "history", "recent", "work", "context") {
		return ""
	}
	if containsAnyLoopgateMemoryTag(queryTagSet, "and", "both") {
		return ""
	}
	if !containsAnyLoopgateMemoryTag(queryTagSet, "user", "profile") {
		return ""
	}

	matchedAnchorTupleKeys := make([]string, 0, 1)
	for _, slotRule := range discoverSlotPreferenceRules {
		if hasAllLoopgateMemoryTags(queryTagSet, slotRule.requiredTags...) {
			matchedAnchorTupleKeys = append(matchedAnchorTupleKeys, slotRule.anchorTupleKey)
		}
	}
	if len(matchedAnchorTupleKeys) == 1 {
		if matchedAnchorTupleKeys[0] == "v1:usr_profile:identity:fact:name" &&
			containsAnyLoopgateMemoryTag(queryTagSet, "user", "profile", "identity") &&
			!containsAnyLoopgateMemoryTag(queryTagSet, "legal", "formal", "given", "full") {
			// In operator-facing profile queries, "name" normally means the display/current
			// identity slot, not a stricter legal-name field. Prefer `preferred_name` when the
			// query stays generic, then let exact-slot lookup fall back to `name` if needed.
			return "v1:usr_profile:identity:fact:preferred_name"
		}
		return matchedAnchorTupleKeys[0]
	}
	if len(matchedAnchorTupleKeys) == 2 &&
		containsStringValue(matchedAnchorTupleKeys, "v1:usr_profile:identity:fact:name") &&
		containsStringValue(matchedAnchorTupleKeys, "v1:usr_profile:identity:fact:preferred_name") {
		return "v1:usr_profile:identity:fact:preferred_name"
	}
	return ""
}

func containsAnyLoopgateMemoryTag(queryTagSet map[string]struct{}, wantedTags ...string) bool {
	for _, wantedTag := range wantedTags {
		if _, found := queryTagSet[wantedTag]; found {
			return true
		}
	}
	return false
}

func hasAllLoopgateMemoryTags(queryTagSet map[string]struct{}, wantedTags ...string) bool {
	for _, wantedTag := range wantedTags {
		if _, found := queryTagSet[wantedTag]; !found {
			return false
		}
	}
	return true
}

func containsStringValue(values []string, wantedValue string) bool {
	for _, value := range values {
		if value == wantedValue {
			return true
		}
	}
	return false
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

func actualObservedContinuityPayloadBytes(events []continuityObservedEventRecord) int {
	totalBytes := 0
	for _, continuityEvent := range events {
		payloadBytes, _ := json.Marshal(continuityEvent)
		totalBytes += len(payloadBytes)
	}
	return totalBytes
}

func actualObservedContinuityPromptTokens(events []continuityObservedEventRecord) int {
	tokenCount := 0
	for _, continuityEvent := range events {
		payloadBytes, _ := json.Marshal(continuityEvent)
		tokenCount += approximateLoopgateTokenCount(string(payloadBytes))
	}
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

func ptrContinuityObservedPacket(observedPacket continuityObservedPacket) *continuityObservedPacket {
	return &observedPacket
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

func cloneContinuityObservedPacket(observedPacket continuityObservedPacket) continuityObservedPacket {
	observedPacket.Tags = append([]string(nil), observedPacket.Tags...)
	observedPacket.Events = append([]continuityObservedEventRecord(nil), observedPacket.Events...)
	for eventIndex := range observedPacket.Events {
		observedPacket.Events[eventIndex] = cloneContinuityObservedEventRecord(observedPacket.Events[eventIndex])
	}
	return observedPacket
}

func cloneContinuityObservedEventRecord(observedEvent continuityObservedEventRecord) continuityObservedEventRecord {
	observedEvent.SourceRefs = append([]continuityArtifactSourceRef(nil), observedEvent.SourceRefs...)
	if observedEvent.Payload != nil {
		clonedPayload := *observedEvent.Payload
		clonedPayload.Facts = append([]continuityObservedFactRecord(nil), observedEvent.Payload.Facts...)
		observedEvent.Payload = &clonedPayload
	}
	return observedEvent
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
