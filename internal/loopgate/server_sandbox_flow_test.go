package loopgate

import (
	"context"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"loopgate/internal/sandbox"
)

func TestSandboxImportAndStageAndExport(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	hostRootPath := t.TempDir()
	resolvedHostRootPath, err := filepath.EvalSymlinks(hostRootPath)
	if err != nil {
		t.Fatalf("eval host root symlinks: %v", err)
	}
	pinTestProcessAsExpectedClient(t, server)
	client.SetOperatorMountPaths([]string{hostRootPath}, hostRootPath)
	client.ConfigureSession("operator", "operator-sandbox-flow", advertisedSessionCapabilityNames(status))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure operator sandbox token: %v", err)
	}

	hostSourcePath := filepath.Join(hostRootPath, "example.txt")
	if err := os.WriteFile(hostSourcePath, []byte("sandbox flow"), 0o600); err != nil {
		t.Fatalf("write host source: %v", err)
	}

	importResponse, err := client.SandboxImport(context.Background(), controlapipkg.SandboxImportRequest{
		HostSourcePath:  hostSourcePath,
		DestinationName: "example.txt",
	})
	if err != nil {
		t.Fatalf("sandbox import: %v", err)
	}
	if importResponse.Action != "import" {
		t.Fatalf("unexpected import response: %#v", importResponse)
	}
	if importResponse.SandboxRoot != sandbox.VirtualHome {
		t.Fatalf("expected virtual sandbox root %q, got %#v", sandbox.VirtualHome, importResponse)
	}
	if importResponse.SandboxAbsolutePath != "/loopgate/home/imports/example.txt" {
		t.Fatalf("expected virtual sandbox path, got %#v", importResponse)
	}

	stageResponse, err := client.SandboxStage(context.Background(), controlapipkg.SandboxStageRequest{
		SandboxSourcePath: "/loopgate/home/imports/example.txt",
		OutputName:        "export-me.txt",
	})
	if err != nil {
		t.Fatalf("sandbox stage: %v", err)
	}
	if stageResponse.Action != "stage" {
		t.Fatalf("unexpected stage response: %#v", stageResponse)
	}
	if stageResponse.ArtifactRef == "" {
		t.Fatalf("expected staged artifact ref, got %#v", stageResponse)
	}
	if stageResponse.SourceSandboxPath != "/loopgate/home/imports/example.txt" {
		t.Fatalf("expected virtual source sandbox path, got %#v", stageResponse)
	}
	if stageResponse.SandboxAbsolutePath != "/loopgate/home/outputs/export-me.txt" {
		t.Fatalf("expected virtual staged path, got %#v", stageResponse)
	}

	metadataResponse, err := client.SandboxMetadata(context.Background(), controlapipkg.SandboxMetadataRequest{
		SandboxSourcePath: "/loopgate/home/outputs/export-me.txt",
	})
	if err != nil {
		t.Fatalf("sandbox metadata: %v", err)
	}
	if metadataResponse.ArtifactRef != stageResponse.ArtifactRef {
		t.Fatalf("expected artifact ref %q, got %#v", stageResponse.ArtifactRef, metadataResponse)
	}
	if metadataResponse.ContentSHA256 != stageResponse.ContentSHA256 {
		t.Fatalf("expected content hash %q, got %#v", stageResponse.ContentSHA256, metadataResponse)
	}
	if metadataResponse.SourceSandboxPath != "/loopgate/home/imports/example.txt" {
		t.Fatalf("expected virtual metadata source path, got %#v", metadataResponse)
	}

	server.mu.Lock()
	controlSession := server.sessionState.sessions[client.controlSessionID]
	if controlSession.OperatorMountWriteGrants == nil {
		controlSession.OperatorMountWriteGrants = make(map[string]time.Time)
	}
	controlSession.OperatorMountWriteGrants[resolvedHostRootPath] = server.now().UTC().Add(operatorMountWriteGrantTTL)
	server.sessionState.sessions[client.controlSessionID] = controlSession
	server.mu.Unlock()

	hostDestinationPath := filepath.Join(hostRootPath, "exported.txt")
	exportResponse, err := client.SandboxExport(context.Background(), controlapipkg.SandboxExportRequest{
		SandboxSourcePath:   "/loopgate/home/outputs/export-me.txt",
		HostDestinationPath: hostDestinationPath,
	})
	if err != nil {
		t.Fatalf("sandbox export: %v", err)
	}
	if exportResponse.Action != "export" {
		t.Fatalf("unexpected export response: %#v", exportResponse)
	}
	if exportResponse.SourceSandboxPath != "/loopgate/home/outputs/export-me.txt" {
		t.Fatalf("expected virtual export source path, got %#v", exportResponse)
	}

	exportedBytes, err := os.ReadFile(hostDestinationPath)
	if err != nil {
		t.Fatalf("read exported path: %v", err)
	}
	if string(exportedBytes) != "sandbox flow" {
		t.Fatalf("unexpected exported contents: %q", string(exportedBytes))
	}

	auditBytes, err := os.ReadFile(filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"))
	if err != nil {
		t.Fatalf("read loopgate events: %v", err)
	}
	auditText := string(auditBytes)
	for _, expectedEventType := range []string{"sandbox.imported", "sandbox.staged", "sandbox.metadata_viewed", "sandbox.exported"} {
		if !strings.Contains(auditText, expectedEventType) {
			t.Fatalf("expected audit to contain %s, got %s", expectedEventType, auditText)
		}
	}
}

func TestSandboxImportRequiresBoundOperatorMountPath(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	client.ConfigureSession("operator", "operator-sandbox-import-unbound", advertisedSessionCapabilityNames(status))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure operator sandbox token: %v", err)
	}

	hostSourcePath := filepath.Join(t.TempDir(), "example.txt")
	if err := os.WriteFile(hostSourcePath, []byte("sandbox flow"), 0o600); err != nil {
		t.Fatalf("write host source: %v", err)
	}

	_, err := client.SandboxImport(context.Background(), controlapipkg.SandboxImportRequest{
		HostSourcePath:  hostSourcePath,
		DestinationName: "example.txt",
	})
	if err == nil {
		t.Fatal("expected sandbox import denial without operator mount binding")
	}
	if !strings.Contains(err.Error(), controlapipkg.DenialCodeControlSessionBindingInvalid) {
		t.Fatalf("expected control session binding denial, got %v", err)
	}
}

func TestSandboxExportRequiresOperatorMountWriteGrant(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	hostRootPath := t.TempDir()
	pinTestProcessAsExpectedClient(t, server)
	client.SetOperatorMountPaths([]string{hostRootPath}, hostRootPath)
	client.ConfigureSession("operator", "operator-sandbox-export-needs-grant", advertisedSessionCapabilityNames(status))
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
	if _, err := client.SandboxStage(context.Background(), controlapipkg.SandboxStageRequest{
		SandboxSourcePath: "/loopgate/home/imports/example.txt",
		OutputName:        "export-me.txt",
	}); err != nil {
		t.Fatalf("sandbox stage: %v", err)
	}

	_, err := client.SandboxExport(context.Background(), controlapipkg.SandboxExportRequest{
		SandboxSourcePath:   "/loopgate/home/outputs/export-me.txt",
		HostDestinationPath: filepath.Join(hostRootPath, "exported.txt"),
	})
	if err == nil {
		t.Fatal("expected sandbox export denial without operator mount write grant")
	}
	if !strings.Contains(err.Error(), controlapipkg.DenialCodeApprovalRequired) {
		t.Fatalf("expected approval-required denial, got %v", err)
	}
}

// --- Security hardening tests ---
