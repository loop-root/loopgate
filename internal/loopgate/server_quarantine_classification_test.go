package loopgate

import (
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"testing"
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

func TestContradictoryClassificationIsRejected(t *testing.T) {
	_, err := normalizeResultClassification(controlapipkg.ResultClassification{
		Exposure: controlapipkg.ResultExposureDisplay,
		Eligibility: controlapipkg.ResultEligibility{
			Prompt: true,
		},
	}, "quarantine://bad")
	if err == nil {
		t.Fatal("expected contradictory prompt_eligible + quarantined classification to be denied")
	}

	_, err = normalizeResultClassification(controlapipkg.ResultClassification{
		Exposure: controlapipkg.ResultExposureAudit,
		Eligibility: controlapipkg.ResultEligibility{
			Prompt: true,
		},
	}, "")
	if err == nil {
		t.Fatal("expected contradictory audit_only + prompt_eligible classification to be denied")
	}
}
