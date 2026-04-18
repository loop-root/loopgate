package loopgate

import (
	"context"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
)

func (client *Client) SandboxImport(ctx context.Context, request controlapipkg.SandboxImportRequest) (controlapipkg.SandboxOperationResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.SandboxOperationResponse{}, err
	}
	var response controlapipkg.SandboxOperationResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/sandbox/import", capabilityToken, request, &response, nil); err != nil {
		return controlapipkg.SandboxOperationResponse{}, err
	}
	return response, nil
}

func (client *Client) SandboxStage(ctx context.Context, request controlapipkg.SandboxStageRequest) (controlapipkg.SandboxOperationResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.SandboxOperationResponse{}, err
	}
	var response controlapipkg.SandboxOperationResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/sandbox/stage", capabilityToken, request, &response, nil); err != nil {
		return controlapipkg.SandboxOperationResponse{}, err
	}
	return response, nil
}

func (client *Client) SandboxMetadata(ctx context.Context, request controlapipkg.SandboxMetadataRequest) (controlapipkg.SandboxArtifactMetadataResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.SandboxArtifactMetadataResponse{}, err
	}
	var response controlapipkg.SandboxArtifactMetadataResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/sandbox/metadata", capabilityToken, request, &response, nil); err != nil {
		return controlapipkg.SandboxArtifactMetadataResponse{}, err
	}
	return response, nil
}

func (client *Client) SandboxList(ctx context.Context, request controlapipkg.SandboxListRequest) (controlapipkg.SandboxListResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.SandboxListResponse{}, err
	}
	var response controlapipkg.SandboxListResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/sandbox/list", capabilityToken, request, &response, nil); err != nil {
		return controlapipkg.SandboxListResponse{}, err
	}
	return response, nil
}

func (client *Client) SandboxExport(ctx context.Context, request controlapipkg.SandboxExportRequest) (controlapipkg.SandboxOperationResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.SandboxOperationResponse{}, err
	}
	var response controlapipkg.SandboxOperationResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/sandbox/export", capabilityToken, request, &response, nil); err != nil {
		return controlapipkg.SandboxOperationResponse{}, err
	}
	return response, nil
}
