package loopgate

import (
	"context"
	"testing"
)

func TestClientRefreshSessionMACKeyFromServer_alignsWithMacKeysCurrentSlot(t *testing.T) {
	ctx := context.Background()
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	keys, err := client.SessionMACKeys(ctx)
	if err != nil {
		t.Fatalf("SessionMACKeys: %v", err)
	}
	if keys.Current.DerivedSessionMACKey == keys.Previous.DerivedSessionMACKey {
		t.Fatal("expected previous and current derived keys to differ for the test scenario")
	}

	// Use the previous epoch's derived key: still inside the server's verify window (prev/current/next),
	// but not the canonical current slot we want the client to hold after refresh.
	client.mu.Lock()
	client.sessionMACKey = keys.Previous.DerivedSessionMACKey
	client.mu.Unlock()

	if err := client.RefreshSessionMACKeyFromServer(ctx); err != nil {
		t.Fatalf("RefreshSessionMACKeyFromServer: %v", err)
	}

	client.mu.Lock()
	got := client.sessionMACKey
	client.mu.Unlock()
	if got != keys.Current.DerivedSessionMACKey {
		t.Fatalf("after refresh want current derived key %q, got %q", keys.Current.DerivedSessionMACKey, got)
	}

	resp, err := client.ExecuteCapability(ctx, CapabilityRequest{
		RequestID:  "req-mac-refresh-align",
		Capability: "fs_list",
		Arguments:  map[string]string{"path": "."},
	})
	if err != nil {
		t.Fatalf("execute after refresh: %v", err)
	}
	if resp.Status != ResponseStatusSuccess {
		t.Fatalf("expected success after refresh, got %#v", resp)
	}
}
