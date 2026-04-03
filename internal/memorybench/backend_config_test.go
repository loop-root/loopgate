package memorybench

import "testing"

func TestNormalizeBenchmarkBackendName_DefaultsToContinuityTCL(t *testing.T) {
	backendName, err := NormalizeBenchmarkBackendName("")
	if err != nil {
		t.Fatalf("NormalizeBenchmarkBackendName: %v", err)
	}
	if backendName != BackendContinuityTCL {
		t.Fatalf("expected continuity backend, got %q", backendName)
	}
}

func TestNormalizeBenchmarkBackendName_AcceptsRAGStronger(t *testing.T) {
	backendName, err := NormalizeBenchmarkBackendName("rag_stronger")
	if err != nil {
		t.Fatalf("NormalizeBenchmarkBackendName: %v", err)
	}
	if backendName != BackendRAGStronger {
		t.Fatalf("expected stronger rag backend, got %q", backendName)
	}
}

func TestNormalizeBenchmarkBackendName_RejectsUnknownBackend(t *testing.T) {
	_, err := NormalizeBenchmarkBackendName("mystery_backend")
	if err == nil {
		t.Fatal("expected unknown backend to fail normalization")
	}
}

func TestIsRAGBenchmarkBackend_RecognizesBothRAGBackends(t *testing.T) {
	for _, backendName := range []string{BackendRAGBaseline, BackendRAGStronger} {
		isRAGBackend, err := IsRAGBenchmarkBackend(backendName)
		if err != nil {
			t.Fatalf("IsRAGBenchmarkBackend(%q): %v", backendName, err)
		}
		if !isRAGBackend {
			t.Fatalf("expected %q to be recognized as a rag benchmark backend", backendName)
		}
	}
}

func TestValidateRAGBaselineConfig_RequiresQdrantURLAndCollection(t *testing.T) {
	if err := ValidateRAGBaselineConfig(RAGBaselineConfig{}); err == nil {
		t.Fatal("expected empty rag baseline config to fail")
	}
	if err := ValidateRAGBaselineConfig(RAGBaselineConfig{
		QdrantURL: "http://127.0.0.1:6333",
	}); err == nil {
		t.Fatal("expected missing collection name to fail")
	}
	if err := ValidateRAGBaselineConfig(RAGBaselineConfig{
		QdrantURL:      "http://127.0.0.1:6333",
		CollectionName: "memorybench_default",
	}); err != nil {
		t.Fatalf("expected minimal rag baseline config to validate, got %v", err)
	}
}

func TestNewRAGBaselineDiscoverer_FailsClosedWhileUnwired(t *testing.T) {
	_, err := NewRAGBaselineDiscoverer(RAGBaselineConfig{
		QdrantURL:      "http://127.0.0.1:6333",
		CollectionName: "memorybench_default",
	})
	if err == nil {
		t.Fatal("expected unwired rag baseline discoverer to fail closed")
	}
}

func TestNormalizeCandidateGovernanceMode_DefaultsToBackendDefault(t *testing.T) {
	candidateGovernanceMode, err := NormalizeCandidateGovernanceMode("")
	if err != nil {
		t.Fatalf("NormalizeCandidateGovernanceMode: %v", err)
	}
	if candidateGovernanceMode != CandidateGovernanceBackendDefault {
		t.Fatalf("expected backend_default governance mode, got %q", candidateGovernanceMode)
	}
}

func TestNormalizeCandidateGovernanceMode_AcceptsContinuityAndPermissive(t *testing.T) {
	for _, rawCandidateGovernanceMode := range []string{CandidateGovernanceContinuityTCL, CandidateGovernancePermissive} {
		candidateGovernanceMode, err := NormalizeCandidateGovernanceMode(rawCandidateGovernanceMode)
		if err != nil {
			t.Fatalf("NormalizeCandidateGovernanceMode(%q): %v", rawCandidateGovernanceMode, err)
		}
		if candidateGovernanceMode != rawCandidateGovernanceMode {
			t.Fatalf("expected candidate governance mode %q, got %q", rawCandidateGovernanceMode, candidateGovernanceMode)
		}
	}
}

func TestNormalizeCandidateGovernanceMode_RejectsUnknownMode(t *testing.T) {
	_, err := NormalizeCandidateGovernanceMode("mystery_governance")
	if err == nil {
		t.Fatal("expected unknown governance mode to fail normalization")
	}
}
