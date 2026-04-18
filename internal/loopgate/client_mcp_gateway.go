package loopgate

import (
	"context"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
)

func (client *Client) LoadMCPGatewayInventory(ctx context.Context) (controlapipkg.MCPGatewayInventoryResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.MCPGatewayInventoryResponse{}, err
	}
	var response controlapipkg.MCPGatewayInventoryResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/mcp-gateway/inventory", capabilityToken, nil, &response, nil); err != nil {
		return controlapipkg.MCPGatewayInventoryResponse{}, err
	}
	return response, nil
}

func (client *Client) LoadMCPGatewayServerStatus(ctx context.Context) (controlapipkg.MCPGatewayServerStatusResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.MCPGatewayServerStatusResponse{}, err
	}
	var response controlapipkg.MCPGatewayServerStatusResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/mcp-gateway/server/status", capabilityToken, nil, &response, nil); err != nil {
		return controlapipkg.MCPGatewayServerStatusResponse{}, err
	}
	return response, nil
}

func (client *Client) CheckMCPGatewayDecision(ctx context.Context, request controlapipkg.MCPGatewayDecisionRequest) (controlapipkg.MCPGatewayDecisionResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.MCPGatewayDecisionResponse{}, err
	}
	var response controlapipkg.MCPGatewayDecisionResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/mcp-gateway/decision", capabilityToken, request, &response, nil); err != nil {
		return controlapipkg.MCPGatewayDecisionResponse{}, err
	}
	return response, nil
}

func (client *Client) EnsureMCPGatewayServerLaunched(ctx context.Context, request controlapipkg.MCPGatewayEnsureLaunchRequest) (controlapipkg.MCPGatewayEnsureLaunchResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.MCPGatewayEnsureLaunchResponse{}, err
	}
	var response controlapipkg.MCPGatewayEnsureLaunchResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/mcp-gateway/server/ensure-launched", capabilityToken, request, &response, nil); err != nil {
		return controlapipkg.MCPGatewayEnsureLaunchResponse{}, err
	}
	return response, nil
}

func (client *Client) StopMCPGatewayServer(ctx context.Context, request controlapipkg.MCPGatewayStopRequest) (controlapipkg.MCPGatewayStopResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.MCPGatewayStopResponse{}, err
	}
	var response controlapipkg.MCPGatewayStopResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/mcp-gateway/server/stop", capabilityToken, request, &response, nil); err != nil {
		return controlapipkg.MCPGatewayStopResponse{}, err
	}
	return response, nil
}

func (client *Client) ValidateMCPGatewayInvocation(ctx context.Context, request controlapipkg.MCPGatewayInvocationRequest) (controlapipkg.MCPGatewayInvocationValidationResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.MCPGatewayInvocationValidationResponse{}, err
	}
	var response controlapipkg.MCPGatewayInvocationValidationResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/mcp-gateway/invocation/validate", capabilityToken, request, &response, nil); err != nil {
		return controlapipkg.MCPGatewayInvocationValidationResponse{}, err
	}
	return response, nil
}

func (client *Client) RequestMCPGatewayInvocationApproval(ctx context.Context, request controlapipkg.MCPGatewayInvocationRequest) (controlapipkg.MCPGatewayInvocationApprovalResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.MCPGatewayInvocationApprovalResponse{}, err
	}
	var response controlapipkg.MCPGatewayInvocationApprovalResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/mcp-gateway/invocation/request-approval", capabilityToken, request, &response, nil); err != nil {
		return controlapipkg.MCPGatewayInvocationApprovalResponse{}, err
	}
	return response, nil
}

func (client *Client) DecideMCPGatewayInvocationApproval(ctx context.Context, request controlapipkg.MCPGatewayApprovalDecisionRequest) (controlapipkg.MCPGatewayApprovalDecisionResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.MCPGatewayApprovalDecisionResponse{}, err
	}
	var response controlapipkg.MCPGatewayApprovalDecisionResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/mcp-gateway/invocation/decide-approval", capabilityToken, request, &response, nil); err != nil {
		return controlapipkg.MCPGatewayApprovalDecisionResponse{}, err
	}
	return response, nil
}

func (client *Client) ValidateMCPGatewayExecution(ctx context.Context, request controlapipkg.MCPGatewayExecutionRequest) (controlapipkg.MCPGatewayExecutionValidationResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.MCPGatewayExecutionValidationResponse{}, err
	}
	var response controlapipkg.MCPGatewayExecutionValidationResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/mcp-gateway/invocation/validate-execution", capabilityToken, request, &response, nil); err != nil {
		return controlapipkg.MCPGatewayExecutionValidationResponse{}, err
	}
	return response, nil
}

func (client *Client) ExecuteMCPGatewayInvocation(ctx context.Context, request controlapipkg.MCPGatewayExecutionRequest) (controlapipkg.MCPGatewayExecutionResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.MCPGatewayExecutionResponse{}, err
	}
	var response controlapipkg.MCPGatewayExecutionResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/mcp-gateway/invocation/execute", capabilityToken, request, &response, nil); err != nil {
		return controlapipkg.MCPGatewayExecutionResponse{}, err
	}
	return response, nil
}
