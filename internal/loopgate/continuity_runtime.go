package loopgate

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"morph/internal/config"
	"morph/internal/secrets"
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
	CandidatePanelSize       int    `json:"candidate_panel_size"`
	DecompositionPreference  string `json:"decomposition_preference"`
	ReviewPreference         string `json:"review_preference"`
	SoftMorphlingConcurrency int    `json:"soft_morphling_concurrency"`
	BatchingPreference       string `json:"batching_preference"`
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

func normalizeGoalSummarySlug(rawGoalSummary string) string {
	lowerGoalSummary := strings.ToLower(strings.TrimSpace(rawGoalSummary))
	if lowerGoalSummary == "" {
		return "empty_goal"
	}
	var slugBuilder strings.Builder
	lastWasUnderscore := false
	for _, goalRune := range lowerGoalSummary {
		switch {
		case goalRune >= 'a' && goalRune <= 'z':
			slugBuilder.WriteRune(goalRune)
			lastWasUnderscore = false
		case goalRune >= '0' && goalRune <= '9':
			slugBuilder.WriteRune(goalRune)
			lastWasUnderscore = false
		default:
			if !lastWasUnderscore {
				slugBuilder.WriteRune('_')
				lastWasUnderscore = true
			}
		}
	}
	slugValue := strings.Trim(slugBuilder.String(), "_")
	if slugValue == "" {
		return "empty_goal"
	}
	return slugValue
}

func normalizeGoalFamily(goalSummary string, goalAliases config.GoalAliases) continuityGoalNormalization {
	normalizedSummarySlug := normalizeGoalSummarySlug(goalSummary)
	lowerGoalSummary := strings.ToLower(strings.TrimSpace(goalSummary))

	for _, goalType := range config.GoalTypeKeys(goalAliases) {
		for _, rawAlias := range goalAliases.Aliases[goalType] {
			normalizedAlias := config.NormalizeGoalAliasPublic(rawAlias)
			aliasNeedle := strings.ReplaceAll(normalizedAlias, "_", " ")
			if strings.Contains(lowerGoalSummary, aliasNeedle) || normalizedSummarySlug == normalizedAlias {
				return continuityGoalNormalization{
					GoalType:             goalType,
					GoalFamilyID:         goalType + ":" + normalizedAlias,
					NormalizationVersion: continuityNormalizationVersion,
					AliasMatched:         true,
					AliasKey:             normalizedAlias,
				}
			}
		}
	}

	switch {
	case strings.Contains(lowerGoalSummary, "security") || strings.Contains(lowerGoalSummary, "hardening") || strings.Contains(lowerGoalSummary, "threat"):
		return fallbackGoalNormalization(goalTypeSecurityHardening, normalizedSummarySlug)
	case strings.Contains(lowerGoalSummary, "architecture") || strings.Contains(lowerGoalSummary, "system design") || strings.Contains(lowerGoalSummary, "design"):
		return fallbackGoalNormalization(goalTypeArchitecturePlanning, normalizedSummarySlug)
	case strings.Contains(lowerGoalSummary, "readme") || strings.Contains(lowerGoalSummary, "docs") || strings.Contains(lowerGoalSummary, "documentation") || strings.Contains(lowerGoalSummary, "rfc edit"):
		return fallbackGoalNormalization(goalTypeDocumentationUpdate, normalizedSummarySlug)
	case strings.Contains(lowerGoalSummary, "bug") || strings.Contains(lowerGoalSummary, "incident") || strings.Contains(lowerGoalSummary, "debug") || strings.Contains(lowerGoalSummary, "failure"):
		return fallbackGoalNormalization(goalTypeDebuggingInvestigation, normalizedSummarySlug)
	case strings.Contains(lowerGoalSummary, "deadline") || strings.Contains(lowerGoalSummary, "calendar") || strings.Contains(lowerGoalSummary, "reminder"):
		return fallbackGoalNormalization(goalTypeSchedulingCommitment, normalizedSummarySlug)
	case strings.Contains(lowerGoalSummary, "review") || strings.Contains(lowerGoalSummary, "design doc") || strings.Contains(lowerGoalSummary, "rfc"):
		return fallbackGoalNormalization(goalTypeTechnicalReview, normalizedSummarySlug)
	case strings.Contains(lowerGoalSummary, "research") || strings.Contains(lowerGoalSummary, "option") || strings.Contains(lowerGoalSummary, "recommendation") || strings.Contains(lowerGoalSummary, "summary"):
		return fallbackGoalNormalization(goalTypeResearchSynthesis, normalizedSummarySlug)
	case strings.Contains(lowerGoalSummary, "config") || strings.Contains(lowerGoalSummary, "preference") || strings.Contains(lowerGoalSummary, "setup"):
		return fallbackGoalNormalization(goalTypePreferenceOrConfigUpdate, normalizedSummarySlug)
	case strings.Contains(lowerGoalSummary, "implement") || strings.Contains(lowerGoalSummary, "change"):
		return fallbackGoalNormalization(goalTypeImplementationChange, normalizedSummarySlug)
	default:
		return fallbackGoalNormalization(goalTypeWorkflowFollowup, normalizedSummarySlug)
	}
}

func fallbackGoalNormalization(goalType string, summarySlug string) continuityGoalNormalization {
	return continuityGoalNormalization{
		GoalType:             goalType,
		GoalFamilyID:         goalType + ":fallback_" + summarySlug,
		NormalizationVersion: continuityNormalizationVersion,
		NeedsAliasCuration:   true,
	}
}

func redactedWakeSummary(rawValue string) string {
	trimmedValue := secrets.RedactText(strings.TrimSpace(rawValue))
	if trimmedValue == "" {
		return ""
	}
	if len(trimmedValue) > 96 {
		trimmedValue = trimmedValue[:96]
	}
	trimmedValue = strings.ReplaceAll(trimmedValue, "\n", " ")
	return trimmedValue
}

func itemKindID(itemKind string, itemID string) string {
	return itemKind + ":" + itemID
}

func stableWakeEntryLess(leftEntry continuityDiagnosticWakeEntry, rightEntry continuityDiagnosticWakeEntry) bool {
	if leftEntry.ItemKind != rightEntry.ItemKind {
		return leftEntry.ItemKind < rightEntry.ItemKind
	}
	return leftEntry.ItemID < rightEntry.ItemID
}

func writeJSONArtifact(path string, payload interface{}) error {
	payloadBytes, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return memoryWritePrivateJSONAtomically(path, payloadBytes)
}

func memoryWritePrivateJSONAtomically(targetPath string, fileContents []byte) error {
	parentDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(parentDir, 0o700); err != nil {
		return err
	}
	tempFileHandle, err := os.CreateTemp(parentDir, ".tmp-*")
	if err != nil {
		return err
	}
	tempPath := tempFileHandle.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()
	if _, err := tempFileHandle.Write(fileContents); err != nil {
		_ = tempFileHandle.Close()
		return err
	}
	if len(fileContents) == 0 || fileContents[len(fileContents)-1] != '\n' {
		if _, err := tempFileHandle.Write([]byte("\n")); err != nil {
			_ = tempFileHandle.Close()
			return err
		}
	}
	if err := tempFileHandle.Sync(); err != nil {
		_ = tempFileHandle.Close()
		return err
	}
	if err := tempFileHandle.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, targetPath); err != nil {
		return err
	}
	parentHandle, err := os.Open(parentDir)
	if err == nil {
		_ = parentHandle.Sync()
		_ = parentHandle.Close()
	}
	return nil
}

func hashStableJSONIdentifier(payload interface{}) string {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	payloadHash := sha256.Sum256(payloadBytes)
	return hex.EncodeToString(payloadHash[:8])
}

func runtimeConfigToResolvedProfile(runtimeConfig config.RuntimeConfig) continuityResolvedProfileConfig {
	return continuityResolvedProfileConfig{
		CandidatePanelSize:       runtimeConfig.Memory.CandidatePanelSize,
		DecompositionPreference:  runtimeConfig.Memory.DecompositionPreference,
		ReviewPreference:         runtimeConfig.Memory.ReviewPreference,
		SoftMorphlingConcurrency: runtimeConfig.Memory.SoftMorphlingConcurrency,
		BatchingPreference:       runtimeConfig.Memory.BatchingPreference,
	}
}

func runtimeConfigCorrections(runtimeConfig config.RuntimeConfig, nowUTC time.Time) []continuityCorrectionRecord {
	correctionRecords := make([]continuityCorrectionRecord, 0, len(runtimeConfig.Memory.Corrections))
	for _, rawCorrection := range runtimeConfig.Memory.Corrections {
		createdAtUTC := strings.TrimSpace(rawCorrection.CreatedAtUTC)
		correctionID := strings.TrimSpace(rawCorrection.ID)
		if correctionID == "" {
			correctionID = makeEventPayloadID("corr", rawCorrection)
		}
		activeState := correctionStateActive
		if strings.TrimSpace(rawCorrection.StrengthClass) == correctionStrengthPreference {
			referenceTime := parseTimeOrZero(createdAtUTC)
			if reviewTime := parseTimeOrZero(rawCorrection.ReviewAtUTC); !reviewTime.IsZero() {
				referenceTime = reviewTime
			}
			if !referenceTime.IsZero() && nowUTC.Sub(referenceTime) > 90*24*time.Hour {
				activeState = correctionStateInactive
			}
		}
		correctionRecords = append(correctionRecords, continuityCorrectionRecord{
			SchemaVersion:           continuityMemorySchemaVersion,
			CorrectionID:            correctionID,
			Type:                    strings.TrimSpace(rawCorrection.Type),
			Scope:                   strings.TrimSpace(rawCorrection.Scope),
			GoalType:                strings.TrimSpace(rawCorrection.GoalType),
			GoalFamilyID:            strings.TrimSpace(rawCorrection.GoalFamilyID),
			TargetRuleClass:         strings.TrimSpace(rawCorrection.TargetRuleClass),
			TargetDerivationStage:   strings.TrimSpace(rawCorrection.TargetDerivationStage),
			TargetOutputKind:        strings.TrimSpace(rawCorrection.TargetOutputKind),
			CorrectionStrengthClass: strings.TrimSpace(rawCorrection.StrengthClass),
			Precedence:              100,
			ActiveState:             activeState,
			CreatedAtUTC:            createdAtUTC,
			ReasonSummary:           strings.TrimSpace(rawCorrection.Reason),
			InterceptedProposal:     strings.TrimSpace(rawCorrection.InterceptedProposal),
			InterceptedStage:        strings.TrimSpace(rawCorrection.InterceptedStage),
			ReviewAtUTC:             strings.TrimSpace(rawCorrection.ReviewAtUTC),
		})
	}
	sort.Slice(correctionRecords, func(leftIndex int, rightIndex int) bool {
		return correctionRecords[leftIndex].CorrectionID < correctionRecords[rightIndex].CorrectionID
	})
	return correctionRecords
}

func buildResolvedProfileSnapshot(runtimeConfig config.RuntimeConfig, nowUTC time.Time) continuityResolvedProfileSnapshot {
	activeCorrections := runtimeConfigCorrections(runtimeConfig, nowUTC)
	filteredCorrections := make([]continuityCorrectionRecord, 0, len(activeCorrections))
	for _, correctionRecord := range activeCorrections {
		if correctionRecord.ActiveState == correctionStateActive {
			filteredCorrections = append(filteredCorrections, correctionRecord)
		}
	}
	return continuityResolvedProfileSnapshot{
		SchemaVersion:     continuityMemorySchemaVersion,
		ResolutionVersion: continuityResolutionVersion,
		CreatedAtUTC:      nowUTC.Format(time.RFC3339Nano),
		ExplicitConfig:    runtimeConfigToResolvedProfile(runtimeConfig),
		ActiveCorrections: filteredCorrections,
		ActiveScopedRules: []continuityLearnedRuleRecord{},
		ActiveGlobalRules: []continuityLearnedRuleRecord{},
	}
}

func buildRankingCache(currentState continuityMemoryState, nowUTC time.Time) continuityRankingCache {
	rankingEntries := make([]continuityRankingCacheEntry, 0, len(currentState.Distillates)+len(currentState.ResonateKeys))
	for _, distillateRecord := range currentState.Distillates {
		rankingEntries = append(rankingEntries, continuityRankingCacheEntry{
			ItemKind:         wakeEntryKindDistillate,
			ItemID:           distillateRecord.DistillateID,
			GoalFamilyID:     distillateRecord.GoalFamilyID,
			Scope:            distillateRecord.Scope,
			RetentionScore:   distillateRecord.RetentionScore,
			EffectiveHotness: distillateRecord.EffectiveHotness,
		})
	}
	for _, resonateKeyRecord := range currentState.ResonateKeys {
		rankingEntries = append(rankingEntries, continuityRankingCacheEntry{
			ItemKind:         wakeEntryKindResonateKey,
			ItemID:           resonateKeyRecord.KeyID,
			GoalFamilyID:     resonateKeyRecord.GoalFamilyID,
			Scope:            resonateKeyRecord.Scope,
			RetentionScore:   resonateKeyRecord.RetentionScore,
			EffectiveHotness: resonateKeyRecord.EffectiveHotness,
		})
	}
	sort.Slice(rankingEntries, func(leftIndex int, rightIndex int) bool {
		if rankingEntries[leftIndex].EffectiveHotness != rankingEntries[rightIndex].EffectiveHotness {
			return rankingEntries[leftIndex].EffectiveHotness > rankingEntries[rightIndex].EffectiveHotness
		}
		if rankingEntries[leftIndex].RetentionScore != rankingEntries[rightIndex].RetentionScore {
			return rankingEntries[leftIndex].RetentionScore > rankingEntries[rightIndex].RetentionScore
		}
		return itemKindID(rankingEntries[leftIndex].ItemKind, rankingEntries[leftIndex].ItemID) < itemKindID(rankingEntries[rightIndex].ItemKind, rankingEntries[rightIndex].ItemID)
	})
	return continuityRankingCache{
		SchemaVersion:     continuityMemorySchemaVersion,
		ResolutionVersion: continuityResolutionVersion,
		CreatedAtUTC:      nowUTC.Format(time.RFC3339Nano),
		Entries:           rankingEntries,
	}
}

func buildRevalidationTickets(runtimeConfig config.RuntimeConfig, nowUTC time.Time) []continuityRevalidationTicket {
	correctionRecords := runtimeConfigCorrections(runtimeConfig, nowUTC)
	revalidationTickets := make([]continuityRevalidationTicket, 0, len(correctionRecords))
	for _, correctionRecord := range correctionRecords {
		if correctionRecord.CorrectionStrengthClass != correctionStrengthPreference {
			continue
		}
		referenceTime := parseTimeOrZero(correctionRecord.CreatedAtUTC)
		if reviewTime := parseTimeOrZero(correctionRecord.ReviewAtUTC); !reviewTime.IsZero() {
			referenceTime = reviewTime
		}
		if referenceTime.IsZero() {
			continue
		}
		inactiveAge := nowUTC.Sub(referenceTime)
		if inactiveAge < 60*24*time.Hour {
			continue
		}
		status := revalidationStatusQueued
		if inactiveAge >= 90*24*time.Hour {
			status = revalidationStatusExpired
		}
		revalidationTickets = append(revalidationTickets, continuityRevalidationTicket{
			SchemaVersion: continuityMemorySchemaVersion,
			RevalidationID: makeEventPayloadID("reval", struct {
				CorrectionID string `json:"correction_id"`
				Status       string `json:"status"`
			}{
				CorrectionID: correctionRecord.CorrectionID,
				Status:       status,
			}),
			CorrectionID: correctionRecord.CorrectionID,
			Scope:        correctionRecord.Scope,
			GoalType:     correctionRecord.GoalType,
			GoalFamilyID: correctionRecord.GoalFamilyID,
			CreatedAtUTC: nowUTC.Format(time.RFC3339Nano),
			DueAtUTC:     referenceTime.Add(60 * 24 * time.Hour).Format(time.RFC3339Nano),
			Status:       status,
			Reason:       "preference_correction_inactive_without_reaffirmation",
		})
	}
	sort.Slice(revalidationTickets, func(leftIndex int, rightIndex int) bool {
		return revalidationTickets[leftIndex].CorrectionID < revalidationTickets[rightIndex].CorrectionID
	})
	return revalidationTickets
}

func newDiagnosticWakeReportID(nowUTC time.Time) string {
	return "wake_diag_" + nowUTC.Format("20060102T150405Z")
}

func timeBandKeyFor(rawTimestamp string) string {
	parsedTime := parseTimeOrZero(rawTimestamp)
	if parsedTime.IsZero() {
		return "unknown"
	}
	yearValue, weekValue := parsedTime.ISOWeek()
	return fmt.Sprintf("%04d-%02d", yearValue, weekValue)
}

func writeContinuityArtifacts(memoryPaths continuityMemoryPaths, currentState continuityMemoryState, runtimeConfig config.RuntimeConfig, nowUTC time.Time) error {
	if err := memoryPaths.ensure(); err != nil {
		return err
	}
	nowUTC = nowUTC.UTC()

	for _, derivedDir := range []string{
		memoryPaths.DistillatesDir,
		memoryPaths.WakeRuntimeDir,
		memoryPaths.WakeDiagnosticDir,
		memoryPaths.ProfilesCorrectionsDir,
		memoryPaths.ProfilesRevalidationDir,
	} {
		if err := removeDerivedJSONArtifacts(derivedDir); err != nil {
			return err
		}
	}

	goalSnapshot := buildGoalsCurrentSnapshot(currentState)
	if err := writeJSONArtifact(memoryPaths.GoalsCurrentPath, goalSnapshot); err != nil {
		return err
	}
	taskSnapshot := buildTasksCurrentSnapshot(currentState)
	if err := writeJSONArtifact(memoryPaths.TasksCurrentPath, taskSnapshot); err != nil {
		return err
	}
	reviewSnapshot := buildReviewsCurrentSnapshot(currentState)
	if err := writeJSONArtifact(memoryPaths.ReviewsCurrentPath, reviewSnapshot); err != nil {
		return err
	}
	resolvedProfileSnapshot := buildResolvedProfileSnapshot(runtimeConfig, nowUTC)
	if err := writeJSONArtifact(memoryPaths.ProfileResolvedPath, resolvedProfileSnapshot); err != nil {
		return err
	}
	rankingCache := buildRankingCache(currentState, nowUTC)
	if err := writeJSONArtifact(memoryPaths.RankingCachePath, rankingCache); err != nil {
		return err
	}
	for _, correctionRecord := range resolvedProfileSnapshot.ActiveCorrections {
		if err := writeJSONArtifact(filepath.Join(memoryPaths.ProfilesCorrectionsDir, correctionRecord.CorrectionID+".json"), correctionRecord); err != nil {
			return err
		}
	}
	for _, revalidationTicket := range buildRevalidationTickets(runtimeConfig, nowUTC) {
		if err := writeJSONArtifact(filepath.Join(memoryPaths.ProfilesRevalidationDir, revalidationTicket.RevalidationID+".json"), revalidationTicket); err != nil {
			return err
		}
	}
	for _, distillateRecord := range currentState.Distillates {
		if err := writeJSONArtifact(filepath.Join(memoryPaths.DistillatesDir, distillateRecord.DistillateID+".json"), distillateRecord); err != nil {
			return err
		}
	}
	if strings.TrimSpace(currentState.WakeState.ID) != "" {
		if err := writeJSONArtifact(filepath.Join(memoryPaths.WakeRuntimeDir, currentState.WakeState.ID+".json"), currentState.WakeState); err != nil {
			return err
		}
	}
	if strings.TrimSpace(currentState.DiagnosticWake.ReportID) != "" {
		if err := writeJSONArtifact(filepath.Join(memoryPaths.WakeDiagnosticDir, currentState.DiagnosticWake.ReportID+".json"), currentState.DiagnosticWake); err != nil {
			return err
		}
	}
	return nil
}

func buildGoalsCurrentSnapshot(currentState continuityMemoryState) map[string]interface{} {
	goalsByID := map[string]string{}
	for _, distillateRecord := range activeLoopgateDistillates(currentState) {
		for _, goalOp := range distillateRecord.GoalOps {
			switch goalOp.Action {
			case "opened":
				goalsByID[goalOp.GoalID] = goalOp.Text
			case "closed":
				delete(goalsByID, goalOp.GoalID)
			}
		}
	}
	goalIDs := make([]string, 0, len(goalsByID))
	for goalID := range goalsByID {
		goalIDs = append(goalIDs, goalID)
	}
	sort.Strings(goalIDs)
	goals := make([]map[string]string, 0, len(goalIDs))
	for _, goalID := range goalIDs {
		goals = append(goals, map[string]string{
			"goal_id": goalID,
			"text":    goalsByID[goalID],
		})
	}
	return map[string]interface{}{
		"schema_version": continuityMemorySchemaVersion,
		"goals":          goals,
	}
}

func buildTasksCurrentSnapshot(currentState continuityMemoryState) map[string]interface{} {
	itemsByID := map[string]MemoryWakeStateOpenItem{}
	for _, distillateRecord := range activeLoopgateDistillates(currentState) {
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
				itemsByID[itemOp.ItemID] = taskMetadata
			case "closed":
				delete(itemsByID, itemOp.ItemID)
			case todoItemOpStatusSet:
				if existingItem, ok := itemsByID[itemOp.ItemID]; ok {
					if normalized := normalizeExplicitTodoWorkflowStatus(itemOp.Status); normalized != "" {
						existingItem.Status = normalized
						itemsByID[itemOp.ItemID] = existingItem
					}
				}
			}
		}
	}
	itemIDs := make([]string, 0, len(itemsByID))
	for itemID := range itemsByID {
		itemIDs = append(itemIDs, itemID)
	}
	sort.Strings(itemIDs)
	items := make([]map[string]string, 0, len(itemIDs))
	for _, itemID := range itemIDs {
		itemRecord := itemsByID[itemID]
		status := itemRecord.Status
		if status == "" {
			status = explicitTodoWorkflowStatusTodo
		}
		items = append(items, map[string]string{
			"item_id":           itemID,
			"text":              itemRecord.Text,
			"task_kind":         itemRecord.TaskKind,
			"source_kind":       itemRecord.SourceKind,
			"next_step":         itemRecord.NextStep,
			"scheduled_for_utc": itemRecord.ScheduledForUTC,
			"execution_class":   itemRecord.ExecutionClass,
			"created_at_utc":    itemRecord.CreatedAtUTC,
			"status":            status,
		})
	}
	return map[string]interface{}{
		"schema_version": continuityMemorySchemaVersion,
		"tasks":          items,
	}
}

func buildReviewsCurrentSnapshot(currentState continuityMemoryState) map[string]interface{} {
	reviews := make([]map[string]string, 0, len(currentState.Inspections))
	for _, inspectionRecord := range currentState.Inspections {
		if inspectionRecord.Review.Status == continuityReviewStatusPendingReview {
			reviews = append(reviews, map[string]string{
				"inspection_id": inspectionRecord.InspectionID,
				"thread_id":     inspectionRecord.ThreadID,
				"review_status": inspectionRecord.Review.Status,
			})
		}
	}
	sort.Slice(reviews, func(leftIndex int, rightIndex int) bool {
		return reviews[leftIndex]["inspection_id"] < reviews[rightIndex]["inspection_id"]
	})
	return map[string]interface{}{
		"schema_version": continuityMemorySchemaVersion,
		"reviews":        reviews,
	}
}

func buildDerivationSignature(observedPacket continuityObservedPacket) string {
	return hashStableJSONIdentifier(observedPacket)
}

func importanceBase(runtimeConfig config.RuntimeConfig, userImportance string) int {
	switch userImportance {
	case "critical":
		return runtimeConfig.Memory.Scoring.ImportanceBase.Critical
	case "not_important":
		return runtimeConfig.Memory.Scoring.ImportanceBase.NotImportant
	default:
		return runtimeConfig.Memory.Scoring.ImportanceBase.SomewhatImportant
	}
}

func hotnessBase(runtimeConfig config.RuntimeConfig, userImportance string) int {
	switch userImportance {
	case "critical":
		return runtimeConfig.Memory.Scoring.HotnessBase.Critical
	case "not_important":
		return runtimeConfig.Memory.Scoring.HotnessBase.NotImportant
	default:
		return runtimeConfig.Memory.Scoring.HotnessBase.SomewhatImportant
	}
}

func parseTimeOrZero(rawTimestamp string) time.Time {
	if strings.TrimSpace(rawTimestamp) == "" {
		return time.Time{}
	}
	parsedTime, err := time.Parse(time.RFC3339Nano, rawTimestamp)
	if err != nil {
		return time.Time{}
	}
	return parsedTime
}

func deriveMemoryState(effectiveHotness int, lineageStatus string) string {
	switch lineageStatus {
	case continuityLineageStatusPurged:
		return memoryStatePurged
	case continuityLineageStatusTombstoned:
		return memoryStateTombstoned
	}
	switch {
	case effectiveHotness >= 60:
		return memoryStateHot
	case effectiveHotness >= 30:
		return memoryStateWarm
	default:
		return memoryStateCold
	}
}

func makeEventPayloadID(prefix string, payload interface{}) string {
	return prefix + "_" + hashStableJSONIdentifier(payload)
}

func removeDerivedJSONArtifacts(dir string) error {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, dirEntry := range dirEntries {
		if dirEntry.IsDir() {
			continue
		}
		if filepath.Ext(dirEntry.Name()) != ".json" {
			continue
		}
		if err := os.Remove(filepath.Join(dir, dirEntry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func appendPrivateJSONL(path string, payload interface{}) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	payloadBytes = append(payloadBytes, '\n')
	fileHandle, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := fileHandle.Write(payloadBytes); err != nil {
		_ = fileHandle.Close()
		return err
	}
	if err := fileHandle.Sync(); err != nil {
		_ = fileHandle.Close()
		return err
	}
	return fileHandle.Close()
}

func appendContinuityMutationEvents(memoryPaths continuityMemoryPaths, mutationEvents continuityMutationEvents) error {
	if err := memoryPaths.ensure(); err != nil {
		return err
	}
	for _, continuityEvent := range mutationEvents.Continuity {
		if err := appendPrivateJSONL(memoryPaths.ContinuityEventsPath, continuityEvent); err != nil {
			return err
		}
	}
	for _, goalEvent := range mutationEvents.Goal {
		if err := appendPrivateJSONL(memoryPaths.GoalEventsPath, goalEvent); err != nil {
			return err
		}
	}
	for _, profileEvent := range mutationEvents.Profile {
		if err := appendPrivateJSONL(memoryPaths.ProfileEventsPath, profileEvent); err != nil {
			return err
		}
	}
	return nil
}

func replayContinuityMemoryStateFromEvents(memoryPaths continuityMemoryPaths) (continuityMemoryState, error) {
	replayedState := newEmptyContinuityMemoryState()
	if err := replayJSONL(memoryPaths.ContinuityEventsPath, func(rawLine []byte) error {
		var continuityEvent continuityAuthoritativeEvent
		decoder := json.NewDecoder(strings.NewReader(string(rawLine)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&continuityEvent); err != nil {
			return err
		}
		switch continuityEvent.EventType {
		case "continuity_inspection_recorded":
			if continuityEvent.Inspection == nil {
				return fmt.Errorf("continuity inspection event %q missing inspection", continuityEvent.EventID)
			}
			if _, found := replayedState.Inspections[continuityEvent.Inspection.InspectionID]; found {
				return fmt.Errorf("duplicate continuity inspection %q", continuityEvent.Inspection.InspectionID)
			}
			replayedState.Inspections[continuityEvent.Inspection.InspectionID] = cloneContinuityInspectionRecord(*continuityEvent.Inspection)
			if continuityEvent.Distillate != nil {
				replayedState.Distillates[continuityEvent.Distillate.DistillateID] = cloneContinuityDistillateRecord(*continuityEvent.Distillate)
			}
			if continuityEvent.ResonateKey != nil {
				replayedState.ResonateKeys[continuityEvent.ResonateKey.KeyID] = cloneContinuityResonateKeyRecord(*continuityEvent.ResonateKey)
			}
		case "memory_fact_remembered":
			if continuityEvent.Inspection == nil {
				return fmt.Errorf("memory fact event %q missing inspection", continuityEvent.EventID)
			}
			if continuityEvent.Distillate == nil {
				return fmt.Errorf("memory fact event %q missing distillate", continuityEvent.EventID)
			}
			if continuityEvent.ResonateKey == nil {
				return fmt.Errorf("memory fact event %q missing resonate_key", continuityEvent.EventID)
			}
			replayedState.Inspections[continuityEvent.Inspection.InspectionID] = cloneContinuityInspectionRecord(*continuityEvent.Inspection)
			replayedState.Distillates[continuityEvent.Distillate.DistillateID] = cloneContinuityDistillateRecord(*continuityEvent.Distillate)
			replayedState.ResonateKeys[continuityEvent.ResonateKey.KeyID] = cloneContinuityResonateKeyRecord(*continuityEvent.ResonateKey)
		case "todo_item_added":
			if continuityEvent.Inspection == nil {
				return fmt.Errorf("todo add event %q missing inspection", continuityEvent.EventID)
			}
			if continuityEvent.Distillate == nil {
				return fmt.Errorf("todo add event %q missing distillate", continuityEvent.EventID)
			}
			if continuityEvent.ResonateKey == nil {
				return fmt.Errorf("todo add event %q missing resonate_key", continuityEvent.EventID)
			}
			replayedState.Inspections[continuityEvent.Inspection.InspectionID] = cloneContinuityInspectionRecord(*continuityEvent.Inspection)
			replayedState.Distillates[continuityEvent.Distillate.DistillateID] = cloneContinuityDistillateRecord(*continuityEvent.Distillate)
			replayedState.ResonateKeys[continuityEvent.ResonateKey.KeyID] = cloneContinuityResonateKeyRecord(*continuityEvent.ResonateKey)
		case "todo_item_completed":
			if continuityEvent.Inspection == nil {
				return fmt.Errorf("todo complete event %q missing inspection", continuityEvent.EventID)
			}
			if continuityEvent.Distillate == nil {
				return fmt.Errorf("todo complete event %q missing distillate", continuityEvent.EventID)
			}
			replayedState.Inspections[continuityEvent.Inspection.InspectionID] = cloneContinuityInspectionRecord(*continuityEvent.Inspection)
			replayedState.Distillates[continuityEvent.Distillate.DistillateID] = cloneContinuityDistillateRecord(*continuityEvent.Distillate)
		case "todo_item_status_changed":
			if continuityEvent.Inspection == nil {
				return fmt.Errorf("todo status event %q missing inspection", continuityEvent.EventID)
			}
			if continuityEvent.Distillate == nil {
				return fmt.Errorf("todo status event %q missing distillate", continuityEvent.EventID)
			}
			replayedState.Inspections[continuityEvent.Inspection.InspectionID] = cloneContinuityInspectionRecord(*continuityEvent.Inspection)
			replayedState.Distillates[continuityEvent.Distillate.DistillateID] = cloneContinuityDistillateRecord(*continuityEvent.Distillate)
		case "continuity_inspection_reviewed":
			inspectionRecord, found := replayedState.Inspections[continuityEvent.InspectionID]
			if !found {
				return fmt.Errorf("review event references unknown inspection %q", continuityEvent.InspectionID)
			}
			if continuityEvent.Review == nil {
				return fmt.Errorf("review event %q missing review", continuityEvent.EventID)
			}
			inspectionRecord.Review = *continuityEvent.Review
			replayedState.Inspections[continuityEvent.InspectionID] = inspectionRecord
		case "continuity_inspection_lineage_updated":
			inspectionRecord, found := replayedState.Inspections[continuityEvent.InspectionID]
			if !found {
				return fmt.Errorf("lineage event references unknown inspection %q", continuityEvent.InspectionID)
			}
			if continuityEvent.Lineage == nil {
				return fmt.Errorf("lineage event %q missing lineage", continuityEvent.EventID)
			}
			inspectionRecord.Lineage = *continuityEvent.Lineage
			stampContinuityDerivedArtifactsExcluded(&replayedState, inspectionRecord, parseTimeOrZero(inspectionRecord.Lineage.ChangedAtUTC))
			replayedState.Inspections[continuityEvent.InspectionID] = inspectionRecord
		default:
			return fmt.Errorf("unknown continuity event type %q", continuityEvent.EventType)
		}
		return nil
	}); err != nil {
		return continuityMemoryState{}, err
	}
	replayedState.SchemaVersion = continuityMemorySchemaVersion
	if err := replayedState.Validate(); err != nil {
		return continuityMemoryState{}, err
	}
	return canonicalizeContinuityMemoryState(replayedState), nil
}

func replayJSONL(path string, applyLine func([]byte) error) error {
	fileHandle, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fileHandle.Close()

	fileScanner := bufio.NewScanner(fileHandle)
	fileScanner.Buffer(make([]byte, 0, 1024*1024), 4*1024*1024)
	lineNumber := 0
	for fileScanner.Scan() {
		lineNumber++
		rawLine := bytes.TrimSpace(fileScanner.Bytes())
		if len(rawLine) == 0 {
			continue
		}
		if err := applyLine(rawLine); err != nil {
			return fmt.Errorf("%s line %d: %w", path, lineNumber, err)
		}
	}
	if err := fileScanner.Err(); err != nil {
		return err
	}
	return nil
}
