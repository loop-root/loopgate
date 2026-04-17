package approval

import (
	"testing"

	protocolpkg "loopgate/internal/loopgate/protocol"
)

func TestValidateDecisionRequest_AllowsOwnerBoundPendingApproval(t *testing.T) {
	pendingApproval := PendingApproval{
		ControlSessionID:       "control-session-1",
		State:                  StatePending,
		DecisionNonce:          "nonce-1",
		ApprovalManifestSHA256: "manifest-1",
		ExecutionContext: ExecutionContext{
			TenantID: "tenant-1",
		},
	}

	validationError := ValidateDecisionRequest(
		pendingApproval,
		protocolpkg.ApprovalDecisionRequest{
			Approved:               true,
			DecisionNonce:          "nonce-1",
			ApprovalManifestSHA256: "manifest-1",
		},
		DecisionActor{ControlSessionID: "control-session-1"},
	)
	if validationError != nil {
		t.Fatalf("expected approval decision to validate, got %v", validationError)
	}
}

func TestValidateDecisionRequest_DeniesExpectedFailures(t *testing.T) {
	baseApproval := PendingApproval{
		ControlSessionID:       "control-session-1",
		State:                  StatePending,
		DecisionNonce:          "nonce-1",
		ApprovalManifestSHA256: "manifest-1",
		ExecutionContext: ExecutionContext{
			TenantID: "tenant-1",
		},
	}
	baseDecision := protocolpkg.ApprovalDecisionRequest{
		Approved:               true,
		DecisionNonce:          "nonce-1",
		ApprovalManifestSHA256: "manifest-1",
	}

	testCases := []struct {
		name            string
		pendingApproval PendingApproval
		decisionRequest protocolpkg.ApprovalDecisionRequest
		decisionActor   DecisionActor
		expectedCode    DecisionValidationCode
		expectedReason  string
	}{
		{
			name: "owner mismatch wins before state check",
			pendingApproval: func() PendingApproval {
				approval := baseApproval
				approval.State = StateConsumed
				return approval
			}(),
			decisionRequest: baseDecision,
			decisionActor:   DecisionActor{ControlSessionID: "other-session"},
			expectedCode:    DecisionValidationOwnerMismatch,
			expectedReason:  "approval token does not match approval owner",
		},
		{
			name:            "operator tenant mismatch is not found",
			pendingApproval: baseApproval,
			decisionRequest: baseDecision,
			decisionActor: DecisionActor{
				Operator: true,
				TenantID: "tenant-2",
			},
			expectedCode:   DecisionValidationNotFound,
			expectedReason: "approval request not found",
		},
		{
			name:            "missing nonce",
			pendingApproval: baseApproval,
			decisionRequest: protocolpkg.ApprovalDecisionRequest{Approved: true},
			decisionActor:   DecisionActor{ControlSessionID: "control-session-1"},
			expectedCode:    DecisionValidationNonceMissing,
			expectedReason:  "approval decision nonce is required",
		},
		{
			name:            "consumed approval is a state conflict",
			pendingApproval: func() PendingApproval { approval := baseApproval; approval.State = StateConsumed; return approval }(),
			decisionRequest: baseDecision,
			decisionActor:   DecisionActor{ControlSessionID: "control-session-1"},
			expectedCode:    DecisionValidationStateConflict,
			expectedReason:  "approval request is no longer pending",
		},
		{
			name:            "missing manifest on approved decision",
			pendingApproval: baseApproval,
			decisionRequest: protocolpkg.ApprovalDecisionRequest{Approved: true, DecisionNonce: "nonce-1"},
			decisionActor:   DecisionActor{ControlSessionID: "control-session-1"},
			expectedCode:    DecisionValidationManifestInvalid,
			expectedReason:  "approval manifest sha256 is required for this approval",
		},
		{
			name:            "mismatched manifest on approved decision",
			pendingApproval: baseApproval,
			decisionRequest: protocolpkg.ApprovalDecisionRequest{Approved: true, DecisionNonce: "nonce-1", ApprovalManifestSHA256: "other-manifest"},
			decisionActor:   DecisionActor{ControlSessionID: "control-session-1"},
			expectedCode:    DecisionValidationManifestInvalid,
			expectedReason:  "approval manifest sha256 does not match the pending approval",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			validationError := ValidateDecisionRequest(testCase.pendingApproval, testCase.decisionRequest, testCase.decisionActor)
			if validationError == nil {
				t.Fatalf("expected validation failure")
			}
			if validationError.Code != testCase.expectedCode {
				t.Fatalf("expected code %q, got %q", testCase.expectedCode, validationError.Code)
			}
			if validationError.Reason != testCase.expectedReason {
				t.Fatalf("expected reason %q, got %q", testCase.expectedReason, validationError.Reason)
			}
		})
	}
}
