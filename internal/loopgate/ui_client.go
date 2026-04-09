package loopgate

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// LoadHavenMemoryInventory returns the UI-safe memory inventory projection.
// The method name is retained for compatibility with existing local clients.
func (client *Client) LoadHavenMemoryInventory(ctx context.Context) (HavenMemoryInventoryResponse, error) {
	token, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return HavenMemoryInventoryResponse{}, err
	}

	var response HavenMemoryInventoryResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/ui/memory", token, nil, &response, nil); err != nil {
		return HavenMemoryInventoryResponse{}, err
	}
	return response, nil
}

// ResetHavenMemory performs the auditable fresh-start memory reset path.
// The method name is retained for compatibility with existing local clients.
func (client *Client) ResetHavenMemory(ctx context.Context, request HavenMemoryResetRequest) (HavenMemoryResetResponse, error) {
	token, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return HavenMemoryResetResponse{}, err
	}

	var response HavenMemoryResetResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/ui/memory/reset", token, request, &response, nil); err != nil {
		return HavenMemoryResetResponse{}, err
	}
	return response, nil
}

func (client *Client) UIStatus(ctx context.Context) (UIStatusResponse, error) {
	token, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return UIStatusResponse{}, err
	}

	var response UIStatusResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/ui/status", token, nil, &response, nil); err != nil {
		return UIStatusResponse{}, err
	}
	return response, nil
}

func (client *Client) UpdateUIOperatorMountWriteGrant(ctx context.Context, request UIOperatorMountWriteGrantUpdateRequest) (UIOperatorMountWriteGrantStatusResponse, error) {
	token, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return UIOperatorMountWriteGrantStatusResponse{}, err
	}

	var response UIOperatorMountWriteGrantStatusResponse
	if err := client.doJSON(ctx, http.MethodPut, "/v1/ui/operator-mount-write-grants", token, request, &response, nil); err != nil {
		return UIOperatorMountWriteGrantStatusResponse{}, err
	}
	return response, nil
}

func (client *Client) UIApprovals(ctx context.Context) (UIApprovalsResponse, error) {
	approvalToken, err := client.ensureApprovalToken(ctx)
	if err != nil {
		return UIApprovalsResponse{}, err
	}

	var response UIApprovalsResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/ui/approvals", "", nil, &response, map[string]string{
		"X-Loopgate-Approval-Token": approvalToken,
	}); err != nil {
		return UIApprovalsResponse{}, err
	}
	return response, nil
}

func (client *Client) UIDecideApproval(ctx context.Context, approvalRequestID string, approved bool) (CapabilityResponse, error) {
	approvalToken, err := client.ensureApprovalToken(ctx)
	if err != nil {
		return CapabilityResponse{}, err
	}

	var response CapabilityResponse
	path := fmt.Sprintf("/v1/ui/approvals/%s/decision", approvalRequestID)
	if err := client.doCapabilityJSON(ctx, client.defaultRequestTimeout, http.MethodPost, path, "", UIApprovalDecisionRequest{
		Approved: &approved,
	}, &response, map[string]string{
		"X-Loopgate-Approval-Token": approvalToken,
	}); err != nil {
		return CapabilityResponse{}, err
	}
	client.mu.Lock()
	delete(client.approvalDecisionNonce, approvalRequestID)
	client.mu.Unlock()
	return response, nil
}

func (client *Client) SharedFolderStatus(ctx context.Context) (SharedFolderStatusResponse, error) {
	token, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return SharedFolderStatusResponse{}, err
	}

	var response SharedFolderStatusResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/ui/shared-folder", token, nil, &response, nil); err != nil {
		return SharedFolderStatusResponse{}, err
	}
	return response, nil
}

func (client *Client) SyncSharedFolder(ctx context.Context) (SharedFolderStatusResponse, error) {
	token, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return SharedFolderStatusResponse{}, err
	}

	var response SharedFolderStatusResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/ui/shared-folder/sync", token, struct{}{}, &response, nil); err != nil {
		return SharedFolderStatusResponse{}, err
	}
	return response, nil
}

func (client *Client) FolderAccessStatus(ctx context.Context) (FolderAccessStatusResponse, error) {
	token, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return FolderAccessStatusResponse{}, err
	}

	var response FolderAccessStatusResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/ui/folder-access", token, nil, &response, nil); err != nil {
		return FolderAccessStatusResponse{}, err
	}
	return response, nil
}

func (client *Client) SyncFolderAccess(ctx context.Context) (FolderAccessSyncResponse, error) {
	token, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return FolderAccessSyncResponse{}, err
	}

	var response FolderAccessSyncResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/ui/folder-access/sync", token, struct{}{}, &response, nil); err != nil {
		return FolderAccessSyncResponse{}, err
	}
	return response, nil
}

func (client *Client) UpdateFolderAccess(ctx context.Context, request FolderAccessUpdateRequest) (FolderAccessStatusResponse, error) {
	token, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return FolderAccessStatusResponse{}, err
	}

	var response FolderAccessStatusResponse
	if err := client.doJSON(ctx, http.MethodPut, "/v1/ui/folder-access", token, request, &response, nil); err != nil {
		return FolderAccessStatusResponse{}, err
	}
	return response, nil
}

func (client *Client) TaskStandingGrantStatus(ctx context.Context) (TaskStandingGrantStatusResponse, error) {
	token, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return TaskStandingGrantStatusResponse{}, err
	}

	var response TaskStandingGrantStatusResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/ui/task-standing-grants", token, nil, &response, nil); err != nil {
		return TaskStandingGrantStatusResponse{}, err
	}
	return response, nil
}

func (client *Client) UpdateTaskStandingGrant(ctx context.Context, request TaskStandingGrantUpdateRequest) (TaskStandingGrantStatusResponse, error) {
	token, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return TaskStandingGrantStatusResponse{}, err
	}

	var response TaskStandingGrantStatusResponse
	if err := client.doJSON(ctx, http.MethodPut, "/v1/ui/task-standing-grants", token, request, &response, nil); err != nil {
		return TaskStandingGrantStatusResponse{}, err
	}
	return response, nil
}

// HavenAgentWorkItemEnsure creates or dedupes a Task Board item via the signed
// agent work-item route. The method name is retained for compatibility.
func (client *Client) HavenAgentWorkItemEnsure(ctx context.Context, request HavenAgentWorkEnsureRequest) (HavenAgentWorkItemResponse, error) {
	token, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return HavenAgentWorkItemResponse{}, err
	}
	var response HavenAgentWorkItemResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/agent/work-item/ensure", token, request, &response, nil); err != nil {
		return HavenAgentWorkItemResponse{}, err
	}
	return response, nil
}

// HavenAgentWorkItemComplete marks a Task Board item done via the signed
// agent work-item route. The method name is retained for compatibility.
func (client *Client) HavenAgentWorkItemComplete(ctx context.Context, itemID string, reason string) (HavenAgentWorkItemResponse, error) {
	token, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return HavenAgentWorkItemResponse{}, err
	}
	body := HavenAgentWorkCompleteRequest{ItemID: strings.TrimSpace(itemID), Reason: strings.TrimSpace(reason)}
	var response HavenAgentWorkItemResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/agent/work-item/complete", token, body, &response, nil); err != nil {
		return HavenAgentWorkItemResponse{}, err
	}
	return response, nil
}
