package mcpserve

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"morph/internal/loopgate"
)

func TestMCPToolRegistrationAndExecution(t *testing.T) {
	socketPath := "/tmp/mock-lg-" + strconv.FormatInt(time.Now().UnixNano(), 10) + ".sock"
	defer os.Remove(socketPath)

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

	mux.HandleFunc("/v1/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(loopgate.StatusResponse{
			Capabilities: []loopgate.CapabilitySummary{
				{Name: "fs_list", Description: "List files"},
				{Name: "memory.remember", Description: "Remember fact"},
			},
		})
	})

	var lastExecuteReq loopgate.CapabilityRequest
	mux.HandleFunc("/v1/capabilities/execute", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &lastExecuteReq)
		
		w.Header().Set("Content-Type", "application/json")
		
		if lastExecuteReq.Capability == "fs_list" {
			json.NewEncoder(w).Encode(loopgate.CapabilityResponse{
				Status: loopgate.ResponseStatusSuccess,
				StructuredResult: map[string]interface{}{
					"files": []string{"a.txt"},
				},
			})
		} else {
			json.NewEncoder(w).Encode(loopgate.CapabilityResponse{
				Status:       loopgate.ResponseStatusDenied,
				DenialCode:   "policy_denied",
				DenialReason: "not allowed",
			})
		}
	})

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	ts := httptest.NewUnstartedServer(mux)
	ts.Listener = listener
	ts.Start()
	defer ts.Close()

	delegatedCfg := loopgate.DelegatedSessionConfig{
		ControlSessionID: "mock-session",
		CapabilityToken:  "mock-cap-token",
		ApprovalToken:    "mock-app-token",
		SessionMACKey:    "mock-mac-key",
		ExpiresAt:        time.Now().Add(1 * time.Hour),
		TenantID:         "tenant-1",
		UserID:           "user-1",
	}

	lgClient, err := loopgate.NewClientFromDelegatedSession(socketPath, delegatedCfg)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	// Test status tool
	statusHandler := handleStatusTool(lgClient, slog.Default())
	statusReq := mcp.CallToolRequest{}
	statusReq.Params.Name = "loopgate.status"
	statusRes, err := statusHandler(context.Background(), statusReq)
	if err != nil {
		t.Fatalf("status handler err: %v", err)
	}
	if statusRes.IsError {
		t.Fatalf("expected success, got error")
	}

	// Test execute memory.remember (denied)
	typedHandler := handleTypedCapabilityTool(lgClient, "memory.remember", slog.Default())
	execReq := mcp.CallToolRequest{}
	execReq.Params.Name = "memory.remember"
	execReq.Params.Arguments = map[string]interface{}{
		"arguments_json": `{"fact":"something"}`,
	}
	execRes, err := typedHandler(context.Background(), execReq)
	if err != nil {
		t.Fatalf("execute handler err: %v", err)
	}
	if !execRes.IsError {
		t.Fatalf("expected error (denied), got success")
	}

	// Test execute fs_list (success)
	typedHandlerFs := handleTypedCapabilityTool(lgClient, "fs_list", slog.Default())
	execReqFs := mcp.CallToolRequest{}
	execReqFs.Params.Name = "fs_list"
	execReqFs.Params.Arguments = map[string]interface{}{}
	execResFs, err := typedHandlerFs(context.Background(), execReqFs)
	if err != nil {
		t.Fatalf("execute fs handler err: %v", err)
	}
	if execResFs.IsError {
		t.Fatalf("expected success, got error: %v", execResFs.Content)
	}
}
