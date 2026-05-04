package loopgate

import (
	"context"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientExecuteCapability_ReadAndWrite(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	writeResponse, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-write",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "notes.txt",
			"content": "hello loopgate",
		},
	})
	if err != nil {
		t.Fatalf("execute fs_write: %v", err)
	}
	if writeResponse.Status != controlapipkg.ResponseStatusSuccess {
		t.Fatalf("unexpected write response: %#v", writeResponse)
	}

	readResponse, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-read",
		Capability: "fs_read",
		Arguments: map[string]string{
			"path": "notes.txt",
		},
	})
	if err != nil {
		t.Fatalf("execute fs_read: %v", err)
	}
	if readResponse.StructuredResult["content"] != "hello loopgate" {
		t.Fatalf("unexpected structured read result: %#v", readResponse.StructuredResult)
	}
	if readResponse.QuarantineRef != "" {
		t.Fatalf("expected no quarantine ref for filesystem read, got %#v", readResponse)
	}
	if promptEligible, ok := readResponse.Metadata["prompt_eligible"].(bool); !ok || !promptEligible {
		t.Fatalf("expected fs_read to be prompt-eligible, got %#v", readResponse.Metadata)
	}
	if displayOnly, ok := readResponse.Metadata["display_only"].(bool); !ok || displayOnly {
		t.Fatalf("expected fs_read to not be display_only, got %#v", readResponse.Metadata)
	}

	if len(status.Capabilities) == 0 {
		t.Fatal("expected capabilities in status")
	}
}

func TestClientExecuteCapability_DeniesRawMemoryFilesystemAccess(t *testing.T) {
	repoRoot := t.TempDir()
	memoryDir := filepath.Join(repoRoot, ".loopgate", "memory")
	if err := os.MkdirAll(memoryDir, 0o700); err != nil {
		t.Fatalf("mkdir memory dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoryDir, "keys.json"), []byte("{\"keys\":[]}\n"), 0o600); err != nil {
		t.Fatalf("write keys file: %v", err)
	}

	policyYAML := strings.Replace(loopgatePolicyYAML(false), "    denied_paths: []\n", "    denied_paths:\n      - \".loopgate/memory\"\n      - \"runtime/state/memory\"\n", 1)
	client, _, _ := startLoopgateServer(t, repoRoot, policyYAML)

	readResponse, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-memory-read",
		Capability: "fs_read",
		Arguments: map[string]string{
			"path": ".loopgate/memory/keys.json",
		},
	})
	if err != nil {
		t.Fatalf("execute fs_read: %v", err)
	}
	if readResponse.Status != controlapipkg.ResponseStatusError {
		t.Fatalf("expected blocked read response, got %#v", readResponse)
	}
	if !strings.Contains(readResponse.DenialReason, "path denied") {
		t.Fatalf("expected path denial, got %#v", readResponse)
	}

	writeResponse, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-memory-write",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    ".loopgate/memory/keys.json",
			"content": "{\"keys\":[{\"id\":\"user.name\"}]}\n",
		},
	})
	if err != nil {
		t.Fatalf("execute fs_write: %v", err)
	}
	if writeResponse.Status != controlapipkg.ResponseStatusError {
		t.Fatalf("expected blocked write response, got %#v", writeResponse)
	}
	if !strings.Contains(writeResponse.DenialReason, "path denied") {
		t.Fatalf("expected path denial, got %#v", writeResponse)
	}
}
