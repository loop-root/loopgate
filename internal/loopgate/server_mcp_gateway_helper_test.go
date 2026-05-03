package loopgate

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

func TestMCPGatewayFakeStdioServerHelper(t *testing.T) {
	if os.Getenv("LOOPGATE_MCP_GATEWAY_TEST_HELPER") != "1" {
		return
	}
	helperMode := strings.TrimSpace(os.Getenv("LOOPGATE_MCP_GATEWAY_TEST_HELPER_MODE"))

	helperTransport := &mcpGatewayLaunchedServer{
		StdinWriter:          os.Stdout,
		StdoutBufferedReader: bufio.NewReader(os.Stdin),
	}

	type helperRequest struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id,omitempty"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params,omitempty"`
	}

	for {
		requestBodyBytes, err := readMCPGatewayJSONRPCFrame(helperTransport)
		if err != nil {
			if errors.Is(err, io.EOF) || strings.Contains(err.Error(), "EOF") {
				os.Exit(0)
			}
			_, _ = fmt.Fprintf(os.Stderr, "helper read frame: %v\n", err)
			os.Exit(2)
		}

		var request helperRequest
		if err := json.Unmarshal(requestBodyBytes, &request); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "helper decode request: %v\n", err)
			os.Exit(2)
		}

		switch request.Method {
		case "initialize":
			responseBytes, err := json.Marshal(map[string]interface{}{
				"jsonrpc": mcpGatewayJSONRPCVersion,
				"id":      json.RawMessage(request.ID),
				"result": map[string]interface{}{
					"protocolVersion": mcpGatewayProtocolVersion,
					"capabilities": map[string]interface{}{
						"tools": map[string]interface{}{},
					},
					"serverInfo": map[string]interface{}{
						"name":    "loopgate-test-helper",
						"version": "1.0.0",
					},
				},
			})
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "helper marshal initialize response: %v\n", err)
				os.Exit(2)
			}
			if err := writeMCPGatewayJSONRPCFrame(helperTransport, responseBytes); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "helper write initialize response: %v\n", err)
				os.Exit(2)
			}
		case "notifications/initialized":
			continue
		case "tools/call":
			switch helperMode {
			case "notification_flood":
				for notificationIndex := 0; notificationIndex <= mcpGatewayMaxNotificationFrames; notificationIndex++ {
					notificationBytes, err := json.Marshal(map[string]interface{}{
						"jsonrpc": mcpGatewayJSONRPCVersion,
						"method":  "notifications/tools/list_changed",
						"params": map[string]interface{}{
							"sequence": notificationIndex,
						},
					})
					if err != nil {
						_, _ = fmt.Fprintf(os.Stderr, "helper marshal notification flood frame: %v\n", err)
						os.Exit(2)
					}
					if err := writeMCPGatewayJSONRPCFrame(helperTransport, notificationBytes); err != nil {
						os.Exit(0)
					}
				}
				select {}
			case "block_tools_call":
				select {}
			}

			var params struct {
				Name      string                     `json:"name"`
				Arguments map[string]json.RawMessage `json:"arguments"`
			}
			if err := json.Unmarshal(request.Params, &params); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "helper decode tools/call params: %v\n", err)
				os.Exit(2)
			}
			responseBytes, err := json.Marshal(map[string]interface{}{
				"jsonrpc": mcpGatewayJSONRPCVersion,
				"id":      json.RawMessage(request.ID),
				"result": map[string]interface{}{
					"content": []map[string]interface{}{
						{
							"type": "text",
							"text": "ok",
						},
					},
					"structuredContent": map[string]interface{}{
						"echo_name":      params.Name,
						"echo_arguments": params.Arguments,
					},
				},
			})
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "helper marshal tools/call response: %v\n", err)
				os.Exit(2)
			}
			if err := writeMCPGatewayJSONRPCFrame(helperTransport, responseBytes); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "helper write tools/call response: %v\n", err)
				os.Exit(2)
			}
		default:
			responseBytes, err := json.Marshal(map[string]interface{}{
				"jsonrpc": mcpGatewayJSONRPCVersion,
				"id":      json.RawMessage(request.ID),
				"error": map[string]interface{}{
					"code":    -32601,
					"message": "method not found",
				},
			})
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "helper marshal error response: %v\n", err)
				os.Exit(2)
			}
			if err := writeMCPGatewayJSONRPCFrame(helperTransport, responseBytes); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "helper write error response: %v\n", err)
				os.Exit(2)
			}
		}
	}
}
