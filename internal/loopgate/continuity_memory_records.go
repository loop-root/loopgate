package loopgate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	tclpkg "loopgate/internal/tcl"
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
