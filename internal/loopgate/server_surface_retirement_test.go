package loopgate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewServerWithOptions_InitializesWithoutAdminSurface(t *testing.T) {
	repoRoot := t.TempDir()

	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	socketFile, err := os.CreateTemp("", "loopgate-*.sock")
	if err != nil {
		t.Fatalf("create temp socket file: %v", err)
	}
	socketPath := socketFile.Name()
	_ = socketFile.Close()
	_ = os.Remove(socketPath)
	t.Cleanup(func() { _ = os.Remove(socketPath) })

	server, err := NewServerWithOptions(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("NewServerWithOptions: %v", err)
	}
	if server == nil {
		t.Fatal("expected initialized server")
	}
	server.CloseDiagnosticLogs()
}

func TestRetiredHavenRoutesAreNotRegistered(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	var statusResponse StatusResponse
	if err := client.doJSON(context.Background(), http.MethodGet, "/v1/status", client.capabilityToken, nil, &statusResponse, nil); err != nil {
		t.Fatalf("status with retired Haven routes absent: %v", err)
	}

	err := client.doJSON(context.Background(), http.MethodPost, "/v1/chat", client.capabilityToken, map[string]string{
		"message": "retired route should be unregistered",
	}, &CapabilityResponse{}, nil)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 for retired /v1/chat route, got %v", err)
	}
}

func TestRetiredHavenSandboxCapabilitiesAreAbsent(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	if len(status.Capabilities) == 0 {
		t.Fatal("expected status capabilities")
	}

	hiddenCapabilities := []string{
		"journal.list",
		"journal.read",
		"journal.write",
		"haven.operator_context",
		"notes.list",
		"notes.read",
		"notes.write",
		"paint.list",
		"paint.save",
		"note.create",
		"desktop.organize",
	}
	for _, capabilityName := range hiddenCapabilities {
		if containsCapability(status.Capabilities, capabilityName) {
			t.Fatalf("expected %s to be absent after Haven retirement", capabilityName)
		}
	}

	if !containsCapability(status.Capabilities, "fs_read") {
		t.Fatal("expected core fs_read capability to remain available")
	}
	if !containsCapability(status.Capabilities, "host.folder.list") {
		t.Fatal("expected host.folder.list capability to remain available")
	}

	refreshedStatus, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("reload status: %v", err)
	}
	for _, capabilityName := range hiddenCapabilities {
		if containsCapability(refreshedStatus.Capabilities, capabilityName) {
			t.Fatalf("expected %s to stay absent in refreshed status", capabilityName)
		}
	}
}

func TestRetiredMemoryRoutesAreNotRegistered(t *testing.T) {
	repoRoot := newShortLoopgateTestRepoRoot(t)
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))

	socketPath := filepath.Join(repoRoot, "runtime", "state", "l.sock")
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	for _, path := range []string{
		"/v1/memory/wake-state",
		"/v1/memory/diagnostic-wake",
		"/v1/memory/discover",
		"/v1/memory/artifacts/lookup",
		"/v1/memory/recall",
		"/v1/memory/artifacts/get",
		"/v1/memory/remember",
		"/v1/memory/inspections/test-id/review",
		"/v1/memory/inspections/test-id/tombstone",
		"/v1/memory/inspections/test-id/purge",
	} {
		method := http.MethodPost
		if strings.HasSuffix(path, "wake-state") || strings.HasSuffix(path, "diagnostic-wake") {
			method = http.MethodGet
		}
		request := httptest.NewRequest(method, path, strings.NewReader(`{}`))
		recorder := httptest.NewRecorder()
		server.server.Handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("expected retired memory route %s to return 404, got %d", path, recorder.Code)
		}
	}
}

func TestRetiredMemorySurfaceIsAbsentFromStatus(t *testing.T) {
	repoRoot := newShortLoopgateTestRepoRoot(t)
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))

	socketPath := filepath.Join(repoRoot, "runtime", "state", "l.sock")
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	for _, hiddenControlCapability := range []string{} {
		if containsCapability(controlCapabilitySummaries(), hiddenControlCapability) {
			t.Fatalf("expected retired control capability %s to be absent", hiddenControlCapability)
		}
	}

	if server.currentPolicyRuntime().registry.Has("memory.remember") {
		t.Fatal("expected memory.remember to be absent after memory surface retirement")
	}
}

func TestMorphlingRoutesRetired(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	for _, path := range []string{
		"/v1/morphlings/spawn",
		"/v1/morphlings/status",
		"/v1/morphlings/terminate",
		"/v1/morphlings/review",
		"/v1/morphlings/worker/launch",
		"/v1/morphlings/worker/open",
		"/v1/morphlings/worker/start",
		"/v1/morphlings/worker/update",
		"/v1/morphlings/worker/complete",
	} {
		request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, client.baseURL+path, strings.NewReader(`{}`))
		if err != nil {
			t.Fatalf("build retired morphling request for %s: %v", path, err)
		}
		response, err := client.httpClient.Do(request)
		if err != nil {
			t.Fatalf("request retired morphling route %s: %v", path, err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("expected retired morphling route %s to return 404, got %d", path, response.StatusCode)
		}
	}
}
