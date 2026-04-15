package loopgate

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

func TestSessionOpen_ControlPlaneSessionStoreSaturated(t *testing.T) {
	repoRoot := t.TempDir()
	client1, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	_ = client1
	server.maxTotalControlSessions = 1

	client2 := NewClient(server.socketPath)
	client2.ConfigureSession("test-actor", "second-session", capabilityNames(status.Capabilities))
	_, err := client2.ensureCapabilityToken(context.Background())
	if err == nil {
		t.Fatalf("expected second session open when at session cap")
	}
}

func TestHealthUnauthenticatedSucceeds(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				d := net.Dialer{}
				return d.DialContext(ctx, "unix", server.socketPath)
			},
		},
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://loopgate/v1/health", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	response, err := httpClient.Do(request)
	if err != nil {
		t.Fatalf("health request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected health 200, got %d", response.StatusCode)
	}
	var payload HealthResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if !payload.OK || payload.Version == "" {
		t.Fatalf("unexpected health payload %#v", payload)
	}
}

func TestStatusAndConnectionsStatusRejectUnauthenticatedSocketClient(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				d := net.Dialer{}
				return d.DialContext(ctx, "unix", server.socketPath)
			},
		},
	}

	for _, path := range []string{"/v1/status", "/v1/connections/status"} {
		request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://loopgate"+path, nil)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		response, err := httpClient.Do(request)
		if err != nil {
			t.Fatalf("request %s: %v", path, err)
		}
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("path %s: expected 401 without auth, got %d body %s", path, response.StatusCode, string(body))
		}
	}
}

func TestSessionOpen_RejectsUnknownCapabilities(t *testing.T) {
	repoRoot := t.TempDir()
	_, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	client2 := NewClient(server.socketPath)
	client2.ConfigureSession("test-actor", "unknown-cap-session", []string{"fs_read", "totally_fake_capability"})
	_, err := client2.ensureCapabilityToken(context.Background())
	if err == nil {
		t.Fatal("expected error when requesting unknown capability")
	}
	if !strings.Contains(err.Error(), "unknown capabilities") && !strings.Contains(err.Error(), "denied") {
		t.Fatalf("expected unknown capability rejection, got: %v", err)
	}
}

func TestSessionOpen_GrantsOnlyRegisteredCapabilities(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	for _, cap := range status.Capabilities {
		response, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
			RequestID:  "req-" + cap.Name,
			Capability: cap.Name,
			Arguments:  map[string]string{"path": "."},
		})
		if err != nil {
			t.Fatalf("execute %s: %v", cap.Name, err)
		}
		if response.DenialCode == DenialCodeCapabilityTokenScopeDenied {
			t.Errorf("capability %s should be in granted scope", cap.Name)
		}
	}
}

func TestSessionOpen_DuplicateLabelReplacesOldSession(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	firstToken := client.capabilityToken

	client.ConfigureSession("different-actor", "test-session", capabilityNames(status.Capabilities))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("second ensure (should replace old session): %v", err)
	}
	secondToken := client.capabilityToken

	if firstToken == secondToken {
		t.Error("new session should get a new token")
	}

	response, err := client.ExecuteCapability(context.Background(), CapabilityRequest{
		RequestID:  "req-after-replace",
		Capability: "fs_list",
		Arguments:  map[string]string{"path": "."},
	})
	if err != nil {
		t.Fatalf("execute after replace: %v", err)
	}
	if response.Status != ResponseStatusSuccess {
		t.Fatalf("expected success after session replace, got %#v", response)
	}
}
