package loopgate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStatusOmitsConnectionsWithoutConnectionReadScope(t *testing.T) {
	repoRoot := t.TempDir()
	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"status":"ok"}`))
	}))
	defer providerServer.Close()
	writeConfiguredConnectionYAML(t, repoRoot, providerServer.URL)

	client, _, _ := startLoopgateServer(t, repoRoot, loopgateHTTPPolicyYAML(false))

	limitedClient := NewClient(client.socketPath)
	limitedClient.ConfigureSession("test-actor", "status-no-connection-read", []string{"fs_list"})
	if _, err := limitedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure limited token: %v", err)
	}
	status, err := limitedClient.Status(context.Background())
	if err != nil {
		t.Fatalf("status without connection.read: %v", err)
	}
	if len(status.Connections) != 0 {
		t.Fatalf("expected status to omit connection summaries without connection.read, got %#v", status.Connections)
	}
}

func TestConnectionRoutesRequireConnectionScopes(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgateHTTPPolicyYAML(false))

	readDeniedClient := NewClient(client.socketPath)
	readDeniedClient.ConfigureSession("test-actor", "connection-read-denied", []string{"fs_list"})
	if _, err := readDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied connection.read token: %v", err)
	}
	if _, err := readDeniedClient.ConnectionsStatus(context.Background()); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected connection.read scope denial, got %v", err)
	}

	writeDeniedClient := NewClient(client.socketPath)
	writeDeniedClient.ConfigureSession("test-actor", "connection-write-denied", []string{controlCapabilityConnectionRead})
	if _, err := writeDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied connection.write token: %v", err)
	}
	if _, err := writeDeniedClient.ValidateConnection(context.Background(), "missing", "subject"); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected connection.write scope denial for validate, got %v", err)
	}
	if _, err := writeDeniedClient.StartPKCEConnection(context.Background(), PKCEStartRequest{
		Provider: "examplepkce",
		Subject:  "workspace-user",
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected connection.write scope denial for pkce start, got %v", err)
	}
}

func TestSiteRoutesRequireScopedCapabilities(t *testing.T) {
	repoRoot := t.TempDir()
	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"status":{"description":"All Systems Operational","indicator":"none"}}`))
	}))
	defer providerServer.Close()

	client, _, _ := startLoopgateServer(t, repoRoot, loopgateHTTPPolicyYAML(false))

	inspectDeniedClient := NewClient(client.socketPath)
	inspectDeniedClient.ConfigureSession("test-actor", "site-inspect-denied", []string{controlCapabilityConnectionRead})
	if _, err := inspectDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied site.inspect token: %v", err)
	}
	if _, err := inspectDeniedClient.InspectSite(context.Background(), SiteInspectionRequest{URL: providerServer.URL}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected site.inspect scope denial, got %v", err)
	}

	trustDeniedClient := NewClient(client.socketPath)
	trustDeniedClient.ConfigureSession("test-actor", "site-trust-denied", []string{controlCapabilitySiteInspect})
	if _, err := trustDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied site.trust.write token: %v", err)
	}
	if _, err := trustDeniedClient.CreateTrustDraft(context.Background(), SiteTrustDraftRequest{URL: providerServer.URL}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected site.trust.write scope denial, got %v", err)
	}
}

func TestDiagnosticRouteRequiresScopedCapability(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	pinTestProcessAsExpectedClient(t, server)

	deniedClient := NewClient(client.socketPath)
	deniedClient.ConfigureSession("test-actor", "diagnostic-read-denied", []string{controlCapabilityConnectionRead})
	if _, err := deniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied diagnostic.read token: %v", err)
	}
	var deniedReport map[string]interface{}
	if err := deniedClient.FetchDiagnosticReport(context.Background(), &deniedReport); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected diagnostic.read scope denial, got %v", err)
	}
	if _, err := deniedClient.CheckAuditExportTrust(context.Background()); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected diagnostic.read scope denial for audit export trust check, got %v", err)
	}
	if _, err := deniedClient.LoadMCPGatewayInventory(context.Background()); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected diagnostic.read scope denial for MCP gateway inventory, got %v", err)
	}
	if _, err := deniedClient.LoadMCPGatewayServerStatus(context.Background()); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected diagnostic.read scope denial for MCP gateway server status, got %v", err)
	}
	if _, err := deniedClient.CheckMCPGatewayDecision(context.Background(), MCPGatewayDecisionRequest{
		ServerID: "github",
		ToolName: "search_repositories",
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected diagnostic.read scope denial for MCP gateway decision, got %v", err)
	}
	if _, err := deniedClient.ValidateMCPGatewayInvocation(context.Background(), MCPGatewayInvocationRequest{
		ServerID: "github",
		ToolName: "search_repositories",
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected diagnostic.read scope denial for MCP gateway invocation validation, got %v", err)
	}
	if _, err := deniedClient.RequestMCPGatewayInvocationApproval(context.Background(), MCPGatewayInvocationRequest{
		ServerID: "github",
		ToolName: "search_repositories",
		Arguments: map[string]json.RawMessage{
			"query": json.RawMessage(`"loopgate"`),
		},
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected mcp_gateway.write scope denial for MCP gateway approval preparation, got %v", err)
	}
	if _, err := deniedClient.DecideMCPGatewayInvocationApproval(context.Background(), MCPGatewayApprovalDecisionRequest{
		ApprovalRequestID: "missing",
		Approved:          true,
		DecisionNonce:     "nonce",
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected mcp_gateway.write scope denial for MCP gateway approval decision, got %v", err)
	}
	if _, err := deniedClient.ValidateMCPGatewayExecution(context.Background(), MCPGatewayExecutionRequest{
		ApprovalRequestID:      "missing",
		ApprovalManifestSHA256: "abcd",
		ServerID:               "github",
		ToolName:               "search_repositories",
		Arguments: map[string]json.RawMessage{
			"query": json.RawMessage(`"loopgate"`),
		},
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected mcp_gateway.write scope denial for MCP gateway execution validation, got %v", err)
	}
	if _, err := deniedClient.EnsureMCPGatewayServerLaunched(context.Background(), MCPGatewayEnsureLaunchRequest{
		ServerID: "github",
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected mcp_gateway.write scope denial for MCP gateway server launch, got %v", err)
	}
	if _, err := deniedClient.StopMCPGatewayServer(context.Background(), MCPGatewayStopRequest{
		ServerID: "github",
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected mcp_gateway.write scope denial for MCP gateway server stop, got %v", err)
	}
	if _, err := deniedClient.ExecuteMCPGatewayInvocation(context.Background(), MCPGatewayExecutionRequest{
		ApprovalRequestID:      "missing",
		ApprovalManifestSHA256: "abcd",
		ServerID:               "github",
		ToolName:               "search_repositories",
		Arguments: map[string]json.RawMessage{
			"query": json.RawMessage(`"loopgate"`),
		},
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected mcp_gateway.write scope denial for MCP gateway execution, got %v", err)
	}

	allowedClient := NewClient(client.socketPath)
	allowedClient.ConfigureSession("test-actor", "diagnostic-read-allowed", []string{controlCapabilityDiagnosticRead})
	if _, err := allowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure diagnostic.read token: %v", err)
	}
	var allowedReport map[string]interface{}
	if err := allowedClient.FetchDiagnosticReport(context.Background(), &allowedReport); err != nil {
		t.Fatalf("fetch diagnostic report with diagnostic.read: %v", err)
	}
	if _, err := allowedClient.CheckAuditExportTrust(context.Background()); err != nil {
		t.Fatalf("check audit export trust with diagnostic.read: %v", err)
	}
	if _, err := allowedClient.LoadMCPGatewayInventory(context.Background()); err != nil {
		t.Fatalf("load MCP gateway inventory with diagnostic.read: %v", err)
	}
	if _, err := allowedClient.LoadMCPGatewayServerStatus(context.Background()); err != nil {
		t.Fatalf("load MCP gateway server status with diagnostic.read: %v", err)
	}
	decisionResponse, err := allowedClient.CheckMCPGatewayDecision(context.Background(), MCPGatewayDecisionRequest{
		ServerID: "github",
		ToolName: "search_repositories",
	})
	if err != nil {
		t.Fatalf("check MCP gateway decision with diagnostic.read: %v", err)
	}
	if decisionResponse.Decision != "deny" || decisionResponse.DenialCode != DenialCodeMCPGatewayServerNotFound {
		t.Fatalf("expected typed MCP gateway deny on unknown server with diagnostic.read, got %#v", decisionResponse)
	}
	if _, err := allowedClient.ValidateMCPGatewayInvocation(context.Background(), MCPGatewayInvocationRequest{
		ServerID: "github",
		ToolName: "search_repositories",
	}); err == nil || !strings.Contains(err.Error(), DenialCodeMalformedRequest) {
		t.Fatalf("expected malformed invocation validation under diagnostic.read, got %v", err)
	}

	mcpWriteClient := NewClient(client.socketPath)
	mcpWriteClient.ConfigureSession("test-actor", "mcp-gateway-write-allowed", []string{controlCapabilityMCPGatewayWrite})
	if _, err := mcpWriteClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure mcp_gateway.write token: %v", err)
	}
	if _, err := mcpWriteClient.RequestMCPGatewayInvocationApproval(context.Background(), MCPGatewayInvocationRequest{
		ServerID: "github",
		ToolName: "search_repositories",
	}); err == nil || !strings.Contains(err.Error(), DenialCodeMalformedRequest) {
		t.Fatalf("expected malformed approval preparation request under mcp_gateway.write, got %v", err)
	}
	if _, err := mcpWriteClient.DecideMCPGatewayInvocationApproval(context.Background(), MCPGatewayApprovalDecisionRequest{}); err == nil || !strings.Contains(err.Error(), DenialCodeMalformedRequest) {
		t.Fatalf("expected malformed approval decision request under mcp_gateway.write, got %v", err)
	}
	if _, err := mcpWriteClient.ValidateMCPGatewayExecution(context.Background(), MCPGatewayExecutionRequest{}); err == nil || !strings.Contains(err.Error(), DenialCodeMalformedRequest) {
		t.Fatalf("expected malformed MCP gateway execution validation request under mcp_gateway.write, got %v", err)
	}
	if _, err := mcpWriteClient.EnsureMCPGatewayServerLaunched(context.Background(), MCPGatewayEnsureLaunchRequest{}); err == nil || !strings.Contains(err.Error(), DenialCodeMalformedRequest) {
		t.Fatalf("expected malformed MCP gateway ensure-launched request under mcp_gateway.write, got %v", err)
	}
	if _, err := mcpWriteClient.StopMCPGatewayServer(context.Background(), MCPGatewayStopRequest{}); err == nil || !strings.Contains(err.Error(), DenialCodeMalformedRequest) {
		t.Fatalf("expected malformed MCP gateway stop request under mcp_gateway.write, got %v", err)
	}
	if _, err := mcpWriteClient.ExecuteMCPGatewayInvocation(context.Background(), MCPGatewayExecutionRequest{}); err == nil || !strings.Contains(err.Error(), DenialCodeMalformedRequest) {
		t.Fatalf("expected malformed MCP gateway execute request under mcp_gateway.write, got %v", err)
	}
}

func TestFolderAccessRoutesRequireScopedCapabilities(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	server.resolveUserHomeDir = func() (string, error) { return repoRoot, nil }

	deniedClient := NewClient(client.socketPath)
	deniedClient.ConfigureSession("test-actor", "folder-access-denied", []string{"fs_list"})
	if _, err := deniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied folder_access token: %v", err)
	}
	if _, err := deniedClient.FolderAccessStatus(context.Background()); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected folder_access.read scope denial, got %v", err)
	}
	if _, err := deniedClient.SharedFolderStatus(context.Background()); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected shared-folder read scope denial, got %v", err)
	}
	if _, err := deniedClient.SyncFolderAccess(context.Background()); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected folder_access.write scope denial for sync, got %v", err)
	}
	if _, err := deniedClient.SyncSharedFolder(context.Background()); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
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

	quarantineRef, err := server.storeQuarantinedPayload(CapabilityRequest{
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
	if _, err := readDeniedClient.QuarantineMetadata(context.Background(), quarantineRef); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected quarantine.read scope denial for metadata, got %v", err)
	}
	if _, err := readDeniedClient.ViewQuarantinedPayload(context.Background(), quarantineRef); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected quarantine.read scope denial for view, got %v", err)
	}

	writeDeniedClient := NewClient(client.socketPath)
	writeDeniedClient.ConfigureSession("test-actor", "quarantine-write-denied", []string{controlCapabilityQuarantineRead})
	if _, err := writeDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied quarantine.write token: %v", err)
	}
	if _, err := writeDeniedClient.PruneQuarantinedPayload(context.Background(), quarantineRef); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
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
	if _, err := listDeniedClient.SandboxList(context.Background(), SandboxListRequest{SandboxPath: "workspace"}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected fs_list scope denial for sandbox list, got %v", err)
	}

	listAllowedClient := NewClient(client.socketPath)
	listAllowedClient.ConfigureSession("test-actor", "sandbox-list-allowed", []string{"fs_list"})
	if _, err := listAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure fs_list token: %v", err)
	}
	if _, err := listAllowedClient.SandboxList(context.Background(), SandboxListRequest{SandboxPath: "workspace"}); err != nil {
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
	if _, err := importDeniedClient.SandboxImport(context.Background(), SandboxImportRequest{
		HostSourcePath:  hostImportPath,
		DestinationName: "scope-import-denied.txt",
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected fs_write scope denial for sandbox import, got %v", err)
	}

	importAllowedClient := NewClient(client.socketPath)
	importAllowedClient.SetOperatorMountPaths([]string{filepath.Dir(hostImportPath)}, filepath.Dir(hostImportPath))
	importAllowedClient.ConfigureSession("operator", "sandbox-import-allowed", []string{"fs_write"})
	if _, err := importAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure fs_write token: %v", err)
	}
	if _, err := importAllowedClient.SandboxImport(context.Background(), SandboxImportRequest{
		HostSourcePath:  hostImportPath,
		DestinationName: "scope-import-allowed.txt",
	}); err != nil {
		t.Fatalf("sandbox import with fs_write: %v", err)
	}
	stageResponse, err := importAllowedClient.SandboxStage(context.Background(), SandboxStageRequest{
		SandboxSourcePath: "workspace/scope-test.txt",
		OutputName:        "scope-output.txt",
	})
	if err != nil {
		t.Fatalf("sandbox stage with fs_write: %v", err)
	}

	if _, err := metadataDeniedClient.SandboxMetadata(context.Background(), SandboxMetadataRequest{SandboxSourcePath: stageResponse.SandboxRelativePath}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected fs_read scope denial for sandbox metadata, got %v", err)
	}

	metadataAllowedClient := NewClient(client.socketPath)
	metadataAllowedClient.ConfigureSession("test-actor", "sandbox-metadata-allowed", []string{"fs_read"})
	if _, err := metadataAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure fs_read token: %v", err)
	}
	if _, err := metadataAllowedClient.SandboxMetadata(context.Background(), SandboxMetadataRequest{SandboxSourcePath: stageResponse.SandboxRelativePath}); err != nil {
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
	if _, err := deniedClient.UpdateUIOperatorMountWriteGrant(context.Background(), UIOperatorMountWriteGrantUpdateRequest{
		RootPath: resolvedRepoRoot,
		Action:   OperatorMountWriteGrantActionRevoke,
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected operator mount grant scope denial, got %v", err)
	}

	if _, err := client.UpdateUIOperatorMountWriteGrant(context.Background(), UIOperatorMountWriteGrantUpdateRequest{
		RootPath: resolvedRepoRoot,
		Action:   OperatorMountWriteGrantActionRenew,
	}); err == nil || !strings.Contains(err.Error(), DenialCodeApprovalRequired) {
		t.Fatalf("expected renew to require fresh approval, got %v", err)
	}
}

func TestNewServerRejectsSocketPathOutsideAllowedRoots(t *testing.T) {
	repoRoot := t.TempDir()

	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))

	socketPath := filepath.Join(repoRoot, "loopgate.sock")
	if _, err := NewServer(repoRoot, socketPath); err == nil || !strings.Contains(err.Error(), "outside allowed runtime roots") {
		t.Fatalf("expected socket path validation error, got %v", err)
	}
}

func TestNewServerAllowsSocketPathUnderRepoRuntime(t *testing.T) {
	repoRoot := t.TempDir()

	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))

	socketPath := filepath.Join(repoRoot, "runtime", "memorybench-loopgate.sock")
	if _, err := NewServer(repoRoot, socketPath); err != nil {
		t.Fatalf("expected repo runtime socket path to be accepted, got %v", err)
	}
}

func TestServeRejectsDirectorySocketPathWithoutRemovingIt(t *testing.T) {
	repoRoot := t.TempDir()

	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))

	socketPath := filepath.Join(os.TempDir(), "loopgate-dir-target.sock")
	if err := os.RemoveAll(socketPath); err != nil {
		t.Fatalf("clear stale socket path: %v", err)
	}
	if err := os.MkdirAll(socketPath, 0o700); err != nil {
		t.Fatalf("mkdir socket path directory: %v", err)
	}
	defer func() { _ = os.RemoveAll(socketPath) }()

	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if err := server.Serve(context.Background()); err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("expected directory socket path error, got %v", err)
	}
	if _, err := os.Stat(socketPath); err != nil {
		t.Fatalf("expected socket path directory to remain after failed serve, got %v", err)
	}
}
