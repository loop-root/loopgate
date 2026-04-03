package memorybench

import (
	"fmt"
	"strings"
)

const (
	BackendContinuityTCL = "continuity_tcl"
	BackendRAGBaseline   = "rag_baseline"
	BackendRAGStronger   = "rag_stronger"
	BackendHybrid        = "hybrid"

	CandidateGovernanceBackendDefault = "backend_default"
	CandidateGovernanceContinuityTCL  = "continuity_tcl"
	CandidateGovernancePermissive     = "permissive"
)

type RAGBaselineConfig struct {
	QdrantURL      string
	CollectionName string
	EmbeddingModel string
	RerankerName   string
}

func NormalizeBenchmarkBackendName(rawBackendName string) (string, error) {
	normalizedBackendName := strings.TrimSpace(rawBackendName)
	switch normalizedBackendName {
	case "", BackendContinuityTCL:
		return BackendContinuityTCL, nil
	case BackendRAGBaseline:
		return BackendRAGBaseline, nil
	case BackendRAGStronger:
		return BackendRAGStronger, nil
	case BackendHybrid:
		return BackendHybrid, nil
	default:
		return "", fmt.Errorf("unknown benchmark backend %q", rawBackendName)
	}
}

func IsRAGBenchmarkBackend(rawBackendName string) (bool, error) {
	normalizedBackendName, err := NormalizeBenchmarkBackendName(rawBackendName)
	if err != nil {
		return false, err
	}
	switch normalizedBackendName {
	case BackendRAGBaseline, BackendRAGStronger:
		return true, nil
	default:
		return false, nil
	}
}

func ValidateRAGBaselineConfig(ragBaselineConfig RAGBaselineConfig) error {
	if strings.TrimSpace(ragBaselineConfig.QdrantURL) == "" {
		return fmt.Errorf("rag baseline qdrant url is required")
	}
	if strings.TrimSpace(ragBaselineConfig.CollectionName) == "" {
		return fmt.Errorf("rag baseline collection name is required")
	}
	return nil
}

func NormalizeCandidateGovernanceMode(rawCandidateGovernanceMode string) (string, error) {
	normalizedCandidateGovernanceMode := strings.TrimSpace(rawCandidateGovernanceMode)
	switch normalizedCandidateGovernanceMode {
	case "", CandidateGovernanceBackendDefault:
		return CandidateGovernanceBackendDefault, nil
	case CandidateGovernanceContinuityTCL:
		return CandidateGovernanceContinuityTCL, nil
	case CandidateGovernancePermissive:
		return CandidateGovernancePermissive, nil
	default:
		return "", fmt.Errorf("unknown candidate governance mode %q", rawCandidateGovernanceMode)
	}
}
