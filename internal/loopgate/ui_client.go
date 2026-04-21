package loopgate

import (
	"context"
	"fmt"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"strings"
)

func (client *Client) UIStatus(ctx context.Context) (controlapipkg.UIStatusResponse, error) {
	token, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.UIStatusResponse{}, err
	}

	var response controlapipkg.UIStatusResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/ui/status", token, nil, &response, nil); err != nil {
		return controlapipkg.UIStatusResponse{}, err
	}
	return response, nil
}

func (client *Client) UIRecentEvents(ctx context.Context, lastEventID string) (controlapipkg.UIRecentEventsResponse, error) {
	token, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.UIRecentEventsResponse{}, err
	}

	path := "/v1/ui/events/recent"
	if trimmedLastEventID := strings.TrimSpace(lastEventID); trimmedLastEventID != "" {
		path += "?last_event_id=" + trimmedLastEventID
	}

	var response controlapipkg.UIRecentEventsResponse
	if err := client.doJSON(ctx, http.MethodGet, path, token, nil, &response, nil); err != nil {
		return controlapipkg.UIRecentEventsResponse{}, err
	}
	return response, nil
}

func (client *Client) UpdateUIOperatorMountWriteGrant(ctx context.Context, request controlapipkg.UIOperatorMountWriteGrantUpdateRequest) (controlapipkg.UIOperatorMountWriteGrantStatusResponse, error) {
	token, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.UIOperatorMountWriteGrantStatusResponse{}, err
	}

	var response controlapipkg.UIOperatorMountWriteGrantStatusResponse
	if err := client.doJSON(ctx, http.MethodPut, "/v1/ui/operator-mount-write-grants", token, request, &response, nil); err != nil {
		return controlapipkg.UIOperatorMountWriteGrantStatusResponse{}, err
	}
	return response, nil
}

func (client *Client) UIApprovals(ctx context.Context) (controlapipkg.UIApprovalsResponse, error) {
	approvalToken, err := client.ensureApprovalToken(ctx)
	if err != nil {
		return controlapipkg.UIApprovalsResponse{}, err
	}

	var response controlapipkg.UIApprovalsResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/ui/approvals", "", nil, &response, map[string]string{
		"X-Loopgate-Approval-Token": approvalToken,
	}); err != nil {
		return controlapipkg.UIApprovalsResponse{}, err
	}
	return response, nil
}

func (client *Client) UIDecideApproval(ctx context.Context, approvalRequestID string, approved bool) (controlapipkg.CapabilityResponse, error) {
	return client.UIDecideApprovalWithReason(ctx, approvalRequestID, approved, "")
}

func (client *Client) UIDecideApprovalWithReason(ctx context.Context, approvalRequestID string, approved bool, reason string) (controlapipkg.CapabilityResponse, error) {
	approvalToken, err := client.ensureApprovalToken(ctx)
	if err != nil {
		return controlapipkg.CapabilityResponse{}, err
	}

	var response controlapipkg.CapabilityResponse
	path := fmt.Sprintf("/v1/ui/approvals/%s/decision", approvalRequestID)
	if err := client.doCapabilityJSON(ctx, client.defaultRequestTimeout, http.MethodPost, path, "", controlapipkg.UIApprovalDecisionRequest{
		Approved: &approved,
		Reason:   strings.TrimSpace(reason),
	}, &response, map[string]string{
		"X-Loopgate-Approval-Token": approvalToken,
	}); err != nil {
		return controlapipkg.CapabilityResponse{}, err
	}
	client.mu.Lock()
	delete(client.approvalDecisionNonce, approvalRequestID)
	client.mu.Unlock()
	return response, nil
}

func (client *Client) SharedFolderStatus(ctx context.Context) (controlapipkg.SharedFolderStatusResponse, error) {
	token, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.SharedFolderStatusResponse{}, err
	}

	var response controlapipkg.SharedFolderStatusResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/ui/shared-folder", token, nil, &response, nil); err != nil {
		return controlapipkg.SharedFolderStatusResponse{}, err
	}
	return response, nil
}

func (client *Client) SyncSharedFolder(ctx context.Context) (controlapipkg.SharedFolderStatusResponse, error) {
	token, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.SharedFolderStatusResponse{}, err
	}

	var response controlapipkg.SharedFolderStatusResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/ui/shared-folder/sync", token, struct{}{}, &response, nil); err != nil {
		return controlapipkg.SharedFolderStatusResponse{}, err
	}
	return response, nil
}

func (client *Client) FolderAccessStatus(ctx context.Context) (controlapipkg.FolderAccessStatusResponse, error) {
	token, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.FolderAccessStatusResponse{}, err
	}

	var response controlapipkg.FolderAccessStatusResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/ui/folder-access", token, nil, &response, nil); err != nil {
		return controlapipkg.FolderAccessStatusResponse{}, err
	}
	return response, nil
}

func (client *Client) SyncFolderAccess(ctx context.Context) (controlapipkg.FolderAccessSyncResponse, error) {
	token, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.FolderAccessSyncResponse{}, err
	}

	var response controlapipkg.FolderAccessSyncResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/ui/folder-access/sync", token, struct{}{}, &response, nil); err != nil {
		return controlapipkg.FolderAccessSyncResponse{}, err
	}
	return response, nil
}

func (client *Client) UpdateFolderAccess(ctx context.Context, request controlapipkg.FolderAccessUpdateRequest) (controlapipkg.FolderAccessStatusResponse, error) {
	token, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.FolderAccessStatusResponse{}, err
	}

	var response controlapipkg.FolderAccessStatusResponse
	if err := client.doJSON(ctx, http.MethodPut, "/v1/ui/folder-access", token, request, &response, nil); err != nil {
		return controlapipkg.FolderAccessStatusResponse{}, err
	}
	return response, nil
}
