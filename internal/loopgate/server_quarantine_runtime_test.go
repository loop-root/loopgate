package loopgate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"loopgate/internal/ledger"
)

func TestUnclassifiedCapabilityResultsDefaultToQuarantine(t *testing.T) {
	structuredResult, fieldsMeta, classification, quarantineRef, err := buildCapabilityResult("remote_fetch", map[string]string{}, "raw remote payload")
	if err != nil {
		t.Fatalf("build capability result: %v", err)
	}
	if len(structuredResult) != 0 {
		t.Fatalf("expected unclassified capability result to avoid returning raw output, got %#v", structuredResult)
	}
	if len(fieldsMeta) != 0 {
		t.Fatalf("expected no fields_meta for empty structured_result, got %#v", fieldsMeta)
	}
	if !classification.Quarantined() || !classification.AuditOnly() {
		t.Fatalf("expected unclassified capability to default to quarantine + audit_only, got %#v", classification)
	}
	if quarantineRef != "" {
		t.Fatalf("expected no placeholder quarantine ref before persistence, got %q", quarantineRef)
	}
	classification, err = normalizeResultClassification(classification, "quarantine://payloads/test-record")
	if err != nil {
		t.Fatalf("normalize classification: %v", err)
	}
	if classification.PromptEligible() {
		t.Fatalf("expected quarantined result to be non-prompt-eligible, got %#v", classification)
	}
}

func TestExecuteCapabilityRequest_PersistsQuarantinedPayload(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	remoteTool := fakeLoopgateTool{
		name:        "remote_fetch",
		category:    "filesystem",
		operation:   "read",
		description: "test remote fetch",
		output:      "raw remote payload",
	}
	server.registry.Register(remoteTool)
	client.ConfigureSession("test-actor", "test-session", append(capabilityNames(server.capabilitySummaries()), "remote_fetch"))

	response, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-remote",
		Capability: "remote_fetch",
	})
	if err != nil {
		t.Fatalf("execute remote_fetch: %v", err)
	}
	if response.Status != ResponseStatusSuccess {
		t.Fatalf("expected successful quarantined response, got %#v", response)
	}
	if len(response.StructuredResult) != 0 {
		t.Fatalf("expected quarantined response to avoid structured raw output, got %#v", response.StructuredResult)
	}
	if !strings.HasPrefix(response.QuarantineRef, quarantineRefPrefix) {
		t.Fatalf("expected persisted quarantine ref, got %q", response.QuarantineRef)
	}

	quarantinePath, err := quarantinePathFromRef(repoRoot, response.QuarantineRef)
	if err != nil {
		t.Fatalf("quarantine path from ref: %v", err)
	}
	recordBytes, err := os.ReadFile(quarantinePath)
	if err != nil {
		t.Fatalf("read quarantine record: %v", err)
	}

	var quarantinedPayloadRecord quarantinedPayloadRecord
	if err := json.Unmarshal(recordBytes, &quarantinedPayloadRecord); err != nil {
		t.Fatalf("decode quarantine record: %v", err)
	}
	if quarantinedPayloadRecord.StorageState != quarantineStorageStateBlobPresent {
		t.Fatalf("expected blob_present quarantine storage state, got %#v", quarantinedPayloadRecord)
	}
	if quarantinedPayloadRecord.RequestID != "req-remote" || quarantinedPayloadRecord.Capability != "remote_fetch" {
		t.Fatalf("unexpected quarantine metadata: %#v", quarantinedPayloadRecord)
	}
	if quarantinedPayloadRecord.RawPayloadSHA256 != payloadSHA256("raw remote payload") {
		t.Fatalf("unexpected payload hash: %#v", quarantinedPayloadRecord)
	}
	blobPath, err := quarantineBlobPathFromRef(repoRoot, response.QuarantineRef)
	if err != nil {
		t.Fatalf("quarantine blob path from ref: %v", err)
	}
	blobBytes, err := os.ReadFile(blobPath)
	if err != nil {
		t.Fatalf("read quarantine blob: %v", err)
	}
	if string(blobBytes) != "raw remote payload" {
		t.Fatalf("unexpected quarantined blob payload: %q", string(blobBytes))
	}

	metadata, err := response.ResultClassification()
	if err != nil {
		t.Fatalf("result classification: %v", err)
	}
	if !metadata.Quarantined() || !metadata.AuditOnly() {
		t.Fatalf("expected quarantined audit-only result, got %#v", metadata)
	}
}

func TestPromoteQuarantinedArtifact_CreatesDerivedArtifactAndAuditEvent(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	sourceQuarantineRef, err := server.storeQuarantinedPayload(CapabilityRequest{
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
	if gotFieldMeta := derivedArtifactRecord.DerivedFieldsMeta["summary"]; gotFieldMeta.Sensitivity != ResultFieldSensitivityTaintedText {
		t.Fatalf("expected promoted text field to remain tainted, got %#v", gotFieldMeta)
	}
}

func TestPromoteQuarantinedArtifact_DeniesHashMismatch(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	sourceQuarantineRef, err := server.storeQuarantinedPayload(CapabilityRequest{
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

	sourceQuarantineRef, err := server.storeQuarantinedPayload(CapabilityRequest{
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
}

func TestPromoteQuarantinedArtifact_DeniesExactDuplicatePromotion(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	sourcePayload := `{"summary":"safe display summary","healthy":true}`
	sourceQuarantineRef, err := server.storeQuarantinedPayload(CapabilityRequest{
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

func TestPruneQuarantinedPayload_PreservesLineageAndDeniesPromotion(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	sourcePayload := `{"summary":"safe display summary"}`
	sourceQuarantineRef, err := server.storeQuarantinedPayload(CapabilityRequest{
		RequestID:  "req-prune",
		Capability: "remote_fetch",
	}, sourcePayload)
	if err != nil {
		t.Fatalf("store quarantined payload: %v", err)
	}
	ageQuarantineRecordForPrune(t, repoRoot, sourceQuarantineRef)

	if err := server.pruneQuarantinedPayload(sourceQuarantineRef, "retention test"); err != nil {
		t.Fatalf("prune quarantined payload: %v", err)
	}

	prunedRecord, err := server.loadQuarantinedPayloadRecord(sourceQuarantineRef)
	if err != nil {
		t.Fatalf("load pruned record: %v", err)
	}
	if prunedRecord.StorageState != quarantineStorageStateBlobPruned {
		t.Fatalf("expected blob_pruned state, got %#v", prunedRecord)
	}
	if prunedRecord.RawPayloadSHA256 != payloadSHA256(sourcePayload) {
		t.Fatalf("expected source hash to remain after prune, got %#v", prunedRecord)
	}
	if prunedRecord.BlobPrunedAtUTC == "" {
		t.Fatalf("expected blob_pruned_at_utc to be set, got %#v", prunedRecord)
	}
	blobPath, err := quarantineBlobPathFromRef(repoRoot, sourceQuarantineRef)
	if err != nil {
		t.Fatalf("quarantine blob path from ref: %v", err)
	}
	if _, err := os.Stat(blobPath); !os.IsNotExist(err) {
		t.Fatalf("expected source blob to be removed after prune, got err=%v", err)
	}

	auditBytes, err := os.ReadFile(server.auditPath)
	if err != nil {
		t.Fatalf("read audit path: %v", err)
	}
	if !strings.Contains(string(auditBytes), "\"type\":\"artifact.blob_pruned\"") {
		t.Fatalf("expected artifact.blob_pruned audit event, got %s", auditBytes)
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
		TransformationType:    promotionTransformationIdentityCopy,
		DerivedClassification: derivedClassification,
	})
	if err == nil {
		t.Fatal("expected promotion from pruned source to be denied")
	}
	if !strings.Contains(err.Error(), "source_bytes_unavailable") {
		t.Fatalf("unexpected prune/promotion denial: %v", err)
	}
	if !strings.Contains(err.Error(), "blob_pruned") {
		t.Fatalf("expected blob_pruned detail in prune/promotion denial, got %v", err)
	}
}

func TestQuarantineMetadata_RemainsAvailableAfterPrune(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	sourcePayload := `{"summary":"safe display summary"}`
	sourceQuarantineRef, err := server.storeQuarantinedPayload(CapabilityRequest{
		RequestID:  "req-prune-metadata",
		Capability: "remote_fetch",
	}, sourcePayload)
	if err != nil {
		t.Fatalf("store quarantined payload: %v", err)
	}
	ageQuarantineRecordForPrune(t, repoRoot, sourceQuarantineRef)
	if err := server.pruneQuarantinedPayload(sourceQuarantineRef, "retention metadata test"); err != nil {
		t.Fatalf("prune quarantined payload: %v", err)
	}

	metadataResponse, err := client.QuarantineMetadata(context.Background(), sourceQuarantineRef)
	if err != nil {
		t.Fatalf("quarantine metadata after prune: %v", err)
	}
	if metadataResponse.QuarantineRef != sourceQuarantineRef {
		t.Fatalf("unexpected quarantine ref: %#v", metadataResponse)
	}
	if metadataResponse.StorageState != quarantineStorageStateBlobPruned {
		t.Fatalf("expected blob_pruned metadata state, got %#v", metadataResponse)
	}
	if metadataResponse.TrustState != quarantineTrustStateQuarantined {
		t.Fatalf("expected quarantined trust state, got %#v", metadataResponse)
	}
	if metadataResponse.ContentAvailability != quarantineContentAvailabilityMetadataOnly {
		t.Fatalf("expected metadata_only content availability after prune, got %#v", metadataResponse)
	}
	if metadataResponse.ContentSHA256 != payloadSHA256(sourcePayload) {
		t.Fatalf("expected retained source hash, got %#v", metadataResponse)
	}
	if metadataResponse.BlobPrunedAtUTC == "" {
		t.Fatalf("expected blob_pruned_at_utc to remain visible, got %#v", metadataResponse)
	}
}

func TestQuarantineMetadata_ReportsPruneEligibility(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	sourcePayload := `{"summary":"safe display summary"}`
	sourceQuarantineRef, err := server.storeQuarantinedPayload(CapabilityRequest{
		RequestID:  "req-prune-eligibility",
		Capability: "remote_fetch",
	}, sourcePayload)
	if err != nil {
		t.Fatalf("store quarantined payload: %v", err)
	}

	initialMetadata, err := client.QuarantineMetadata(context.Background(), sourceQuarantineRef)
	if err != nil {
		t.Fatalf("quarantine metadata: %v", err)
	}
	if initialMetadata.PruneEligible {
		t.Fatalf("expected fresh quarantine blob to be prune-ineligible, got %#v", initialMetadata)
	}
	if initialMetadata.TrustState != quarantineTrustStateQuarantined {
		t.Fatalf("expected quarantined trust state, got %#v", initialMetadata)
	}
	if initialMetadata.ContentAvailability != quarantineContentAvailabilityBlobAvailable {
		t.Fatalf("expected blob_available content availability, got %#v", initialMetadata)
	}
	if initialMetadata.PruneEligibleAtUTC == "" {
		t.Fatalf("expected prune eligibility timestamp, got %#v", initialMetadata)
	}

	ageQuarantineRecordForPrune(t, repoRoot, sourceQuarantineRef)

	eligibleMetadata, err := client.QuarantineMetadata(context.Background(), sourceQuarantineRef)
	if err != nil {
		t.Fatalf("quarantine metadata after aging: %v", err)
	}
	if !eligibleMetadata.PruneEligible {
		t.Fatalf("expected aged quarantine blob to be prune-eligible, got %#v", eligibleMetadata)
	}
}

func TestQuarantinePrune_DeniesIneligibleBlob(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	sourcePayload := `{"summary":"safe display summary"}`
	sourceQuarantineRef, err := server.storeQuarantinedPayload(CapabilityRequest{
		RequestID:  "req-prune-ineligible",
		Capability: "remote_fetch",
	}, sourcePayload)
	if err != nil {
		t.Fatalf("store quarantined payload: %v", err)
	}

	_, err = client.PruneQuarantinedPayload(context.Background(), sourceQuarantineRef)
	if err == nil {
		t.Fatal("expected ineligible prune to be denied")
	}
	if !strings.Contains(err.Error(), DenialCodeQuarantinePruneNotEligible) {
		t.Fatalf("expected quarantine_prune_not_eligible denial, got %v", err)
	}
}

func TestQuarantinePrune_DeniesDoublePrune(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	sourcePayload := `{"summary":"safe display summary"}`
	sourceQuarantineRef, err := server.storeQuarantinedPayload(CapabilityRequest{
		RequestID:  "req-prune-double",
		Capability: "remote_fetch",
	}, sourcePayload)
	if err != nil {
		t.Fatalf("store quarantined payload: %v", err)
	}

	ageQuarantineRecordForPrune(t, repoRoot, sourceQuarantineRef)
	if _, err := client.PruneQuarantinedPayload(context.Background(), sourceQuarantineRef); err != nil {
		t.Fatalf("first prune: %v", err)
	}

	_, err = client.PruneQuarantinedPayload(context.Background(), sourceQuarantineRef)
	if err == nil {
		t.Fatal("expected second prune to be denied")
	}
	if !strings.Contains(err.Error(), DenialCodeQuarantinePruneNotEligible) {
		t.Fatalf("expected quarantine_prune_not_eligible denial, got %v", err)
	}
}

func TestQuarantineView_LogsViewedEvent(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	sourcePayload := `{"summary":"safe display summary"}`
	sourceQuarantineRef, err := server.storeQuarantinedPayload(CapabilityRequest{
		RequestID:  "req-view",
		Capability: "remote_fetch",
	}, sourcePayload)
	if err != nil {
		t.Fatalf("store quarantined payload: %v", err)
	}

	viewResponse, err := client.ViewQuarantinedPayload(context.Background(), sourceQuarantineRef)
	if err != nil {
		t.Fatalf("view quarantined payload: %v", err)
	}
	if viewResponse.Metadata.QuarantineRef != sourceQuarantineRef {
		t.Fatalf("unexpected viewed quarantine ref: %#v", viewResponse)
	}
	if viewResponse.RawPayload != sourcePayload {
		t.Fatalf("unexpected viewed payload: %#v", viewResponse)
	}

	auditBytes, err := os.ReadFile(server.auditPath)
	if err != nil {
		t.Fatalf("read audit path: %v", err)
	}
	if !strings.Contains(string(auditBytes), "\"type\":\"artifact.viewed\"") {
		t.Fatalf("expected artifact.viewed audit event, got %s", auditBytes)
	}
}

func TestQuarantineView_DeniesPrunedBlob(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	sourcePayload := `{"summary":"safe display summary"}`
	sourceQuarantineRef, err := server.storeQuarantinedPayload(CapabilityRequest{
		RequestID:  "req-view-pruned",
		Capability: "remote_fetch",
	}, sourcePayload)
	if err != nil {
		t.Fatalf("store quarantined payload: %v", err)
	}
	ageQuarantineRecordForPrune(t, repoRoot, sourceQuarantineRef)
	if err := server.pruneQuarantinedPayload(sourceQuarantineRef, "retention view test"); err != nil {
		t.Fatalf("prune quarantined payload: %v", err)
	}

	_, err = client.ViewQuarantinedPayload(context.Background(), sourceQuarantineRef)
	if err == nil {
		t.Fatal("expected pruned blob view to be denied")
	}
	if !strings.Contains(err.Error(), DenialCodeSourceBytesUnavailable) {
		t.Fatalf("expected source_bytes_unavailable denial, got %v", err)
	}
	if !strings.Contains(err.Error(), "blob_pruned") {
		t.Fatalf("expected blob_pruned detail in view denial, got %v", err)
	}
}

func TestQuarantineView_FailsClosedOnAuditFailure(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	sourcePayload := `{"summary":"safe display summary"}`
	sourceQuarantineRef, err := server.storeQuarantinedPayload(CapabilityRequest{
		RequestID:  "req-view-audit-failure",
		Capability: "remote_fetch",
	}, sourcePayload)
	if err != nil {
		t.Fatalf("store quarantined payload: %v", err)
	}
	ageQuarantineRecordForPrune(t, repoRoot, sourceQuarantineRef)

	server.appendAuditEvent = func(string, ledger.Event) error {
		return errors.New("audit down")
	}

	_, err = client.ViewQuarantinedPayload(context.Background(), sourceQuarantineRef)
	if err == nil {
		t.Fatal("expected view audit failure to deny content access")
	}
	if !strings.Contains(err.Error(), DenialCodeAuditUnavailable) {
		t.Fatalf("expected audit_unavailable denial, got %v", err)
	}
}

func TestPruneQuarantinedPayload_FailsClosedOnAuditFailure(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	sourcePayload := `{"summary":"safe display summary"}`
	sourceQuarantineRef, err := server.storeQuarantinedPayload(CapabilityRequest{
		RequestID:  "req-prune",
		Capability: "remote_fetch",
	}, sourcePayload)
	if err != nil {
		t.Fatalf("store quarantined payload: %v", err)
	}

	server.appendAuditEvent = func(string, ledger.Event) error {
		return errors.New("audit down")
	}

	err = server.pruneQuarantinedPayload(sourceQuarantineRef, "retention test")
	if err == nil {
		t.Fatal("expected prune audit failure to deny pruning")
	}

	recordAfterFailure, err := server.loadQuarantinedPayloadRecord(sourceQuarantineRef)
	if err != nil {
		t.Fatalf("load record after prune failure: %v", err)
	}
	if recordAfterFailure.StorageState != quarantineStorageStateBlobPresent {
		t.Fatalf("expected blob to remain present after failed prune: %#v", recordAfterFailure)
	}
	blobPath, err := quarantineBlobPathFromRef(repoRoot, sourceQuarantineRef)
	if err != nil {
		t.Fatalf("quarantine blob path from ref: %v", err)
	}
	blobBytes, err := os.ReadFile(blobPath)
	if err != nil {
		t.Fatalf("read source blob after failed prune: %v", err)
	}
	if string(blobBytes) != sourcePayload {
		t.Fatalf("expected source blob rollback after failed prune, got %q", string(blobBytes))
	}
}

func TestPromoteQuarantinedArtifact_DeniesNonIdentityTransform(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	sourcePayload := `{"summary":"safe display summary"}`
	sourceQuarantineRef, err := server.storeQuarantinedPayload(CapabilityRequest{
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
	sourceQuarantineRef, err := server.storeQuarantinedPayload(CapabilityRequest{
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
	sourceQuarantineRef, err := server.storeQuarantinedPayload(CapabilityRequest{
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

func TestContradictoryClassificationIsRejected(t *testing.T) {
	_, err := normalizeResultClassification(ResultClassification{
		Exposure: ResultExposureDisplay,
		Eligibility: ResultEligibility{
			Prompt: true,
		},
	}, "quarantine://bad")
	if err == nil {
		t.Fatal("expected contradictory prompt_eligible + quarantined classification to be denied")
	}

	_, err = normalizeResultClassification(ResultClassification{
		Exposure: ResultExposureAudit,
		Eligibility: ResultEligibility{
			Memory: true,
		},
	}, "")
	if err == nil {
		t.Fatal("expected contradictory audit_only + display_only classification to be denied")
	}
}
