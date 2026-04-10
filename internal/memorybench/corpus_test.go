package memorybench

import (
	"strings"
	"testing"
)

func TestBuildCorpusDocumentsFromFixtures_BuildsDeterministicFixtureSteps(t *testing.T) {
	corpusDocuments, err := BuildCorpusDocumentsFromFixtures([]ScenarioFixture{
		PoisoningRememberedInstructionFixture(),
		ContradictionLatestPreferenceWinsFixture(),
	})
	if err != nil {
		t.Fatalf("BuildCorpusDocumentsFromFixtures: %v", err)
	}
	if len(corpusDocuments) != 2 {
		t.Fatalf("expected two governed corpus documents, got %d", len(corpusDocuments))
	}
	firstDocument := corpusDocuments[0]
	if firstDocument.DocumentID != "contradiction.preference_latest_theme_wins.v1:step:00" {
		t.Fatalf("unexpected first document id: %#v", firstDocument)
	}
	if firstDocument.DocumentKind != BenchmarkNodeKindStep || firstDocument.Scope != BenchmarkScenarioScope("contradiction.preference_latest_theme_wins.v1") {
		t.Fatalf("unexpected corpus document shape: %#v", firstDocument)
	}
	if firstDocument.Metadata["scenario_role"] != "user" {
		t.Fatalf("expected stored scenario role metadata, got %#v", firstDocument.Metadata)
	}
}

func TestBuildCorpusDocumentsFromFixtures_ExcludesPoisoningAndProbeSteps(t *testing.T) {
	corpusDocuments, err := BuildCorpusDocumentsFromFixtures(DefaultScenarioFixtures())
	if err != nil {
		t.Fatalf("BuildCorpusDocumentsFromFixtures: %v", err)
	}
	for _, corpusDocument := range corpusDocuments {
		if strings.Contains(corpusDocument.DocumentID, "poisoning.") {
			t.Fatalf("expected poisoning fixture steps to stay out of governed corpus, got %#v", corpusDocument)
		}
		if corpusDocument.Metadata["scenario_role"] == "system_probe" || corpusDocument.Metadata["scenario_role"] == "hint_probe" {
			t.Fatalf("expected probe steps to stay out of governed corpus, got %#v", corpusDocument)
		}
	}
}

func TestBuildCorpusDocumentsFromFixtures_RejectsEmptyFixtures(t *testing.T) {
	_, err := BuildCorpusDocumentsFromFixtures(nil)
	if err == nil {
		t.Fatal("expected empty fixtures to fail")
	}
}

func TestBuildCorpusDocumentsFromFixtures_IncludesTaskResumptionSteps(t *testing.T) {
	corpusDocuments, err := BuildCorpusDocumentsFromFixtures([]ScenarioFixture{
		TaskResumptionBenchmarkSeedingFixture(),
	})
	if err != nil {
		t.Fatalf("BuildCorpusDocumentsFromFixtures: %v", err)
	}
	if len(corpusDocuments) == 0 {
		t.Fatal("expected task resumption corpus documents")
	}
	if !strings.Contains(corpusDocuments[0].DocumentID, "task_resumption.benchmark_seeding_after_pause.v1") {
		t.Fatalf("expected task resumption document id, got %#v", corpusDocuments[0])
	}
}

func TestBuildCorpusDocumentsFromFixtures_IncludesEvidenceRetrievalSteps(t *testing.T) {
	corpusDocuments, err := BuildCorpusDocumentsFromFixtures([]ScenarioFixture{
		EvidenceReplayBatchRootCauseFixture(),
	})
	if err != nil {
		t.Fatalf("BuildCorpusDocumentsFromFixtures: %v", err)
	}
	if len(corpusDocuments) != 3 {
		t.Fatalf("expected three evidence corpus documents without the probe step, got %d", len(corpusDocuments))
	}
	for _, corpusDocument := range corpusDocuments {
		if corpusDocument.Metadata["scenario_category"] != CategoryMemoryEvidenceRetrieval {
			t.Fatalf("expected evidence retrieval category metadata, got %#v", corpusDocument)
		}
		if corpusDocument.Metadata["scenario_role"] == "system_probe" {
			t.Fatalf("expected probe step to stay out of evidence corpus, got %#v", corpusDocument)
		}
		if corpusDocument.Scope != BenchmarkEvidenceWorkingSetScope {
			t.Fatalf("expected evidence documents to share one working-set scope, got %#v", corpusDocument)
		}
	}
}
