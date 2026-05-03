package loopgate

import (
	"context"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFolderAccessRoutesRequireScopedCapabilities(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.resolveUserHomeDir = func() (string, error) { return repoRoot, nil }

	deniedClient := NewClient(client.socketPath)
	deniedClient.ConfigureSession("test-actor", "folder-access-denied", []string{"fs_list"})
	if _, err := deniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied folder_access token: %v", err)
	}
	if _, err := deniedClient.FolderAccessStatus(context.Background()); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected folder_access.read scope denial, got %v", err)
	}
	if _, err := deniedClient.SharedFolderStatus(context.Background()); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected shared-folder read scope denial, got %v", err)
	}
	if _, err := deniedClient.SyncFolderAccess(context.Background()); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected folder_access.write scope denial for sync, got %v", err)
	}
	if _, err := deniedClient.SyncSharedFolder(context.Background()); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected shared-folder write scope denial for sync, got %v", err)
	}

	readAllowedClient := NewClient(client.socketPath)
	readAllowedClient.ConfigureSession("test-actor", "folder-access-read-allowed", []string{controlCapabilityFolderAccessRead})
	if _, err := readAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure folder_access.read token: %v", err)
	}
	if _, err := readAllowedClient.FolderAccessStatus(context.Background()); err != nil {
		t.Fatalf("folder access status with folder_access.read: %v", err)
	}
	if _, err := readAllowedClient.SharedFolderStatus(context.Background()); err != nil {
		t.Fatalf("shared folder status with folder_access.read: %v", err)
	}

	writeAllowedClient := NewClient(client.socketPath)
	writeAllowedClient.ConfigureSession("test-actor", "folder-access-write-allowed", []string{controlCapabilityFolderAccessWrite})
	if _, err := writeAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure folder_access.write token: %v", err)
	}
	if _, err := writeAllowedClient.SyncFolderAccess(context.Background()); err != nil {
		t.Fatalf("sync folder access with folder_access.write: %v", err)
	}
	if _, err := writeAllowedClient.SyncSharedFolder(context.Background()); err != nil {
		t.Fatalf("sync shared folder with folder_access.write: %v", err)
	}
}

func TestQuarantineRoutesRequireScopedCapabilities(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	quarantineRef, err := server.storeQuarantinedPayload(controlapipkg.CapabilityRequest{
		RequestID:  "req-quarantine-scope",
		Capability: "remote_fetch",
	}, "quarantined payload")
	if err != nil {
		t.Fatalf("store quarantined payload: %v", err)
	}

	readDeniedClient := NewClient(client.socketPath)
	readDeniedClient.ConfigureSession("test-actor", "quarantine-read-denied", []string{"fs_read"})
	if _, err := readDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied quarantine.read token: %v", err)
	}
	if _, err := readDeniedClient.QuarantineMetadata(context.Background(), quarantineRef); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected quarantine.read scope denial for metadata, got %v", err)
	}
	if _, err := readDeniedClient.ViewQuarantinedPayload(context.Background(), quarantineRef); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected quarantine.read scope denial for view, got %v", err)
	}

	writeDeniedClient := NewClient(client.socketPath)
	writeDeniedClient.ConfigureSession("test-actor", "quarantine-write-denied", []string{controlCapabilityQuarantineRead})
	if _, err := writeDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied quarantine.write token: %v", err)
	}
	if _, err := writeDeniedClient.PruneQuarantinedPayload(context.Background(), quarantineRef); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected quarantine.write scope denial for prune, got %v", err)
	}

	readAllowedClient := NewClient(client.socketPath)
	readAllowedClient.ConfigureSession("test-actor", "quarantine-read-allowed", []string{controlCapabilityQuarantineRead})
	if _, err := readAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure quarantine.read token: %v", err)
	}
	metadataResponse, err := readAllowedClient.QuarantineMetadata(context.Background(), quarantineRef)
	if err != nil {
		t.Fatalf("quarantine metadata with quarantine.read: %v", err)
	}
	if metadataResponse.QuarantineRef != quarantineRef {
		t.Fatalf("expected metadata for %q, got %#v", quarantineRef, metadataResponse)
	}
	viewResponse, err := readAllowedClient.ViewQuarantinedPayload(context.Background(), quarantineRef)
	if err != nil {
		t.Fatalf("quarantine view with quarantine.read: %v", err)
	}
	if viewResponse.Metadata.QuarantineRef != quarantineRef {
		t.Fatalf("expected view metadata for %q, got %#v", quarantineRef, viewResponse)
	}

	ageQuarantineRecordForPrune(t, repoRoot, quarantineRef)

	writeAllowedClient := NewClient(client.socketPath)
	writeAllowedClient.ConfigureSession("test-actor", "quarantine-write-allowed", []string{controlCapabilityQuarantineWrite})
	if _, err := writeAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure quarantine.write token: %v", err)
	}
	prunedMetadata, err := writeAllowedClient.PruneQuarantinedPayload(context.Background(), quarantineRef)
	if err != nil {
		t.Fatalf("quarantine prune with quarantine.write: %v", err)
	}
	if prunedMetadata.StorageState != quarantineStorageStateBlobPruned {
		t.Fatalf("expected blob_pruned storage state, got %#v", prunedMetadata)
	}
}

func TestSandboxRoutesRequireCapabilityScopes(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	pinTestProcessAsExpectedClient(t, server)
	if err := server.sandboxPaths.Ensure(); err != nil {
		t.Fatalf("ensure sandbox paths: %v", err)
	}

	workspaceFilePath := filepath.Join(server.sandboxPaths.Workspace, "scope-test.txt")
	if err := os.MkdirAll(filepath.Dir(workspaceFilePath), 0o755); err != nil {
		t.Fatalf("mkdir sandbox workspace: %v", err)
	}
	if err := os.WriteFile(workspaceFilePath, []byte("sandbox scope"), 0o600); err != nil {
		t.Fatalf("seed sandbox file: %v", err)
	}

	hostImportPath := filepath.Join(t.TempDir(), "import.txt")
	if err := os.WriteFile(hostImportPath, []byte("import me"), 0o600); err != nil {
		t.Fatalf("seed host import file: %v", err)
	}

	listDeniedClient := NewClient(client.socketPath)
	listDeniedClient.ConfigureSession("test-actor", "sandbox-list-denied", []string{"fs_read"})
	if _, err := listDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied fs_list token: %v", err)
	}
	if _, err := listDeniedClient.SandboxList(context.Background(), controlapipkg.SandboxListRequest{SandboxPath: "workspace"}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected fs_list scope denial for sandbox list, got %v", err)
	}

	listAllowedClient := NewClient(client.socketPath)
	listAllowedClient.ConfigureSession("test-actor", "sandbox-list-allowed", []string{"fs_list"})
	if _, err := listAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure fs_list token: %v", err)
	}
	if _, err := listAllowedClient.SandboxList(context.Background(), controlapipkg.SandboxListRequest{SandboxPath: "workspace"}); err != nil {
		t.Fatalf("sandbox list with fs_list: %v", err)
	}

	metadataDeniedClient := NewClient(client.socketPath)
	metadataDeniedClient.ConfigureSession("test-actor", "sandbox-metadata-denied", []string{"fs_list"})
	if _, err := metadataDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied fs_read token: %v", err)
	}

	importDeniedClient := NewClient(client.socketPath)
	importDeniedClient.SetOperatorMountPaths([]string{filepath.Dir(hostImportPath)}, filepath.Dir(hostImportPath))
	importDeniedClient.ConfigureSession("operator", "sandbox-import-denied", []string{"fs_read"})
	if _, err := importDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied fs_write token: %v", err)
	}
	if _, err := importDeniedClient.SandboxImport(context.Background(), controlapipkg.SandboxImportRequest{
		HostSourcePath:  hostImportPath,
		DestinationName: "scope-import-denied.txt",
	}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected fs_write scope denial for sandbox import, got %v", err)
	}

	importAllowedClient := NewClient(client.socketPath)
	importAllowedClient.SetOperatorMountPaths([]string{filepath.Dir(hostImportPath)}, filepath.Dir(hostImportPath))
	importAllowedClient.ConfigureSession("operator", "sandbox-import-allowed", []string{"fs_write"})
	if _, err := importAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure fs_write token: %v", err)
	}
	if _, err := importAllowedClient.SandboxImport(context.Background(), controlapipkg.SandboxImportRequest{
		HostSourcePath:  hostImportPath,
		DestinationName: "scope-import-allowed.txt",
	}); err != nil {
		t.Fatalf("sandbox import with fs_write: %v", err)
	}
	stageResponse, err := importAllowedClient.SandboxStage(context.Background(), controlapipkg.SandboxStageRequest{
		SandboxSourcePath: "workspace/scope-test.txt",
		OutputName:        "scope-output.txt",
	})
	if err != nil {
		t.Fatalf("sandbox stage with fs_write: %v", err)
	}

	if _, err := metadataDeniedClient.SandboxMetadata(context.Background(), controlapipkg.SandboxMetadataRequest{SandboxSourcePath: stageResponse.SandboxRelativePath}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected fs_read scope denial for sandbox metadata, got %v", err)
	}

	metadataAllowedClient := NewClient(client.socketPath)
	metadataAllowedClient.ConfigureSession("test-actor", "sandbox-metadata-allowed", []string{"fs_read"})
	if _, err := metadataAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure fs_read token: %v", err)
	}
	if _, err := metadataAllowedClient.SandboxMetadata(context.Background(), controlapipkg.SandboxMetadataRequest{SandboxSourcePath: stageResponse.SandboxRelativePath}); err != nil {
		t.Fatalf("sandbox metadata with fs_read: %v", err)
	}
}

func TestUIOperatorMountWriteGrantRouteRequiresScopeAndFreshApprovalForRenewal(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}

	server.mu.Lock()
	controlSession := server.sessionState.sessions[client.controlSessionID]
	controlSession.OperatorMountPaths = []string{resolvedRepoRoot}
	controlSession.OperatorMountWriteGrants = map[string]time.Time{
		resolvedRepoRoot: server.now().UTC().Add(time.Hour),
	}
	server.sessionState.sessions[client.controlSessionID] = controlSession
	server.mu.Unlock()

	deniedClient := NewClient(client.socketPath)
	deniedClient.ConfigureSession("test-actor", "operator-mount-grant-denied", []string{"fs_write"})
	if _, err := deniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied operator mount grant token: %v", err)
	}
	if _, err := deniedClient.UpdateUIOperatorMountWriteGrant(context.Background(), controlapipkg.UIOperatorMountWriteGrantUpdateRequest{
		RootPath: resolvedRepoRoot,
		Action:   controlapipkg.OperatorMountWriteGrantActionRevoke,
	}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected operator mount grant scope denial, got %v", err)
	}

	if _, err := client.UpdateUIOperatorMountWriteGrant(context.Background(), controlapipkg.UIOperatorMountWriteGrantUpdateRequest{
		RootPath: resolvedRepoRoot,
		Action:   controlapipkg.OperatorMountWriteGrantActionRenew,
	}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeApprovalRequired) {
		t.Fatalf("expected renew to require fresh approval, got %v", err)
	}
}
