package tcl

import (
	"strings"
	"testing"
)

func TestAnalyzeMemoryCandidate_ExplicitFactIncludesAnchorTuple(t *testing.T) {
	analysisResult, err := AnalyzeMemoryCandidate(MemoryCandidate{
		Source:              CandidateSourceExplicitFact,
		SourceChannel:       "user_input",
		RawSourceText:       "remember that my name is Ada",
		NormalizedFactKey:   "name",
		NormalizedFactValue: "Ada",
		Trust:               TrustUserOriginated,
		Actor:               ObjectUser,
	})
	if err != nil {
		t.Fatalf("analyze explicit memory candidate: %v", err)
	}
	if analysisResult.AnchorVersion != "v1" || analysisResult.AnchorKey != "usr_profile:identity:fact:name" {
		t.Fatalf("expected explicit candidate anchor tuple, got version=%q key=%q", analysisResult.AnchorVersion, analysisResult.AnchorKey)
	}
	if analysisResult.Projection.AnchorVersion != analysisResult.AnchorVersion || analysisResult.Projection.AnchorKey != analysisResult.AnchorKey {
		t.Fatalf("expected semantic projection to mirror anchor tuple, got %#v", analysisResult.Projection)
	}
	if analysisResult.Projection.ExactSignature == "" || analysisResult.Projection.FamilySignature == "" {
		t.Fatalf("expected semantic projection signatures, got %#v", analysisResult.Projection)
	}
	if analysisResult.PolicyDecision.DISP != DispositionKeep {
		t.Fatalf("expected keep disposition for benign explicit candidate, got %#v", analysisResult.PolicyDecision)
	}
}

func TestAnalyzeMemoryCandidate_ContinuityCandidateDerivesAnchorWhenRecognized(t *testing.T) {
	analysisResult, err := AnalyzeMemoryCandidate(MemoryCandidate{
		Source:              CandidateSourceContinuity,
		SourceChannel:       "continuity_inspection",
		NormalizedFactKey:   "name",
		NormalizedFactValue: "Charlie",
		Trust:               TrustInferred,
		Actor:               ObjectSystem,
	})
	if err != nil {
		t.Fatalf("analyze continuity memory candidate: %v", err)
	}
	if analysisResult.AnchorVersion != "v1" || analysisResult.AnchorKey != "usr_profile:identity:fact:name" {
		t.Fatalf("expected continuity candidate anchor tuple, got version=%q key=%q", analysisResult.AnchorVersion, analysisResult.AnchorKey)
	}
	if analysisResult.Projection.AnchorVersion != "v1" || analysisResult.Projection.AnchorKey != "usr_profile:identity:fact:name" {
		t.Fatalf("expected continuity projection anchor tuple, got %#v", analysisResult.Projection)
	}
	if analysisResult.Projection.ExactSignature == "" || analysisResult.Projection.FamilySignature == "" {
		t.Fatalf("expected continuity projection signatures, got %#v", analysisResult.Projection)
	}
}

func TestAnalyzeMemoryCandidate_ContinuityCandidateAllowsUnanchoredFacts(t *testing.T) {
	analysisResult, err := AnalyzeMemoryCandidate(MemoryCandidate{
		Source:              CandidateSourceContinuity,
		SourceChannel:       "continuity_inspection",
		NormalizedFactKey:   "status_indicator",
		NormalizedFactValue: "green",
		Trust:               TrustInferred,
		Actor:               ObjectSystem,
	})
	if err != nil {
		t.Fatalf("analyze unanchored continuity candidate: %v", err)
	}
	if analysisResult.AnchorVersion != "" || analysisResult.AnchorKey != "" {
		t.Fatalf("expected unsupported continuity fact to remain unanchored, got version=%q key=%q", analysisResult.AnchorVersion, analysisResult.AnchorKey)
	}
	if analysisResult.Projection.AnchorVersion != "" || analysisResult.Projection.AnchorKey != "" {
		t.Fatalf("expected unsupported continuity projection to remain unanchored, got %#v", analysisResult.Projection)
	}
	if analysisResult.Projection.ExactSignature == "" || analysisResult.Projection.FamilySignature == "" {
		t.Fatalf("expected unanchored continuity projection signatures, got %#v", analysisResult.Projection)
	}
}

func TestAnalyzeMemoryCandidate_TaskMetadataUsesTaskSemanticFamily(t *testing.T) {
	analysisResult, err := AnalyzeMemoryCandidate(MemoryCandidate{
		Source:              CandidateSourceTaskMetadata,
		SourceChannel:       "capability_request",
		NormalizedFactKey:   "task.execution_class",
		NormalizedFactValue: "local_workspace_organize",
		Trust:               TrustSystemDerived,
		Actor:               ObjectSystem,
	})
	if err != nil {
		t.Fatalf("analyze task metadata candidate: %v", err)
	}
	if analysisResult.Node.OBJ != ObjectTask {
		t.Fatalf("expected task metadata to use task semantic object, got %#v", analysisResult.Node)
	}
	if analysisResult.AnchorVersion != "" || analysisResult.AnchorKey != "" {
		t.Fatalf("expected task metadata to remain unanchored, got version=%q key=%q", analysisResult.AnchorVersion, analysisResult.AnchorKey)
	}
	if analysisResult.Projection.ExactSignature == "" || analysisResult.Projection.FamilySignature == "" {
		t.Fatalf("expected task metadata projection signatures, got %#v", analysisResult.Projection)
	}
	if analysisResult.PolicyDecision.DISP != DispositionKeep {
		t.Fatalf("expected keep disposition for task metadata, got %#v", analysisResult.PolicyDecision)
	}
}

func TestAnalyzeMemoryCandidate_WorkflowTransitionUsesTaskPlanShape(t *testing.T) {
	analysisResult, err := AnalyzeMemoryCandidate(MemoryCandidate{
		Source:            CandidateSourceWorkflowStep,
		SourceChannel:     "continuity_inspection",
		NormalizedFactKey: "goal.closed",
		Trust:             TrustInferred,
		Actor:             ObjectSystem,
	})
	if err != nil {
		t.Fatalf("analyze workflow transition candidate: %v", err)
	}
	if analysisResult.Node.ACT != ActionPlan || analysisResult.Node.OBJ != ObjectTask || analysisResult.Node.STA != StateDone {
		t.Fatalf("expected workflow transition to use task plan shape, got %#v", analysisResult.Node)
	}
	if analysisResult.AnchorVersion != "" || analysisResult.AnchorKey != "" {
		t.Fatalf("expected workflow transition to remain unanchored, got version=%q key=%q", analysisResult.AnchorVersion, analysisResult.AnchorKey)
	}
	if analysisResult.Projection.ExactSignature == "" || analysisResult.Projection.FamilySignature == "" {
		t.Fatalf("expected workflow transition projection signatures, got %#v", analysisResult.Projection)
	}
}

func TestValidateSemanticProjection_DeniesInvalidAnchorShape(t *testing.T) {
	err := ValidateSemanticProjection(SemanticProjection{
		AnchorVersion:   "v1",
		AnchorKey:       "invalid anchor key",
		ExactSignature:  "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		FamilySignature: "sha256:2222222222222222222222222222222222222222222222222222222222222222",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid anchor key") {
		t.Fatalf("expected invalid anchor key denial, got %v", err)
	}
}

func TestValidateSemanticProjection_DeniesInvalidSignatureShape(t *testing.T) {
	err := ValidateSemanticProjection(SemanticProjection{
		AnchorVersion:   "v1",
		AnchorKey:       "usr_profile:identity:fact:name",
		ExactSignature:  "not-a-sha256",
		FamilySignature: "sha256:2222222222222222222222222222222222222222222222222222222222222222",
	})
	if err == nil || !strings.Contains(err.Error(), "exact_signature") {
		t.Fatalf("expected invalid signature denial, got %v", err)
	}
}
