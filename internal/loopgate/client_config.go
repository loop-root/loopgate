package loopgate

import (
	"context"
	"net/http"

	"morph/internal/config"
)

func (client *Client) LoadPolicyConfig(ctx context.Context) (config.Policy, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return config.Policy{}, err
	}

	var response config.Policy
	if err := client.doJSON(ctx, http.MethodGet, "/v1/config/policy", capabilityToken, nil, &response, nil); err != nil {
		return config.Policy{}, err
	}
	return response, nil
}

func (client *Client) ReloadPolicyFromDisk(ctx context.Context) (ConfigPolicyReloadResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return ConfigPolicyReloadResponse{}, err
	}

	var response ConfigPolicyReloadResponse
	if err := client.doJSON(ctx, http.MethodPut, "/v1/config/policy", capabilityToken, nil, &response, nil); err != nil {
		return ConfigPolicyReloadResponse{}, err
	}
	return response, nil
}
