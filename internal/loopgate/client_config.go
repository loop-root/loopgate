package loopgate

import (
	"context"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"

	"loopgate/internal/config"
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

func (client *Client) ReloadPolicyFromDisk(ctx context.Context) (controlapipkg.ConfigPolicyReloadResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.ConfigPolicyReloadResponse{}, err
	}

	var response controlapipkg.ConfigPolicyReloadResponse
	if err := client.doJSON(ctx, http.MethodPut, "/v1/config/policy", capabilityToken, nil, &response, nil); err != nil {
		return controlapipkg.ConfigPolicyReloadResponse{}, err
	}
	return response, nil
}

func (client *Client) LoadOperatorOverrideConfig(ctx context.Context) (config.OperatorOverrideDocument, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return config.OperatorOverrideDocument{}, err
	}

	var response config.OperatorOverrideDocument
	if err := client.doJSON(ctx, http.MethodGet, "/v1/config/operator-overrides", capabilityToken, nil, &response, nil); err != nil {
		return config.OperatorOverrideDocument{}, err
	}
	return response, nil
}

func (client *Client) ReloadOperatorOverridesFromDisk(ctx context.Context) (controlapipkg.ConfigOperatorOverrideReloadResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.ConfigOperatorOverrideReloadResponse{}, err
	}

	var response controlapipkg.ConfigOperatorOverrideReloadResponse
	if err := client.doJSON(ctx, http.MethodPut, "/v1/config/operator-overrides", capabilityToken, nil, &response, nil); err != nil {
		return controlapipkg.ConfigOperatorOverrideReloadResponse{}, err
	}
	return response, nil
}
