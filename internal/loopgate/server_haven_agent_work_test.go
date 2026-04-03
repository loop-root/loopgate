package loopgate

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestHavenAgentWorkItemEnsureAndComplete(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	client.ConfigureSession("haven", "haven-agent-work-test", capabilityNames(status.Capabilities))
	ctx := context.Background()
	token, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		t.Fatalf("token: %v", err)
	}

	var first HavenAgentWorkItemResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/haven/agent/work-item/ensure", token, map[string]string{
		"text": "Organize granted Downloads (agent test)",
	}, &first, nil); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if strings.TrimSpace(first.ItemID) == "" {
		t.Fatalf("missing item_id: %#v", first)
	}
	if first.AlreadyPresent {
		t.Fatalf("expected first add not already_present")
	}

	var second HavenAgentWorkItemResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/haven/agent/work-item/ensure", token, map[string]string{
		"text": "Organize granted Downloads (agent test)",
	}, &second, nil); err != nil {
		t.Fatalf("ensure duplicate: %v", err)
	}
	if second.ItemID != first.ItemID {
		t.Fatalf("dedupe: got %q want %q", second.ItemID, first.ItemID)
	}
	if !second.AlreadyPresent {
		t.Fatalf("expected already_present on second ensure")
	}

	var done HavenAgentWorkItemResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/haven/agent/work-item/complete", token, map[string]string{
		"item_id": first.ItemID,
		"reason":  "test_complete",
	}, &done, nil); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if done.ItemID != first.ItemID {
		t.Fatalf("complete item_id: got %q", done.ItemID)
	}
}
