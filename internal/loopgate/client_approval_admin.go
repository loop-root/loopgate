package loopgate

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

func (client *Client) ListPendingApprovals(ctx context.Context) (OperatorApprovalsResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return OperatorApprovalsResponse{}, err
	}

	var response OperatorApprovalsResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/control/approvals", capabilityToken, nil, &response, nil); err != nil {
		return OperatorApprovalsResponse{}, err
	}
	return response, nil
}

func (client *Client) DecidePendingApproval(ctx context.Context, approvalRequestID string, approved bool, reason string) (OperatorApprovalDecisionResponse, error) {
	approvalRequestID = strings.TrimSpace(approvalRequestID)
	if approvalRequestID == "" {
		return OperatorApprovalDecisionResponse{}, fmt.Errorf("approval request id must not be blank")
	}

	approvalsResponse, err := client.ListPendingApprovals(ctx)
	if err != nil {
		return OperatorApprovalDecisionResponse{}, err
	}

	var approvalSummary OperatorApprovalSummary
	found := false
	for _, candidateApproval := range approvalsResponse.Approvals {
		if candidateApproval.ApprovalRequestID == approvalRequestID {
			approvalSummary = candidateApproval
			found = true
			break
		}
	}
	if !found {
		return OperatorApprovalDecisionResponse{}, fmt.Errorf("pending approval %q not found", approvalRequestID)
	}

	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return OperatorApprovalDecisionResponse{}, err
	}

	var response OperatorApprovalDecisionResponse
	path := fmt.Sprintf("/v1/control/approvals/%s/decision", approvalRequestID)
	if err := client.doJSONWithTimeout(ctx, client.defaultRequestTimeout, http.MethodPost, path, capabilityToken, ApprovalDecisionRequest{
		Approved:               approved,
		Reason:                 strings.TrimSpace(reason),
		DecisionNonce:          approvalSummary.DecisionNonce,
		ApprovalManifestSHA256: approvalSummary.ApprovalManifestSHA256,
	}, &response, nil); err != nil {
		return OperatorApprovalDecisionResponse{}, err
	}
	return response, nil
}
