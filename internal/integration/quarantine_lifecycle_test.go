package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	controlapipkg "loopgate/internal/loopgate/controlapi"
)

const integrationQuarantineRefPrefix = "quarantine://payloads/"

type integrationQuarantineRecord struct {
	SchemaVersion        string `json:"schema_version"`
	QuarantineID         string `json:"quarantine_id"`
	StoredAtUTC          string `json:"stored_at_utc"`
	StorageState         string `json:"storage_state"`
	RequestID            string `json:"request_id"`
	Capability           string `json:"capability"`
	NormalizedArgHash    string `json:"normalized_argument_hash"`
	RawPayloadSHA256     string `json:"raw_payload_sha256"`
	RawPayloadByteLength int    `json:"raw_payload_byte_length"`
	BlobPrunedAtUTC      string `json:"blob_pruned_at_utc,omitempty"`
}

func TestQuarantineLifecycleOverRealSocket(t *testing.T) {
	rawPayload := `{"summary":"degraded","status":"yellow"}`
	statusServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/status" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(rawPayload))
	}))
	defer statusServer.Close()

	statusServerURL, err := url.Parse(statusServer.URL)
	if err != nil {
		t.Fatalf("parse status server url: %v", err)
	}

	capabilityName := "statuspage.summary_get"
	harness := newLoopgateHarnessWithSetup(t, integrationHTTPEnabledPolicyYAML(), func(repoRoot string) error {
		return writeConfiguredPublicReadCapability(repoRoot, statusServer.URL, statusServerURL.Hostname(), capabilityName)
	})

	status := harness.waitForStatus(t)
	if !containsString(capabilityNames(status.Capabilities), capabilityName) {
		t.Fatalf("expected status capabilities to include %q, got %#v", capabilityName, status.Capabilities)
	}

	client := harness.newClient("integration-actor", "integration-quarantine", advertisedSessionCapabilityNames(status))
	executeResponse, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-quarantine",
		Capability: capabilityName,
	})
	if err != nil {
		t.Fatalf("execute configured capability: %v", err)
	}
	if executeResponse.Status != controlapipkg.ResponseStatusSuccess {
		t.Fatalf("expected configured capability success, got %#v", executeResponse)
	}
	if !strings.HasPrefix(executeResponse.QuarantineRef, integrationQuarantineRefPrefix) {
		t.Fatalf("expected quarantine ref, got %#v", executeResponse)
	}

	metadataBeforePrune, err := client.QuarantineMetadata(context.Background(), executeResponse.QuarantineRef)
	if err != nil {
		t.Fatalf("quarantine metadata before prune: %v", err)
	}
	if metadataBeforePrune.ContentSHA256 != integrationPayloadSHA256(rawPayload) {
		t.Fatalf("expected quarantine metadata content hash %q, got %#v", integrationPayloadSHA256(rawPayload), metadataBeforePrune)
	}
	if metadataBeforePrune.ContentAvailability != "blob_available" {
		t.Fatalf("expected blob_available content, got %#v", metadataBeforePrune)
	}
	if metadataBeforePrune.StorageState != "blob_present" {
		t.Fatalf("expected blob_present storage state, got %#v", metadataBeforePrune)
	}
	if metadataBeforePrune.PruneEligible {
		t.Fatalf("expected fresh quarantine blob to be prune-ineligible, got %#v", metadataBeforePrune)
	}

	viewResponse, err := client.ViewQuarantinedPayload(context.Background(), executeResponse.QuarantineRef)
	if err != nil {
		t.Fatalf("view quarantined payload: %v", err)
	}
	if viewResponse.RawPayload != rawPayload {
		t.Fatalf("expected quarantined payload %q, got %#v", rawPayload, viewResponse)
	}
	if viewResponse.Metadata.ContentSHA256 != metadataBeforePrune.ContentSHA256 {
		t.Fatalf("expected view metadata hash to match metadata response, got %#v", viewResponse)
	}

	if err := ageQuarantineRecordForPrune(harness.repoRoot, executeResponse.QuarantineRef); err != nil {
		t.Fatalf("age quarantine record for prune: %v", err)
	}

	metadataEligibleForPrune, err := client.QuarantineMetadata(context.Background(), executeResponse.QuarantineRef)
	if err != nil {
		t.Fatalf("quarantine metadata after aging: %v", err)
	}
	if !metadataEligibleForPrune.PruneEligible {
		t.Fatalf("expected aged quarantine blob to be prune-eligible, got %#v", metadataEligibleForPrune)
	}

	prunedMetadata, err := client.PruneQuarantinedPayload(context.Background(), executeResponse.QuarantineRef)
	if err != nil {
		t.Fatalf("prune quarantined payload: %v", err)
	}
	if prunedMetadata.StorageState != "blob_pruned" {
		t.Fatalf("expected blob_pruned storage state, got %#v", prunedMetadata)
	}
	if prunedMetadata.ContentAvailability != "metadata_only" {
		t.Fatalf("expected metadata_only content after prune, got %#v", prunedMetadata)
	}
	if prunedMetadata.BlobPrunedAtUTC == "" {
		t.Fatalf("expected blob_pruned_at_utc after prune, got %#v", prunedMetadata)
	}

	blobPath, err := quarantineBlobPath(harness.repoRoot, executeResponse.QuarantineRef)
	if err != nil {
		t.Fatalf("quarantine blob path: %v", err)
	}
	if _, statErr := os.Stat(blobPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected quarantine blob to be removed after prune, got err=%v", statErr)
	}

	metadataAfterPrune, err := client.QuarantineMetadata(context.Background(), executeResponse.QuarantineRef)
	if err != nil {
		t.Fatalf("quarantine metadata after prune: %v", err)
	}
	if metadataAfterPrune.ContentSHA256 != integrationPayloadSHA256(rawPayload) {
		t.Fatalf("expected quarantine hash to remain after prune, got %#v", metadataAfterPrune)
	}
	if metadataAfterPrune.ContentAvailability != "metadata_only" {
		t.Fatalf("expected metadata_only content after prune, got %#v", metadataAfterPrune)
	}

	_, err = client.ViewQuarantinedPayload(context.Background(), executeResponse.QuarantineRef)
	if err == nil {
		t.Fatal("expected view after prune to be denied")
	}
	if !strings.Contains(err.Error(), controlapipkg.DenialCodeSourceBytesUnavailable) {
		t.Fatalf("expected source bytes unavailable denial after prune, got %v", err)
	}

	_, auditBytes := harness.readAuditEvents(t)
	if !strings.Contains(string(auditBytes), "\"type\":\"artifact.viewed\"") {
		t.Fatalf("expected audit to contain artifact.viewed, got %s", auditBytes)
	}
	if !strings.Contains(string(auditBytes), "\"type\":\"artifact.blob_pruned\"") {
		t.Fatalf("expected audit to contain artifact.blob_pruned, got %s", auditBytes)
	}
}

func writeConfiguredPublicReadCapability(repoRoot string, apiBaseURL string, allowedHost string, capabilityName string) error {
	connectionDir := filepath.Join(repoRoot, "loopgate", "connections")
	if err := os.MkdirAll(connectionDir, 0o700); err != nil {
		return err
	}

	configYAML := "" +
		"provider: statuspage\n" +
		"grant_type: public_read\n" +
		"subject: statusapi\n" +
		"api_base_url: " + apiBaseURL + "\n" +
		"allowed_hosts:\n" +
		"  - " + allowedHost + "\n" +
		"capabilities:\n" +
		"  - name: " + capabilityName + "\n" +
		"    description: Read public status summary.\n" +
		"    method: GET\n" +
		"    path: /status\n" +
		"    content_class: structured_json\n" +
		"    extractor: json_field_allowlist\n" +
		"    response_fields:\n" +
		"      - name: summary\n" +
		"        json_field: summary\n" +
		"        sensitivity: tainted_text\n" +
		"        max_inline_bytes: 256\n"

	return os.WriteFile(filepath.Join(connectionDir, "statuspage.yaml"), []byte(configYAML), 0o600)
}

func ageQuarantineRecordForPrune(repoRoot string, quarantineRef string) error {
	recordPath, err := quarantineRecordPath(repoRoot, quarantineRef)
	if err != nil {
		return err
	}
	recordBytes, err := os.ReadFile(recordPath)
	if err != nil {
		return err
	}

	var record integrationQuarantineRecord
	if err := json.Unmarshal(recordBytes, &record); err != nil {
		return err
	}
	record.StoredAtUTC = time.Now().UTC().Add(-(8 * 24 * time.Hour)).Format(time.RFC3339Nano)

	updatedBytes, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	updatedBytes = append(updatedBytes, '\n')
	return os.WriteFile(recordPath, updatedBytes, 0o600)
}

func quarantineRecordPath(repoRoot string, quarantineRef string) (string, error) {
	quarantineID, err := quarantineIDFromRef(quarantineRef)
	if err != nil {
		return "", err
	}
	return filepath.Join(repoRoot, "runtime", "state", "quarantine", quarantineID+".json"), nil
}

func quarantineBlobPath(repoRoot string, quarantineRef string) (string, error) {
	quarantineID, err := quarantineIDFromRef(quarantineRef)
	if err != nil {
		return "", err
	}
	return filepath.Join(repoRoot, "runtime", "state", "quarantine", "blobs", quarantineID+".blob"), nil
}

func quarantineIDFromRef(quarantineRef string) (string, error) {
	if !strings.HasPrefix(quarantineRef, integrationQuarantineRefPrefix) {
		return "", os.ErrInvalid
	}
	return strings.TrimPrefix(quarantineRef, integrationQuarantineRefPrefix), nil
}

func integrationPayloadSHA256(rawPayload string) string {
	payloadHash := sha256.Sum256([]byte(rawPayload))
	return hex.EncodeToString(payloadHash[:])
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func integrationHTTPEnabledPolicyYAML() string {
	return "version: 0.1.0\n\n" +
		"tools:\n" +
		"  filesystem:\n" +
		"    allowed_roots:\n" +
		"      - \".\"\n" +
		"    denied_paths:\n" +
		"      - \"core/policy\"\n" +
		"    read_enabled: true\n" +
		"    write_enabled: true\n" +
		"    write_requires_approval: false\n" +
		"  http:\n" +
		"    enabled: true\n" +
		"    allowed_domains: []\n" +
		"    requires_approval: false\n" +
		"    timeout_seconds: 10\n" +
		"  shell:\n" +
		"    enabled: false\n" +
		"    allowed_commands: []\n" +
		"    requires_approval: true\n" +
		"logging:\n" +
		"  log_commands: true\n" +
		"  log_tool_calls: true\n" +
		"safety:\n" +
		"  allow_persona_modification: false\n" +
		"  allow_policy_modification: false\n"
}
