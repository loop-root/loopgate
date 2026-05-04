package loopgate

import (
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromoteQuarantinedArtifact_DeniesExactDuplicatePromotion(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	sourcePayload := `{"summary":"safe display summary","healthy":true}`
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

	promotionInput := promotionRequest{
		SourceQuarantineRef:   sourceQuarantineRef,
		SourceContentSHA256:   payloadSHA256(sourcePayload),
		PromotionTarget:       PromotionTargetDisplay,
		PromotedBy:            "operator_123",
		SelectedFieldPaths:    []string{"summary"},
		TransformationType:    promotionTransformationIdentityCopy,
		DerivedClassification: derivedClassification,
	}

	if _, err := server.promoteQuarantinedArtifact(promotionInput); err != nil {
		t.Fatalf("first promotion: %v", err)
	}

	_, err = server.promoteQuarantinedArtifact(promotionInput)
	if err == nil {
		t.Fatal("expected exact duplicate promotion to be denied")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("unexpected duplicate denial: %v", err)
	}
}

func TestPromoteQuarantinedArtifact_DeniesExactDuplicatePromotionFromInMemoryIndex(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	sourcePayload := `{"summary":"safe display summary","healthy":true}`
	sourceQuarantineRef, err := server.storeQuarantinedPayload(controlapipkg.CapabilityRequest{
		RequestID:  "req-promote-index",
		Capability: "remote_fetch",
	}, sourcePayload)
	if err != nil {
		t.Fatalf("store quarantined payload: %v", err)
	}

	derivedClassification, err := canonicalDerivedClassificationForTarget(PromotionTargetDisplay)
	if err != nil {
		t.Fatalf("canonical derived classification: %v", err)
	}

	promotionInput := promotionRequest{
		SourceQuarantineRef:   sourceQuarantineRef,
		SourceContentSHA256:   payloadSHA256(sourcePayload),
		PromotionTarget:       PromotionTargetDisplay,
		PromotedBy:            "operator_123",
		SelectedFieldPaths:    []string{"summary"},
		TransformationType:    promotionTransformationIdentityCopy,
		DerivedClassification: derivedClassification,
	}

	firstPromotion, err := server.promoteQuarantinedArtifact(promotionInput)
	if err != nil {
		t.Fatalf("first promotion: %v", err)
	}
	if !server.promotionDuplicateIndexLoaded {
		t.Fatal("expected first promotion to initialize duplicate index")
	}

	server.derivedArtifactDir = filepath.Join(t.TempDir(), "missing-derived-artifacts")

	_, err = server.promoteQuarantinedArtifact(promotionInput)
	if err == nil {
		t.Fatal("expected exact duplicate promotion to be denied from the in-memory index")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("unexpected duplicate denial: %v", err)
	}
	if gotArtifactID := server.promotionDuplicateIndex[promotionDuplicateFingerprintForTest(t, firstPromotion)]; gotArtifactID != firstPromotion.DerivedArtifactID {
		t.Fatalf("expected duplicate index to retain first artifact id %q, got %q", firstPromotion.DerivedArtifactID, gotArtifactID)
	}
}

func TestPromoteQuarantinedArtifact_DeniesExactDuplicatePromotionAfterRestart(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	sourcePayload := `{"summary":"safe display summary","healthy":true}`
	sourceQuarantineRef, err := server.storeQuarantinedPayload(controlapipkg.CapabilityRequest{
		RequestID:  "req-promote-restart",
		Capability: "remote_fetch",
	}, sourcePayload)
	if err != nil {
		t.Fatalf("store quarantined payload: %v", err)
	}

	derivedClassification, err := canonicalDerivedClassificationForTarget(PromotionTargetDisplay)
	if err != nil {
		t.Fatalf("canonical derived classification: %v", err)
	}

	promotionInput := promotionRequest{
		SourceQuarantineRef:   sourceQuarantineRef,
		SourceContentSHA256:   payloadSHA256(sourcePayload),
		PromotionTarget:       PromotionTargetDisplay,
		PromotedBy:            "operator_123",
		SelectedFieldPaths:    []string{"summary"},
		TransformationType:    promotionTransformationIdentityCopy,
		DerivedClassification: derivedClassification,
	}

	firstPromotion, err := server.promoteQuarantinedArtifact(promotionInput)
	if err != nil {
		t.Fatalf("first promotion: %v", err)
	}

	restartedServer, err := NewServer(repoRoot, newShortLoopgateSocketPath(t))
	if err != nil {
		t.Fatalf("restart server: %v", err)
	}
	if restartedServer.promotionDuplicateIndexLoaded {
		t.Fatal("expected promotion duplicate index to stay lazy until first duplicate check")
	}

	_, err = restartedServer.promoteQuarantinedArtifact(promotionInput)
	if err == nil {
		t.Fatal("expected exact duplicate promotion to be denied after restart")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("unexpected duplicate denial after restart: %v", err)
	}
	if !restartedServer.promotionDuplicateIndexLoaded {
		t.Fatal("expected duplicate check to rebuild the promotion duplicate index after restart")
	}
	if gotArtifactID := restartedServer.promotionDuplicateIndex[promotionDuplicateFingerprintForTest(t, firstPromotion)]; gotArtifactID != firstPromotion.DerivedArtifactID {
		t.Fatalf("expected restarted duplicate index to resolve first artifact id %q, got %q", firstPromotion.DerivedArtifactID, gotArtifactID)
	}
}

func promotionDuplicateFingerprintForTest(t *testing.T, derivedArtifactRecord derivedArtifactRecord) string {
	t.Helper()

	fingerprint, err := promotionDuplicateDigest(derivedArtifactRecord)
	if err != nil {
		t.Fatalf("promotion duplicate digest: %v", err)
	}
	return fingerprint
}
