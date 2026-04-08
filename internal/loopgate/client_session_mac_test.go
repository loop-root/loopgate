package loopgate

import (
	"context"
	"strings"
	"testing"
)

func TestClientRefreshSessionMACKeyFromServer_restoresValidSigning(t *testing.T) {
	ctx := context.Background()
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	fsList := func(requestID string) (CapabilityResponse, error) {
		return client.ExecuteCapability(ctx, CapabilityRequest{
			RequestID:  requestID,
			Capability: "fs_list",
			Arguments:  map[string]string{"path": "."},
		})
	}

	baseline, err := fsList("req-mac-refresh-1")
	if err != nil {
		t.Fatalf("baseline execute: %v", err)
	}
	if baseline.Status != ResponseStatusSuccess {
		t.Fatalf("expected baseline success, got %#v", baseline)
	}

	// Corrupt the in-memory MAC key so signed requests fail (same pattern as signature denial tests).
	client.mu.Lock()
	client.sessionMACKey = strings.Repeat("aa", 32)
	client.mu.Unlock()

	broken, err := fsList("req-mac-refresh-2")
	if err != nil {
		t.Fatalf("execute with bad mac key: %v", err)
	}
	if broken.Status != ResponseStatusDenied || broken.DenialCode != DenialCodeRequestSignatureInvalid {
		t.Fatalf("expected request_signature_invalid after corrupting session_mac_key, got %#v", broken)
	}

	if err := client.RefreshSessionMACKeyFromServer(ctx); err != nil {
		t.Fatalf("RefreshSessionMACKeyFromServer: %v", err)
	}

	restored, err := fsList("req-mac-refresh-3")
	if err != nil {
		t.Fatalf("execute after refresh: %v", err)
	}
	if restored.Status != ResponseStatusSuccess {
		t.Fatalf("expected success after refresh, got %#v", restored)
	}
}
