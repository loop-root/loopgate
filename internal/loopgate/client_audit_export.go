package loopgate

import (
	"context"
	"net/http"
)

func (client *Client) FlushAuditExport(ctx context.Context) (AuditExportFlushResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return AuditExportFlushResponse{}, err
	}
	var response AuditExportFlushResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/audit/export/flush", capabilityToken, nil, &response, nil); err != nil {
		return AuditExportFlushResponse{}, err
	}
	return response, nil
}

func (client *Client) CheckAuditExportTrust(ctx context.Context) (AuditExportTrustCheckResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return AuditExportTrustCheckResponse{}, err
	}
	var response AuditExportTrustCheckResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/audit/export/trust-check", capabilityToken, nil, &response, nil); err != nil {
		return AuditExportTrustCheckResponse{}, err
	}
	return response, nil
}
