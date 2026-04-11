package loopgate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHavenMemoryUIRoutesRequireScopedCapabilities(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	inventoryDeniedClient := NewClient(client.socketPath)
	inventoryDeniedClient.ConfigureSession("haven", "ui-memory-read-denied", []string{"fs_read"})
	if _, err := inventoryDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied inventory token: %v", err)
	}
	if _, err := inventoryDeniedClient.LoadHavenMemoryInventory(context.Background()); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected memory.read scope denial for ui inventory, got %v", err)
	}

	resetDeniedClient := NewClient(client.socketPath)
	resetDeniedClient.ConfigureSession("haven", "ui-memory-reset-denied", []string{controlCapabilityMemoryWrite})
	if _, err := resetDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied reset token: %v", err)
	}
	if _, err := resetDeniedClient.ResetHavenMemory(context.Background(), HavenMemoryResetRequest{
		OperationID: "ui-memory-reset-denied",
		Reason:      "scope check",
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected memory.reset scope denial for ui reset, got %v", err)
	}
}

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

func TestUIOperatorMountWriteGrantRouteRequiresScopeAndFreshApprovalForRenewal(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}

	server.mu.Lock()
	controlSession := server.sessions[client.controlSessionID]
	controlSession.OperatorMountPaths = []string{resolvedRepoRoot}
	controlSession.OperatorMountWriteGrants = map[string]time.Time{
		resolvedRepoRoot: server.now().UTC().Add(time.Hour),
	}
	server.sessions[client.controlSessionID] = controlSession
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

	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir policy dir: %v", err)
	}
	if err := os.WriteFile(policyPath, []byte(loopgatePolicyYAML(false)), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	writeTestMorphlingClassPolicy(t, repoRoot)

	socketPath := filepath.Join(repoRoot, "loopgate.sock")
	if _, err := NewServer(repoRoot, socketPath); err == nil || !strings.Contains(err.Error(), "outside allowed runtime roots") {
		t.Fatalf("expected socket path validation error, got %v", err)
	}
}

func TestNewServerAllowsSocketPathUnderRepoRuntime(t *testing.T) {
	repoRoot := t.TempDir()

	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir policy dir: %v", err)
	}
	if err := os.WriteFile(policyPath, []byte(loopgatePolicyYAML(false)), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	writeTestMorphlingClassPolicy(t, repoRoot)

	socketPath := filepath.Join(repoRoot, "runtime", "memorybench-loopgate.sock")
	if _, err := NewServer(repoRoot, socketPath); err != nil {
		t.Fatalf("expected repo runtime socket path to be accepted, got %v", err)
	}
}

func TestServeRejectsDirectorySocketPathWithoutRemovingIt(t *testing.T) {
	repoRoot := t.TempDir()

	policyPath := filepath.Join(repoRoot, "core", "policy", "policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir policy dir: %v", err)
	}
	if err := os.WriteFile(policyPath, []byte(loopgatePolicyYAML(false)), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	writeTestMorphlingClassPolicy(t, repoRoot)

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
