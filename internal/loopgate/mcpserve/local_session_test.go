package mcpserve

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"morph/internal/loopgate"
)

func TestMCPServeLocalMode_SkipsDelegatedEnvRequirement(t *testing.T) {
	socketPath, capturedRequests := newLocalMCPServeTestSocket(t)
	defer os.Remove(socketPath)

	localClient, _, _, err := newLoopgateClientForServeMode(socketPath, nil, &LocalOpenSessionConfig{
		Actor:                 "claude_code",
		ClientSession:         "cursor_demo",
		RequestedCapabilities: []string{"fs_list"},
	})
	if err != nil {
		t.Fatalf("new local MCP serve client: %v", err)
	}

	if _, err := localClient.Status(context.Background()); err != nil {
		t.Fatalf("local MCP client status: %v", err)
	}
	if capturedRequests.sessionOpenCount != 1 {
		t.Fatalf("expected local mode to open one Loopgate session, got %d", capturedRequests.sessionOpenCount)
	}
}

func TestMCPServeLocalMode_RejectsEmptyCapabilitySet(t *testing.T) {
	socketPath, _ := newLocalMCPServeTestSocket(t)
	defer os.Remove(socketPath)

	_, _, _, err := newLoopgateClientForServeMode(socketPath, nil, &LocalOpenSessionConfig{
		Actor:         "claude_code",
		ClientSession: "cursor_demo",
	})
	if err == nil {
		t.Fatal("expected empty requested capability set to be denied")
	}
}

func TestMCPServeLocalMode_UsesRequestedActorAndSessionID(t *testing.T) {
	socketPath, capturedRequests := newLocalMCPServeTestSocket(t)
	defer os.Remove(socketPath)

	localClient, actor, clientSession, err := newLoopgateClientForServeMode(socketPath, nil, &LocalOpenSessionConfig{
		Actor:                 "claude_code",
		ClientSession:         "cursor_demo",
		RequestedCapabilities: []string{"fs_list", "memory.remember"},
	})
	if err != nil {
		t.Fatalf("new local MCP serve client: %v", err)
	}
	if actor != "claude_code" || clientSession != "cursor_demo" {
		t.Fatalf("expected returned actor/session to match local config, got actor=%q session=%q", actor, clientSession)
	}

	if _, err := localClient.Status(context.Background()); err != nil {
		t.Fatalf("local MCP client status: %v", err)
	}
	if capturedRequests.openSessionRequest.Actor != "claude_code" {
		t.Fatalf("expected actor claude_code, got %#v", capturedRequests.openSessionRequest)
	}
	if capturedRequests.openSessionRequest.SessionID != "cursor_demo" {
		t.Fatalf("expected session cursor_demo, got %#v", capturedRequests.openSessionRequest)
	}
	if !slices.Equal(capturedRequests.openSessionRequest.RequestedCapabilities, []string{"fs_list", "memory.remember"}) {
		t.Fatalf("expected requested capabilities to match local config, got %#v", capturedRequests.openSessionRequest)
	}
}

type capturedLocalServeRequests struct {
	sessionOpenCount int
	openSessionRequest loopgate.OpenSessionRequest
}

func newLocalMCPServeTestSocket(t *testing.T) (string, *capturedLocalServeRequests) {
	t.Helper()

	socketPath := "/tmp/mock-lg-local-" + strconv.FormatInt(time.Now().UnixNano(), 10) + ".sock"
	capturedRequests := &capturedLocalServeRequests{}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/session/open", func(w http.ResponseWriter, r *http.Request) {
		capturedRequests.sessionOpenCount++
		if err := json.NewDecoder(r.Body).Decode(&capturedRequests.openSessionRequest); err != nil {
			t.Fatalf("decode session open request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(loopgate.OpenSessionResponse{
			ControlSessionID: "csess-1",
			CapabilityToken:  "cap-token",
			ApprovalToken:    "app-token",
			SessionMACKey:    "mac-key",
			ExpiresAtUTC:     time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339Nano),
		})
	})
	mux.HandleFunc("/v1/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(loopgate.StatusResponse{})
	})

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	testServer := httptest.NewUnstartedServer(mux)
	testServer.Listener = listener
	testServer.Start()
	t.Cleanup(testServer.Close)

	return socketPath, capturedRequests
}
