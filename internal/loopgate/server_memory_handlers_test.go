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

	for _, requiredCapability := range []string{"memory.read", "memory.write", "memory.review", "memory.lineage"} {
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
}

func TestMemoryWriteRoutesRequireMemoryWriteScope(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	deniedClient := NewClient(client.socketPath)
	deniedClient.ConfigureSession("memory-writer", "memory-write-denied", []string{"memory.read"})
	if _, err := deniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied token: %v", err)
	}
	if _, err := deniedClient.RememberMemoryFact(context.Background(), MemoryRememberRequest{
		FactKey:   "name",
		FactValue: "Ada",
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected memory.write scope denial for remember, got %v", err)
	}
	if _, err := deniedClient.InspectContinuityThread(context.Background(), testContinuityInspectRequestForSession("inspect_scope_denied", "thread_scope_denied", "monitor github status", "memory-write-denied")); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected memory.write scope denial for continuity inspect, got %v", err)
	}

	allowedClient := NewClient(client.socketPath)
	allowedClient.ConfigureSession("memory-writer", "memory-write-allowed", []string{"memory.write"})
	if _, err := allowedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure memory.write token: %v", err)
	}
	if _, err := allowedClient.RememberMemoryFact(context.Background(), MemoryRememberRequest{
		FactKey:   "name",
		FactValue: "Ada",
	}); err != nil {
		t.Fatalf("remember memory with memory.write: %v", err)
	}
	if _, err := allowedClient.InspectContinuityThread(context.Background(), testContinuityInspectRequestForSession("inspect_scope_allowed", "thread_scope_allowed", "monitor github status", "memory-write-allowed")); err != nil {
		t.Fatalf("inspect continuity with memory.write: %v", err)
	}
}

func TestRawContinuityInspectDisabledByPolicy(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAMLWithRawContinuityInspect(false, false))

	writerClient := NewClient(client.socketPath)
	writerClient.ConfigureSession("memory-writer", "memory-write-raw-continuity-disabled", []string{"memory.write"})
	if _, err := writerClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure memory.write token: %v", err)
	}
	if _, err := writerClient.InspectContinuityThread(context.Background(), testContinuityInspectRequestForSession("inspect_policy_disabled", "thread_policy_disabled", "monitor github status", "memory-write-raw-continuity-disabled")); err == nil || !strings.Contains(err.Error(), DenialCodePolicyDenied) || !strings.Contains(err.Error(), "/v1/continuity/inspect-thread") {
		t.Fatalf("expected raw continuity inspect policy denial directing caller to inspect-thread, got %v", err)
	}
}

func TestMemoryGovernanceRoutesRequireReviewAndLineageScope(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	writerClient := NewClient(client.socketPath)
	writerClient.ConfigureSession("memory-writer", "memory-governance-writer", []string{"memory.write"})
	if _, err := writerClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure memory.write token: %v", err)
	}
	inspectResponse, err := writerClient.InspectContinuityThread(context.Background(), testContinuityInspectRequestForSession("inspect_governance_scope", "thread_governance_scope", "monitor github status", "memory-governance-writer"))
	if err != nil {
		t.Fatalf("seed continuity inspection: %v", err)
	}

	reviewDeniedClient := NewClient(client.socketPath)
	reviewDeniedClient.ConfigureSession("memory-reviewer", "memory-review-denied", []string{"memory.write"})
	if _, err := reviewDeniedClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure denied review token: %v", err)
	}
	if _, err := reviewDeniedClient.ReviewMemoryInspection(context.Background(), inspectResponse.InspectionID, MemoryInspectionReviewRequest{
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
	if _, err := reviewAllowedClient.ReviewMemoryInspection(context.Background(), inspectResponse.InspectionID, MemoryInspectionReviewRequest{
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
	if _, err := lineageDeniedClient.TombstoneMemoryInspection(context.Background(), inspectResponse.InspectionID, MemoryInspectionLineageRequest{
		OperationID: "lineage_scope_denied",
		Reason:      "operator tombstoned lineage",
	}); err == nil || !strings.Contains(err.Error(), DenialCodeCapabilityTokenScopeDenied) {
		t.Fatalf("expected memory.lineage scope denial, got %v", err)
	}
}
