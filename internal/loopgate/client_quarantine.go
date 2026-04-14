package loopgate

import (
	"context"
	"net/http"
)

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
