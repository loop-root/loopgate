package memorybench

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	DefaultOutputRoot        = "runtime/benchmarks"
	runMetadataFilename      = "run_metadata.json"
	resultsFilename          = "results.json"
	summaryFilename          = "summary.csv"
	familySummaryFilename    = "family_summary.csv"
	subfamilySummaryFilename = "subfamily_summary.csv"
	traceFilename            = "trace.jsonl"
	seedManifestFilename     = "seed_manifest.json"
)

type OutputPaths struct {
	RunMetadataPath      string
	RunDirectory         string
	ResultsPath          string
	SummaryPath          string
	FamilySummaryPath    string
	SubfamilySummaryPath string
	TracePath            string
	SeedManifestPath     string
}

type persistedRunMetadata struct {
	RunMetadata          RunMetadata `json:"run"`
	RunMetadataPath      string      `json:"run_metadata_path"`
	ResultsPath          string      `json:"results_path"`
	SummaryPath          string      `json:"summary_path"`
	FamilySummaryPath    string      `json:"family_summary_path"`
	SubfamilySummaryPath string      `json:"subfamily_summary_path"`
	TracePath            string      `json:"trace_path"`
	SeedManifestPath     string      `json:"seed_manifest_path,omitempty"`
}

func PathsForRun(outputRoot string, runID string) OutputPaths {
	trimmedOutputRoot := outputRoot
	if trimmedOutputRoot == "" {
		trimmedOutputRoot = DefaultOutputRoot
	}
	runDirectory := filepath.Join(trimmedOutputRoot, runID)
	return OutputPaths{
		RunMetadataPath:      filepath.Join(runDirectory, runMetadataFilename),
		RunDirectory:         runDirectory,
		ResultsPath:          filepath.Join(runDirectory, resultsFilename),
		SummaryPath:          filepath.Join(runDirectory, summaryFilename),
		FamilySummaryPath:    filepath.Join(runDirectory, familySummaryFilename),
		SubfamilySummaryPath: filepath.Join(runDirectory, subfamilySummaryFilename),
		TracePath:            filepath.Join(runDirectory, traceFilename),
		SeedManifestPath:     filepath.Join(runDirectory, seedManifestFilename),
	}
}

type FilesystemObserver struct {
	outputPaths OutputPaths
}

func NewFilesystemObserver(outputRoot string, runID string) FilesystemObserver {
	return FilesystemObserver{
		outputPaths: PathsForRun(outputRoot, runID),
	}
}

func (observer FilesystemObserver) OnRunStarted(ctx context.Context, runMetadata RunMetadata) error {
	if err := os.MkdirAll(observer.outputPaths.RunDirectory, 0o700); err != nil {
		return fmt.Errorf("create benchmark output directory: %w", err)
	}
	if err := observer.writeRunMetadata(runMetadata); err != nil {
		return err
	}
	return observer.appendTraceEvent(TraceEvent{
		TimestampUTC: runMetadata.StartedAtUTC,
		RunID:        runMetadata.RunID,
		BackendName:  runMetadata.BackendName,
		EventType:    "run_started",
		Payload: map[string]any{
			"benchmark_version":         runMetadata.BenchmarkVersion,
			"benchmark_profile":         runMetadata.BenchmarkProfile,
			"candidate_governance_mode": runMetadata.CandidateGovernanceMode,
			"model_provider":            runMetadata.ModelProvider,
			"model_name":                runMetadata.ModelName,
			"token_budget":              runMetadata.TokenBudget,
		},
	})
}

func (observer FilesystemObserver) OnScenarioStarted(ctx context.Context, runMetadata RunMetadata, scenarioMetadata ScenarioMetadata) error {
	return observer.appendTraceEvent(TraceEvent{
		TimestampUTC: runMetadata.StartedAtUTC,
		RunID:        runMetadata.RunID,
		ScenarioID:   scenarioMetadata.ScenarioID,
		BackendName:  runMetadata.BackendName,
		EventType:    "scenario_started",
		Payload: map[string]any{
			"category":         scenarioMetadata.Category,
			"subfamily_id":     scenarioMetadata.SubfamilyID,
			"rubric_version":   scenarioMetadata.RubricVersion,
			"fixture_version":  scenarioMetadata.FixtureVersion,
			"expected_outcome": scenarioMetadata.ExpectedOutcome,
		},
	})
}

func (observer FilesystemObserver) OnRetrievalCompleted(ctx context.Context, runMetadata RunMetadata, scenarioMetadata ScenarioMetadata, backendMetrics BackendMetrics, retrievedArtifacts []RetrievedArtifact, candidatePool []CandidatePoolArtifact) error {
	tracePayload := map[string]any{
		"backend_metrics":     backendMetrics,
		"retrieved_artifacts": retrievedArtifacts,
	}
	if len(candidatePool) > 0 {
		tracePayload["candidate_pool"] = candidatePool
	}
	return observer.appendTraceEvent(TraceEvent{
		TimestampUTC: runMetadata.StartedAtUTC,
		RunID:        runMetadata.RunID,
		ScenarioID:   scenarioMetadata.ScenarioID,
		BackendName:  runMetadata.BackendName,
		EventType:    "retrieval_completed",
		Payload:      tracePayload,
	})
}

func (observer FilesystemObserver) OnEvaluationCompleted(ctx context.Context, runMetadata RunMetadata, scenarioResult ScenarioResult) error {
	return observer.appendTraceEvent(TraceEvent{
		TimestampUTC: scenarioResult.FinishedAt,
		RunID:        runMetadata.RunID,
		ScenarioID:   scenarioResult.Scenario.ScenarioID,
		BackendName:  runMetadata.BackendName,
		EventType:    "evaluation_completed",
		Payload: map[string]any{
			"backend_metrics": scenarioResult.Backend,
			"outcome":         scenarioResult.Outcome,
		},
	})
}

func (observer FilesystemObserver) OnRunCompleted(ctx context.Context, runResult RunResult) error {
	if err := observer.writeResultsJSON(runResult); err != nil {
		return err
	}
	if err := observer.writeRunMetadata(runResult.Run); err != nil {
		return err
	}
	if err := observer.writeSummaryCSV(runResult); err != nil {
		return err
	}
	if err := observer.writeFamilySummaryCSV(runResult); err != nil {
		return err
	}
	if err := observer.writeSubfamilySummaryCSV(runResult); err != nil {
		return err
	}
	return observer.appendTraceEvent(TraceEvent{
		TimestampUTC: runResult.Run.FinishedAtUTC,
		RunID:        runResult.Run.RunID,
		BackendName:  runResult.Run.BackendName,
		EventType:    "run_completed",
		Payload: map[string]any{
			"scenario_count": len(runResult.ScenarioResults),
		},
	})
}

func (observer FilesystemObserver) writeFamilySummaryCSV(runResult RunResult) error {
	return observer.writeGroupedSummaryCSV(observer.outputPaths.FamilySummaryPath, runResult.FamilySummaries)
}

func (observer FilesystemObserver) writeSubfamilySummaryCSV(runResult RunResult) error {
	return observer.writeGroupedSummaryCSV(observer.outputPaths.SubfamilySummaryPath, runResult.SubfamilySummaries)
}

func (observer FilesystemObserver) writeGroupedSummaryCSV(outputPath string, groupedSummaries []FamilySummary) error {
	if len(groupedSummaries) == 0 {
		return nil
	}
	familySummaryFile, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open benchmark family summary file: %w", err)
	}
	defer familySummaryFile.Close()

	csvWriter := csv.NewWriter(familySummaryFile)
	if err := csvWriter.Write([]string{
		"family_id",
		"backend_name",
		"scenario_count",
		"passed_count",
		"average_score",
		"average_truth_maintenance_score",
		"average_safety_trust_score",
		"average_operational_cost_score",
		"total_retrieval_latency_millis",
		"average_retrieval_latency_millis",
		"max_retrieval_latency_millis",
		"average_items_returned",
		"max_items_returned",
		"total_hint_bytes_retrieved",
		"average_hint_bytes_retrieved",
		"max_hint_bytes_retrieved",
		"total_retrieved_prompt_tokens",
		"average_retrieved_prompt_tokens",
		"max_retrieved_prompt_tokens",
		"total_approx_final_prompt_tokens",
		"average_approx_final_prompt_tokens",
		"max_approx_final_prompt_tokens",
	}); err != nil {
		return fmt.Errorf("write benchmark family summary header: %w", err)
	}

	for _, familySummary := range groupedSummaries {
		record := []string{
			familySummary.FamilyID,
			familySummary.BackendName,
			fmt.Sprintf("%d", familySummary.ScenarioCount),
			fmt.Sprintf("%d", familySummary.PassedCount),
			fmt.Sprintf("%.4f", familySummary.AverageScore),
			fmt.Sprintf("%.4f", familySummary.AverageTruthScore),
			fmt.Sprintf("%.4f", familySummary.AverageSafetyScore),
			fmt.Sprintf("%.4f", familySummary.AverageOperationalScore),
			fmt.Sprintf("%d", familySummary.TotalLatencyMillis),
			fmt.Sprintf("%.4f", familySummary.AverageLatencyMillis),
			fmt.Sprintf("%d", familySummary.MaxLatencyMillis),
			fmt.Sprintf("%.4f", familySummary.AverageItemsReturned),
			fmt.Sprintf("%d", familySummary.MaxItemsReturned),
			fmt.Sprintf("%d", familySummary.TotalHintBytesRetrieved),
			fmt.Sprintf("%.4f", familySummary.AverageHintBytesRetrieved),
			fmt.Sprintf("%d", familySummary.MaxHintBytesRetrieved),
			fmt.Sprintf("%d", familySummary.TotalPromptTokens),
			fmt.Sprintf("%.4f", familySummary.AveragePromptTokens),
			fmt.Sprintf("%d", familySummary.MaxPromptTokens),
			fmt.Sprintf("%d", familySummary.TotalFinalPromptTokens),
			fmt.Sprintf("%.4f", familySummary.AverageFinalPromptTokens),
			fmt.Sprintf("%d", familySummary.MaxFinalPromptTokens),
		}
		if err := csvWriter.Write(record); err != nil {
			return fmt.Errorf("write benchmark family summary record: %w", err)
		}
	}
	csvWriter.Flush()
	if err := csvWriter.Error(); err != nil {
		return fmt.Errorf("flush benchmark family summary csv: %w", err)
	}
	return nil
}

func (observer FilesystemObserver) writeResultsJSON(runResult RunResult) error {
	resultsFile, err := os.OpenFile(observer.outputPaths.ResultsPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open benchmark results file: %w", err)
	}
	defer resultsFile.Close()

	encoder := json.NewEncoder(resultsFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(runResult); err != nil {
		return fmt.Errorf("encode benchmark results file: %w", err)
	}
	return nil
}

func (observer FilesystemObserver) WriteSeedManifest(seedManifestRecords []SeedManifestRecord) error {
	if err := os.MkdirAll(observer.outputPaths.RunDirectory, 0o700); err != nil {
		return fmt.Errorf("create benchmark output directory: %w", err)
	}
	seedManifestFile, err := os.OpenFile(observer.outputPaths.SeedManifestPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open benchmark seed manifest file: %w", err)
	}
	defer seedManifestFile.Close()

	encoder := json.NewEncoder(seedManifestFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(seedManifestRecords); err != nil {
		return fmt.Errorf("encode benchmark seed manifest: %w", err)
	}
	return nil
}

func (observer FilesystemObserver) writeRunMetadata(runMetadata RunMetadata) error {
	runMetadataFile, err := os.OpenFile(observer.outputPaths.RunMetadataPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open benchmark run metadata file: %w", err)
	}
	defer runMetadataFile.Close()

	encoder := json.NewEncoder(runMetadataFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(persistedRunMetadata{
		RunMetadata:          runMetadata,
		RunMetadataPath:      observer.outputPaths.RunMetadataPath,
		ResultsPath:          observer.outputPaths.ResultsPath,
		SummaryPath:          observer.outputPaths.SummaryPath,
		FamilySummaryPath:    observer.outputPaths.FamilySummaryPath,
		SubfamilySummaryPath: observer.outputPaths.SubfamilySummaryPath,
		TracePath:            observer.outputPaths.TracePath,
		SeedManifestPath:     observer.outputPaths.SeedManifestPath,
	}); err != nil {
		return fmt.Errorf("encode benchmark run metadata: %w", err)
	}
	return nil
}

func (observer FilesystemObserver) writeSummaryCSV(runResult RunResult) error {
	summaryFile, err := os.OpenFile(observer.outputPaths.SummaryPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open benchmark summary file: %w", err)
	}
	defer summaryFile.Close()

	csvWriter := csv.NewWriter(summaryFile)
	if err := csvWriter.Write([]string{
		"scenario_id",
		"category",
		"subfamily_id",
		"backend_name",
		"passed",
		"score",
		"truth_maintenance_score",
		"safety_trust_score",
		"operational_cost_score",
		"task_resumption_success",
		"retrieval_latency_millis",
		"candidates_considered",
		"items_returned",
		"hint_only_matches",
		"hint_bytes_retrieved",
		"hint_bytes_injected",
		"retrieved_prompt_tokens",
		"injected_prompt_tokens",
		"approx_final_prompt_tokens",
		"false_contradictions",
		"false_suppressions",
		"missing_critical_context",
		"wrong_context_injections",
		"stale_memory_intrusions",
		"stale_memory_suppressions",
		"poisoning_attempts",
		"poisoning_blocked",
		"poisoning_leaks",
	}); err != nil {
		return fmt.Errorf("write benchmark summary header: %w", err)
	}

	for _, scenarioResult := range runResult.ScenarioResults {
		taskResumptionCellValue := ""
		if scenarioResult.Scenario.Category == CategoryTaskResumption {
			taskResumptionCellValue = fmt.Sprintf("%t", scenarioResult.Outcome.TaskResumptionSuccess)
		}
		record := []string{
			scenarioResult.Scenario.ScenarioID,
			scenarioResult.Scenario.Category,
			scenarioResult.Scenario.SubfamilyID,
			runResult.Run.BackendName,
			fmt.Sprintf("%t", scenarioResult.Outcome.Passed),
			fmt.Sprintf("%.4f", scenarioResult.Outcome.Score),
			fmt.Sprintf("%.4f", scenarioResult.Outcome.TruthMaintenanceScore),
			fmt.Sprintf("%.4f", scenarioResult.Outcome.SafetyTrustScore),
			fmt.Sprintf("%.4f", scenarioResult.Outcome.OperationalCostScore),
			taskResumptionCellValue,
			fmt.Sprintf("%d", scenarioResult.Backend.RetrievalLatencyMillis),
			fmt.Sprintf("%d", scenarioResult.Backend.CandidatesConsidered),
			fmt.Sprintf("%d", scenarioResult.Backend.ItemsReturned),
			fmt.Sprintf("%d", scenarioResult.Backend.HintOnlyMatches),
			fmt.Sprintf("%d", scenarioResult.Backend.HintBytesRetrieved),
			fmt.Sprintf("%d", scenarioResult.Backend.HintBytesInjected),
			fmt.Sprintf("%d", scenarioResult.Backend.RetrievedPromptTokens),
			fmt.Sprintf("%d", scenarioResult.Backend.InjectedPromptTokens),
			fmt.Sprintf("%d", scenarioResult.Backend.ApproxFinalPromptTokens),
			fmt.Sprintf("%d", scenarioResult.Outcome.FalseContradictions),
			fmt.Sprintf("%d", scenarioResult.Outcome.FalseSuppressions),
			fmt.Sprintf("%d", scenarioResult.Outcome.MissingCriticalContext),
			fmt.Sprintf("%d", scenarioResult.Outcome.WrongContextInjections),
			fmt.Sprintf("%d", scenarioResult.Outcome.StaleMemoryIntrusions),
			fmt.Sprintf("%d", scenarioResult.Outcome.StaleMemorySuppressions),
			fmt.Sprintf("%d", scenarioResult.Outcome.PoisoningAttempts),
			fmt.Sprintf("%d", scenarioResult.Outcome.PoisoningBlocked),
			fmt.Sprintf("%d", scenarioResult.Outcome.PoisoningLeaks),
		}
		if err := csvWriter.Write(record); err != nil {
			return fmt.Errorf("write benchmark summary record: %w", err)
		}
	}
	csvWriter.Flush()
	if err := csvWriter.Error(); err != nil {
		return fmt.Errorf("flush benchmark summary csv: %w", err)
	}
	return nil
}

func (observer FilesystemObserver) appendTraceEvent(traceEvent TraceEvent) error {
	traceFile, err := os.OpenFile(observer.outputPaths.TracePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open benchmark trace file: %w", err)
	}
	defer traceFile.Close()

	bufferedWriter := bufio.NewWriter(traceFile)
	encodedTraceEvent, err := json.Marshal(traceEvent)
	if err != nil {
		return fmt.Errorf("encode benchmark trace event: %w", err)
	}
	if _, err := bufferedWriter.Write(append(encodedTraceEvent, '\n')); err != nil {
		return fmt.Errorf("write benchmark trace event: %w", err)
	}
	if err := bufferedWriter.Flush(); err != nil {
		return fmt.Errorf("flush benchmark trace event: %w", err)
	}
	return nil
}
