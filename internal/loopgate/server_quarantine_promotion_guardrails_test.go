package loopgate

import (
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"strings"
	"testing"
)

func TestPromoteQuarantinedArtifact_DeniesNonIdentityTransform(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	sourcePayload := `{"summary":"safe display summary"}`
	sourceQuarantineRef, err := server.storeQuarantinedPayload(controlapipkg.CapabilityRequest{
		RequestID:  "req-promote",
		Capability: "remote_fetch",
	}, sourcePayload)
	if err != nil {
		t.Fatalf("store quarantined payload: %v", err)
	}

	derivedClassification, err := canonicalDerivedClassificationForTarget(PromotionTargetDisplay)
	if err != nil {
		t.Fatalf("canonical derived classification: %v", err)
	}

	_, err = server.promoteQuarantinedArtifact(promotionRequest{
		SourceQuarantineRef:   sourceQuarantineRef,
		SourceContentSHA256:   payloadSHA256(sourcePayload),
		PromotionTarget:       PromotionTargetDisplay,
		PromotedBy:            "operator_123",
		SelectedFieldPaths:    []string{"summary"},
		TransformationType:    "rewrite_summary",
		DerivedClassification: derivedClassification,
	})
	if err == nil {
		t.Fatal("expected non-identity transform to be denied")
	}
	if !strings.Contains(err.Error(), "identity_copy") {
		t.Fatalf("unexpected transform denial: %v", err)
	}
}

func TestPromoteQuarantinedArtifact_DeniesNestedOrNonScalarSelection(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	sourcePayload := `{"nested":{"value":"nope"},"summary":"safe display summary"}`
	sourceQuarantineRef, err := server.storeQuarantinedPayload(controlapipkg.CapabilityRequest{
		RequestID:  "req-promote",
		Capability: "remote_fetch",
	}, sourcePayload)
	if err != nil {
		t.Fatalf("store quarantined payload: %v", err)
	}

	derivedClassification, err := canonicalDerivedClassificationForTarget(PromotionTargetDisplay)
	if err != nil {
		t.Fatalf("canonical derived classification: %v", err)
	}

	_, err = server.promoteQuarantinedArtifact(promotionRequest{
		SourceQuarantineRef:   sourceQuarantineRef,
		SourceContentSHA256:   payloadSHA256(sourcePayload),
		PromotionTarget:       PromotionTargetDisplay,
		PromotedBy:            "operator_123",
		SelectedFieldPaths:    []string{"nested.value"},
		TransformationType:    promotionTransformationIdentityCopy,
		DerivedClassification: derivedClassification,
	})
	if err == nil {
		t.Fatal("expected nested field selection to be denied")
	}
	if !strings.Contains(err.Error(), "top-level only") {
		t.Fatalf("unexpected nested selection denial: %v", err)
	}

	_, err = server.promoteQuarantinedArtifact(promotionRequest{
		SourceQuarantineRef:   sourceQuarantineRef,
		SourceContentSHA256:   payloadSHA256(sourcePayload),
		PromotionTarget:       PromotionTargetDisplay,
		PromotedBy:            "operator_123",
		SelectedFieldPaths:    []string{"nested"},
		TransformationType:    promotionTransformationIdentityCopy,
		DerivedClassification: derivedClassification,
	})
	if err == nil {
		t.Fatal("expected non-scalar selected field to be denied")
	}
	if !strings.Contains(err.Error(), "non-scalar") {
		t.Fatalf("unexpected non-scalar denial: %v", err)
	}
}

func TestPromoteQuarantinedArtifact_DeniesTaintedTextForPromptTarget(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	sourcePayload := `{"summary":"tainted remote text"}`
	sourceQuarantineRef, err := server.storeQuarantinedPayload(controlapipkg.CapabilityRequest{
		RequestID:  "req-promote-prompt",
		Capability: "remote_fetch",
	}, sourcePayload)
	if err != nil {
		t.Fatalf("store quarantined payload: %v", err)
	}

	derivedClassification, err := canonicalDerivedClassificationForTarget(PromotionTargetPrompt)
	if err != nil {
		t.Fatalf("canonical derived classification: %v", err)
	}

	_, err = server.promoteQuarantinedArtifact(promotionRequest{
		SourceQuarantineRef:   sourceQuarantineRef,
		SourceContentSHA256:   payloadSHA256(sourcePayload),
		PromotionTarget:       PromotionTargetPrompt,
		PromotedBy:            "operator_123",
		SelectedFieldPaths:    []string{"summary"},
		TransformationType:    promotionTransformationIdentityCopy,
		DerivedClassification: derivedClassification,
	})
	if err == nil {
		t.Fatal("expected tainted text promotion to prompt to be denied")
	}
	if !strings.Contains(err.Error(), "display-only") {
		t.Fatalf("unexpected tainted text prompt denial: %v", err)
	}
}
