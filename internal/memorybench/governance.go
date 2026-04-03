package memorybench

import (
	"context"
	"fmt"
	"strings"
)

type ragBaselineCandidateGovernanceEvaluator struct{}

func NewPermissiveCandidateGovernanceEvaluator() CandidateGovernanceEvaluator {
	return ragBaselineCandidateGovernanceEvaluator{}
}

func NewRAGBaselineCandidateGovernanceEvaluator(ragBaselineConfig RAGBaselineConfig) (CandidateGovernanceEvaluator, error) {
	if err := ValidateRAGBaselineConfig(ragBaselineConfig); err != nil {
		return nil, err
	}
	return NewPermissiveCandidateGovernanceEvaluator(), nil
}

func (ragBaselineCandidateGovernanceEvaluator) EvaluateCandidate(ctx context.Context, candidate GovernedMemoryCandidate) (CandidateGovernanceDecision, error) {
	_ = ctx
	if strings.TrimSpace(candidate.FactKey) == "" {
		return CandidateGovernanceDecision{}, fmt.Errorf("fact_key is required")
	}
	if strings.TrimSpace(candidate.FactValue) == "" {
		return CandidateGovernanceDecision{}, fmt.Errorf("fact_value is required")
	}
	return CandidateGovernanceDecision{
		PersistenceDisposition: "persist",
		ShouldPersist:          true,
		HardDeny:               false,
		ReasonCode:             "rag_baseline_raw_ingest",
	}, nil
}
