package loopgate

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"morph/internal/threadstore"
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
		controlCapabilitySiteInspect,
		controlCapabilitySiteTrustWrite,
		controlCapabilityTaskStandingGrantRead,
		controlCapabilityTaskStandingGrantWrite,
		controlCapabilityTasksRead,
		controlCapabilityTasksWrite,
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
	threadID, workspaceID := createContinuityInspectThreadForTests(t, server, repoRoot)

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
	if _, err := deniedClient.SubmitHavenContinuityInspectionForThread(context.Background(), threadID); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected memory.write scope denial for continuity inspect-thread, got %v", err)
	}

	allowedClient := NewClient(client.socketPath)
	allowedClient.SetWorkspaceID(workspaceID)
	allowedClient.ConfigureSession("haven", "memory-write-allowed", []string{"memory.write"})
	if _, err := allowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure memory.write token: %v", err)
	}
	if _, err := allowedClient.RememberMemoryFact(context.Background(), MemoryRememberRequest{
		FactKey:   "name",
		FactValue: "Ada",
	}); err != nil {
		t.Fatalf("remember memory with memory.write: %v", err)
	}
	if _, err := allowedClient.SubmitHavenContinuityInspectionForThread(context.Background(), threadID); err != nil {
		t.Fatalf("inspect continuity-thread with memory.write: %v", err)
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

func TestMemoryGovernanceRoutesRequireReviewAndLineageScope(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	threadID, workspaceID := createContinuityInspectThreadForTests(t, server, repoRoot)

	writerClient := NewClient(client.socketPath)
	writerClient.SetWorkspaceID(workspaceID)
	writerClient.ConfigureSession("haven", "memory-governance-writer", []string{"memory.write"})
	if _, err := writerClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure memory.write token: %v", err)
	}
	inspectThreadResponse, err := writerClient.SubmitHavenContinuityInspectionForThread(context.Background(), threadID)
	if err != nil {
		t.Fatalf("seed continuity inspection: %v", err)
	}
	if strings.TrimSpace(inspectThreadResponse.InspectionID) == "" {
		t.Fatalf("expected inspect-thread to create an inspection, got %#v", inspectThreadResponse)
	}

	reviewDeniedClient := NewClient(client.socketPath)
	reviewDeniedClient.ConfigureSession("memory-reviewer", "memory-review-denied", []string{"memory.write"})
	if _, err := reviewDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied review token: %v", err)
	}
	if _, err := reviewDeniedClient.ReviewMemoryInspection(context.Background(), inspectThreadResponse.InspectionID, MemoryInspectionReviewRequest{
		Decision:    continuityReviewStatusRejected,
		OperationID: "review_scope_denied",
		Reason:      "operator rejected lineage",
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected memory.review scope denial, got %v", err)
	}

	reviewAllowedClient := NewClient(client.socketPath)
	reviewAllowedClient.ConfigureSession("memory-reviewer", "memory-review-allowed", []string{"memory.review"})
	if _, err := reviewAllowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure memory.review token: %v", err)
	}
	if _, err := reviewAllowedClient.ReviewMemoryInspection(context.Background(), inspectThreadResponse.InspectionID, MemoryInspectionReviewRequest{
		Decision:    continuityReviewStatusAccepted,
		OperationID: "review_scope_allowed",
		Reason:      "operator accepted lineage",
	}); err != nil {
		t.Fatalf("review memory inspection with memory.review: %v", err)
	}

	lineageDeniedClient := NewClient(client.socketPath)
	lineageDeniedClient.ConfigureSession("memory-lineage", "memory-lineage-denied", []string{"memory.review"})
	if _, err := lineageDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied lineage token: %v", err)
	}
	if _, err := lineageDeniedClient.TombstoneMemoryInspection(context.Background(), inspectThreadResponse.InspectionID, MemoryInspectionLineageRequest{
		OperationID: "lineage_scope_denied",
		Reason:      "operator tombstoned lineage",
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected memory.lineage scope denial, got %v", err)
	}
}

func createContinuityInspectThreadForTests(t *testing.T, server *Server, repoRoot string) (string, string) {
	t.Helper()

	server.resolveUserHomeDir = func() (string, error) { return repoRoot, nil }
	workspaceID := server.deriveWorkspaceIDFromRepoRoot()
	threadStoreRoot := filepath.Join(repoRoot, ".haven", "threads")
	store, err := threadstore.NewStore(threadStoreRoot, workspaceID)
	if err != nil {
		t.Fatalf("thread store: %v", err)
	}
	summary, err := store.NewThread()
	if err != nil {
		t.Fatalf("new thread: %v", err)
	}
	if err := store.AppendEvent(summary.ThreadID, threadstore.ConversationEvent{
		Type: threadstore.EventUserMessage,
		Data: map[string]interface{}{"text": "monitor github status"},
	}); err != nil {
		t.Fatalf("append user event: %v", err)
	}
	if err := store.AppendEvent(summary.ThreadID, threadstore.ConversationEvent{
		Type: threadstore.EventAssistantMessage,
		Data: map[string]interface{}{"text": "noted"},
	}); err != nil {
		t.Fatalf("append assistant event: %v", err)
	}
	return summary.ThreadID, workspaceID
}
