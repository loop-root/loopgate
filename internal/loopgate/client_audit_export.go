package loopgate

import (
	"context"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
)

func (client *Client) FlushAuditExport(ctx context.Context) (controlapipkg.AuditExportFlushResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.AuditExportFlushResponse{}, err
	}
	var response controlapipkg.AuditExportFlushResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/audit/export/flush", capabilityToken, nil, &response, nil); err != nil {
		return controlapipkg.AuditExportFlushResponse{}, err
	}
	return response, nil
}

func (client *Client) CheckAuditExportTrust(ctx context.Context) (controlapipkg.AuditExportTrustCheckResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.AuditExportTrustCheckResponse{}, err
	}
	var response controlapipkg.AuditExportTrustCheckResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/audit/export/trust-check", capabilityToken, nil, &response, nil); err != nil {
		return controlapipkg.AuditExportTrustCheckResponse{}, err
	}
	return response, nil
}
