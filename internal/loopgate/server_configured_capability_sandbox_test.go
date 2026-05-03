package loopgate

import (
	"context"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSandboxExportDeniesNonOutputsPath(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	hostRootPath := t.TempDir()
	pinTestProcessAsExpectedClient(t, server)
	client.SetOperatorMountPaths([]string{hostRootPath}, hostRootPath)
	client.ConfigureSession("operator", "operator-sandbox-export-non-outputs", advertisedSessionCapabilityNames(status))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure operator sandbox token: %v", err)
	}

	hostSourcePath := filepath.Join(hostRootPath, "example.txt")
	if err := os.WriteFile(hostSourcePath, []byte("sandbox flow"), 0o600); err != nil {
		t.Fatalf("write host source: %v", err)
	}
	if _, err := client.SandboxImport(context.Background(), controlapipkg.SandboxImportRequest{
		HostSourcePath:  hostSourcePath,
		DestinationName: "example.txt",
	}); err != nil {
		t.Fatalf("sandbox import: %v", err)
	}

	_, err := client.SandboxExport(context.Background(), controlapipkg.SandboxExportRequest{
		SandboxSourcePath:   "/loopgate/home/imports/example.txt",
		HostDestinationPath: filepath.Join(hostRootPath, "exported.txt"),
	})
	if err == nil {
		t.Fatal("expected sandbox export denial for non-outputs path")
	}
	if !strings.Contains(err.Error(), controlapipkg.DenialCodeSandboxPathInvalid) {
		t.Fatalf("expected sandbox path invalid denial, got %v", err)
	}
}

func TestSandboxExportDeniesOrphanedOutputWithoutStagedRecord(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	hostRootPath := t.TempDir()
	pinTestProcessAsExpectedClient(t, server)
	client.SetOperatorMountPaths([]string{hostRootPath}, hostRootPath)
	client.ConfigureSession("operator", "operator-sandbox-export-orphan", advertisedSessionCapabilityNames(status))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure operator sandbox token: %v", err)
	}
	orphanPath := filepath.Join(server.sandboxPaths.Home, "outputs", "orphan.txt")
	if err := os.MkdirAll(filepath.Dir(orphanPath), 0o700); err != nil {
		t.Fatalf("mkdir outputs: %v", err)
	}
	if err := os.WriteFile(orphanPath, []byte("orphan"), 0o600); err != nil {
		t.Fatalf("write orphan output: %v", err)
	}

	_, err := client.SandboxExport(context.Background(), controlapipkg.SandboxExportRequest{
		SandboxSourcePath:   "/loopgate/home/outputs/orphan.txt",
		HostDestinationPath: filepath.Join(hostRootPath, "exported.txt"),
	})
	if err == nil {
		t.Fatal("expected sandbox export denial for orphaned output")
	}
	if !strings.Contains(err.Error(), controlapipkg.DenialCodeSandboxArtifactNotStaged) {
		t.Fatalf("expected sandbox artifact not staged denial, got %v", err)
	}
}

func TestClientExecuteCapability_DeniesSecretExportRequests(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	response, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-secret",
		Capability: "secret.export",
	})
	if err != nil {
		t.Fatalf("execute secret export denial: %v", err)
	}
	if response.Status != controlapipkg.ResponseStatusDenied {
		t.Fatalf("expected denied response, got %#v", response)
	}
	if !strings.Contains(response.DenialReason, "raw secret export is prohibited") {
		t.Fatalf("unexpected denial reason: %#v", response)
	}
	if response.DenialCode != controlapipkg.DenialCodeSecretExportProhibited {
		t.Fatalf("unexpected denial code: %#v", response)
	}
}

func TestStatusConnectionsDoNotExposeProviderTokens(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, connection := range status.Connections {
		if strings.Contains(strings.ToLower(connection.SecureStoreRefID), "token") {
			t.Fatalf("unexpected token-like field exposure: %#v", connection)
		}
	}
}
