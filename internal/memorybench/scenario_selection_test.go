package memorybench

import "testing"

func TestSelectScenarioFixtures_ByScenarioID(t *testing.T) {
	selectedFixtures, err := SelectScenarioFixtures(DefaultScenarioFixtures(), ScenarioFilter{
		ScenarioIDs: []string{"contradiction.profile_timezone_same_entity_wrong_current_probe.v1"},
	})
	if err != nil {
		t.Fatalf("SelectScenarioFixtures: %v", err)
	}
	if len(selectedFixtures) != 1 || selectedFixtures[0].Metadata.ScenarioID != "contradiction.profile_timezone_same_entity_wrong_current_probe.v1" {
		t.Fatalf("expected one targeted fixture, got %#v", selectedFixtures)
	}
}

func TestSelectScenarioFixtures_ByScenarioSet(t *testing.T) {
	normalizedScenarioFilter, err := NormalizeScenarioFilter(ScenarioFilter{
		ScenarioSets: []string{"profile_slot_same_entity_preview"},
	})
	if err != nil {
		t.Fatalf("NormalizeScenarioFilter: %v", err)
	}
	selectedFixtures, err := SelectScenarioFixtures(DefaultScenarioFixtures(), normalizedScenarioFilter)
	if err != nil {
		t.Fatalf("SelectScenarioFixtures: %v", err)
	}
	if len(selectedFixtures) != 4 {
		t.Fatalf("expected four same-entity preview fixtures, got %d", len(selectedFixtures))
	}
}

func TestSelectScenarioFixtures_ByCategoryAndSubfamily(t *testing.T) {
	normalizedScenarioFilter, err := NormalizeScenarioFilter(ScenarioFilter{
		Categories:  []string{CategoryMemoryContradiction},
		Subfamilies: []string{"slot_only"},
	})
	if err != nil {
		t.Fatalf("NormalizeScenarioFilter: %v", err)
	}
	selectedFixtures, err := SelectScenarioFixtures(DefaultScenarioFixtures(), normalizedScenarioFilter)
	if err != nil {
		t.Fatalf("SelectScenarioFixtures: %v", err)
	}
	if len(selectedFixtures) == 0 {
		t.Fatal("expected slot-only contradiction fixtures")
	}
	for _, selectedFixture := range selectedFixtures {
		if selectedFixture.Metadata.Category != CategoryMemoryContradiction {
			t.Fatalf("expected contradiction fixture, got %#v", selectedFixture.Metadata)
		}
		if selectedFixture.Metadata.SubfamilyID != "slot_only" {
			t.Fatalf("expected slot_only subfamily fixture, got %#v", selectedFixture.Metadata)
		}
	}
}

func TestSelectScenarioFixtures_DemoTaskResumptionSet(t *testing.T) {
	normalizedScenarioFilter, err := NormalizeScenarioFilter(ScenarioFilter{
		ScenarioSets: []string{"demo_task_resumption"},
	})
	if err != nil {
		t.Fatalf("NormalizeScenarioFilter: %v", err)
	}
	selectedFixtures, err := SelectScenarioFixtures(DefaultScenarioFixtures(), normalizedScenarioFilter)
	if err != nil {
		t.Fatalf("SelectScenarioFixtures: %v", err)
	}
	if len(selectedFixtures) != 2 {
		t.Fatalf("expected two demo task resumption fixtures, got %d", len(selectedFixtures))
	}
}

func TestSelectScenarioFixtures_DemoSlotTruthSet(t *testing.T) {
	normalizedScenarioFilter, err := NormalizeScenarioFilter(ScenarioFilter{
		ScenarioSets: []string{"demo_slot_truth"},
	})
	if err != nil {
		t.Fatalf("NormalizeScenarioFilter: %v", err)
	}
	selectedFixtures, err := SelectScenarioFixtures(DefaultScenarioFixtures(), normalizedScenarioFilter)
	if err != nil {
		t.Fatalf("SelectScenarioFixtures: %v", err)
	}
	if len(selectedFixtures) != 2 {
		t.Fatalf("expected two demo slot-truth fixtures, got %d", len(selectedFixtures))
	}
}

func TestSelectScenarioFixtures_PoisoningPolicyMatrixSet(t *testing.T) {
	normalizedScenarioFilter, err := NormalizeScenarioFilter(ScenarioFilter{
		ScenarioSets: []string{"poisoning_policy_matrix"},
	})
	if err != nil {
		t.Fatalf("NormalizeScenarioFilter: %v", err)
	}
	selectedFixtures, err := SelectScenarioFixtures(DefaultScenarioFixtures(), normalizedScenarioFilter)
	if err != nil {
		t.Fatalf("SelectScenarioFixtures: %v", err)
	}
	if len(selectedFixtures) != 23 {
		t.Fatalf("expected twenty-three poisoning-policy fixtures, got %d", len(selectedFixtures))
	}
}

func TestSelectScenarioFixtures_PoisoningFormatLaunderingSet(t *testing.T) {
	normalizedScenarioFilter, err := NormalizeScenarioFilter(ScenarioFilter{
		ScenarioSets: []string{"poisoning_format_laundering"},
	})
	if err != nil {
		t.Fatalf("NormalizeScenarioFilter: %v", err)
	}
	selectedFixtures, err := SelectScenarioFixtures(DefaultScenarioFixtures(), normalizedScenarioFilter)
	if err != nil {
		t.Fatalf("SelectScenarioFixtures: %v", err)
	}
	if len(selectedFixtures) != 4 {
		t.Fatalf("expected four format-laundering fixtures, got %d", len(selectedFixtures))
	}
}

func TestSelectScenarioFixtures_PoisoningDelayedTriggerSet(t *testing.T) {
	normalizedScenarioFilter, err := NormalizeScenarioFilter(ScenarioFilter{
		ScenarioSets: []string{"poisoning_delayed_trigger"},
	})
	if err != nil {
		t.Fatalf("NormalizeScenarioFilter: %v", err)
	}
	selectedFixtures, err := SelectScenarioFixtures(DefaultScenarioFixtures(), normalizedScenarioFilter)
	if err != nil {
		t.Fatalf("SelectScenarioFixtures: %v", err)
	}
	if len(selectedFixtures) != 3 {
		t.Fatalf("expected three delayed-trigger fixtures, got %d", len(selectedFixtures))
	}
}

func TestSelectScenarioFixtures_RAGEvidenceMatrixSet(t *testing.T) {
	normalizedScenarioFilter, err := NormalizeScenarioFilter(ScenarioFilter{
		ScenarioSets: []string{"rag_evidence_matrix"},
	})
	if err != nil {
		t.Fatalf("NormalizeScenarioFilter: %v", err)
	}
	selectedFixtures, err := SelectScenarioFixtures(ExtendedScenarioFixtures(), normalizedScenarioFilter)
	if err != nil {
		t.Fatalf("SelectScenarioFixtures: %v", err)
	}
	if len(selectedFixtures) != 6 {
		t.Fatalf("expected six evidence-retrieval fixtures, got %d", len(selectedFixtures))
	}
	for _, selectedFixture := range selectedFixtures {
		if selectedFixture.Metadata.Category != CategoryMemoryEvidenceRetrieval {
			t.Fatalf("expected evidence retrieval fixture, got %#v", selectedFixture.Metadata)
		}
	}
}

func TestSelectScenarioFixtures_HybridRecallMatrixSet(t *testing.T) {
	normalizedScenarioFilter, err := NormalizeScenarioFilter(ScenarioFilter{
		ScenarioSets: []string{"hybrid_recall_matrix"},
	})
	if err != nil {
		t.Fatalf("NormalizeScenarioFilter: %v", err)
	}
	selectedFixtures, err := SelectScenarioFixtures(ExtendedScenarioFixtures(), normalizedScenarioFilter)
	if err != nil {
		t.Fatalf("SelectScenarioFixtures: %v", err)
	}
	if len(selectedFixtures) != 5 {
		t.Fatalf("expected five hybrid recall fixtures, got %d", len(selectedFixtures))
	}
	for _, selectedFixture := range selectedFixtures {
		if selectedFixture.Metadata.Category != CategoryMemoryHybridRecall {
			t.Fatalf("expected hybrid recall fixture, got %#v", selectedFixture.Metadata)
		}
	}
}

func TestSelectScenarioFixtures_LongHorizonMatrixSet(t *testing.T) {
	normalizedScenarioFilter, err := NormalizeScenarioFilter(ScenarioFilter{
		ScenarioSets: []string{"long_horizon_matrix"},
	})
	if err != nil {
		t.Fatalf("NormalizeScenarioFilter: %v", err)
	}
	selectedFixtures, err := SelectScenarioFixtures(DefaultScenarioFixtures(), normalizedScenarioFilter)
	if err != nil {
		t.Fatalf("SelectScenarioFixtures: %v", err)
	}
	if len(selectedFixtures) != 8 {
		t.Fatalf("expected eight long-horizon fixtures, got %d", len(selectedFixtures))
	}
	for _, selectedFixture := range selectedFixtures {
		switch selectedFixture.Metadata.Category {
		case CategoryMemoryContradiction, CategoryTaskResumption:
		default:
			t.Fatalf("expected contradiction or task resumption fixture, got %#v", selectedFixture.Metadata)
		}
	}
}

func TestSelectScenarioFixtures_EmptySelectionFailsClosed(t *testing.T) {
	normalizedScenarioFilter, err := NormalizeScenarioFilter(ScenarioFilter{
		ScenarioIDs: []string{"missing.fixture.v1"},
	})
	if err != nil {
		t.Fatalf("NormalizeScenarioFilter: %v", err)
	}
	if _, err := SelectScenarioFixtures(DefaultScenarioFixtures(), normalizedScenarioFilter); err == nil {
		t.Fatal("expected zero-match scenario filter to fail closed")
	}
}

func TestNormalizeScenarioFilter_RejectsUnknownScenarioSet(t *testing.T) {
	if _, err := NormalizeScenarioFilter(ScenarioFilter{ScenarioSets: []string{"mystery_set"}}); err == nil {
		t.Fatal("expected unknown scenario set to fail")
	}
}
