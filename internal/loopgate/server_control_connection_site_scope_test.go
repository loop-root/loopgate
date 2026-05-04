package loopgate

import (
	"context"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	if _, err := readDeniedClient.ConnectionsStatus(context.Background()); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected connection.read scope denial, got %v", err)
	}

	writeDeniedClient := NewClient(client.socketPath)
	writeDeniedClient.ConfigureSession("test-actor", "connection-write-denied", []string{controlCapabilityConnectionRead})
	if _, err := writeDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied connection.write token: %v", err)
	}
	if _, err := writeDeniedClient.ValidateConnection(context.Background(), "missing", "subject"); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected connection.write scope denial for validate, got %v", err)
	}
	if _, err := writeDeniedClient.StartPKCEConnection(context.Background(), controlapipkg.PKCEStartRequest{
		Provider: "examplepkce",
		Subject:  "workspace-user",
	}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
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
	if _, err := inspectDeniedClient.InspectSite(context.Background(), controlapipkg.SiteInspectionRequest{URL: providerServer.URL}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected site.inspect scope denial, got %v", err)
	}

	trustDeniedClient := NewClient(client.socketPath)
	trustDeniedClient.ConfigureSession("test-actor", "site-trust-denied", []string{controlCapabilitySiteInspect})
	if _, err := trustDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied site.trust.write token: %v", err)
	}
	if _, err := trustDeniedClient.CreateTrustDraft(context.Background(), controlapipkg.SiteTrustDraftRequest{URL: providerServer.URL}); err == nil || !strings.Contains(err.Error(), controlapipkg.DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected site.trust.write scope denial, got %v", err)
	}
}
