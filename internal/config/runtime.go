package config

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"morph/internal/identifiers"

	"gopkg.in/yaml.v3"
)

const runtimeConfigVersion = "1"

const DefaultSupersededLineageRetentionWindow = 30 * 24 * time.Hour
const DefaultMemoryBackend = "continuity_tcl"
const benchmarkOnlyMemoryBackendErrorSuffix = "is benchmark-only; runtime config currently supports continuity_tcl or hybrid"
const DefaultHybridEvidenceMaxItems = 2
const DefaultHybridEvidenceMaxHintBytes = 580
const defaultHybridEvidencePythonExecutable = "python3"
const defaultHybridEvidenceHelperScriptPath = "cmd/memorybench/rag_search.py"

// DiagnosticLogging configures optional text log files (slog) for local troubleshooting.
// DefaultLevel: error | warn | info | debug | trace (trace is finer than debug).
// Per-channel levels in Levels override DefaultLevel for that channel key:
// audit, server, client, socket, memory, ledger, model.
type DiagnosticLogging struct {
	Enabled      bool              `yaml:"enabled" json:"enabled"`
	DefaultLevel string            `yaml:"default_level" json:"default_level"`
	Directory    string            `yaml:"directory" json:"directory"`
	Files        DiagnosticFiles   `yaml:"files" json:"files"`
	Levels       map[string]string `yaml:"levels" json:"levels,omitempty"`
}

// DiagnosticFiles names operator log basenames (created under Directory).
type DiagnosticFiles struct {
	Audit  string `yaml:"audit" json:"audit"`
	Server string `yaml:"server" json:"server"`
	Client string `yaml:"client" json:"client"`
	Socket string `yaml:"socket" json:"socket"`
	Memory string `yaml:"memory" json:"memory"`
	Ledger string `yaml:"ledger" json:"ledger"`
	Model  string `yaml:"model" json:"model"`
}

// AuditLedgerHMACCheckpoint configures optional HMAC-signed checkpoint lines in the control-plane audit JSONL.
// When enabled, Loopgate appends audit.ledger.hmac_checkpoint after every IntervalEvents non-checkpoint audit events.
// IntervalEvents zero/unset defaults to 256 in applyRuntimeConfigDefaults; negative values are rejected at validate.
// The signing key is loaded via secret_ref (macOS: macos_keychain; CI/tests: env with account_name = env var name).
type AuditLedgerHMACCheckpoint struct {
	Enabled        bool                      `yaml:"enabled" json:"enabled"`
	IntervalEvents int                       `yaml:"interval_events" json:"interval_events"`
	SecretRef      *AuditLedgerHMACSecretRef `yaml:"secret_ref,omitempty" json:"secret_ref,omitempty"`
}

// AuditLedgerHMACSecretRef references secret material for audit ledger checkpoint HMAC (same shape as secrets.SecretRef).
type AuditLedgerHMACSecretRef struct {
	ID          string `yaml:"id" json:"id"`
	Backend     string `yaml:"backend" json:"backend"`
	AccountName string `yaml:"account_name" json:"account_name"`
	Scope       string `yaml:"scope" json:"scope"`
}

// ResolvedDirectory returns the log directory relative to repo root (after defaults).
func (d DiagnosticLogging) ResolvedDirectory() string {
	dir := strings.TrimSpace(d.Directory)
	if dir == "" {
		return "runtime/logs"
	}
	return dir
}

// LevelForChannel returns the configured level for a channel or DefaultLevel.
func (d DiagnosticLogging) LevelForChannel(channel string) string {
	if d.Levels != nil {
		if v, ok := d.Levels[channel]; ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return d.DefaultLevel
}

type RuntimeConfig struct {
	Version string `yaml:"version" json:"version"`
	Logging struct {
		AuditLedger struct {
			MaxEventBytes                 int                       `yaml:"max_event_bytes" json:"max_event_bytes"`
			RotateAtBytes                 int64                     `yaml:"rotate_at_bytes" json:"rotate_at_bytes"`
			SegmentDir                    string                    `yaml:"segment_dir" json:"segment_dir"`
			ManifestPath                  string                    `yaml:"manifest_path" json:"manifest_path"`
			VerifyClosedSegmentsOnStartup *bool                     `yaml:"verify_closed_segments_on_startup" json:"verify_closed_segments_on_startup"`
			HMACCheckpoint                AuditLedgerHMACCheckpoint `yaml:"hmac_checkpoint" json:"hmac_checkpoint"`
		} `yaml:"audit_ledger" json:"audit_ledger"`
		// Diagnostic is non-authoritative operator telemetry (text files under runtime/logs or runtime/state).
		// It must not replace loopgate_events.jsonl; never log secrets or raw tokens.
		Diagnostic DiagnosticLogging `yaml:"diagnostic" json:"diagnostic"`
	} `yaml:"logging" json:"logging"`
	Memory struct {
		Backend                  string `yaml:"backend" json:"backend"`
		CandidatePanelSize       int    `yaml:"candidate_panel_size" json:"candidate_panel_size"`
		DecompositionPreference  string `yaml:"decomposition_preference" json:"decomposition_preference"`
		ReviewPreference         string `yaml:"review_preference" json:"review_preference"`
		SoftMorphlingConcurrency int    `yaml:"soft_morphling_concurrency" json:"soft_morphling_concurrency"`
		BatchingPreference       string `yaml:"batching_preference" json:"batching_preference"`
		ExplicitFactWrites       struct {
			WindowSeconds       int `yaml:"window_seconds" json:"window_seconds"`
			MaxWritesPerSession int `yaml:"max_writes_per_session" json:"max_writes_per_session"`
			MaxWritesPerPeerUID int `yaml:"max_writes_per_peer_uid" json:"max_writes_per_peer_uid"`
			MaxValueBytes       int `yaml:"max_value_bytes" json:"max_value_bytes"`
		} `yaml:"explicit_fact_writes" json:"explicit_fact_writes"`
		Scoring struct {
			ImportanceBase struct {
				NotImportant      int `yaml:"not_important" json:"not_important"`
				SomewhatImportant int `yaml:"somewhat_important" json:"somewhat_important"`
				Critical          int `yaml:"critical" json:"critical"`
			} `yaml:"importance_base" json:"importance_base"`
			ApprovedGoalAnchor     int `yaml:"approved_goal_anchor" json:"approved_goal_anchor"`
			ExplicitUserBonus      int `yaml:"explicit_user_bonus" json:"explicit_user_bonus"`
			StalePenaltyResolved30 int `yaml:"stale_penalty_resolved_30d" json:"stale_penalty_resolved_30d"`
			HotnessBase            struct {
				NotImportant      int `yaml:"not_important" json:"not_important"`
				SomewhatImportant int `yaml:"somewhat_important" json:"somewhat_important"`
				Critical          int `yaml:"critical" json:"critical"`
			} `yaml:"hotness_base" json:"hotness_base"`
			ActiveGoalBonus                 int `yaml:"active_goal_bonus" json:"active_goal_bonus"`
			DueBonusWithin24H               int `yaml:"due_bonus_within_24h" json:"due_bonus_within_24h"`
			DueBonusWithin7D                int `yaml:"due_bonus_within_7d" json:"due_bonus_within_7d"`
			CurrentGoalMatchBonus           int `yaml:"current_goal_match_bonus" json:"current_goal_match_bonus"`
			StalePenaltyOverdue             int `yaml:"stale_penalty_overdue" json:"stale_penalty_overdue"`
			DuplicateFamilyPenalty          int `yaml:"duplicate_family_penalty" json:"duplicate_family_penalty"`
			PositiveSupportReviewedAccepted int `yaml:"positive_support_reviewed_accepted" json:"positive_support_reviewed_accepted"`
			NegativeTaskDismissal           int `yaml:"negative_task_dismissal" json:"negative_task_dismissal"`
			NegativeGoalRejection           int `yaml:"negative_goal_rejection" json:"negative_goal_rejection"`
			NegativeCompletionRejection     int `yaml:"negative_completion_rejection" json:"negative_completion_rejection"`
			PromotionThresholdActive        int `yaml:"promotion_threshold_active" json:"promotion_threshold_active"`
			PromotionThresholdEmerging      int `yaml:"promotion_threshold_emerging" json:"promotion_threshold_emerging"`
		} `yaml:"scoring" json:"scoring"`
		Corrections    []RuntimeMemoryCorrection `yaml:"corrections,omitempty" json:"corrections,omitempty"`
		HybridEvidence struct {
			PythonExecutable string `yaml:"python_executable" json:"python_executable"`
			HelperScriptPath string `yaml:"helper_script_path" json:"helper_script_path"`
			QdrantURL        string `yaml:"qdrant_url" json:"qdrant_url"`
			CollectionName   string `yaml:"collection_name" json:"collection_name"`
			EmbeddingModel   string `yaml:"embedding_model" json:"embedding_model"`
			RerankerModel    string `yaml:"reranker_model" json:"reranker_model"`
			MaxItems         int    `yaml:"max_items" json:"max_items"`
			MaxHintBytes     int    `yaml:"max_hint_bytes" json:"max_hint_bytes"`
		} `yaml:"hybrid_evidence" json:"hybrid_evidence"`
	} `yaml:"memory" json:"memory"`
	// Tenancy holds deployment-scoped identity for single-node enterprise prep.
	// Values are applied at control-session open (never taken from untrusted client JSON).
	Tenancy struct {
		DeploymentTenantID string `yaml:"deployment_tenant_id" json:"deployment_tenant_id"`
		DeploymentUserID   string `yaml:"deployment_user_id" json:"deployment_user_id"`
	} `yaml:"tenancy" json:"tenancy"`
	// ControlPlane holds optional hardening for the local Unix-socket control plane.
	ControlPlane struct {
		// ExpectedSessionClientExecutable, when non-empty, requires POST /v1/session/open peers to
		// resolve to this absolute executable path (after filepath.Clean). Empty disables pinning.
		ExpectedSessionClientExecutable string `yaml:"expected_session_client_executable" json:"expected_session_client_executable"`
	} `yaml:"control_plane" json:"control_plane"`
}

type RuntimeMemoryCorrection struct {
	ID                    string `yaml:"id" json:"id"`
	Type                  string `yaml:"type" json:"type"`
	Scope                 string `yaml:"scope" json:"scope"`
	GoalType              string `yaml:"goal_type,omitempty" json:"goal_type,omitempty"`
	GoalFamilyID          string `yaml:"goal_family_id,omitempty" json:"goal_family_id,omitempty"`
	TargetRuleClass       string `yaml:"target_rule_class,omitempty" json:"target_rule_class,omitempty"`
	TargetDerivationStage string `yaml:"target_derivation_stage,omitempty" json:"target_derivation_stage,omitempty"`
	TargetOutputKind      string `yaml:"target_output_kind,omitempty" json:"target_output_kind,omitempty"`
	StrengthClass         string `yaml:"strength_class" json:"strength_class"`
	Reason                string `yaml:"reason,omitempty" json:"reason,omitempty"`
	InterceptedProposal   string `yaml:"intercepted_proposal,omitempty" json:"intercepted_proposal,omitempty"`
	InterceptedStage      string `yaml:"intercepted_stage,omitempty" json:"intercepted_stage,omitempty"`
	CreatedAtUTC          string `yaml:"created_at_utc,omitempty" json:"created_at_utc,omitempty"`
	ReviewAtUTC           string `yaml:"review_at_utc,omitempty" json:"review_at_utc,omitempty"`
}

func LoadRuntimeConfig(repoRoot string) (RuntimeConfig, error) {
	path := filepath.Join(repoRoot, "config", "runtime.yaml")
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			runtimeConfig := defaultRuntimeConfig()
			if err := ApplyDiagnosticLoggingOverride(repoRoot, &runtimeConfig); err != nil {
				return RuntimeConfig{}, err
			}
			if err := validateRuntimeConfig(repoRoot, runtimeConfig); err != nil {
				return RuntimeConfig{}, err
			}
			return runtimeConfig, nil
		}
		return RuntimeConfig{}, err
	}

	var runtimeConfig RuntimeConfig
	decoder := yaml.NewDecoder(bytes.NewReader(rawBytes))
	decoder.KnownFields(true)
	if err := decoder.Decode(&runtimeConfig); err != nil {
		return RuntimeConfig{}, err
	}
	applyRuntimeConfigDefaults(&runtimeConfig)
	if err := ApplyDiagnosticLoggingOverride(repoRoot, &runtimeConfig); err != nil {
		return RuntimeConfig{}, err
	}
	if err := validateRuntimeConfig(repoRoot, runtimeConfig); err != nil {
		return RuntimeConfig{}, err
	}
	return runtimeConfig, nil
}

func defaultRuntimeConfig() RuntimeConfig {
	return DefaultRuntimeConfig()
}

// DefaultRuntimeConfig returns a RuntimeConfig with all defaults applied.
func DefaultRuntimeConfig() RuntimeConfig {
	runtimeConfig := RuntimeConfig{}
	applyRuntimeConfigDefaults(&runtimeConfig)
	return runtimeConfig
}

func applyRuntimeConfigDefaults(runtimeConfig *RuntimeConfig) {
	if strings.TrimSpace(runtimeConfig.Version) == "" {
		runtimeConfig.Version = runtimeConfigVersion
	}
	if runtimeConfig.Logging.AuditLedger.MaxEventBytes <= 0 {
		runtimeConfig.Logging.AuditLedger.MaxEventBytes = 256 * 1024
	}
	if runtimeConfig.Logging.AuditLedger.RotateAtBytes <= 0 {
		runtimeConfig.Logging.AuditLedger.RotateAtBytes = 128 * 1024 * 1024
	}
	if strings.TrimSpace(runtimeConfig.Logging.AuditLedger.SegmentDir) == "" {
		runtimeConfig.Logging.AuditLedger.SegmentDir = "runtime/state/loopgate_event_segments"
	}
	if strings.TrimSpace(runtimeConfig.Logging.AuditLedger.ManifestPath) == "" {
		runtimeConfig.Logging.AuditLedger.ManifestPath = "runtime/state/loopgate_event_segments/manifest.jsonl"
	}
	if runtimeConfig.Logging.AuditLedger.VerifyClosedSegmentsOnStartup == nil {
		defaultVerifyClosedSegments := true
		runtimeConfig.Logging.AuditLedger.VerifyClosedSegmentsOnStartup = &defaultVerifyClosedSegments
	}
	hc := &runtimeConfig.Logging.AuditLedger.HMACCheckpoint
	// Default only the unset (zero) case so negative values fail validation instead of being coerced.
	if hc.Enabled && hc.IntervalEvents == 0 {
		hc.IntervalEvents = 256
	}
	d := &runtimeConfig.Logging.Diagnostic
	if strings.TrimSpace(d.DefaultLevel) == "" {
		d.DefaultLevel = "info"
	}
	if strings.TrimSpace(d.Directory) == "" {
		d.Directory = "runtime/logs"
	}
	if strings.TrimSpace(d.Files.Audit) == "" {
		d.Files.Audit = "audit.log"
	}
	if strings.TrimSpace(d.Files.Server) == "" {
		d.Files.Server = "server.log"
	}
	if strings.TrimSpace(d.Files.Client) == "" {
		d.Files.Client = "client.log"
	}
	if strings.TrimSpace(d.Files.Socket) == "" {
		d.Files.Socket = "socket.log"
	}
	if strings.TrimSpace(d.Files.Memory) == "" {
		d.Files.Memory = "memory.log"
	}
	if strings.TrimSpace(d.Files.Ledger) == "" {
		d.Files.Ledger = "ledger.log"
	}
	if strings.TrimSpace(d.Files.Model) == "" {
		d.Files.Model = "model.log"
	}
	if runtimeConfig.Memory.CandidatePanelSize <= 0 {
		runtimeConfig.Memory.CandidatePanelSize = 3
	}
	if strings.TrimSpace(runtimeConfig.Memory.Backend) == "" {
		runtimeConfig.Memory.Backend = DefaultMemoryBackend
	}
	if strings.TrimSpace(runtimeConfig.Memory.DecompositionPreference) == "" {
		runtimeConfig.Memory.DecompositionPreference = "hybrid_schema_guided"
	}
	if strings.TrimSpace(runtimeConfig.Memory.ReviewPreference) == "" {
		runtimeConfig.Memory.ReviewPreference = "risk_tiered"
	}
	if runtimeConfig.Memory.SoftMorphlingConcurrency <= 0 {
		runtimeConfig.Memory.SoftMorphlingConcurrency = 3
	}
	if strings.TrimSpace(runtimeConfig.Memory.BatchingPreference) == "" {
		runtimeConfig.Memory.BatchingPreference = "pause_on_wave_failure"
	}
	if runtimeConfig.Memory.ExplicitFactWrites.WindowSeconds <= 0 {
		runtimeConfig.Memory.ExplicitFactWrites.WindowSeconds = 60
	}
	if runtimeConfig.Memory.ExplicitFactWrites.MaxWritesPerSession <= 0 {
		// Large enough for model-driven bursts (many memory.remember calls in one turn);
		// still bounded so hostile or buggy clients cannot spam explicit writes.
		runtimeConfig.Memory.ExplicitFactWrites.MaxWritesPerSession = 50
	}
	if runtimeConfig.Memory.ExplicitFactWrites.MaxWritesPerPeerUID <= 0 {
		runtimeConfig.Memory.ExplicitFactWrites.MaxWritesPerPeerUID = 50
	}
	if runtimeConfig.Memory.ExplicitFactWrites.MaxValueBytes <= 0 {
		runtimeConfig.Memory.ExplicitFactWrites.MaxValueBytes = 128
	}
	if runtimeConfig.Memory.Scoring.ImportanceBase.SomewhatImportant == 0 {
		runtimeConfig.Memory.Scoring.ImportanceBase.NotImportant = 0
		runtimeConfig.Memory.Scoring.ImportanceBase.SomewhatImportant = 30
		runtimeConfig.Memory.Scoring.ImportanceBase.Critical = 60
	}
	if runtimeConfig.Memory.Scoring.ApprovedGoalAnchor == 0 {
		runtimeConfig.Memory.Scoring.ApprovedGoalAnchor = 25
	}
	if runtimeConfig.Memory.Scoring.ExplicitUserBonus == 0 {
		runtimeConfig.Memory.Scoring.ExplicitUserBonus = 25
	}
	if runtimeConfig.Memory.Scoring.StalePenaltyResolved30 == 0 {
		runtimeConfig.Memory.Scoring.StalePenaltyResolved30 = 20
	}
	if runtimeConfig.Memory.Scoring.HotnessBase.SomewhatImportant == 0 {
		runtimeConfig.Memory.Scoring.HotnessBase.NotImportant = 0
		runtimeConfig.Memory.Scoring.HotnessBase.SomewhatImportant = 20
		runtimeConfig.Memory.Scoring.HotnessBase.Critical = 35
	}
	if runtimeConfig.Memory.Scoring.ActiveGoalBonus == 0 {
		runtimeConfig.Memory.Scoring.ActiveGoalBonus = 25
	}
	if runtimeConfig.Memory.Scoring.DueBonusWithin24H == 0 {
		runtimeConfig.Memory.Scoring.DueBonusWithin24H = 25
	}
	if runtimeConfig.Memory.Scoring.DueBonusWithin7D == 0 {
		runtimeConfig.Memory.Scoring.DueBonusWithin7D = 10
	}
	if runtimeConfig.Memory.Scoring.CurrentGoalMatchBonus == 0 {
		runtimeConfig.Memory.Scoring.CurrentGoalMatchBonus = 20
	}
	if runtimeConfig.Memory.Scoring.StalePenaltyOverdue == 0 {
		runtimeConfig.Memory.Scoring.StalePenaltyOverdue = 10
	}
	if runtimeConfig.Memory.Scoring.DuplicateFamilyPenalty == 0 {
		runtimeConfig.Memory.Scoring.DuplicateFamilyPenalty = 15
	}
	if runtimeConfig.Memory.Scoring.PositiveSupportReviewedAccepted == 0 {
		runtimeConfig.Memory.Scoring.PositiveSupportReviewedAccepted = 12
	}
	if runtimeConfig.Memory.Scoring.NegativeTaskDismissal == 0 {
		runtimeConfig.Memory.Scoring.NegativeTaskDismissal = 8
	}
	if runtimeConfig.Memory.Scoring.NegativeGoalRejection == 0 {
		runtimeConfig.Memory.Scoring.NegativeGoalRejection = 10
	}
	if runtimeConfig.Memory.Scoring.NegativeCompletionRejection == 0 {
		runtimeConfig.Memory.Scoring.NegativeCompletionRejection = 15
	}
	if runtimeConfig.Memory.Scoring.PromotionThresholdEmerging == 0 {
		runtimeConfig.Memory.Scoring.PromotionThresholdEmerging = 2
	}
	if runtimeConfig.Memory.Scoring.PromotionThresholdActive == 0 {
		runtimeConfig.Memory.Scoring.PromotionThresholdActive = 3
	}
	if strings.TrimSpace(runtimeConfig.Memory.HybridEvidence.PythonExecutable) == "" {
		runtimeConfig.Memory.HybridEvidence.PythonExecutable = defaultHybridEvidencePythonExecutable
	}
	if strings.TrimSpace(runtimeConfig.Memory.HybridEvidence.HelperScriptPath) == "" {
		runtimeConfig.Memory.HybridEvidence.HelperScriptPath = defaultHybridEvidenceHelperScriptPath
	}
	if runtimeConfig.Memory.HybridEvidence.MaxItems <= 0 {
		runtimeConfig.Memory.HybridEvidence.MaxItems = DefaultHybridEvidenceMaxItems
	}
	if runtimeConfig.Memory.HybridEvidence.MaxHintBytes <= 0 {
		runtimeConfig.Memory.HybridEvidence.MaxHintBytes = DefaultHybridEvidenceMaxHintBytes
	}
}

func validateRuntimeConfig(repoRoot string, runtimeConfig RuntimeConfig) error {
	if strings.TrimSpace(runtimeConfig.Version) == "" {
		return fmt.Errorf("version is required")
	}
	if runtimeConfig.Logging.AuditLedger.MaxEventBytes <= 0 {
		return fmt.Errorf("logging.audit_ledger.max_event_bytes must be positive")
	}
	if runtimeConfig.Logging.AuditLedger.RotateAtBytes <= 0 {
		return fmt.Errorf("logging.audit_ledger.rotate_at_bytes must be positive")
	}
	if err := validateRuntimeInternalPath(runtimeConfig.Logging.AuditLedger.SegmentDir, true); err != nil {
		return fmt.Errorf("logging.audit_ledger.segment_dir %w", err)
	}
	if err := validateRuntimeInternalPath(runtimeConfig.Logging.AuditLedger.ManifestPath, false); err != nil {
		return fmt.Errorf("logging.audit_ledger.manifest_path %w", err)
	}
	if runtimeConfig.Logging.Diagnostic.Enabled {
		if err := validateDiagnosticLogDirectory(runtimeConfig.Logging.Diagnostic.Directory); err != nil {
			return fmt.Errorf("logging.diagnostic.directory %w", err)
		}
		if err := validateDiagnosticLevel(runtimeConfig.Logging.Diagnostic.DefaultLevel); err != nil {
			return fmt.Errorf("logging.diagnostic.default_level: %w", err)
		}
		for channel, level := range runtimeConfig.Logging.Diagnostic.Levels {
			if err := validateDiagnosticLevel(level); err != nil {
				return fmt.Errorf("logging.diagnostic.levels[%s]: %w", channel, err)
			}
		}
	}
	if runtimeConfig.Memory.CandidatePanelSize <= 0 {
		return fmt.Errorf("candidate_panel_size must be positive")
	}
	switch trimmedMemoryBackend := strings.TrimSpace(runtimeConfig.Memory.Backend); trimmedMemoryBackend {
	case DefaultMemoryBackend, "hybrid":
	default:
		switch trimmedMemoryBackend {
		case "rag_baseline":
			return fmt.Errorf("memory.backend %q %s", trimmedMemoryBackend, benchmarkOnlyMemoryBackendErrorSuffix)
		default:
			return fmt.Errorf("memory.backend must be %s or hybrid", DefaultMemoryBackend)
		}
	}
	if strings.TrimSpace(runtimeConfig.Memory.Backend) == "hybrid" {
		if err := validateHybridEvidenceConfig(repoRoot, runtimeConfig); err != nil {
			return err
		}
	}
	if runtimeConfig.Memory.SoftMorphlingConcurrency <= 0 {
		return fmt.Errorf("soft_morphling_concurrency must be positive")
	}
	if runtimeConfig.Memory.ExplicitFactWrites.WindowSeconds <= 0 {
		return fmt.Errorf("explicit_fact_writes.window_seconds must be positive")
	}
	if runtimeConfig.Memory.ExplicitFactWrites.MaxWritesPerSession <= 0 {
		return fmt.Errorf("explicit_fact_writes.max_writes_per_session must be positive")
	}
	if runtimeConfig.Memory.ExplicitFactWrites.MaxWritesPerPeerUID <= 0 {
		return fmt.Errorf("explicit_fact_writes.max_writes_per_peer_uid must be positive")
	}
	if runtimeConfig.Memory.ExplicitFactWrites.MaxValueBytes <= 0 {
		return fmt.Errorf("explicit_fact_writes.max_value_bytes must be positive")
	}
	if err := validateOptionalDeploymentIdentity("tenancy.deployment_tenant_id", runtimeConfig.Tenancy.DeploymentTenantID); err != nil {
		return err
	}
	if err := validateOptionalDeploymentIdentity("tenancy.deployment_user_id", runtimeConfig.Tenancy.DeploymentUserID); err != nil {
		return err
	}
	if err := validateExpectedSessionClientExecutable(runtimeConfig.ControlPlane.ExpectedSessionClientExecutable); err != nil {
		return err
	}
	if err := validateAuditLedgerHMACCheckpoint(runtimeConfig.Logging.AuditLedger.HMACCheckpoint); err != nil {
		return err
	}
	return nil
}

func validateAuditLedgerHMACCheckpoint(hc AuditLedgerHMACCheckpoint) error {
	if !hc.Enabled {
		return nil
	}
	if hc.IntervalEvents <= 0 {
		return fmt.Errorf("logging.audit_ledger.hmac_checkpoint.interval_events must be positive when enabled")
	}
	if hc.SecretRef == nil {
		return fmt.Errorf("logging.audit_ledger.hmac_checkpoint.secret_ref is required when enabled")
	}
	sr := hc.SecretRef
	if err := identifiers.ValidateSafeIdentifier("logging.audit_ledger.hmac_checkpoint.secret_ref.id", sr.ID); err != nil {
		return err
	}
	if err := identifiers.ValidateSafeIdentifier("logging.audit_ledger.hmac_checkpoint.secret_ref.backend", sr.Backend); err != nil {
		return err
	}
	if err := identifiers.ValidateSafeIdentifier("logging.audit_ledger.hmac_checkpoint.secret_ref.account_name", sr.AccountName); err != nil {
		return err
	}
	if err := identifiers.ValidateSafeIdentifier("logging.audit_ledger.hmac_checkpoint.secret_ref.scope", sr.Scope); err != nil {
		return err
	}
	return nil
}

func validateHybridEvidenceConfig(repoRoot string, runtimeConfig RuntimeConfig) error {
	hybridEvidenceConfig := runtimeConfig.Memory.HybridEvidence
	if strings.TrimSpace(hybridEvidenceConfig.QdrantURL) == "" {
		return fmt.Errorf("memory.hybrid_evidence.qdrant_url is required when memory.backend=hybrid")
	}
	if strings.TrimSpace(hybridEvidenceConfig.CollectionName) == "" {
		return fmt.Errorf("memory.hybrid_evidence.collection_name is required when memory.backend=hybrid")
	}
	if hybridEvidenceConfig.MaxItems < 1 || hybridEvidenceConfig.MaxItems > 5 {
		return fmt.Errorf("memory.hybrid_evidence.max_items must be between 1 and 5")
	}
	if hybridEvidenceConfig.MaxHintBytes < 64 || hybridEvidenceConfig.MaxHintBytes > 8192 {
		return fmt.Errorf("memory.hybrid_evidence.max_hint_bytes must be between 64 and 8192")
	}
	if _, err := resolveRuntimeExecutableReference(strings.TrimSpace(hybridEvidenceConfig.PythonExecutable)); err != nil {
		return fmt.Errorf("memory.hybrid_evidence.python_executable %w", err)
	}
	if _, err := resolveRuntimeRepoPath(repoRoot, strings.TrimSpace(hybridEvidenceConfig.HelperScriptPath)); err != nil {
		return fmt.Errorf("memory.hybrid_evidence.helper_script_path %w", err)
	}
	return nil
}

func resolveRuntimeExecutableReference(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("is required")
	}
	if strings.ContainsAny(trimmed, "\x00\n\r") {
		return "", fmt.Errorf("contains control characters")
	}
	if filepath.IsAbs(trimmed) {
		if _, err := os.Stat(trimmed); err != nil {
			return "", fmt.Errorf("is unavailable: %w", err)
		}
		return trimmed, nil
	}
	resolvedExecutablePath, err := exec.LookPath(trimmed)
	if err != nil {
		return "", fmt.Errorf("is unavailable: %w", err)
	}
	return resolvedExecutablePath, nil
}

func resolveRuntimeRepoPath(repoRoot string, rawPath string) (string, error) {
	trimmedPath := strings.TrimSpace(rawPath)
	if trimmedPath == "" {
		return "", fmt.Errorf("is required")
	}
	if strings.ContainsAny(trimmedPath, "\x00\n\r") {
		return "", fmt.Errorf("contains control characters")
	}
	if filepath.IsAbs(trimmedPath) {
		resolvedPath := filepath.Clean(trimmedPath)
		if _, err := os.Stat(resolvedPath); err != nil {
			return "", fmt.Errorf("is unavailable: %w", err)
		}
		return resolvedPath, nil
	}
	cleanedPath := filepath.Clean(trimmedPath)
	if cleanedPath == "." || cleanedPath == ".." || strings.HasPrefix(cleanedPath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("must stay within the repository root")
	}
	resolvedPath := filepath.Join(repoRoot, cleanedPath)
	if _, err := os.Stat(resolvedPath); err != nil {
		return "", fmt.Errorf("is unavailable: %w", err)
	}
	return resolvedPath, nil
}

const maxExpectedSessionClientExecutableRunes = 4096

func validateExpectedSessionClientExecutable(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	if len(trimmed) > maxExpectedSessionClientExecutableRunes {
		return fmt.Errorf("control_plane.expected_session_client_executable exceeds maximum length (%d)", maxExpectedSessionClientExecutableRunes)
	}
	if strings.ContainsAny(trimmed, "\x00\n\r") {
		return fmt.Errorf("control_plane.expected_session_client_executable contains control characters")
	}
	if !filepath.IsAbs(filepath.Clean(trimmed)) {
		return fmt.Errorf("control_plane.expected_session_client_executable must be an absolute path when set")
	}
	return nil
}

const maxDeploymentIdentityRunes = 256

// validateOptionalDeploymentIdentity allows empty (personal / unset) or a bounded opaque string
// without control characters. Enterprise IDs may be UUIDs, opaque slugs, or future email-shaped
// subjects; we deliberately avoid the stricter ValidateSafeIdentifier rules here.
func validateOptionalDeploymentIdentity(fieldName string, raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	if len(trimmed) > maxDeploymentIdentityRunes {
		return fmt.Errorf("%s exceeds maximum length (%d)", fieldName, maxDeploymentIdentityRunes)
	}
	if strings.ContainsAny(trimmed, "\x00\n\r") {
		return fmt.Errorf("%s contains control characters", fieldName)
	}
	return nil
}

func validateDiagnosticLevel(raw string) error {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "error", "warn", "info", "debug", "trace":
		return nil
	default:
		return fmt.Errorf("must be one of error, warn, info, debug, trace")
	}
}

func validateDiagnosticLogDirectory(rawPath string) error {
	trimmedPath := strings.TrimSpace(rawPath)
	if trimmedPath == "" {
		return fmt.Errorf("is required when diagnostic logging is enabled")
	}
	if filepath.IsAbs(trimmedPath) {
		return fmt.Errorf("must be relative to the repository root")
	}
	cleanedPath := filepath.Clean(trimmedPath)
	if cleanedPath == "." || cleanedPath == ".." || strings.HasPrefix(cleanedPath, ".."+string(filepath.Separator)) {
		return fmt.Errorf("must stay under runtime/logs or runtime/state")
	}
	logsPrefix := "runtime" + string(filepath.Separator) + "logs"
	statePrefix := "runtime" + string(filepath.Separator) + "state"
	if cleanedPath == logsPrefix || strings.HasPrefix(cleanedPath, logsPrefix+string(filepath.Separator)) {
		return nil
	}
	if cleanedPath == statePrefix || strings.HasPrefix(cleanedPath, statePrefix+string(filepath.Separator)) {
		return nil
	}
	return fmt.Errorf("must be under runtime/logs or runtime/state")
}

func validateRuntimeInternalPath(rawPath string, requireDirectory bool) error {
	trimmedPath := strings.TrimSpace(rawPath)
	if trimmedPath == "" {
		return fmt.Errorf("is required")
	}
	if filepath.IsAbs(trimmedPath) {
		return fmt.Errorf("must be relative to the repository root")
	}
	cleanedPath := filepath.Clean(trimmedPath)
	if cleanedPath == "." || cleanedPath == ".." || strings.HasPrefix(cleanedPath, ".."+string(filepath.Separator)) {
		return fmt.Errorf("must stay within runtime/state")
	}
	if cleanedPath != trimmedPath {
		return fmt.Errorf("must be normalized and must not contain path traversal")
	}
	runtimeStatePrefix := "runtime" + string(filepath.Separator) + "state" + string(filepath.Separator)
	if cleanedPath != "runtime/state" && !strings.HasPrefix(cleanedPath, runtimeStatePrefix) {
		return fmt.Errorf("must stay within runtime/state")
	}
	if requireDirectory && strings.HasSuffix(cleanedPath, ".jsonl") {
		return fmt.Errorf("must be a directory path")
	}
	if !requireDirectory && strings.HasSuffix(cleanedPath, string(filepath.Separator)) {
		return fmt.Errorf("must be a file path")
	}
	return nil
}
