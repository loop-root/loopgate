package loopgate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"strconv"
	"strings"
)

const (
	mcpGatewayJSONRPCVersion        = "2.0"
	mcpGatewayProtocolVersion       = "2025-03-26"
	mcpGatewayMaxMessageHeaderBytes = 8 * 1024
	mcpGatewayMaxMessageBodyBytes   = 1024 * 1024
	mcpGatewayMaxNotificationFrames = 64
)

type mcpGatewayJSONRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type mcpGatewayJSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      string      `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type mcpGatewayJSONRPCResponseEnvelope struct {
	JSONRPC string                  `json:"jsonrpc"`
	ID      json.RawMessage         `json:"id,omitempty"`
	Method  string                  `json:"method,omitempty"`
	Result  json.RawMessage         `json:"result,omitempty"`
	Error   *mcpGatewayJSONRPCError `json:"error,omitempty"`
}

func buildMCPGatewayJSONRPCFrame(messageBodyBytes []byte) ([]byte, error) {
	if len(messageBodyBytes) > mcpGatewayMaxMessageBodyBytes {
		return nil, fmt.Errorf("mcp gateway frame body exceeds maximum size")
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(messageBodyBytes))
	if len(header) > mcpGatewayMaxMessageHeaderBytes {
		return nil, fmt.Errorf("mcp gateway frame header exceeds maximum size")
	}
	frame := make([]byte, 0, len(header))
	frame = append(frame, []byte(header)...)
	frame = append(frame, messageBodyBytes...)
	return frame, nil
}

func writeMCPGatewayJSONRPCFrame(launchedServer *mcpGatewayLaunchedServer, messageBodyBytes []byte) error {
	if launchedServer == nil || launchedServer.StdinWriter == nil {
		return fmt.Errorf("mcp gateway stdin is unavailable")
	}
	if len(messageBodyBytes) == 0 {
		return fmt.Errorf("mcp gateway message body is required")
	}
	frameBytes, err := buildMCPGatewayJSONRPCFrame(messageBodyBytes)
	if err != nil {
		return err
	}
	if _, err := launchedServer.StdinWriter.Write(frameBytes); err != nil {
		return fmt.Errorf("write mcp gateway frame: %w", err)
	}
	return nil
}

func readMCPGatewayJSONRPCFrame(launchedServer *mcpGatewayLaunchedServer) ([]byte, error) {
	if launchedServer == nil || launchedServer.StdoutBufferedReader == nil {
		return nil, fmt.Errorf("mcp gateway stdout is unavailable")
	}

	contentLength := -1
	headerBytesRead := 0
	for {
		headerLine, err := launchedServer.StdoutBufferedReader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("read mcp gateway frame header: %w", err)
		}
		headerBytesRead += len(headerLine)
		if headerBytesRead > mcpGatewayMaxMessageHeaderBytes {
			return nil, fmt.Errorf("mcp gateway frame header exceeds maximum size")
		}

		trimmedHeaderLine := strings.TrimRight(headerLine, "\r\n")
		if trimmedHeaderLine == "" {
			break
		}
		headerName, headerValue, found := strings.Cut(trimmedHeaderLine, ":")
		if !found {
			return nil, fmt.Errorf("mcp gateway frame header is malformed")
		}
		if strings.EqualFold(strings.TrimSpace(headerName), "Content-Length") {
			parsedContentLength, err := strconv.Atoi(strings.TrimSpace(headerValue))
			if err != nil || parsedContentLength < 0 {
				return nil, fmt.Errorf("mcp gateway content-length header is invalid")
			}
			if parsedContentLength > mcpGatewayMaxMessageBodyBytes {
				return nil, fmt.Errorf("mcp gateway frame body exceeds maximum size")
			}
			contentLength = parsedContentLength
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("mcp gateway content-length header is missing")
	}

	messageBodyBytes := make([]byte, contentLength)
	if _, err := io.ReadFull(launchedServer.StdoutBufferedReader, messageBodyBytes); err != nil {
		return nil, fmt.Errorf("read mcp gateway frame body: %w", err)
	}
	return messageBodyBytes, nil
}

func decodeMCPGatewayJSONRPCResponseID(rawID json.RawMessage) (string, error) {
	if len(rawID) == 0 {
		return "", nil
	}
	var responseID string
	if err := json.Unmarshal(rawID, &responseID); err != nil {
		return "", fmt.Errorf("decode mcp gateway response id: %w", err)
	}
	return responseID, nil
}

func mcpGatewayRoundTripContextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func performMCPGatewayJSONRPCRoundTrip(ctx context.Context, launchedServer *mcpGatewayLaunchedServer, method string, params interface{}) (json.RawMessage, *mcpGatewayJSONRPCError, error) {
	if err := mcpGatewayRoundTripContextError(ctx); err != nil {
		return nil, nil, err
	}

	requestID, err := randomHex(8)
	if err != nil {
		return nil, nil, fmt.Errorf("allocate mcp gateway request id: %w", err)
	}
	requestBodyBytes, err := json.Marshal(mcpGatewayJSONRPCRequest{
		JSONRPC: mcpGatewayJSONRPCVersion,
		ID:      requestID,
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("marshal mcp gateway request: %w", err)
	}
	if err := writeMCPGatewayJSONRPCFrame(launchedServer, requestBodyBytes); err != nil {
		return nil, nil, err
	}

	notificationFramesSeen := 0
	for {
		if err := mcpGatewayRoundTripContextError(ctx); err != nil {
			return nil, nil, err
		}

		responseBodyBytes, err := readMCPGatewayJSONRPCFrame(launchedServer)
		if err != nil {
			return nil, nil, err
		}
		var responseEnvelope mcpGatewayJSONRPCResponseEnvelope
		if err := json.Unmarshal(responseBodyBytes, &responseEnvelope); err != nil {
			return nil, nil, fmt.Errorf("decode mcp gateway response: %w", err)
		}
		if responseEnvelope.JSONRPC != "" && responseEnvelope.JSONRPC != mcpGatewayJSONRPCVersion {
			return nil, nil, fmt.Errorf("mcp gateway response jsonrpc version is invalid")
		}
		if strings.TrimSpace(responseEnvelope.Method) != "" && len(responseEnvelope.ID) == 0 {
			notificationFramesSeen++
			if notificationFramesSeen > mcpGatewayMaxNotificationFrames {
				return nil, nil, fmt.Errorf("mcp gateway notification flood exceeded maximum frames per request")
			}
			continue
		}
		responseID, err := decodeMCPGatewayJSONRPCResponseID(responseEnvelope.ID)
		if err != nil {
			return nil, nil, err
		}
		if responseID != requestID {
			return nil, nil, fmt.Errorf("mcp gateway response id does not match request")
		}
		if responseEnvelope.Error != nil {
			return nil, responseEnvelope.Error, nil
		}
		return append(json.RawMessage(nil), responseEnvelope.Result...), nil, nil
	}
}

func initializeMCPGatewayLaunchedServer(ctx context.Context, launchedServer *mcpGatewayLaunchedServer) error {
	if launchedServer == nil {
		return fmt.Errorf("mcp gateway server is unavailable")
	}
	if launchedServer.Initialized {
		return nil
	}

	initializeResult, remoteError, err := performMCPGatewayJSONRPCRoundTrip(ctx, launchedServer, "initialize", map[string]interface{}{
		"protocolVersion": mcpGatewayProtocolVersion,
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]interface{}{
			"name":    "loopgate",
			"version": statusVersion,
		},
	})
	if err != nil {
		return err
	}
	if remoteError != nil {
		return fmt.Errorf("mcp gateway initialize failed: %s", remoteError.Message)
	}
	if len(initializeResult) == 0 || !json.Valid(initializeResult) {
		return fmt.Errorf("mcp gateway initialize result is invalid")
	}
	if err := mcpGatewayRoundTripContextError(ctx); err != nil {
		return err
	}

	initializedNotificationBytes, err := json.Marshal(mcpGatewayJSONRPCRequest{
		JSONRPC: mcpGatewayJSONRPCVersion,
		Method:  "notifications/initialized",
		Params:  map[string]interface{}{},
	})
	if err != nil {
		return fmt.Errorf("marshal mcp gateway initialized notification: %w", err)
	}
	if err := writeMCPGatewayJSONRPCFrame(launchedServer, initializedNotificationBytes); err != nil {
		return err
	}

	launchedServer.Initialized = true
	return nil
}

func (server *Server) resolveMCPGatewayLaunchedServer(serverID string) (*mcpGatewayLaunchedServer, controlapipkg.CapabilityResponse, bool) {
	server.cleanupDeadMCPGatewayServerIfNeeded(serverID)

	server.mu.Lock()
	defer server.mu.Unlock()

	launchedServer, found := server.mcpGatewayLaunchedServers[strings.TrimSpace(serverID)]
	if !found || launchedServer == nil || launchedServer.LaunchState != mcpGatewayServerStateLaunched || launchedServer.PID <= 0 {
		return nil, controlapipkg.CapabilityResponse{
			RequestID:    strings.TrimSpace(serverID),
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "mcp gateway server is not launched",
			DenialCode:   controlapipkg.DenialCodeMCPGatewayServerNotLaunched,
		}, false
	}
	return launchedServer, controlapipkg.CapabilityResponse{}, true
}

func (server *Server) dropMCPGatewayLaunchedServer(serverID string, launchedServer *mcpGatewayLaunchedServer) {
	server.mu.Lock()
	currentLaunchedServer, found := server.mcpGatewayLaunchedServers[strings.TrimSpace(serverID)]
	if found && currentLaunchedServer == launchedServer {
		delete(server.mcpGatewayLaunchedServers, strings.TrimSpace(serverID))
	}
	server.mu.Unlock()

	closeMCPGatewayLaunchedServerPipes(launchedServer)
	if launchedServer != nil {
		killMCPGatewayProcessByPID(launchedServer.PID)
	}
}

func (server *Server) executeMCPGatewayInvocation(ctx context.Context, tokenClaims capabilityToken, executionRequest controlapipkg.MCPGatewayExecutionRequest) (controlapipkg.MCPGatewayExecutionResponse, controlapipkg.CapabilityResponse, bool) {
	approvalRequest, validationResponse, denialResponse, ok := server.validateMCPGatewayExecutionRequestWithApproval(tokenClaims, executionRequest)
	if !ok {
		return controlapipkg.MCPGatewayExecutionResponse{}, denialResponse, false
	}

	launchedServer, denialResponse, ok := server.resolveMCPGatewayLaunchedServer(validationResponse.ServerID)
	if !ok {
		return controlapipkg.MCPGatewayExecutionResponse{}, denialResponse, false
	}

	if err := server.logEvent("mcp_gateway.execution_started", tokenClaims.ControlSessionID, buildMCPGatewayExecutionStartedAuditData(tokenClaims, approvalRequest, launchedServer)); err != nil {
		return controlapipkg.MCPGatewayExecutionResponse{}, controlapipkg.CapabilityResponse{
			RequestID:         approvalRequest.ID,
			Status:            controlapipkg.ResponseStatusError,
			DenialReason:      "control-plane audit is unavailable",
			DenialCode:        controlapipkg.DenialCodeAuditUnavailable,
			ApprovalRequestID: approvalRequest.ID,
			Redacted:          true,
		}, false
	}

	consumedApproval, denialResponse, ok := server.consumeGrantedMCPGatewayApprovalForExecution(tokenClaims, executionRequest)
	if !ok {
		return controlapipkg.MCPGatewayExecutionResponse{}, denialResponse, false
	}
	if err := mcpGatewayRoundTripContextError(ctx); err != nil {
		server.markMCPGatewayApprovalExecutionFailed(consumedApproval.ID, consumedApproval.ApprovalManifestSHA256)
		if auditErr := server.logEvent("mcp_gateway.execution_failed", tokenClaims.ControlSessionID, buildMCPGatewayExecutionFailedAuditData(tokenClaims, consumedApproval, launchedServer, controlapipkg.DenialCodeExecutionFailed)); auditErr != nil {
			return controlapipkg.MCPGatewayExecutionResponse{}, controlapipkg.CapabilityResponse{
				RequestID:         consumedApproval.ID,
				Status:            controlapipkg.ResponseStatusError,
				DenialReason:      "control-plane audit is unavailable",
				DenialCode:        controlapipkg.DenialCodeAuditUnavailable,
				ApprovalRequestID: consumedApproval.ID,
				Redacted:          true,
			}, false
		}
		return controlapipkg.MCPGatewayExecutionResponse{}, controlapipkg.CapabilityResponse{
			RequestID:         consumedApproval.ID,
			Status:            controlapipkg.ResponseStatusError,
			DenialReason:      "mcp gateway execution context canceled before dispatch",
			DenialCode:        controlapipkg.DenialCodeExecutionFailed,
			ApprovalRequestID: consumedApproval.ID,
			Redacted:          true,
		}, false
	}

	launchedServer.ioMu.Lock()
	defer launchedServer.ioMu.Unlock()

	stopCancellationWatch := func() {}
	if ctx != nil && ctx.Done() != nil {
		cancellationWatchDone := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				closeMCPGatewayLaunchedServerPipes(launchedServer)
				killMCPGatewayProcessByPID(launchedServer.PID)
			case <-cancellationWatchDone:
			}
		}()
		stopCancellationWatch = func() {
			close(cancellationWatchDone)
		}
	}
	defer stopCancellationWatch()

	if err := initializeMCPGatewayLaunchedServer(ctx, launchedServer); err != nil {
		server.markMCPGatewayApprovalExecutionFailed(consumedApproval.ID, consumedApproval.ApprovalManifestSHA256)
		server.dropMCPGatewayLaunchedServer(consumedApproval.ServerID, launchedServer)
		if auditErr := server.logEvent("mcp_gateway.execution_failed", tokenClaims.ControlSessionID, buildMCPGatewayExecutionFailedAuditData(tokenClaims, consumedApproval, launchedServer, controlapipkg.DenialCodeExecutionFailed)); auditErr != nil {
			return controlapipkg.MCPGatewayExecutionResponse{}, controlapipkg.CapabilityResponse{
				RequestID:         consumedApproval.ID,
				Status:            controlapipkg.ResponseStatusError,
				DenialReason:      "control-plane audit is unavailable",
				DenialCode:        controlapipkg.DenialCodeAuditUnavailable,
				ApprovalRequestID: consumedApproval.ID,
				Redacted:          true,
			}, false
		}
		return controlapipkg.MCPGatewayExecutionResponse{}, controlapipkg.CapabilityResponse{
			RequestID:         consumedApproval.ID,
			Status:            controlapipkg.ResponseStatusError,
			DenialReason:      "failed to initialize launched MCP gateway server",
			DenialCode:        controlapipkg.DenialCodeExecutionFailed,
			ApprovalRequestID: consumedApproval.ID,
			Redacted:          true,
		}, false
	}

	toolResult, remoteError, err := performMCPGatewayJSONRPCRoundTrip(ctx, launchedServer, "tools/call", map[string]interface{}{
		"name":      validationResponse.ToolName,
		"arguments": executionRequest.Arguments,
	})
	if err != nil {
		server.markMCPGatewayApprovalExecutionFailed(consumedApproval.ID, consumedApproval.ApprovalManifestSHA256)
		server.dropMCPGatewayLaunchedServer(consumedApproval.ServerID, launchedServer)
		if auditErr := server.logEvent("mcp_gateway.execution_failed", tokenClaims.ControlSessionID, buildMCPGatewayExecutionFailedAuditData(tokenClaims, consumedApproval, launchedServer, controlapipkg.DenialCodeExecutionFailed)); auditErr != nil {
			return controlapipkg.MCPGatewayExecutionResponse{}, controlapipkg.CapabilityResponse{
				RequestID:         consumedApproval.ID,
				Status:            controlapipkg.ResponseStatusError,
				DenialReason:      "control-plane audit is unavailable",
				DenialCode:        controlapipkg.DenialCodeAuditUnavailable,
				ApprovalRequestID: consumedApproval.ID,
				Redacted:          true,
			}, false
		}
		return controlapipkg.MCPGatewayExecutionResponse{}, controlapipkg.CapabilityResponse{
			RequestID:         consumedApproval.ID,
			Status:            controlapipkg.ResponseStatusError,
			DenialReason:      "failed to execute MCP gateway tool call",
			DenialCode:        controlapipkg.DenialCodeExecutionFailed,
			ApprovalRequestID: consumedApproval.ID,
			Redacted:          true,
		}, false
	}

	if err := server.logEvent("mcp_gateway.execution_completed", tokenClaims.ControlSessionID, buildMCPGatewayExecutionCompletedAuditData(tokenClaims, consumedApproval, launchedServer, toolResult, remoteError)); err != nil {
		return controlapipkg.MCPGatewayExecutionResponse{}, controlapipkg.CapabilityResponse{
			RequestID:         consumedApproval.ID,
			Status:            controlapipkg.ResponseStatusError,
			DenialReason:      "control-plane audit is unavailable",
			DenialCode:        controlapipkg.DenialCodeAuditUnavailable,
			ApprovalRequestID: consumedApproval.ID,
			Redacted:          true,
		}, false
	}

	return buildMCPGatewayExecutionResponse(consumedApproval, launchedServer.PID, toolResult, remoteError), controlapipkg.CapabilityResponse{}, true
}
