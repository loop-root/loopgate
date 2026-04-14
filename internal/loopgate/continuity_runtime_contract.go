package loopgate

import (
	"os"
	"path/filepath"
)

const (
	continuityDerivationVersion    = "1"
	continuityNormalizationVersion = "1"
	continuityResolutionVersion    = "1"

	goalTypeTechnicalReview          = "technical_review"
	goalTypeImplementationChange     = "implementation_change"
	goalTypeDebuggingInvestigation   = "debugging_investigation"
	goalTypeDocumentationUpdate      = "documentation_update"
	goalTypeArchitecturePlanning     = "architecture_planning"
	goalTypeSecurityHardening        = "security_hardening"
	goalTypeWorkflowFollowup         = "workflow_followup"
	goalTypeSchedulingCommitment     = "scheduling_commitment"
	goalTypeResearchSynthesis        = "research_synthesis"
	goalTypePreferenceOrConfigUpdate = "preference_or_config_update"

	memoryStateHot        = "hot"
	memoryStateWarm       = "warm"
	memoryStateCold       = "cold"
	memoryStateTombstoned = "tombstoned"
	memoryStatePurged     = "purged"

	wakeEntryKindConfig      = "config"
	wakeEntryKindGoal        = "goal"
	wakeEntryKindTodo        = "todo"
	wakeEntryKindRule        = "rule"
	wakeEntryKindDistillate  = "distillate"
	wakeEntryKindResonateKey = "resonate_key"

	correctionStrengthBoundary   = "boundary"
	correctionStrengthPreference = "preference"

	correctionStateActive   = "active"
	correctionStateInactive = "inactive"

	revalidationStatusQueued     = "queued"
	revalidationStatusPrompted   = "prompted"
	revalidationStatusReaffirmed = "reaffirmed"
	revalidationStatusExpired    = "expired"
	revalidationStatusSuperseded = "superseded"
)

type continuityMemoryPaths struct {
	RootDir                 string
	CurrentStatePath        string
	LegacyStatePath         string
	ContinuityEventsPath    string
	GoalEventsPath          string
	ProfileEventsPath       string
	GoalsCurrentPath        string
	TasksCurrentPath        string
	ReviewsCurrentPath      string
	ProfileResolvedPath     string
	RankingCachePath        string
	DistillatesDir          string
	EvidenceDir             string
	WakeRuntimeDir          string
	WakeDiagnosticDir       string
	ProfilesActiveDir       string
	ProfilesScopesDir       string
	ProfilesCandidatesDir   string
	ProfilesCorrectionsDir  string
	ProfilesRevalidationDir string
}

type continuityGoalNormalization struct {
	GoalType             string `json:"goal_type"`
	GoalFamilyID         string `json:"goal_family_id"`
	NormalizationVersion string `json:"normalization_version"`
	AliasMatched         bool   `json:"alias_matched"`
	AliasKey             string `json:"alias_key,omitempty"`
	NeedsAliasCuration   bool   `json:"needs_alias_curation,omitempty"`
}

type continuityResolvedProfileSnapshot struct {
	SchemaVersion     string                          `json:"schema_version"`
	ResolutionVersion string                          `json:"resolution_version"`
	CreatedAtUTC      string                          `json:"created_at_utc"`
	ExplicitConfig    continuityResolvedProfileConfig `json:"explicit_config"`
	ActiveCorrections []continuityCorrectionRecord    `json:"active_corrections,omitempty"`
	ActiveScopedRules []continuityLearnedRuleRecord   `json:"active_scoped_rules,omitempty"`
	ActiveGlobalRules []continuityLearnedRuleRecord   `json:"active_global_rules,omitempty"`
}

type continuityResolvedProfileConfig struct {
	CandidatePanelSize      int    `json:"candidate_panel_size"`
	DecompositionPreference string `json:"decomposition_preference"`
	ReviewPreference        string `json:"review_preference"`
	SoftWorkerConcurrency   int    `json:"soft_worker_concurrency"`
	BatchingPreference      string `json:"batching_preference"`
}

type continuityLearnedRuleRecord struct {
	SchemaVersion       string   `json:"schema_version"`
	RuleID              string   `json:"rule_id"`
	RuleClass           string   `json:"rule_class"`
	Scope               string   `json:"scope"`
	GoalType            string   `json:"goal_type,omitempty"`
	GoalFamilyID        string   `json:"goal_family_id,omitempty"`
	Status              string   `json:"status"`
	SupportScore        int      `json:"support_score"`
	LastReinforcedAtUTC string   `json:"last_reinforced_at_utc,omitempty"`
	SourceRefs          []string `json:"source_refs,omitempty"`
}

type continuityCorrectionRecord struct {
	SchemaVersion           string   `json:"schema_version"`
	CorrectionID            string   `json:"correction_id"`
	Type                    string   `json:"type"`
	Scope                   string   `json:"scope"`
	GoalType                string   `json:"goal_type,omitempty"`
	GoalFamilyID            string   `json:"goal_family_id,omitempty"`
	TargetRuleClass         string   `json:"target_rule_class,omitempty"`
	TargetDerivationStage   string   `json:"target_derivation_stage,omitempty"`
	TargetOutputKind        string   `json:"target_output_kind,omitempty"`
	CorrectionStrengthClass string   `json:"correction_strength_class"`
	Precedence              int      `json:"precedence"`
	ActiveState             string   `json:"active_state"`
	CreatedAtUTC            string   `json:"created_at_utc"`
	SourceRefs              []string `json:"source_refs,omitempty"`
	ReasonSummary           string   `json:"reason_summary,omitempty"`
	InterceptedProposal     string   `json:"intercepted_proposal,omitempty"`
	InterceptedStage        string   `json:"intercepted_stage,omitempty"`
	Supersedes              string   `json:"supersedes,omitempty"`
	SupersededBy            string   `json:"superseded_by,omitempty"`
	ReviewAtUTC             string   `json:"review_at_utc,omitempty"`
}

type continuityRevalidationTicket struct {
	SchemaVersion  string `json:"schema_version"`
	RevalidationID string `json:"revalidation_id"`
	CorrectionID   string `json:"correction_id"`
	Scope          string `json:"scope"`
	GoalType       string `json:"goal_type,omitempty"`
	GoalFamilyID   string `json:"goal_family_id,omitempty"`
	CreatedAtUTC   string `json:"created_at_utc"`
	DueAtUTC       string `json:"due_at_utc"`
	Status         string `json:"status"`
	Reason         string `json:"reason,omitempty"`
}

type continuityDiagnosticWakeReport struct {
	SchemaVersion     string                          `json:"schema_version"`
	ResolutionVersion string                          `json:"resolution_version"`
	ReportID          string                          `json:"report_id"`
	CreatedAtUTC      string                          `json:"created_at_utc"`
	RuntimeWakeID     string                          `json:"runtime_wake_id"`
	Entries           []continuityDiagnosticWakeEntry `json:"entries,omitempty"`
	ExcludedEntries   []continuityDiagnosticWakeEntry `json:"excluded_entries,omitempty"`
}

type continuityDiagnosticWakeEntry struct {
	ItemKind         string   `json:"item_kind"`
	ItemID           string   `json:"item_id"`
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

type continuityRankingCache struct {
	SchemaVersion     string                        `json:"schema_version"`
	ResolutionVersion string                        `json:"resolution_version"`
	CreatedAtUTC      string                        `json:"created_at_utc"`
	Entries           []continuityRankingCacheEntry `json:"entries,omitempty"`
}

type continuityRankingCacheEntry struct {
	ItemKind         string `json:"item_kind"`
	ItemID           string `json:"item_id"`
	GoalFamilyID     string `json:"goal_family_id,omitempty"`
	Scope            string `json:"scope,omitempty"`
	RetentionScore   int    `json:"retention_score,omitempty"`
	EffectiveHotness int    `json:"effective_hotness,omitempty"`
}

type continuityAuthoritativeEvent struct {
	SchemaVersion string `json:"schema_version"`
	EventID       string `json:"event_id"`
	EventType     string `json:"event_type"`
	CreatedAtUTC  string `json:"created_at_utc"`
	Actor         string `json:"actor"`
	Scope         string `json:"scope,omitempty"`
	InspectionID  string `json:"inspection_id,omitempty"`
	ThreadID      string `json:"thread_id,omitempty"`
	GoalType      string `json:"goal_type,omitempty"`
	GoalFamilyID  string `json:"goal_family_id,omitempty"`
	// Request is decode-compat only for older continuity JSONL entries.
	// New writes should populate ObservedPacket instead of persisting the raw
	// inspect request body as the authoritative continuity source record.
	Request        *ContinuityInspectRequest    `json:"request,omitempty"`
	ObservedPacket *continuityObservedPacket    `json:"observed_packet,omitempty"`
	Inspection     *continuityInspectionRecord  `json:"inspection,omitempty"`
	Distillate     *continuityDistillateRecord  `json:"distillate,omitempty"`
	ResonateKey    *continuityResonateKeyRecord `json:"resonate_key,omitempty"`
	Review         *continuityInspectionReview  `json:"review,omitempty"`
	Lineage        *continuityInspectionLineage `json:"lineage,omitempty"`
}

type continuityObservedPacket struct {
	ThreadID    string                          `json:"thread_id"`
	Scope       string                          `json:"scope"`
	SealedAtUTC string                          `json:"sealed_at_utc"`
	Tags        []string                        `json:"tags,omitempty"`
	Events      []continuityObservedEventRecord `json:"events,omitempty"`
}

type continuityObservedEventRecord struct {
	TimestampUTC    string                          `json:"ts_utc"`
	SessionID       string                          `json:"session_id"`
	Type            string                          `json:"type"`
	Scope           string                          `json:"scope"`
	ThreadID        string                          `json:"thread_id"`
	EpistemicFlavor string                          `json:"epistemic_flavor"`
	LedgerSequence  int64                           `json:"ledger_sequence"`
	EventHash       string                          `json:"event_hash"`
	SourceRefs      []continuityArtifactSourceRef   `json:"source_refs,omitempty"`
	Payload         *continuityObservedEventPayload `json:"payload,omitempty"`
}

type continuityObservedEventPayload struct {
	Text              string                         `json:"text,omitempty"`
	Output            string                         `json:"output,omitempty"`
	GoalID            string                         `json:"goal_id,omitempty"`
	ItemID            string                         `json:"item_id,omitempty"`
	Capability        string                         `json:"capability,omitempty"`
	Status            string                         `json:"status,omitempty"`
	Reason            string                         `json:"reason,omitempty"`
	DenialCode        string                         `json:"denial_code,omitempty"`
	CallID            string                         `json:"call_id,omitempty"`
	ApprovalRequestID string                         `json:"approval_request_id,omitempty"`
	Facts             []continuityObservedFactRecord `json:"facts,omitempty"`
}

type continuityObservedFactRecord struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type continuityGoalEvent struct {
	SchemaVersion      string                       `json:"schema_version"`
	EventID            string                       `json:"event_id"`
	EventType          string                       `json:"event_type"`
	CreatedAtUTC       string                       `json:"created_at_utc"`
	Actor              string                       `json:"actor"`
	InspectionID       string                       `json:"inspection_id,omitempty"`
	ThreadID           string                       `json:"thread_id,omitempty"`
	GoalType           string                       `json:"goal_type,omitempty"`
	GoalFamilyID       string                       `json:"goal_family_id,omitempty"`
	NeedsAliasCuration bool                         `json:"needs_alias_curation,omitempty"`
	GoalOps            []continuityGoalOp           `json:"goal_ops,omitempty"`
	UnresolvedItemOps  []continuityUnresolvedItemOp `json:"unresolved_item_ops,omitempty"`
}

type continuityProfileEvent struct {
	SchemaVersion  string                       `json:"schema_version"`
	EventID        string                       `json:"event_id"`
	EventType      string                       `json:"event_type"`
	CreatedAtUTC   string                       `json:"created_at_utc"`
	Actor          string                       `json:"actor"`
	InspectionID   string                       `json:"inspection_id,omitempty"`
	ThreadID       string                       `json:"thread_id,omitempty"`
	GoalType       string                       `json:"goal_type,omitempty"`
	GoalFamilyID   string                       `json:"goal_family_id,omitempty"`
	Review         *continuityInspectionReview  `json:"review,omitempty"`
	Lineage        *continuityInspectionLineage `json:"lineage,omitempty"`
	CorrectionID   string                       `json:"correction_id,omitempty"`
	RevalidationID string                       `json:"revalidation_id,omitempty"`
	Status         string                       `json:"status,omitempty"`
}

type continuityMutationEvents struct {
	Continuity []continuityAuthoritativeEvent
	Goal       []continuityGoalEvent
	Profile    []continuityProfileEvent
}

func newContinuityMemoryPaths(rootDir string, legacyStatePath string) continuityMemoryPaths {
	return continuityMemoryPaths{
		RootDir:                 rootDir,
		CurrentStatePath:        filepath.Join(rootDir, "state.json"),
		LegacyStatePath:         legacyStatePath,
		ContinuityEventsPath:    filepath.Join(rootDir, "continuity_events.jsonl"),
		GoalEventsPath:          filepath.Join(rootDir, "goal_events.jsonl"),
		ProfileEventsPath:       filepath.Join(rootDir, "profile_events.jsonl"),
		GoalsCurrentPath:        filepath.Join(rootDir, "goals_current.json"),
		TasksCurrentPath:        filepath.Join(rootDir, "tasks_current.json"),
		ReviewsCurrentPath:      filepath.Join(rootDir, "reviews_current.json"),
		ProfileResolvedPath:     filepath.Join(rootDir, "profile_resolved.json"),
		RankingCachePath:        filepath.Join(rootDir, "ranking_cache.json"),
		DistillatesDir:          filepath.Join(rootDir, "distillates"),
		EvidenceDir:             filepath.Join(rootDir, "evidence"),
		WakeRuntimeDir:          filepath.Join(rootDir, "wake", "runtime"),
		WakeDiagnosticDir:       filepath.Join(rootDir, "wake", "diagnostic"),
		ProfilesActiveDir:       filepath.Join(rootDir, "profiles", "active"),
		ProfilesScopesDir:       filepath.Join(rootDir, "profiles", "active", "scopes"),
		ProfilesCandidatesDir:   filepath.Join(rootDir, "profiles", "candidates"),
		ProfilesCorrectionsDir:  filepath.Join(rootDir, "profiles", "corrections"),
		ProfilesRevalidationDir: filepath.Join(rootDir, "profiles", "revalidation"),
	}
}

func (memoryPaths continuityMemoryPaths) ensure() error {
	requiredDirs := []string{
		memoryPaths.RootDir,
		memoryPaths.DistillatesDir,
		memoryPaths.EvidenceDir,
		memoryPaths.WakeRuntimeDir,
		memoryPaths.WakeDiagnosticDir,
		memoryPaths.ProfilesActiveDir,
		memoryPaths.ProfilesScopesDir,
		memoryPaths.ProfilesCandidatesDir,
		memoryPaths.ProfilesCorrectionsDir,
		memoryPaths.ProfilesRevalidationDir,
	}
	for _, requiredDir := range requiredDirs {
		if err := os.MkdirAll(requiredDir, 0o700); err != nil {
			return err
		}
	}
	return nil
}
