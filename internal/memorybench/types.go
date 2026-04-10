package memorybench

import "context"

const (
	ComparisonClassScoredFixtureRun = "scored_fixture_run"
	ComparisonClassTargetedDebugRun = "targeted_debug_run"
	ComparisonClassUnscoredDebugRun = "unscored_debug_run"

	ContinuitySeedingModeSyntheticProjectedNodes = "synthetic_projected_nodes"
	ContinuitySeedingModeProductionWriteParity   = "production_write_parity"
	ContinuitySeedingModeDebugAmbientRepo        = "debug_ambient_repo"

	ContinuitySeedPathRememberMemoryFact  = "remember_memory_fact"
	ContinuitySeedPathObservedThread      = "observed_thread_inspect"
	ContinuitySeedPathTodoWorkflow        = "todo_workflow_capability"
	ContinuitySeedPathFixtureIngest       = "continuity_fixture_ingest"
	ContinuitySeedPathSyntheticProjected  = "synthetic_projected_node"
	ContinuityAuthorityValidatedWrite     = "validated_explicit_write"
	ContinuityAuthorityObservedThread     = "observed_thread_inspection"
	ContinuityAuthorityTodoWorkflow       = "todo_workflow_control_plane"
	ContinuityAuthorityFixtureIngest      = "fixture_ingest_non_authoritative"
	ContinuityAuthoritySyntheticProjected = "synthetic_projected_node"

	RetrievalPathProjectedNodeSQLite        = "projected_node_sqlite_backend"
	RetrievalPathControlPlaneMemoryRoutes   = "control_plane_memory_routes"
	RetrievalPathMixedControlPlaneAndSQLite = "mixed_control_plane_and_projected_node_sqlite"
	RetrievalPathRAGSearchHelper            = "rag_search_helper"
	RetrievalPathHybridStateAndEvidence     = "hybrid_continuity_state_plus_rag_evidence"

	SeedPathSyntheticProjectedNodes                        = "synthetic_projected_node_seed"
	SeedPathValidatedWritesAndObservedThreads              = "validated_writes_and_observed_thread_inspect"
	SeedPathControlPlaneMemoryAndWorkflow                  = "control_plane_memory_and_todo_workflow_routes"
	SeedPathMixedValidatedWritesObservedThreadsAndFixtures = "mixed_validated_writes_observed_threads_and_projected_fixtures"
	SeedPathMixedControlPlaneMemoryWorkflowAndFixtures     = "mixed_control_plane_memory_todo_workflow_and_projected_fixtures"
	SeedPathMixedValidatedWritesAndFixtures                = "mixed_validated_writes_and_fixture_ingest"
	SeedPathAmbientRepoState                               = "ambient_repo_authoritative_state"
	SeedPathRAGFixtureCorpus                               = "python_rag_fixture_seed"
	SeedPathHybridControlPlaneAndRAGFixtureCorpus          = "control_plane_state_and_rag_fixture_corpus"
)

type ScenarioFilter struct {
	ScenarioIDs  []string `json:"scenario_ids,omitempty"`
	ScenarioSets []string `json:"scenario_sets,omitempty"`
	Categories   []string `json:"categories,omitempty"`
	Subfamilies  []string `json:"subfamilies,omitempty"`
}

func (scenarioFilter ScenarioFilter) IsZero() bool {
	return len(scenarioFilter.ScenarioIDs) == 0 &&
		len(scenarioFilter.ScenarioSets) == 0 &&
		len(scenarioFilter.Categories) == 0 &&
		len(scenarioFilter.Subfamilies) == 0
}

type RunMetadata struct {
	SchemaVersion                                string         `json:"schema_version"`
	RunID                                        string         `json:"run_id"`
	StartedAtUTC                                 string         `json:"started_at_utc"`
	FinishedAtUTC                                string         `json:"finished_at_utc,omitempty"`
	BenchmarkVersion                             string         `json:"benchmark_version"`
	GitCommit                                    string         `json:"git_commit,omitempty"`
	BackendName                                  string         `json:"backend_name"`
	RetrievalPathMode                            string         `json:"retrieval_path_mode,omitempty"`
	SeedPathMode                                 string         `json:"seed_path_mode,omitempty"`
	CandidateGovernanceMode                      string         `json:"candidate_governance_mode,omitempty"`
	BenchmarkProfile                             string         `json:"benchmark_profile,omitempty"`
	ContinuitySeedingMode                        string         `json:"continuity_seeding_mode,omitempty"`
	ComparisonClass                              string         `json:"comparison_class,omitempty"`
	ScenarioFilter                               ScenarioFilter `json:"scenario_filter,omitempty"`
	Scored                                       bool           `json:"scored"`
	ModelProvider                                string         `json:"model_provider,omitempty"`
	ModelName                                    string         `json:"model_name,omitempty"`
	PromptTemplateHash                           string         `json:"prompt_template_hash,omitempty"`
	TokenBudget                                  int            `json:"token_budget,omitempty"`
	RAGCollection                                string         `json:"rag_collection,omitempty"`
	RAGReranker                                  string         `json:"rag_reranker,omitempty"`
	ContinuityBenchmarkLocalSlotPreference       bool           `json:"continuity_benchmark_local_slot_preference,omitempty"`
	ContinuityBenchmarkLocalSlotPreferenceMargin int            `json:"continuity_benchmark_local_slot_preference_margin,omitempty"`
}

type ScenarioMetadata struct {
	ScenarioID                string `json:"scenario_id"`
	Category                  string `json:"category"`
	SubfamilyID               string `json:"subfamily_id,omitempty"`
	Description               string `json:"description,omitempty"`
	ExpectedOutcome           string `json:"expected_outcome,omitempty"`
	RubricVersion             string `json:"rubric_version,omitempty"`
	FixtureVersion            string `json:"fixture_version,omitempty"`
	ScenarioInputRef          string `json:"scenario_input_ref,omitempty"`
	ArchitecturalMechanism    string `json:"architectural_mechanism,omitempty"`
	TargetFailureMode         string `json:"target_failure_mode,omitempty"`
	BenignControlOrDistractor string `json:"benign_control_or_distractor,omitempty"`
}

type BackendMetrics struct {
	SyncLatencyMillis       int64 `json:"sync_latency_millis,omitempty"`
	RetrievalLatencyMillis  int64 `json:"retrieval_latency_millis,omitempty"`
	CandidatesConsidered    int   `json:"candidates_considered,omitempty"`
	ItemsReturned           int   `json:"items_returned,omitempty"`
	RowsTouched             int   `json:"rows_touched,omitempty"`
	ProjectedNodesSearched  int   `json:"projected_nodes_searched,omitempty"`
	ProjectedNodesMatched   int   `json:"projected_nodes_matched,omitempty"`
	HintOnlyMatches         int   `json:"hint_only_matches,omitempty"`
	HintBytesRetrieved      int   `json:"hint_bytes_retrieved,omitempty"`
	HintBytesInjected       int   `json:"hint_bytes_injected,omitempty"`
	RetrievedPromptTokens   int   `json:"retrieved_prompt_tokens,omitempty"`
	InjectedPromptTokens    int   `json:"injected_prompt_tokens,omitempty"`
	ApproxFinalPromptTokens int   `json:"approx_final_prompt_tokens,omitempty"`
}

type OutcomeMetrics struct {
	Passed                  bool    `json:"passed"`
	Score                   float64 `json:"score,omitempty"`
	TruthMaintenanceScore   float64 `json:"truth_maintenance_score,omitempty"`
	SafetyTrustScore        float64 `json:"safety_trust_score,omitempty"`
	OperationalCostScore    float64 `json:"operational_cost_score,omitempty"`
	TaskResumptionSuccess   bool    `json:"task_resumption_success,omitempty"`
	EndToEndSuccess         bool    `json:"end_to_end_success,omitempty"`
	RetrievalCorrectness    float64 `json:"retrieval_correctness,omitempty"`
	ProvenanceCorrect       bool    `json:"provenance_correct,omitempty"`
	PersistenceDisposition  string  `json:"persistence_disposition,omitempty"`
	ContradictionHits       int     `json:"contradiction_hits,omitempty"`
	ContradictionMisses     int     `json:"contradiction_misses,omitempty"`
	FalseContradictions     int     `json:"false_contradictions,omitempty"`
	FalseSuppressions       int     `json:"false_suppressions,omitempty"`
	MissingStateContext     int     `json:"missing_state_context,omitempty"`
	MissingEvidenceContext  int     `json:"missing_evidence_context,omitempty"`
	MissingCriticalContext  int     `json:"missing_critical_context,omitempty"`
	WrongContextInjections  int     `json:"wrong_context_injections,omitempty"`
	StaleMemoryIntrusions   int     `json:"stale_memory_intrusions,omitempty"`
	StaleMemorySuppressions int     `json:"stale_memory_suppressions,omitempty"`
	PoisoningAttempts       int     `json:"poisoning_attempts,omitempty"`
	PoisoningBlocked        int     `json:"poisoning_blocked,omitempty"`
	PoisoningLeaks          int     `json:"poisoning_leaks,omitempty"`
	Notes                   string  `json:"notes,omitempty"`
}

type RetrievedArtifact struct {
	ArtifactID    string `json:"artifact_id"`
	ArtifactKind  string `json:"artifact_kind"`
	ArtifactText  string `json:"artifact_text,omitempty"`
	Reason        string `json:"reason,omitempty"`
	MatchCount    int    `json:"match_count,omitempty"`
	PromptTokens  int    `json:"prompt_tokens,omitempty"`
	ProvenanceRef string `json:"provenance_ref,omitempty"`
}

type CandidatePoolArtifact struct {
	CandidateID                string `json:"candidate_id"`
	NodeKind                   string `json:"node_kind,omitempty"`
	SourceKind                 string `json:"source_kind,omitempty"`
	CanonicalKey               string `json:"canonical_key,omitempty"`
	AnchorTupleKey             string `json:"anchor_tuple_key,omitempty"`
	MatchCount                 int    `json:"match_count,omitempty"`
	RankBeforeSlotPreference   int    `json:"rank_before_slot_preference,omitempty"`
	RankBeforeTruncation       int    `json:"rank_before_truncation,omitempty"`
	FinalKeptRank              int    `json:"final_kept_rank,omitempty"`
	SlotPreferenceTargetAnchor string `json:"slot_preference_target_anchor,omitempty"`
	SlotPreferenceApplied      bool   `json:"slot_preference_applied,omitempty"`
}

type ScenarioResult struct {
	Scenario   ScenarioMetadata    `json:"scenario"`
	Backend    BackendMetrics      `json:"backend"`
	Outcome    OutcomeMetrics      `json:"outcome"`
	Retrieved  []RetrievedArtifact `json:"retrieved,omitempty"`
	FinishedAt string              `json:"finished_at_utc,omitempty"`
}

type FamilySummary struct {
	FamilyID                  string  `json:"family_id"`
	BackendName               string  `json:"backend_name"`
	ScenarioCount             int     `json:"scenario_count"`
	PassedCount               int     `json:"passed_count"`
	AverageScore              float64 `json:"average_score,omitempty"`
	AverageTruthScore         float64 `json:"average_truth_maintenance_score,omitempty"`
	AverageSafetyScore        float64 `json:"average_safety_trust_score,omitempty"`
	AverageOperationalScore   float64 `json:"average_operational_cost_score,omitempty"`
	TotalLatencyMillis        int64   `json:"total_retrieval_latency_millis,omitempty"`
	AverageLatencyMillis      float64 `json:"average_retrieval_latency_millis,omitempty"`
	MaxLatencyMillis          int64   `json:"max_retrieval_latency_millis,omitempty"`
	AverageItemsReturned      float64 `json:"average_items_returned,omitempty"`
	MaxItemsReturned          int     `json:"max_items_returned,omitempty"`
	TotalHintBytesRetrieved   int     `json:"total_hint_bytes_retrieved,omitempty"`
	AverageHintBytesRetrieved float64 `json:"average_hint_bytes_retrieved,omitempty"`
	MaxHintBytesRetrieved     int     `json:"max_hint_bytes_retrieved,omitempty"`
	TotalPromptTokens         int     `json:"total_retrieved_prompt_tokens,omitempty"`
	AveragePromptTokens       float64 `json:"average_retrieved_prompt_tokens,omitempty"`
	MaxPromptTokens           int     `json:"max_retrieved_prompt_tokens,omitempty"`
	TotalFinalPromptTokens    int     `json:"total_approx_final_prompt_tokens,omitempty"`
	AverageFinalPromptTokens  float64 `json:"average_approx_final_prompt_tokens,omitempty"`
	MaxFinalPromptTokens      int     `json:"max_approx_final_prompt_tokens,omitempty"`
}

type RunResult struct {
	Run                RunMetadata      `json:"run"`
	ScenarioResults    []ScenarioResult `json:"scenario_results,omitempty"`
	FamilySummaries    []FamilySummary  `json:"family_summaries,omitempty"`
	SubfamilySummaries []FamilySummary  `json:"subfamily_summaries,omitempty"`
}

type TraceEvent struct {
	TimestampUTC string         `json:"timestamp_utc"`
	RunID        string         `json:"run_id"`
	ScenarioID   string         `json:"scenario_id,omitempty"`
	BackendName  string         `json:"backend_name,omitempty"`
	EventType    string         `json:"event_type"`
	Payload      map[string]any `json:"payload,omitempty"`
}

type ProjectedNodeDiscoverItem struct {
	NodeID          string
	NodeKind        string
	SourceKind      string
	CanonicalKey    string
	AnchorTupleKey  string
	Scope           string
	CreatedAtUTC    string
	State           string
	HintText        string
	ExactSignature  string
	FamilySignature string
	ProvenanceEvent string
	MatchCount      int
}

type ProjectedNodeDiscoverer interface {
	DiscoverProjectedNodes(ctx context.Context, scope string, query string, maxItems int) ([]ProjectedNodeDiscoverItem, error)
}

type DetailedProjectedNodeDiscoverResult struct {
	Items         []ProjectedNodeDiscoverItem
	CandidatePool []CandidatePoolArtifact
}

type DetailedProjectedNodeDiscoverer interface {
	DiscoverProjectedNodesDetailed(ctx context.Context, scope string, query string, maxItems int) (DetailedProjectedNodeDiscoverResult, error)
}

type GovernedMemoryCandidate struct {
	FactKey         string
	FactValue       string
	SourceText      string
	CandidateSource string
	SourceChannel   string
}

type CandidateGovernanceDecision struct {
	PersistenceDisposition string
	ShouldPersist          bool
	HardDeny               bool
	ReasonCode             string
	RiskMotifs             []string
}

type CandidateGovernanceEvaluator interface {
	EvaluateCandidate(ctx context.Context, candidate GovernedMemoryCandidate) (CandidateGovernanceDecision, error)
}

type ContinuityParitySeedSpec struct {
	CurrentPath      string `json:"current_path,omitempty"`
	SuppressedPath   string `json:"suppressed_path,omitempty"`
	DistractorPath   string `json:"distractor_path,omitempty"`
	CanonicalFactKey string `json:"canonical_fact_key,omitempty"`
}

type SeedManifestRecord struct {
	ScenarioID              string `json:"scenario_id"`
	SeedGroup               string `json:"seed_group"`
	SeedPath                string `json:"seed_path"`
	AuthorityClass          string `json:"authority_class"`
	ValidatedWriteSupported bool   `json:"validated_write_supported"`
	FactKey                 string `json:"fact_key,omitempty"`
	CanonicalFactKey        string `json:"canonical_fact_key,omitempty"`
	FactValue               string `json:"fact_value,omitempty"`
	AnchorTupleKey          string `json:"anchor_tuple_key,omitempty"`
	NodeID                  string `json:"node_id,omitempty"`
	SourceKind              string `json:"source_kind,omitempty"`
	SourceRef               string `json:"source_ref,omitempty"`
	LineageStatus           string `json:"lineage_status,omitempty"`
	SupersedesInspectionID  string `json:"supersedes_inspection_id,omitempty"`
}
