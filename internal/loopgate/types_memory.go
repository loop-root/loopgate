package loopgate

import (
	"fmt"
	"strings"
	"time"

	"morph/internal/identifiers"
	tclpkg "morph/internal/tcl"
)

type ContinuitySourceRefInput struct {
	Kind   string `json:"kind"`
	Ref    string `json:"ref"`
	SHA256 string `json:"sha256,omitempty"`
}

type ContinuityEventInput struct {
	TimestampUTC    string                     `json:"ts_utc"`
	SessionID       string                     `json:"session_id"`
	Type            string                     `json:"type"`
	Scope           string                     `json:"scope"`
	ThreadID        string                     `json:"thread_id"`
	EpistemicFlavor string                     `json:"epistemic_flavor"`
	LedgerSequence  int64                      `json:"ledger_sequence"`
	EventHash       string                     `json:"event_hash"`
	SourceRefs      []ContinuitySourceRefInput `json:"source_refs,omitempty"`
	Payload         map[string]interface{}     `json:"payload,omitempty"`
}

// ContinuityInspectRequest is the legacy raw continuity proposal shape. Loopgate no longer
// exposes a public route that accepts this request directly; it remains for test migration
// helpers and backward-compatible replay decoding of older continuity JSONL records.
type ContinuityInspectRequest struct {
	InspectionID       string                 `json:"inspection_id"`
	ThreadID           string                 `json:"thread_id"`
	Scope              string                 `json:"scope"`
	SealedAtUTC        string                 `json:"sealed_at_utc"`
	EventCount         int                    `json:"event_count"`
	ApproxPayloadBytes int                    `json:"approx_payload_bytes"`
	ApproxPromptTokens int                    `json:"approx_prompt_tokens"`
	Tags               []string               `json:"tags,omitempty"`
	Events             []ContinuityEventInput `json:"events"`
}

type ContinuityInspectResponse struct {
	InspectionID          string   `json:"inspection_id"`
	ThreadID              string   `json:"thread_id"`
	Outcome               string   `json:"outcome"`
	DerivationOutcome     string   `json:"derivation_outcome,omitempty"`
	ReviewStatus          string   `json:"review_status,omitempty"`
	LineageStatus         string   `json:"lineage_status,omitempty"`
	DerivedDistillateIDs  []string `json:"derived_distillate_ids,omitempty"`
	DerivedResonateKeyIDs []string `json:"derived_resonate_key_ids,omitempty"`
}

const observedContinuityThreadEventSourceKind = "haven_thread_event"

type MemoryInspectionReviewRequest struct {
	Decision    string `json:"decision"`
	OperationID string `json:"operation_id"`
	Reason      string `json:"reason,omitempty"`
}

type MemoryInspectionLineageRequest struct {
	OperationID string `json:"operation_id"`
	Reason      string `json:"reason,omitempty"`
}

type MemoryInspectionGovernanceResponse struct {
	InspectionID          string   `json:"inspection_id"`
	ThreadID              string   `json:"thread_id"`
	DerivationOutcome     string   `json:"derivation_outcome"`
	ReviewStatus          string   `json:"review_status"`
	LineageStatus         string   `json:"lineage_status"`
	DerivedDistillateIDs  []string `json:"derived_distillate_ids,omitempty"`
	DerivedResonateKeyIDs []string `json:"derived_resonate_key_ids,omitempty"`
}

type MemoryWakeStateSourceRef struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

type MemoryWakeStateOpenItem struct {
	ID              string `json:"id"`
	Text            string `json:"text"`
	TaskKind        string `json:"task_kind,omitempty"`
	SourceKind      string `json:"source_kind,omitempty"`
	NextStep        string `json:"next_step,omitempty"`
	ScheduledForUTC string `json:"scheduled_for_utc,omitempty"`
	ExecutionClass  string `json:"execution_class,omitempty"`
	CreatedAtUTC    string `json:"created_at_utc,omitempty"`
	// Status is explicit Task Board workflow: "todo" or "in_progress" (default "todo" when absent).
	Status string `json:"status,omitempty"`
}

type MemoryWakeStateRecentFact struct {
	Name            string      `json:"name"`
	Value           interface{} `json:"value"`
	SourceRef       string      `json:"source_ref"`
	EpistemicFlavor string      `json:"epistemic_flavor"`
	// StateClass distinguishes hard remembered state from softer derived continuity.
	StateClass         string `json:"state_class,omitempty"`
	ConflictKeyVersion string `json:"conflict_key_version,omitempty"`
	ConflictKey        string `json:"conflict_key,omitempty"`
	CertaintyScore     int    `json:"certainty_score,omitempty"`
}

type MemoryWakeStateResponse struct {
	ID                 string                      `json:"id"`
	Scope              string                      `json:"scope"`
	CreatedAtUTC       string                      `json:"created_at_utc"`
	SourceRefs         []MemoryWakeStateSourceRef  `json:"source_refs,omitempty"`
	ActiveGoals        []string                    `json:"active_goals,omitempty"`
	UnresolvedItems    []MemoryWakeStateOpenItem   `json:"unresolved_items,omitempty"`
	RecentFacts        []MemoryWakeStateRecentFact `json:"recent_facts,omitempty"`
	ResonateKeys       []string                    `json:"resonate_keys,omitempty"`
	PromptTokenBudget  int                         `json:"prompt_token_budget,omitempty"`
	ApproxPromptTokens int                         `json:"approx_prompt_tokens,omitempty"`
}

type MemoryDiagnosticWakeEntry struct {
	ItemKind         string   `json:"item_kind"`
	GoalFamilyID     string   `json:"goal_family_id,omitempty"`
	Scope            string   `json:"scope,omitempty"`
	RetentionScore   int      `json:"retention_score,omitempty"`
	EffectiveHotness int      `json:"effective_hotness,omitempty"`
	Reason           string   `json:"reason,omitempty"`
	TrimReason       string   `json:"trim_reason,omitempty"`
	PrecedenceSource string   `json:"precedence_source,omitempty"`
	ScoreTrace       []string `json:"score_trace,omitempty"`
	RedactedSummary  string   `json:"redacted_summary,omitempty"`
}

type MemoryDiagnosticWakeResponse struct {
	SchemaVersion     string                      `json:"schema_version"`
	ResolutionVersion string                      `json:"resolution_version"`
	ReportID          string                      `json:"report_id"`
	CreatedAtUTC      string                      `json:"created_at_utc"`
	RuntimeWakeID     string                      `json:"runtime_wake_id"`
	IncludedCount     int                         `json:"included_count"`
	ExcludedCount     int                         `json:"excluded_count"`
	Entries           []MemoryDiagnosticWakeEntry `json:"entries,omitempty"`
	ExcludedEntries   []MemoryDiagnosticWakeEntry `json:"excluded_entries,omitempty"`
}

type MemoryDiscoverRequest struct {
	Scope    string `json:"scope,omitempty"`
	Query    string `json:"query"`
	MaxItems int    `json:"max_items,omitempty"`
}

type MemoryDiscoverItem struct {
	KeyID        string   `json:"key_id"`
	ThreadID     string   `json:"thread_id"`
	DistillateID string   `json:"distillate_id"`
	Scope        string   `json:"scope"`
	CreatedAtUTC string   `json:"created_at_utc"`
	Tags         []string `json:"tags,omitempty"`
	MatchCount   int      `json:"match_count"`
}

// MemoryEvidenceItem is bounded, non-authoritative supporting context returned by
// hybrid discovery. It is intentionally separate from MemoryDiscoverItem so RAG
// evidence never masquerades as durable continuity state.
type MemoryEvidenceItem struct {
	EvidenceID       string   `json:"evidence_id"`
	SourceKind       string   `json:"source_kind,omitempty"`
	Scope            string   `json:"scope,omitempty"`
	CreatedAtUTC     string   `json:"created_at_utc,omitempty"`
	Snippet          string   `json:"snippet,omitempty"`
	ProvenanceRef    string   `json:"provenance_ref,omitempty"`
	MatchCount       int      `json:"match_count,omitempty"`
	RelatedStateKeys []string `json:"related_state_keys,omitempty"`
}

type MemoryDiscoverResponse struct {
	Scope         string               `json:"scope"`
	Query         string               `json:"query"`
	RetrievalMode string               `json:"retrieval_mode,omitempty"`
	EvidenceQuery string               `json:"evidence_query,omitempty"`
	Items         []MemoryDiscoverItem `json:"items,omitempty"`
	Evidence      []MemoryEvidenceItem `json:"evidence,omitempty"`
}

// MemoryArtifactRef is a bounded handle to a stored continuity artifact. It is
// a lookup handle, not an authority grant: callers still need normal memory.read
// access and must resolve the ref through the control plane.
type MemoryArtifactRef struct {
	ArtifactRef  string   `json:"artifact_ref"`
	Kind         string   `json:"kind"`
	StateClass   string   `json:"state_class,omitempty"`
	Scope        string   `json:"scope,omitempty"`
	KeyID        string   `json:"key_id,omitempty"`
	ThreadID     string   `json:"thread_id,omitempty"`
	DistillateID string   `json:"distillate_id,omitempty"`
	CreatedAtUTC string   `json:"created_at_utc,omitempty"`
	Title        string   `json:"title,omitempty"`
	Summary      string   `json:"summary,omitempty"`
	Tags         []string `json:"tags,omitempty"`
}

type MemoryArtifactLookupRequest struct {
	Scope    string `json:"scope,omitempty"`
	Query    string `json:"query"`
	MaxItems int    `json:"max_items,omitempty"`
}

type MemoryArtifactLookupResponse struct {
	Scope         string              `json:"scope"`
	Query         string              `json:"query"`
	RetrievalMode string              `json:"retrieval_mode,omitempty"`
	ArtifactRefs  []MemoryArtifactRef `json:"artifact_refs,omitempty"`
}

type MemoryRecallRequest struct {
	Scope         string   `json:"scope,omitempty"`
	Reason        string   `json:"reason,omitempty"`
	MaxItems      int      `json:"max_items,omitempty"`
	MaxTokens     int      `json:"max_tokens,omitempty"`
	RequestedKeys []string `json:"requested_keys"`
}

type MemoryRecallFact struct {
	Name            string      `json:"name"`
	Value           interface{} `json:"value"`
	SourceRef       string      `json:"source_ref"`
	EpistemicFlavor string      `json:"epistemic_flavor"`
	// StateClass distinguishes hard remembered state from softer derived continuity.
	StateClass         string `json:"state_class,omitempty"`
	ConflictKeyVersion string `json:"conflict_key_version,omitempty"`
	ConflictKey        string `json:"conflict_key,omitempty"`
	CertaintyScore     int    `json:"certainty_score,omitempty"`
}

type MemoryRecallItem struct {
	KeyID           string                    `json:"key_id"`
	ThreadID        string                    `json:"thread_id"`
	DistillateID    string                    `json:"distillate_id"`
	Scope           string                    `json:"scope"`
	CreatedAtUTC    string                    `json:"created_at_utc"`
	Tags            []string                  `json:"tags,omitempty"`
	ActiveGoals     []string                  `json:"active_goals,omitempty"`
	UnresolvedItems []MemoryWakeStateOpenItem `json:"unresolved_items,omitempty"`
	Facts           []MemoryRecallFact        `json:"facts,omitempty"`
	EpistemicFlavor string                    `json:"epistemic_flavor"`
}

type MemoryRecallResponse struct {
	Scope            string             `json:"scope"`
	MaxItems         int                `json:"max_items"`
	MaxTokens        int                `json:"max_tokens"`
	ApproxTokenCount int                `json:"approx_token_count"`
	Items            []MemoryRecallItem `json:"items,omitempty"`
}

type MemoryArtifactGetRequest struct {
	Scope        string   `json:"scope,omitempty"`
	MaxItems     int      `json:"max_items,omitempty"`
	MaxTokens    int      `json:"max_tokens,omitempty"`
	ArtifactRefs []string `json:"artifact_refs"`
}

type MemoryArtifactGetItem struct {
	Ref             MemoryArtifactRef         `json:"ref"`
	ContentText     string                    `json:"content_text,omitempty"`
	ActiveGoals     []string                  `json:"active_goals,omitempty"`
	UnresolvedItems []MemoryWakeStateOpenItem `json:"unresolved_items,omitempty"`
	Facts           []MemoryRecallFact        `json:"facts,omitempty"`
	EpistemicFlavor string                    `json:"epistemic_flavor,omitempty"`
}

type MemoryArtifactGetResponse struct {
	Scope            string                  `json:"scope"`
	MaxItems         int                     `json:"max_items"`
	MaxTokens        int                     `json:"max_tokens"`
	ApproxTokenCount int                     `json:"approx_token_count"`
	Items            []MemoryArtifactGetItem `json:"items,omitempty"`
}

type MemoryRememberRequest struct {
	Scope           string `json:"scope,omitempty"`
	FactKey         string `json:"fact_key"`
	FactValue       string `json:"fact_value"`
	Reason          string `json:"reason,omitempty"`
	SourceText      string `json:"source_text,omitempty"`
	CandidateSource string `json:"candidate_source,omitempty"`
	SourceChannel   string `json:"source_channel,omitempty"`
}

type MemoryRememberResponse struct {
	Scope               string `json:"scope"`
	FactKey             string `json:"fact_key"`
	FactValue           string `json:"fact_value"`
	InspectionID        string `json:"inspection_id"`
	DistillateID        string `json:"distillate_id"`
	ResonateKeyID       string `json:"resonate_key_id"`
	RememberedAtUTC     string `json:"remembered_at_utc"`
	SupersededFactValue string `json:"superseded_fact_value,omitempty"`
	UpdatedExisting     bool   `json:"updated_existing"`
}

type TodoAddRequest struct {
	Scope           string `json:"scope,omitempty"`
	Text            string `json:"text"`
	TaskKind        string `json:"task_kind,omitempty"`
	SourceKind      string `json:"source_kind,omitempty"`
	NextStep        string `json:"next_step,omitempty"`
	ScheduledForUTC string `json:"scheduled_for_utc,omitempty"`
	ExecutionClass  string `json:"execution_class,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

type TodoAddResponse struct {
	Scope           string `json:"scope"`
	ItemID          string `json:"item_id"`
	Text            string `json:"text"`
	TaskKind        string `json:"task_kind,omitempty"`
	SourceKind      string `json:"source_kind,omitempty"`
	NextStep        string `json:"next_step,omitempty"`
	ScheduledForUTC string `json:"scheduled_for_utc,omitempty"`
	ExecutionClass  string `json:"execution_class,omitempty"`
	InspectionID    string `json:"inspection_id"`
	DistillateID    string `json:"distillate_id"`
	ResonateKeyID   string `json:"resonate_key_id"`
	AddedAtUTC      string `json:"added_at_utc"`
	AlreadyPresent  bool   `json:"already_present"`
}

type TodoCompleteRequest struct {
	Scope  string `json:"scope,omitempty"`
	ItemID string `json:"item_id"`
	Reason string `json:"reason,omitempty"`
}

type TodoCompleteResponse struct {
	Scope          string `json:"scope"`
	ItemID         string `json:"item_id"`
	Text           string `json:"text,omitempty"`
	InspectionID   string `json:"inspection_id,omitempty"`
	DistillateID   string `json:"distillate_id,omitempty"`
	CompletedAtUTC string `json:"completed_at_utc"`
}

type TodoListResponse struct {
	Scope           string                    `json:"scope"`
	UnresolvedItems []MemoryWakeStateOpenItem `json:"unresolved_items,omitempty"`
	ActiveGoals     []string                  `json:"active_goals,omitempty"`
}

func (continuityInspectRequest ContinuityInspectRequest) Validate() error {
	if err := identifiers.ValidateSafeIdentifier("inspection_id", continuityInspectRequest.InspectionID); err != nil {
		return err
	}
	if err := identifiers.ValidateSafeIdentifier("thread_id", continuityInspectRequest.ThreadID); err != nil {
		return err
	}
	if strings.TrimSpace(continuityInspectRequest.Scope) == "" {
		return fmt.Errorf("scope is required")
	}
	if strings.TrimSpace(continuityInspectRequest.SealedAtUTC) == "" {
		return fmt.Errorf("sealed_at_utc is required")
	}
	if _, err := time.Parse(time.RFC3339Nano, continuityInspectRequest.SealedAtUTC); err != nil {
		return fmt.Errorf("sealed_at_utc is invalid: %w", err)
	}
	if continuityInspectRequest.EventCount < 0 {
		return fmt.Errorf("event_count must be non-negative")
	}
	if continuityInspectRequest.ApproxPayloadBytes < 0 {
		return fmt.Errorf("approx_payload_bytes must be non-negative")
	}
	if continuityInspectRequest.ApproxPromptTokens < 0 {
		return fmt.Errorf("approx_prompt_tokens must be non-negative")
	}
	if len(continuityInspectRequest.Events) == 0 {
		return fmt.Errorf("events is required")
	}
	if len(continuityInspectRequest.Events) > maxContinuityEventsPerInspection {
		return fmt.Errorf("events exceeds maximum allowed (%d)", maxContinuityEventsPerInspection)
	}
	if continuityInspectRequest.ApproxPayloadBytes > maxContinuityInspectApproxPayloadBytes {
		return fmt.Errorf("approx_payload_bytes exceeds maximum allowed (%d)", maxContinuityInspectApproxPayloadBytes)
	}
	measuredPayloadBytes := actualContinuityPayloadBytes(continuityInspectRequest.Events)
	if measuredPayloadBytes > maxContinuityInspectApproxPayloadBytes {
		return fmt.Errorf("continuity event payload size exceeds maximum allowed (%d bytes)", maxContinuityInspectApproxPayloadBytes)
	}
	for _, continuityEvent := range continuityInspectRequest.Events {
		if err := continuityEvent.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (continuityEvent ContinuityEventInput) Validate() error {
	if strings.TrimSpace(continuityEvent.TimestampUTC) == "" {
		return fmt.Errorf("continuity event ts_utc is required")
	}
	if _, err := time.Parse(time.RFC3339Nano, continuityEvent.TimestampUTC); err != nil {
		return fmt.Errorf("continuity event ts_utc is invalid: %w", err)
	}
	if strings.TrimSpace(continuityEvent.SessionID) == "" {
		return fmt.Errorf("continuity event session_id is required")
	}
	if strings.TrimSpace(continuityEvent.Type) == "" {
		return fmt.Errorf("continuity event type is required")
	}
	if strings.TrimSpace(continuityEvent.Scope) == "" {
		return fmt.Errorf("continuity event scope is required")
	}
	if strings.TrimSpace(continuityEvent.ThreadID) == "" {
		return fmt.Errorf("continuity event thread_id is required")
	}
	if continuityEvent.LedgerSequence < 0 {
		return fmt.Errorf("continuity event ledger_sequence must be non-negative")
	}
	if strings.TrimSpace(continuityEvent.EventHash) == "" {
		return fmt.Errorf("continuity event event_hash is required")
	}
	return nil
}

func (memoryDiscoverRequest *MemoryDiscoverRequest) Validate() error {
	if strings.TrimSpace(memoryDiscoverRequest.Scope) == "" {
		memoryDiscoverRequest.Scope = "global"
	}
	if strings.TrimSpace(memoryDiscoverRequest.Query) == "" {
		return fmt.Errorf("query is required")
	}
	if memoryDiscoverRequest.MaxItems == 0 {
		memoryDiscoverRequest.MaxItems = 5
	}
	if memoryDiscoverRequest.MaxItems < 1 || memoryDiscoverRequest.MaxItems > 10 {
		return fmt.Errorf("max_items must be between 1 and 10")
	}
	return nil
}

func (memoryRecallRequest *MemoryRecallRequest) Validate() error {
	if strings.TrimSpace(memoryRecallRequest.Scope) == "" {
		memoryRecallRequest.Scope = memoryScopeGlobal
	}
	if memoryRecallRequest.MaxItems == 0 {
		memoryRecallRequest.MaxItems = 10
	}
	if memoryRecallRequest.MaxTokens == 0 {
		memoryRecallRequest.MaxTokens = 2000
	}
	if memoryRecallRequest.MaxItems < 1 || memoryRecallRequest.MaxItems > 10 {
		return fmt.Errorf("max_items must be between 1 and 10")
	}
	if memoryRecallRequest.MaxTokens < 1 || memoryRecallRequest.MaxTokens > 8000 {
		return fmt.Errorf("max_tokens must be between 1 and 8000")
	}
	if len(memoryRecallRequest.RequestedKeys) == 0 {
		return fmt.Errorf("requested_keys is required")
	}
	requestedKeySet := make(map[string]struct{}, len(memoryRecallRequest.RequestedKeys))
	for _, rawKeyID := range memoryRecallRequest.RequestedKeys {
		validatedKeyID := strings.TrimSpace(rawKeyID)
		if validatedKeyID == "" {
			return fmt.Errorf("requested_keys entries must be non-empty")
		}
		if _, duplicate := requestedKeySet[validatedKeyID]; duplicate {
			return fmt.Errorf("requested_keys contains duplicate %q", validatedKeyID)
		}
		requestedKeySet[validatedKeyID] = struct{}{}
	}
	if len(memoryRecallRequest.RequestedKeys) > memoryRecallRequest.MaxItems {
		return fmt.Errorf("requested_keys exceeds max_items")
	}
	return nil
}

func (memoryArtifactLookupRequest *MemoryArtifactLookupRequest) Validate() error {
	if strings.TrimSpace(memoryArtifactLookupRequest.Scope) == "" {
		memoryArtifactLookupRequest.Scope = memoryScopeGlobal
	}
	if strings.TrimSpace(memoryArtifactLookupRequest.Query) == "" {
		return fmt.Errorf("query is required")
	}
	if memoryArtifactLookupRequest.MaxItems == 0 {
		memoryArtifactLookupRequest.MaxItems = 5
	}
	if memoryArtifactLookupRequest.MaxItems < 1 || memoryArtifactLookupRequest.MaxItems > 10 {
		return fmt.Errorf("max_items must be between 1 and 10")
	}
	return nil
}

func (memoryArtifactGetRequest *MemoryArtifactGetRequest) Validate() error {
	if strings.TrimSpace(memoryArtifactGetRequest.Scope) == "" {
		memoryArtifactGetRequest.Scope = memoryScopeGlobal
	}
	if memoryArtifactGetRequest.MaxItems == 0 {
		memoryArtifactGetRequest.MaxItems = 10
	}
	if memoryArtifactGetRequest.MaxTokens == 0 {
		memoryArtifactGetRequest.MaxTokens = 2000
	}
	if memoryArtifactGetRequest.MaxItems < 1 || memoryArtifactGetRequest.MaxItems > 10 {
		return fmt.Errorf("max_items must be between 1 and 10")
	}
	if memoryArtifactGetRequest.MaxTokens < 1 || memoryArtifactGetRequest.MaxTokens > 8000 {
		return fmt.Errorf("max_tokens must be between 1 and 8000")
	}
	if len(memoryArtifactGetRequest.ArtifactRefs) == 0 {
		return fmt.Errorf("artifact_refs is required")
	}
	seenArtifactRefs := make(map[string]struct{}, len(memoryArtifactGetRequest.ArtifactRefs))
	for _, rawArtifactRef := range memoryArtifactGetRequest.ArtifactRefs {
		validatedArtifactRef := strings.TrimSpace(rawArtifactRef)
		if validatedArtifactRef == "" {
			return fmt.Errorf("artifact_refs entries must be non-empty")
		}
		if _, duplicate := seenArtifactRefs[validatedArtifactRef]; duplicate {
			return fmt.Errorf("artifact_refs contains duplicate %q", validatedArtifactRef)
		}
		seenArtifactRefs[validatedArtifactRef] = struct{}{}
	}
	if len(memoryArtifactGetRequest.ArtifactRefs) > memoryArtifactGetRequest.MaxItems {
		return fmt.Errorf("artifact_refs exceeds max_items")
	}
	return nil
}

func (memoryRememberRequest MemoryRememberRequest) Validate() error {
	if strings.TrimSpace(memoryRememberRequest.FactKey) == "" {
		return fmt.Errorf("fact_key is required")
	}
	if strings.TrimSpace(memoryRememberRequest.FactValue) == "" {
		return fmt.Errorf("fact_value is required")
	}
	if len([]byte(strings.TrimSpace(memoryRememberRequest.FactValue))) > 256 {
		return fmt.Errorf("fact_value exceeds maximum length")
	}
	if strings.ContainsAny(memoryRememberRequest.FactValue, "\r\n") {
		return fmt.Errorf("fact_value must be a single line")
	}
	if len(strings.TrimSpace(memoryRememberRequest.Reason)) > 200 {
		return fmt.Errorf("reason exceeds maximum length")
	}
	if len(strings.TrimSpace(memoryRememberRequest.SourceText)) > 512 {
		return fmt.Errorf("source_text exceeds maximum length")
	}
	trimmedCandidateSource := strings.TrimSpace(memoryRememberRequest.CandidateSource)
	if len(trimmedCandidateSource) > 64 {
		return fmt.Errorf("candidate_source exceeds maximum length")
	}
	if trimmedCandidateSource != "" && trimmedCandidateSource != string(tclpkg.CandidateSourceExplicitFact) {
		return fmt.Errorf("candidate_source %q is not supported; only %q is implemented for explicit memory writes", trimmedCandidateSource, tclpkg.CandidateSourceExplicitFact)
	}
	if len(strings.TrimSpace(memoryRememberRequest.SourceChannel)) > 64 {
		return fmt.Errorf("source_channel exceeds maximum length")
	}
	return nil
}

func (todoAddRequest TodoAddRequest) Validate() error {
	if strings.TrimSpace(todoAddRequest.Text) == "" {
		return fmt.Errorf("text is required")
	}
	if len([]byte(strings.TrimSpace(todoAddRequest.Text))) > 200 {
		return fmt.Errorf("text exceeds maximum length")
	}
	if strings.ContainsAny(todoAddRequest.Text, "\r\n") {
		return fmt.Errorf("text must be a single line")
	}
	if len(strings.TrimSpace(todoAddRequest.TaskKind)) > 32 {
		return fmt.Errorf("task_kind exceeds maximum length")
	}
	if len(strings.TrimSpace(todoAddRequest.SourceKind)) > 64 {
		return fmt.Errorf("source_kind exceeds maximum length")
	}
	if len([]byte(strings.TrimSpace(todoAddRequest.NextStep))) > 200 {
		return fmt.Errorf("next_step exceeds maximum length")
	}
	if strings.ContainsAny(todoAddRequest.NextStep, "\r\n") {
		return fmt.Errorf("next_step must be a single line")
	}
	if strings.TrimSpace(todoAddRequest.ScheduledForUTC) != "" {
		if _, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(todoAddRequest.ScheduledForUTC)); err != nil {
			return fmt.Errorf("scheduled_for_utc is invalid: %w", err)
		}
	}
	if len(strings.TrimSpace(todoAddRequest.ExecutionClass)) > 64 {
		return fmt.Errorf("execution_class exceeds maximum length")
	}
	if len(strings.TrimSpace(todoAddRequest.Reason)) > 200 {
		return fmt.Errorf("reason exceeds maximum length")
	}
	return nil
}

func (todoCompleteRequest TodoCompleteRequest) Validate() error {
	if strings.TrimSpace(todoCompleteRequest.ItemID) == "" {
		return fmt.Errorf("item_id is required")
	}
	if len([]byte(strings.TrimSpace(todoCompleteRequest.ItemID))) > 96 {
		return fmt.Errorf("item_id exceeds maximum length")
	}
	if len(strings.TrimSpace(todoCompleteRequest.Reason)) > 200 {
		return fmt.Errorf("reason exceeds maximum length")
	}
	return nil
}

func (memoryInspectionReviewRequest MemoryInspectionReviewRequest) Validate() error {
	switch strings.TrimSpace(memoryInspectionReviewRequest.Decision) {
	case "accepted", "rejected":
	default:
		return fmt.Errorf("decision must be accepted or rejected")
	}
	if err := identifiers.ValidateSafeIdentifier("operation_id", strings.TrimSpace(memoryInspectionReviewRequest.OperationID)); err != nil {
		return err
	}
	return nil
}

func (memoryInspectionLineageRequest MemoryInspectionLineageRequest) Validate() error {
	if err := identifiers.ValidateSafeIdentifier("operation_id", strings.TrimSpace(memoryInspectionLineageRequest.OperationID)); err != nil {
		return err
	}
	return nil
}
