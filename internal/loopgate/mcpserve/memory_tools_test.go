package mcpserve

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"morph/internal/loopgate"
)

func TestRegisterLoopgateTools_IncludesTypedMemoryTools(t *testing.T) {
	socketPath, delegatedClient := newDelegatedMCPMemoryTestClient(t, func(mux *http.ServeMux) {
		mux.HandleFunc("/v1/status", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(loopgate.StatusResponse{
				Capabilities: []loopgate.CapabilitySummary{
					{Name: "fs_list", Description: "List files"},
					{Name: "memory.remember", Description: "Remember fact"},
				},
			})
		})
	})
	defer os.Remove(socketPath)

	registeredTools, err := registeredLoopgateTools(context.Background(), delegatedClient, slog.Default())
	if err != nil {
		t.Fatalf("register loopgate tools: %v", err)
	}

	var toolNames []string
	for _, registeredTool := range registeredTools {
		toolNames = append(toolNames, registeredTool.Tool.Name)
	}
	for _, requiredToolName := range []string{
		"loopgate.status",
		"loopgate.execute_capability",
		"loopgate.memory_wake_state",
		"loopgate.memory_discover",
		"loopgate.memory_remember",
		"fs_list",
		"memory.remember",
	} {
		if !slices.Contains(toolNames, requiredToolName) {
			t.Fatalf("expected registered tools to include %q, got %#v", requiredToolName, toolNames)
		}
	}
}

func TestHandleMemoryWakeStateTool_LoadsWakeState(t *testing.T) {
	socketPath, delegatedClient := newDelegatedMCPMemoryTestClient(t, func(mux *http.ServeMux) {
		mux.HandleFunc("/v1/memory/wake-state", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(loopgate.MemoryWakeStateResponse{
				ID:                 "wake-1",
				Scope:              "global",
				ApproxPromptTokens: 14,
			})
		})
	})
	defer os.Remove(socketPath)

	handler := handleMemoryWakeStateTool(delegatedClient, slog.Default())
	result, err := handler(context.Background(), mcpCall("loopgate.memory_wake_state", nil))
	if err != nil {
		t.Fatalf("wake-state handler err: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected wake-state success, got error: %#v", result.Content)
	}
	if !toolResultTextContains(t, result, `"id": "wake-1"`) {
		t.Fatalf("expected wake-state payload, got %#v", result.Content)
	}
}

func TestHandleMemoryDiscoverTool_RejectsEmptyQuery(t *testing.T) {
	socketPath, delegatedClient := newDelegatedMCPMemoryTestClient(t, nil)
	defer os.Remove(socketPath)

	handler := handleMemoryDiscoverTool(delegatedClient, slog.Default())
	result, err := handler(context.Background(), mcpCall("loopgate.memory_discover", map[string]interface{}{
		"scope": "global",
	}))
	if err != nil {
		t.Fatalf("discover handler err: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected empty-query discover request to fail, got %#v", result.Content)
	}
	if !toolResultTextContains(t, result, "query is required") {
		t.Fatalf("expected query validation error, got %#v", result.Content)
	}
}

func TestHandleMemoryRememberTool_UsesTypedFields(t *testing.T) {
	var capturedRememberRequest loopgate.MemoryRememberRequest
	socketPath, delegatedClient := newDelegatedMCPMemoryTestClient(t, func(mux *http.ServeMux) {
		mux.HandleFunc("/v1/memory/remember", func(w http.ResponseWriter, r *http.Request) {
			if err := json.NewDecoder(r.Body).Decode(&capturedRememberRequest); err != nil {
				t.Fatalf("decode remember request: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(loopgate.MemoryRememberResponse{
				Scope:           "global",
				FactKey:         "profile.timezone",
				FactValue:       "America/Denver",
				InspectionID:    "insp-1",
				DistillateID:    "dist-1",
				ResonateKeyID:   "key-1",
				RememberedAtUTC: "2026-04-03T05:00:00Z",
			})
		})
	})
	defer os.Remove(socketPath)

	handler := handleMemoryRememberTool(delegatedClient, slog.Default())
	result, err := handler(context.Background(), mcpCall("loopgate.memory_remember", map[string]interface{}{
		"scope":            "global",
		"fact_key":         "profile.timezone",
		"fact_value":       "America/Denver",
		"reason":           "user explicitly stated timezone",
		"source_text":      "My timezone is America/Denver.",
		"candidate_source": "explicit_fact",
		"source_channel":   "conversation",
	}))
	if err != nil {
		t.Fatalf("remember handler err: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected remember success, got error: %#v", result.Content)
	}
	if capturedRememberRequest.FactKey != "profile.timezone" || capturedRememberRequest.FactValue != "America/Denver" {
		t.Fatalf("expected typed remember fields to populate request, got %#v", capturedRememberRequest)
	}
	if capturedRememberRequest.Reason != "user explicitly stated timezone" || capturedRememberRequest.SourceText != "My timezone is America/Denver." {
		t.Fatalf("expected typed optional fields to propagate, got %#v", capturedRememberRequest)
	}
	if capturedRememberRequest.CandidateSource != "explicit_fact" || capturedRememberRequest.SourceChannel != "conversation" {
		t.Fatalf("expected typed memory source fields to propagate, got %#v", capturedRememberRequest)
	}
}

func TestHandleMemoryRememberTool_PropagatesDeniedResponse(t *testing.T) {
	socketPath, delegatedClient := newDelegatedMCPMemoryTestClient(t, func(mux *http.ServeMux) {
		mux.HandleFunc("/v1/memory/remember", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(loopgate.CapabilityResponse{
				Status:       loopgate.ResponseStatusError,
				DenialCode:   loopgate.DenialCodeMalformedRequest,
				DenialReason: "fact_key is required",
			})
		})
	})
	defer os.Remove(socketPath)

	handler := handleMemoryRememberTool(delegatedClient, slog.Default())
	result, err := handler(context.Background(), mcpCall("loopgate.memory_remember", map[string]interface{}{
		"fact_value": "America/Denver",
	}))
	if err != nil {
		t.Fatalf("remember handler err: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected denied remember response to be surfaced as error, got %#v", result.Content)
	}
	if !toolResultTextContains(t, result, loopgate.DenialCodeMalformedRequest) || !toolResultTextContains(t, result, "fact_key is required") {
		t.Fatalf("expected structured denial details, got %#v", result.Content)
	}
}

func newDelegatedMCPMemoryTestClient(t *testing.T, registerRoutes func(mux *http.ServeMux)) (string, *loopgate.Client) {
	t.Helper()

	socketPath := "/tmp/mock-lg-" + strconv.FormatInt(time.Now().UnixNano(), 10) + ".sock"

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/session/open", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(loopgate.OpenSessionResponse{
			ControlSessionID: "csess-1",
			CapabilityToken:  "cap-token",
			ApprovalToken:    "app-token",
			SessionMACKey:    "mac-key",
			ExpiresAtUTC:     time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339Nano),
		})
	})
	if registerRoutes != nil {
		registerRoutes(mux)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	testServer := httptest.NewUnstartedServer(mux)
	testServer.Listener = listener
	testServer.Start()
	t.Cleanup(testServer.Close)

	delegatedCfg := loopgate.DelegatedSessionConfig{
		ControlSessionID: "mock-session",
		CapabilityToken:  "mock-cap-token",
		ApprovalToken:    "mock-app-token",
		SessionMACKey:    "mock-mac-key",
		ExpiresAt:        time.Now().Add(1 * time.Hour),
		TenantID:         "tenant-1",
		UserID:           "user-1",
	}

	delegatedClient, err := loopgate.NewClientFromDelegatedSession(socketPath, delegatedCfg)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return socketPath, delegatedClient
}

func mcpCall(toolName string, arguments map[string]interface{}) mcp.CallToolRequest {
	var request mcp.CallToolRequest
	request.Params.Name = toolName
	request.Params.Arguments = arguments
	return request
}

func toolResultTextContains(t *testing.T, result *mcp.CallToolResult, want string) bool {
	t.Helper()
	for _, content := range result.Content {
		textContent, ok := content.(mcp.TextContent)
		if !ok {
			continue
		}
		if strings.Contains(textContent.Text, want) {
			return true
		}
	}
	return false
}
