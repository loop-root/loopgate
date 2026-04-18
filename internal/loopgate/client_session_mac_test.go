package loopgate

import (
	"context"
	"io"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"strings"
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

	resp, err := client.ExecuteCapability(ctx, controlapipkg.CapabilityRequest{
		RequestID:  "req-mac-refresh-align",
		Capability: "fs_list",
		Arguments:  map[string]string{"path": "."},
	})
	if err != nil {
		t.Fatalf("execute after refresh: %v", err)
	}
	if resp.Status != controlapipkg.ResponseStatusSuccess {
		t.Fatalf("expected success after refresh, got %#v", resp)
	}
}

func TestSessionMACKeysRoute_DoesNotExposeEpochKeyMaterial(t *testing.T) {
	ctx := context.Background()
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL+"/v1/session/mac-keys", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+capabilityToken)
	if err := client.attachRequestSignature(request, "/v1/session/mac-keys", nil); err != nil {
		t.Fatalf("attach request signature: %v", err)
	}

	httpResponse, err := client.httpClient.Do(request)
	if err != nil {
		t.Fatalf("session mac keys request: %v", err)
	}
	defer httpResponse.Body.Close()
	if httpResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", httpResponse.StatusCode)
	}

	responseBytes, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if strings.Contains(string(responseBytes), "epoch_key_material_hex") {
		t.Fatalf("session mac route leaked epoch key material: %s", responseBytes)
	}
}
