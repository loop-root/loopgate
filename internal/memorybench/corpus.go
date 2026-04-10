package memorybench

import (
	"fmt"
	"strings"
	"time"
)

const (
	BenchmarkScopeGlobal             = "global"
	BenchmarkEvidenceWorkingSetScope = "benchmark:evidence_working_set"
	BenchmarkHybridEvidenceScope     = "benchmark:hybrid_evidence_working_set"
	BenchmarkNodeKindStep            = "benchmark_fixture_step"
	BenchmarkSourceFixture           = "memorybench_fixture"
)

type CorpusDocument struct {
	DocumentID      string            `json:"document_id"`
	Content         string            `json:"content"`
	DocumentKind    string            `json:"document_kind"`
	Scope           string            `json:"scope"`
	CreatedAtUTC    string            `json:"created_at_utc,omitempty"`
	ExactSignature  string            `json:"exact_signature,omitempty"`
	FamilySignature string            `json:"family_signature,omitempty"`
	ProvenanceRef   string            `json:"provenance_ref,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

func BuildCorpusDocumentsFromFixtures(scenarioFixtures []ScenarioFixture) ([]CorpusDocument, error) {
	if len(scenarioFixtures) == 0 {
		return nil, fmt.Errorf("at least one scenario fixture is required")
	}

	corpusDocuments := make([]CorpusDocument, 0, len(scenarioFixtures)*2)
	baseTimestampUTC := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	documentOffset := 0
	for _, scenarioFixture := range scenarioFixtures {
		trimmedScenarioID := strings.TrimSpace(scenarioFixture.Metadata.ScenarioID)
		if trimmedScenarioID == "" {
			return nil, fmt.Errorf("scenario fixture is missing scenario id")
		}
		for stepIndex, scenarioStep := range scenarioFixture.Steps {
			if !fixtureStepEligibleForCorpus(scenarioFixture, scenarioStep) {
				continue
			}
			trimmedContent := strings.TrimSpace(scenarioStep.Content)
			if trimmedContent == "" {
				continue
			}
			trimmedRole := strings.TrimSpace(scenarioStep.Role)
			documentID := fmt.Sprintf("%s:step:%02d", trimmedScenarioID, stepIndex)
			corpusDocuments = append(corpusDocuments, CorpusDocument{
				DocumentID:    documentID,
				Content:       trimmedContent,
				DocumentKind:  BenchmarkNodeKindStep,
				Scope:         BenchmarkCorpusScope(scenarioFixture),
				CreatedAtUTC:  baseTimestampUTC.Add(time.Duration(documentOffset) * time.Minute).Format(time.RFC3339),
				ProvenanceRef: fmt.Sprintf("fixture:%s", documentID),
				Metadata: map[string]string{
					"scenario_id":       trimmedScenarioID,
					"scenario_category": strings.TrimSpace(scenarioFixture.Metadata.Category),
					"scenario_role":     trimmedRole,
					"fixture_version":   strings.TrimSpace(scenarioFixture.Metadata.FixtureVersion),
					"source_kind":       BenchmarkSourceFixture,
				},
			})
			documentOffset++
		}
	}

	if len(corpusDocuments) == 0 {
		return nil, fmt.Errorf("scenario fixtures did not produce any corpus documents")
	}
	return corpusDocuments, nil
}

func BenchmarkScenarioScope(scenarioID string) string {
	trimmedScenarioID := strings.TrimSpace(scenarioID)
	if trimmedScenarioID == "" {
		return BenchmarkScopeGlobal
	}
	return "scenario:" + trimmedScenarioID
}

func BenchmarkFixtureScope(scenarioFixture ScenarioFixture) string {
	if strings.TrimSpace(scenarioFixture.Metadata.Category) == CategoryMemoryEvidenceRetrieval {
		// Evidence fixtures model broad working-set retrieval, not per-scenario slot
		// recall. Give them a shared scope so RAG-style backends compete over one
		// evidence corpus instead of four tiny isolated corpora.
		return BenchmarkEvidenceWorkingSetScope
	}
	return BenchmarkScenarioScope(scenarioFixture.Metadata.ScenarioID)
}

func BenchmarkCorpusScope(scenarioFixture ScenarioFixture) string {
	switch strings.TrimSpace(scenarioFixture.Metadata.Category) {
	case CategoryMemoryEvidenceRetrieval:
		return BenchmarkEvidenceWorkingSetScope
	case CategoryMemoryHybridRecall:
		// Hybrid fixtures keep their continuity state isolated per scenario but share
		// one evidence working set so the benchmark has to do real evidence lookup
		// rather than retrieve from tiny per-scenario document islands.
		return BenchmarkHybridEvidenceScope
	default:
		return BenchmarkScenarioScope(scenarioFixture.Metadata.ScenarioID)
	}
}

func fixtureStepEligibleForCorpus(scenarioFixture ScenarioFixture, scenarioStep ScenarioStep) bool {
	trimmedRole := strings.TrimSpace(scenarioStep.Role)
	switch trimmedRole {
	case "system_probe", "hint_probe", "state_probe", "evidence_probe":
		return false
	}

	switch strings.TrimSpace(scenarioFixture.Metadata.Category) {
	case CategoryMemoryPoisoning, CategoryMemorySafetyPrecision:
		return false
	default:
		return trimmedRole == "user" || trimmedRole == "continuity_candidate"
	}
}
