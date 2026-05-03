package loopgate

import (
	"context"
	"errors"
	"loopgate/internal/ledger"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"strings"
	"testing"
)

func TestPruneQuarantinedPayload_PreservesLineageAndDeniesPromotion(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	sourcePayload := `{"summary":"safe display summary"}`
	sourceQuarantineRef, err := server.storeQuarantinedPayload(controlapipkg.CapabilityRequest{
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
	sourceQuarantineRef, err := server.storeQuarantinedPayload(controlapipkg.CapabilityRequest{
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
	sourceQuarantineRef, err := server.storeQuarantinedPayload(controlapipkg.CapabilityRequest{
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
	sourceQuarantineRef, err := server.storeQuarantinedPayload(controlapipkg.CapabilityRequest{
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
	if !strings.Contains(err.Error(), controlapipkg.DenialCodeQuarantinePruneNotEligible) {
		t.Fatalf("expected quarantine_prune_not_eligible denial, got %v", err)
	}
}

func TestQuarantinePrune_DeniesDoublePrune(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	sourcePayload := `{"summary":"safe display summary"}`
	sourceQuarantineRef, err := server.storeQuarantinedPayload(controlapipkg.CapabilityRequest{
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
	if !strings.Contains(err.Error(), controlapipkg.DenialCodeQuarantinePruneNotEligible) {
		t.Fatalf("expected quarantine_prune_not_eligible denial, got %v", err)
	}
}

func TestQuarantineView_LogsViewedEvent(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	sourcePayload := `{"summary":"safe display summary"}`
	sourceQuarantineRef, err := server.storeQuarantinedPayload(controlapipkg.CapabilityRequest{
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
	sourceQuarantineRef, err := server.storeQuarantinedPayload(controlapipkg.CapabilityRequest{
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
	if !strings.Contains(err.Error(), controlapipkg.DenialCodeSourceBytesUnavailable) {
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
	sourceQuarantineRef, err := server.storeQuarantinedPayload(controlapipkg.CapabilityRequest{
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
	if !strings.Contains(err.Error(), controlapipkg.DenialCodeAuditUnavailable) {
		t.Fatalf("expected audit_unavailable denial, got %v", err)
	}
}

func TestPruneQuarantinedPayload_FailsClosedOnAuditFailure(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	sourcePayload := `{"summary":"safe display summary"}`
	sourceQuarantineRef, err := server.storeQuarantinedPayload(controlapipkg.CapabilityRequest{
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
