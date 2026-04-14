package loopgate

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestStatusAdvertisesMemoryControlCapabilities(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	var rawStatus map[string]interface{}
	if err := client.doJSON(context.Background(), http.MethodGet, "/v1/status", client.capabilityToken, nil, &rawStatus, nil); err != nil {
		t.Fatalf("GET /v1/status: %v", err)
	}

	rawControlCapabilities, found := rawStatus["control_capabilities"]
	if !found {
		t.Fatalf("expected status to advertise control_capabilities, got %#v", rawStatus)
	}
	controlCapabilities, ok := rawControlCapabilities.([]interface{})
	if !ok {
		t.Fatalf("expected control_capabilities array, got %#v", rawControlCapabilities)
	}

	for _, requiredCapability := range []string{"memory.read", "memory.write", "memory.reset", "memory.review", "memory.lineage"} {
		found = false
		for _, rawCapability := range controlCapabilities {
			capabilityMap, ok := rawCapability.(map[string]interface{})
			if !ok {
				t.Fatalf("expected control capability object, got %#v", rawCapability)
			}
			if capabilityMap["name"] == requiredCapability {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected control capability %q in status payload, got %#v", requiredCapability, controlCapabilities)
		}
	}
}

func TestStatusAdvertisesAdditionalControlCapabilities(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("GET /v1/status: %v", err)
	}

	for _, requiredCapability := range []string{
		controlCapabilityConnectionRead,
		controlCapabilityConnectionWrite,
		controlCapabilityDiagnosticRead,
		controlCapabilityFolderAccessRead,
		controlCapabilityFolderAccessWrite,
		controlCapabilityModelReply,
		controlCapabilityModelSettingsRead,
		controlCapabilityModelSettingsWrite,
		controlCapabilityModelValidate,
		controlCapabilityOperatorMountWriteGrant,
		controlCapabilityQuarantineRead,
		controlCapabilityQuarantineWrite,
		controlCapabilitySiteInspect,
		controlCapabilitySiteTrustWrite,
		controlCapabilityUIRead,
		controlCapabilityUIWrite,
	} {
		if !containsCapability(status.ControlCapabilities, requiredCapability) {
			t.Fatalf("expected control capability %q in status payload, got %#v", requiredCapability, status.ControlCapabilities)
		}
	}
}

func TestMemoryReadRoutesRequireMemoryReadScope(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	deniedClient := NewClient(client.socketPath)
	deniedClient.ConfigureSession("memory-reader", "memory-read-denied", []string{"fs_read"})
	if _, err := deniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure fs_read token: %v", err)
	}
	if _, err := deniedClient.LoadMemoryWakeState(context.Background()); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected memory.read scope denial for wake-state load, got %v", err)
	}
	if _, err := deniedClient.DiscoverMemory(context.Background(), MemoryDiscoverRequest{Query: "name"}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected memory.read scope denial for discovery, got %v", err)
	}
	if _, err := deniedClient.LookupMemoryArtifacts(context.Background(), MemoryArtifactLookupRequest{Query: "name"}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected memory.read scope denial for artifact lookup, got %v", err)
	}
	if _, err := deniedClient.GetMemoryArtifacts(context.Background(), MemoryArtifactGetRequest{ArtifactRefs: []string{buildStateMemoryArtifactRef("rk_test")}}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected memory.read scope denial for artifact get, got %v", err)
	}

	allowedClient := NewClient(client.socketPath)
	allowedClient.ConfigureSession("memory-reader", "memory-read-allowed", []string{"memory.read"})
	if _, err := allowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure memory.read token: %v", err)
	}
	if _, err := allowedClient.LoadMemoryWakeState(context.Background()); err != nil {
		t.Fatalf("load wake state with memory.read: %v", err)
	}
	if _, err := allowedClient.DiscoverMemory(context.Background(), MemoryDiscoverRequest{Query: "name"}); err != nil {
		t.Fatalf("discover memory with memory.read: %v", err)
	}
	if _, err := allowedClient.LookupMemoryArtifacts(context.Background(), MemoryArtifactLookupRequest{Query: "name"}); err != nil {
		t.Fatalf("lookup memory artifacts with memory.read: %v", err)
	}
}

func TestMemoryWriteRoutesRequireMemoryWriteScope(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	pinTestProcessAsExpectedClient(t, server)
	server.resolveUserHomeDir = func() (string, error) { return repoRoot, nil }
	workspaceID := server.deriveWorkspaceIDFromRepoRoot()

	deniedClient := NewClient(client.socketPath)
	deniedClient.SetWorkspaceID(workspaceID)
	deniedClient.ConfigureSession("haven", "memory-write-denied", []string{"memory.read"})
	if _, err := deniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied token: %v", err)
	}
	if _, err := deniedClient.RememberMemoryFact(context.Background(), MemoryRememberRequest{
		FactKey:   "name",
		FactValue: "Ada",
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected memory.write scope denial for remember, got %v", err)
	}

	allowedClient := NewClient(client.socketPath)
	allowedClient.SetWorkspaceID(workspaceID)
	allowedClient.ConfigureSession("haven", "memory-write-allowed", []string{controlCapabilityUIWrite, "memory.write"})
	if _, err := allowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure memory.write token: %v", err)
	}
	if _, err := allowedClient.RememberMemoryFact(context.Background(), MemoryRememberRequest{
		FactKey:   "name",
		FactValue: "Ada",
	}); err != nil {
		t.Fatalf("remember memory with memory.write: %v", err)
	}
}

func TestRawContinuityInspectRouteIsRemoved(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, client.baseURL+"/v1/continuity/inspect", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("build removed raw continuity inspect request: %v", err)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		t.Fatalf("POST removed raw continuity inspect route: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected removed raw continuity inspect route to return 404, got %d", response.StatusCode)
	}
}
