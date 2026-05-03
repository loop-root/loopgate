package loopgate

import (
	"context"
	"encoding/json"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"strings"
	"testing"
)

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
	if err := server.registry.Register(remoteTool); err != nil {
		t.Fatalf("register remote_fetch: %v", err)
	}
	client.ConfigureSession("test-actor", "test-session", append(capabilityNames(server.capabilitySummaries()), "remote_fetch"))

	response, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-remote",
		Capability: "remote_fetch",
	})
	if err != nil {
		t.Fatalf("execute remote_fetch: %v", err)
	}
	if response.Status != controlapipkg.ResponseStatusSuccess {
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
