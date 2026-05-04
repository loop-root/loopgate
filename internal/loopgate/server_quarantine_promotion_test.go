package loopgate

import (
	"errors"
	"loopgate/internal/ledger"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"strings"
	"testing"
)

func TestPromoteQuarantinedArtifact_CreatesDerivedArtifactAndAuditEvent(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	sourceQuarantineRef, err := server.storeQuarantinedPayload(controlapipkg.CapabilityRequest{
		RequestID:  "req-promote",
		Capability: "remote_fetch",
	}, `{"summary":"safe display summary","healthy":true}`)
	if err != nil {
		t.Fatalf("store quarantined payload: %v", err)
	}

	derivedClassification, err := canonicalDerivedClassificationForTarget(PromotionTargetDisplay)
	if err != nil {
		t.Fatalf("normalize derived classification: %v", err)
	}

	derivedArtifactRecord, err := server.promoteQuarantinedArtifact(promotionRequest{
		SourceQuarantineRef:   sourceQuarantineRef,
		SourceContentSHA256:   payloadSHA256(`{"summary":"safe display summary","healthy":true}`),
		PromotionTarget:       PromotionTargetDisplay,
		PromotedBy:            "operator_123",
		SelectedFieldPaths:    []string{"summary"},
		TransformationType:    promotionTransformationIdentityCopy,
		DerivedClassification: derivedClassification,
	})
	if err != nil {
		t.Fatalf("promote quarantined artifact: %v", err)
	}
	if derivedArtifactRecord.SourceQuarantineRef != sourceQuarantineRef {
		t.Fatalf("unexpected derived artifact source ref: %#v", derivedArtifactRecord)
	}
	derivedArtifactPath := server.derivedArtifactPath(derivedArtifactRecord.DerivedArtifactID)
	if _, err := os.Stat(derivedArtifactPath); err != nil {
		t.Fatalf("expected derived artifact file to exist: %v", err)
	}
	sourceQuarantinePath, err := quarantinePathFromRef(repoRoot, sourceQuarantineRef)
	if err != nil {
		t.Fatalf("quarantine path from ref: %v", err)
	}
	if _, err := os.Stat(sourceQuarantinePath); err != nil {
		t.Fatalf("expected source quarantine file to remain after promotion: %v", err)
	}

	auditBytes, err := os.ReadFile(server.auditPath)
	if err != nil {
		t.Fatalf("read audit path: %v", err)
	}
	if !strings.Contains(string(auditBytes), "\"type\":\"artifact.promoted\"") {
		t.Fatalf("expected artifact.promoted audit event, got %s", auditBytes)
	}
	if gotSummary := derivedArtifactRecord.DerivedArtifact["summary"]; gotSummary != "safe display summary" {
		t.Fatalf("unexpected derived artifact payload: %#v", derivedArtifactRecord)
	}
	if gotFieldMeta := derivedArtifactRecord.DerivedFieldsMeta["summary"]; gotFieldMeta.Sensitivity != controlapipkg.ResultFieldSensitivityTaintedText {
		t.Fatalf("expected promoted text field to remain tainted, got %#v", gotFieldMeta)
	}
}

func TestPromoteQuarantinedArtifact_DeniesHashMismatch(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	sourceQuarantineRef, err := server.storeQuarantinedPayload(controlapipkg.CapabilityRequest{
		RequestID:  "req-promote",
		Capability: "remote_fetch",
	}, `{"summary":"safe display summary"}`)
	if err != nil {
		t.Fatalf("store quarantined payload: %v", err)
	}

	derivedClassification, err := canonicalDerivedClassificationForTarget(PromotionTargetDisplay)
	if err != nil {
		t.Fatalf("canonical derived classification: %v", err)
	}

	_, err = server.promoteQuarantinedArtifact(promotionRequest{
		SourceQuarantineRef:   sourceQuarantineRef,
		SourceContentSHA256:   payloadSHA256(`{"summary":"different payload"}`),
		PromotionTarget:       PromotionTargetDisplay,
		PromotedBy:            "operator_123",
		SelectedFieldPaths:    []string{"summary"},
		TransformationType:    promotionTransformationIdentityCopy,
		DerivedClassification: derivedClassification,
	})
	if err == nil {
		t.Fatal("expected source hash mismatch to be denied")
	}
	if !strings.Contains(err.Error(), "source_content_sha256") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPromoteQuarantinedArtifact_DeniesMissingSource(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	derivedClassification, err := canonicalDerivedClassificationForTarget(PromotionTargetDisplay)
	if err != nil {
		t.Fatalf("canonical derived classification: %v", err)
	}

	_, err = server.promoteQuarantinedArtifact(promotionRequest{
		SourceQuarantineRef:   quarantineRefForID("missingartifact"),
		SourceContentSHA256:   payloadSHA256(`{"summary":"safe display summary"}`),
		PromotionTarget:       PromotionTargetDisplay,
		PromotedBy:            "operator_123",
		SelectedFieldPaths:    []string{"summary"},
		TransformationType:    promotionTransformationIdentityCopy,
		DerivedClassification: derivedClassification,
	})
	if err == nil {
		t.Fatal("expected missing source to be denied")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPromoteQuarantinedArtifact_FailsClosedOnAuditFailure(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	sourceQuarantineRef, err := server.storeQuarantinedPayload(controlapipkg.CapabilityRequest{
		RequestID:  "req-promote",
		Capability: "remote_fetch",
	}, `{"summary":"safe display summary"}`)
	if err != nil {
		t.Fatalf("store quarantined payload: %v", err)
	}

	server.appendAuditEvent = func(string, ledger.Event) error {
		return errors.New("audit down")
	}

	derivedClassification, err := canonicalDerivedClassificationForTarget(PromotionTargetDisplay)
	if err != nil {
		t.Fatalf("canonical derived classification: %v", err)
	}

	_, err = server.promoteQuarantinedArtifact(promotionRequest{
		SourceQuarantineRef:   sourceQuarantineRef,
		SourceContentSHA256:   payloadSHA256(`{"summary":"safe display summary"}`),
		PromotionTarget:       PromotionTargetDisplay,
		PromotedBy:            "operator_123",
		SelectedFieldPaths:    []string{"summary"},
		TransformationType:    promotionTransformationIdentityCopy,
		DerivedClassification: derivedClassification,
	})
	if err == nil {
		t.Fatal("expected audit failure to deny promotion")
	}
	derivedEntries, err := os.ReadDir(server.derivedArtifactDir)
	if err != nil {
		t.Fatalf("read derived artifact dir: %v", err)
	}
	if len(derivedEntries) != 0 {
		t.Fatalf("expected no derived artifacts to remain after audit failure, got %d", len(derivedEntries))
	}
	if !server.promotionDuplicateIndexLoaded {
		t.Fatal("expected promotion duplicate index to be initialized during promotion attempt")
	}
	if len(server.promotionDuplicateIndex) != 0 {
		t.Fatalf("expected audit-failed promotion cleanup to remove duplicate fingerprint, got %#v", server.promotionDuplicateIndex)
	}
}
