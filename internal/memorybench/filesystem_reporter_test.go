package memorybench

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathsForRun_DefaultRoot(t *testing.T) {
	outputPaths := PathsForRun("", "run_123")
	if outputPaths.RunDirectory != filepath.Join(DefaultOutputRoot, "run_123") {
		t.Fatalf("unexpected run directory: %q", outputPaths.RunDirectory)
	}
	if outputPaths.ResultsPath != filepath.Join(DefaultOutputRoot, "run_123", resultsFilename) {
		t.Fatalf("unexpected results path: %q", outputPaths.ResultsPath)
	}
	if outputPaths.SummaryPath != filepath.Join(DefaultOutputRoot, "run_123", summaryFilename) {
		t.Fatalf("unexpected summary path: %q", outputPaths.SummaryPath)
	}
	if outputPaths.FamilySummaryPath != filepath.Join(DefaultOutputRoot, "run_123", familySummaryFilename) {
		t.Fatalf("unexpected family summary path: %q", outputPaths.FamilySummaryPath)
	}
	if outputPaths.SubfamilySummaryPath != filepath.Join(DefaultOutputRoot, "run_123", subfamilySummaryFilename) {
		t.Fatalf("unexpected subfamily summary path: %q", outputPaths.SubfamilySummaryPath)
	}
	if outputPaths.TracePath != filepath.Join(DefaultOutputRoot, "run_123", traceFilename) {
		t.Fatalf("unexpected trace path: %q", outputPaths.TracePath)
	}
}

func TestFilesystemObserver_WritesStructuredArtifacts(t *testing.T) {
	outputRoot := t.TempDir()
	observer := NewFilesystemObserver(outputRoot, "run_123")
	runMetadata := RunMetadata{
		SchemaVersion:           SchemaVersion,
		RunID:                   "run_123",
		StartedAtUTC:            "2026-03-26T12:00:00Z",
		FinishedAtUTC:           "2026-03-26T12:01:00Z",
		BenchmarkVersion:        "v0",
		BackendName:             "continuity_tcl",
		CandidateGovernanceMode: CandidateGovernanceBackendDefault,
		ModelProvider:           "test",
		ModelName:               "stub",
		TokenBudget:             4096,
	}
	scenarioMetadata := ScenarioMetadata{
		ScenarioID:      "scenario_pref_update",
		Category:        CategoryTaskResumption,
		SubfamilyID:     "answer_in_query",
		RubricVersion:   "r1",
		FixtureVersion:  "f1",
		ExpectedOutcome: "latest value wins",
	}
	retrievedArtifacts := []RetrievedArtifact{{
		ArtifactID:   "dist_123",
		ArtifactKind: "explicit_remembered_fact",
		Reason:       "anchor_exact_match",
		MatchCount:   2,
	}}
	backendMetrics := BackendMetrics{
		RetrievalLatencyMillis: 12,
		CandidatesConsidered:   4,
		ItemsReturned:          1,
		RetrievedPromptTokens:  16,
	}
	runResult := RunResult{
		Run: runMetadata,
		ScenarioResults: []ScenarioResult{{
			Scenario: scenarioMetadata,
			Backend: BackendMetrics{
				RetrievalLatencyMillis: 12,
				CandidatesConsidered:   4,
				ItemsReturned:          1,
				RetrievedPromptTokens:  16,
				InjectedPromptTokens:   16,
			},
			Outcome: OutcomeMetrics{
				Passed:                true,
				Score:                 1,
				TruthMaintenanceScore: 1,
				SafetyTrustScore:      1,
				OperationalCostScore:  1,
				TaskResumptionSuccess: true,
				EndToEndSuccess:       true,
				RetrievalCorrectness:  1,
				ProvenanceCorrect:     true,
				StaleMemoryIntrusions: 0,
			},
			Retrieved:  retrievedArtifacts,
			FinishedAt: "2026-03-26T12:00:30Z",
		}},
		FamilySummaries: []FamilySummary{{
			FamilyID:                  CategoryTaskResumption,
			BackendName:               "continuity_tcl",
			ScenarioCount:             1,
			PassedCount:               1,
			AverageScore:              1,
			AverageTruthScore:         1,
			AverageSafetyScore:        1,
			AverageOperationalScore:   1,
			TotalLatencyMillis:        12,
			AverageLatencyMillis:      12,
			MaxLatencyMillis:          12,
			AverageItemsReturned:      1,
			MaxItemsReturned:          1,
			TotalHintBytesRetrieved:   0,
			AverageHintBytesRetrieved: 0,
			MaxHintBytesRetrieved:     0,
			TotalPromptTokens:         16,
			AveragePromptTokens:       16,
			MaxPromptTokens:           16,
			TotalFinalPromptTokens:    16,
			AverageFinalPromptTokens:  16,
			MaxFinalPromptTokens:      16,
		}},
		SubfamilySummaries: []FamilySummary{{
			FamilyID:                  CategoryTaskResumption + ".answer_in_query",
			BackendName:               "continuity_tcl",
			ScenarioCount:             1,
			PassedCount:               1,
			AverageScore:              1,
			AverageTruthScore:         1,
			AverageSafetyScore:        1,
			AverageOperationalScore:   1,
			TotalLatencyMillis:        12,
			AverageLatencyMillis:      12,
			MaxLatencyMillis:          12,
			AverageItemsReturned:      1,
			MaxItemsReturned:          1,
			TotalHintBytesRetrieved:   0,
			AverageHintBytesRetrieved: 0,
			MaxHintBytesRetrieved:     0,
			TotalPromptTokens:         16,
			AveragePromptTokens:       16,
			MaxPromptTokens:           16,
			TotalFinalPromptTokens:    16,
			AverageFinalPromptTokens:  16,
			MaxFinalPromptTokens:      16,
		}},
	}

	if err := observer.OnRunStarted(context.Background(), runMetadata); err != nil {
		t.Fatalf("OnRunStarted: %v", err)
	}
	if err := observer.OnScenarioStarted(context.Background(), runMetadata, scenarioMetadata); err != nil {
		t.Fatalf("OnScenarioStarted: %v", err)
	}
	if err := observer.OnRetrievalCompleted(context.Background(), runMetadata, scenarioMetadata, backendMetrics, retrievedArtifacts); err != nil {
		t.Fatalf("OnRetrievalCompleted: %v", err)
	}
	if err := observer.OnEvaluationCompleted(context.Background(), runMetadata, runResult.ScenarioResults[0]); err != nil {
		t.Fatalf("OnEvaluationCompleted: %v", err)
	}
	if err := observer.OnRunCompleted(context.Background(), runResult); err != nil {
		t.Fatalf("OnRunCompleted: %v", err)
	}

	outputPaths := PathsForRun(outputRoot, "run_123")
	resultsBytes, err := os.ReadFile(outputPaths.ResultsPath)
	if err != nil {
		t.Fatalf("read results.json: %v", err)
	}
	if !strings.Contains(string(resultsBytes), `"run_id": "run_123"`) {
		t.Fatalf("expected run id in results.json, got %s", string(resultsBytes))
	}
	if !strings.Contains(string(resultsBytes), `"scenario_id": "scenario_pref_update"`) {
		t.Fatalf("expected scenario id in results.json, got %s", string(resultsBytes))
	}

	summaryBytes, err := os.ReadFile(outputPaths.SummaryPath)
	if err != nil {
		t.Fatalf("read summary.csv: %v", err)
	}
	if !strings.Contains(string(summaryBytes), "scenario_id,category,subfamily_id,backend_name,passed,score,truth_maintenance_score,safety_trust_score,operational_cost_score,task_resumption_success") {
		t.Fatalf("expected csv header in summary.csv, got %s", string(summaryBytes))
	}
	if !strings.Contains(string(summaryBytes), "scenario_pref_update,task_resumption,answer_in_query,continuity_tcl,true,1.0000,1.0000,1.0000,1.0000,true") {
		t.Fatalf("expected scenario row in summary.csv, got %s", string(summaryBytes))
	}

	familySummaryBytes, err := os.ReadFile(outputPaths.FamilySummaryPath)
	if err != nil {
		t.Fatalf("read family_summary.csv: %v", err)
	}
	if !strings.Contains(string(familySummaryBytes), "family_id,backend_name,scenario_count,passed_count,average_score,average_truth_maintenance_score") {
		t.Fatalf("expected family summary header, got %s", string(familySummaryBytes))
	}
	if !strings.Contains(string(familySummaryBytes), "task_resumption,continuity_tcl,1,1,1.0000,1.0000,1.0000,1.0000,12,12.0000,12,1.0000,1,0,0.0000,0,16,16.0000,16,16,16.0000,16") {
		t.Fatalf("expected family summary row, got %s", string(familySummaryBytes))
	}

	subfamilySummaryBytes, err := os.ReadFile(outputPaths.SubfamilySummaryPath)
	if err != nil {
		t.Fatalf("read subfamily_summary.csv: %v", err)
	}
	if !strings.Contains(string(subfamilySummaryBytes), "family_id,backend_name,scenario_count,passed_count,average_score,average_truth_maintenance_score") {
		t.Fatalf("expected subfamily summary header, got %s", string(subfamilySummaryBytes))
	}
	if !strings.Contains(string(subfamilySummaryBytes), "task_resumption.answer_in_query,continuity_tcl,1,1,1.0000,1.0000,1.0000,1.0000,12,12.0000,12,1.0000,1,0,0.0000,0,16,16.0000,16,16,16.0000,16") {
		t.Fatalf("expected subfamily summary row, got %s", string(subfamilySummaryBytes))
	}

	traceBytes, err := os.ReadFile(outputPaths.TracePath)
	if err != nil {
		t.Fatalf("read trace.jsonl: %v", err)
	}
	traceText := string(traceBytes)
	for _, expectedEvent := range []string{`"event_type":"run_started"`, `"event_type":"scenario_started"`, `"event_type":"retrieval_completed"`, `"event_type":"evaluation_completed"`, `"event_type":"run_completed"`} {
		if !strings.Contains(traceText, expectedEvent) {
			t.Fatalf("expected %s in trace.jsonl, got %s", expectedEvent, traceText)
		}
	}
}
