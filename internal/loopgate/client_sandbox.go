package loopgate

import (
	"context"
	"net/http"
)

func (client *Client) SandboxImport(ctx context.Context, request SandboxImportRequest) (SandboxOperationResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return SandboxOperationResponse{}, err
	}
	var response SandboxOperationResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/sandbox/import", capabilityToken, request, &response, nil); err != nil {
		return SandboxOperationResponse{}, err
	}
	return response, nil
}

func (client *Client) SandboxStage(ctx context.Context, request SandboxStageRequest) (SandboxOperationResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return SandboxOperationResponse{}, err
	}
	var response SandboxOperationResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/sandbox/stage", capabilityToken, request, &response, nil); err != nil {
		return SandboxOperationResponse{}, err
	}
	return response, nil
}

func (client *Client) SandboxMetadata(ctx context.Context, request SandboxMetadataRequest) (SandboxArtifactMetadataResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return SandboxArtifactMetadataResponse{}, err
	}
	var response SandboxArtifactMetadataResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/sandbox/metadata", capabilityToken, request, &response, nil); err != nil {
		return SandboxArtifactMetadataResponse{}, err
	}
	return response, nil
}

func (client *Client) SandboxList(ctx context.Context, request SandboxListRequest) (SandboxListResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return SandboxListResponse{}, err
	}
	var response SandboxListResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/sandbox/list", capabilityToken, request, &response, nil); err != nil {
		return SandboxListResponse{}, err
	}
	return response, nil
}

func (client *Client) SandboxExport(ctx context.Context, request SandboxExportRequest) (SandboxOperationResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return SandboxOperationResponse{}, err
	}
	var response SandboxOperationResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/sandbox/export", capabilityToken, request, &response, nil); err != nil {
		return SandboxOperationResponse{}, err
	}
	return response, nil
}
