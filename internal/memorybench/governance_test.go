package memorybench

import "testing"

func TestNewRAGBaselineCandidateGovernanceEvaluator_PersistsCandidates(t *testing.T) {
	evaluator, err := NewRAGBaselineCandidateGovernanceEvaluator(RAGBaselineConfig{
		QdrantURL:      "http://127.0.0.1:6333",
		CollectionName: "memorybench_default",
	})
	if err != nil {
		t.Fatalf("NewRAGBaselineCandidateGovernanceEvaluator: %v", err)
	}

	governanceDecision, err := evaluator.EvaluateCandidate(t.Context(), GovernedMemoryCandidate{
		FactKey:         "name",
		FactValue:       "Ada",
		SourceText:      "Remember that my name is Ada.",
		CandidateSource: "explicit_fact",
		SourceChannel:   "user_input",
	})
	if err != nil {
		t.Fatalf("EvaluateCandidate: %v", err)
	}
	if !governanceDecision.ShouldPersist || governanceDecision.PersistenceDisposition != "persist" {
		t.Fatalf("expected permissive rag governance decision, got %#v", governanceDecision)
	}
}
