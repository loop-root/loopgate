package loopgate

import (
	"context"
	"fmt"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"strings"
)

func (client *Client) ListPendingApprovals(ctx context.Context) (controlapipkg.OperatorApprovalsResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.OperatorApprovalsResponse{}, err
	}

	var response controlapipkg.OperatorApprovalsResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/control/approvals", capabilityToken, nil, &response, nil); err != nil {
		return controlapipkg.OperatorApprovalsResponse{}, err
	}
	return response, nil
}

func (client *Client) DecidePendingApproval(ctx context.Context, approvalRequestID string, approved bool, reason string) (controlapipkg.OperatorApprovalDecisionResponse, error) {
	approvalRequestID = strings.TrimSpace(approvalRequestID)
	if approvalRequestID == "" {
		return controlapipkg.OperatorApprovalDecisionResponse{}, fmt.Errorf("approval request id must not be blank")
	}

	approvalsResponse, err := client.ListPendingApprovals(ctx)
	if err != nil {
		return controlapipkg.OperatorApprovalDecisionResponse{}, err
	}

	var approvalSummary controlapipkg.OperatorApprovalSummary
	found := false
	for _, candidateApproval := range approvalsResponse.Approvals {
		if candidateApproval.ApprovalRequestID == approvalRequestID {
			approvalSummary = candidateApproval
			found = true
			break
		}
	}
	if !found {
		return controlapipkg.OperatorApprovalDecisionResponse{}, fmt.Errorf("pending approval %q not found", approvalRequestID)
	}

	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.OperatorApprovalDecisionResponse{}, err
	}

	var response controlapipkg.OperatorApprovalDecisionResponse
	path := fmt.Sprintf("/v1/control/approvals/%s/decision", approvalRequestID)
	if err := client.doJSONWithTimeout(ctx, client.defaultRequestTimeout, http.MethodPost, path, capabilityToken, controlapipkg.ApprovalDecisionRequest{
		Approved:               approved,
		Reason:                 strings.TrimSpace(reason),
		DecisionNonce:          approvalSummary.DecisionNonce,
		ApprovalManifestSHA256: approvalSummary.ApprovalManifestSHA256,
	}, &response, nil); err != nil {
		return controlapipkg.OperatorApprovalDecisionResponse{}, err
	}
	return response, nil
}
