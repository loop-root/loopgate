package loopgate

import (
	"context"
	"net/http"
)

func (client *Client) LoadMCPGatewayInventory(ctx context.Context) (MCPGatewayInventoryResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MCPGatewayInventoryResponse{}, err
	}
	var response MCPGatewayInventoryResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/mcp-gateway/inventory", capabilityToken, nil, &response, nil); err != nil {
		return MCPGatewayInventoryResponse{}, err
	}
	return response, nil
}

func (client *Client) LoadMCPGatewayServerStatus(ctx context.Context) (MCPGatewayServerStatusResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MCPGatewayServerStatusResponse{}, err
	}
	var response MCPGatewayServerStatusResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/mcp-gateway/server/status", capabilityToken, nil, &response, nil); err != nil {
		return MCPGatewayServerStatusResponse{}, err
	}
	return response, nil
}

func (client *Client) CheckMCPGatewayDecision(ctx context.Context, request MCPGatewayDecisionRequest) (MCPGatewayDecisionResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MCPGatewayDecisionResponse{}, err
	}
	var response MCPGatewayDecisionResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/mcp-gateway/decision", capabilityToken, request, &response, nil); err != nil {
		return MCPGatewayDecisionResponse{}, err
	}
	return response, nil
}

func (client *Client) EnsureMCPGatewayServerLaunched(ctx context.Context, request MCPGatewayEnsureLaunchRequest) (MCPGatewayEnsureLaunchResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MCPGatewayEnsureLaunchResponse{}, err
	}
	var response MCPGatewayEnsureLaunchResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/mcp-gateway/server/ensure-launched", capabilityToken, request, &response, nil); err != nil {
		return MCPGatewayEnsureLaunchResponse{}, err
	}
	return response, nil
}

func (client *Client) StopMCPGatewayServer(ctx context.Context, request MCPGatewayStopRequest) (MCPGatewayStopResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MCPGatewayStopResponse{}, err
	}
	var response MCPGatewayStopResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/mcp-gateway/server/stop", capabilityToken, request, &response, nil); err != nil {
		return MCPGatewayStopResponse{}, err
	}
	return response, nil
}

func (client *Client) ValidateMCPGatewayInvocation(ctx context.Context, request MCPGatewayInvocationRequest) (MCPGatewayInvocationValidationResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MCPGatewayInvocationValidationResponse{}, err
	}
	var response MCPGatewayInvocationValidationResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/mcp-gateway/invocation/validate", capabilityToken, request, &response, nil); err != nil {
		return MCPGatewayInvocationValidationResponse{}, err
	}
	return response, nil
}

func (client *Client) RequestMCPGatewayInvocationApproval(ctx context.Context, request MCPGatewayInvocationRequest) (MCPGatewayInvocationApprovalResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MCPGatewayInvocationApprovalResponse{}, err
	}
	var response MCPGatewayInvocationApprovalResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/mcp-gateway/invocation/request-approval", capabilityToken, request, &response, nil); err != nil {
		return MCPGatewayInvocationApprovalResponse{}, err
	}
	return response, nil
}

func (client *Client) DecideMCPGatewayInvocationApproval(ctx context.Context, request MCPGatewayApprovalDecisionRequest) (MCPGatewayApprovalDecisionResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MCPGatewayApprovalDecisionResponse{}, err
	}
	var response MCPGatewayApprovalDecisionResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/mcp-gateway/invocation/decide-approval", capabilityToken, request, &response, nil); err != nil {
		return MCPGatewayApprovalDecisionResponse{}, err
	}
	return response, nil
}

func (client *Client) ValidateMCPGatewayExecution(ctx context.Context, request MCPGatewayExecutionRequest) (MCPGatewayExecutionValidationResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MCPGatewayExecutionValidationResponse{}, err
	}
	var response MCPGatewayExecutionValidationResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/mcp-gateway/invocation/validate-execution", capabilityToken, request, &response, nil); err != nil {
		return MCPGatewayExecutionValidationResponse{}, err
	}
	return response, nil
}

func (client *Client) ExecuteMCPGatewayInvocation(ctx context.Context, request MCPGatewayExecutionRequest) (MCPGatewayExecutionResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MCPGatewayExecutionResponse{}, err
	}
	var response MCPGatewayExecutionResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/mcp-gateway/invocation/execute", capabilityToken, request, &response, nil); err != nil {
		return MCPGatewayExecutionResponse{}, err
	}
	return response, nil
}
