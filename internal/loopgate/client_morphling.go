package loopgate

import (
	"context"
	"net/http"
	"strings"
)

func (client *Client) SpawnMorphling(ctx context.Context, request MorphlingSpawnRequest) (MorphlingSpawnResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MorphlingSpawnResponse{}, err
	}
	var response MorphlingSpawnResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/morphlings/spawn", capabilityToken, request, &response, nil); err != nil {
		return MorphlingSpawnResponse{}, err
	}
	// Pending spawn approvals use the same manifest + nonce binding as capability approvals;
	// cache them so DecideApproval (HTTP) can submit /v1/approvals/.../decision.
	if response.Status == ResponseStatusPendingApproval && strings.TrimSpace(response.ApprovalID) != "" {
		client.mu.Lock()
		if strings.TrimSpace(response.ApprovalManifestSHA256) != "" {
			client.approvalManifestSHA256[response.ApprovalID] = strings.TrimSpace(response.ApprovalManifestSHA256)
		}
		if strings.TrimSpace(response.ApprovalDecisionNonce) != "" {
			client.approvalDecisionNonce[response.ApprovalID] = strings.TrimSpace(response.ApprovalDecisionNonce)
		}
		client.mu.Unlock()
	}
	return response, nil
}

func (client *Client) MorphlingStatus(ctx context.Context, request MorphlingStatusRequest) (MorphlingStatusResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MorphlingStatusResponse{}, err
	}
	var response MorphlingStatusResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/morphlings/status", capabilityToken, request, &response, nil); err != nil {
		return MorphlingStatusResponse{}, err
	}
	return response, nil
}

func (client *Client) TerminateMorphling(ctx context.Context, request MorphlingTerminateRequest) (MorphlingTerminateResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MorphlingTerminateResponse{}, err
	}
	var response MorphlingTerminateResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/morphlings/terminate", capabilityToken, request, &response, nil); err != nil {
		return MorphlingTerminateResponse{}, err
	}
	return response, nil
}

func (client *Client) LaunchMorphlingWorker(ctx context.Context, request MorphlingWorkerLaunchRequest) (MorphlingWorkerLaunchResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MorphlingWorkerLaunchResponse{}, err
	}
	var response MorphlingWorkerLaunchResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/morphlings/worker/launch", capabilityToken, request, &response, nil); err != nil {
		return MorphlingWorkerLaunchResponse{}, err
	}
	return response, nil
}

func (client *Client) ReviewMorphling(ctx context.Context, request MorphlingReviewRequest) (MorphlingReviewResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MorphlingReviewResponse{}, err
	}
	var response MorphlingReviewResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/morphlings/review", capabilityToken, request, &response, nil); err != nil {
		return MorphlingReviewResponse{}, err
	}
	return response, nil
}

func (client *Client) QuarantineMetadata(ctx context.Context, quarantineRef string) (QuarantineMetadataResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return QuarantineMetadataResponse{}, err
	}
	var response QuarantineMetadataResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/quarantine/metadata", capabilityToken, QuarantineLookupRequest{
		QuarantineRef: quarantineRef,
	}, &response, nil); err != nil {
		return QuarantineMetadataResponse{}, err
	}
	return response, nil
}

func (client *Client) ViewQuarantinedPayload(ctx context.Context, quarantineRef string) (QuarantineViewResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return QuarantineViewResponse{}, err
	}
	var response QuarantineViewResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/quarantine/view", capabilityToken, QuarantineLookupRequest{
		QuarantineRef: quarantineRef,
	}, &response, nil); err != nil {
		return QuarantineViewResponse{}, err
	}
	return response, nil
}

func (client *Client) PruneQuarantinedPayload(ctx context.Context, quarantineRef string) (QuarantineMetadataResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return QuarantineMetadataResponse{}, err
	}
	var response QuarantineMetadataResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/quarantine/prune", capabilityToken, QuarantineLookupRequest{
		QuarantineRef: quarantineRef,
	}, &response, nil); err != nil {
		return QuarantineMetadataResponse{}, err
	}
	return response, nil
}
