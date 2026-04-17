package approval

import (
	"strings"

	protocolpkg "loopgate/internal/loopgate/protocol"
)

type DecisionValidationCode string

const (
	DecisionValidationNotFound        DecisionValidationCode = "not_found"
	DecisionValidationOwnerMismatch   DecisionValidationCode = "owner_mismatch"
	DecisionValidationStateInvalid    DecisionValidationCode = "state_invalid"
	DecisionValidationStateConflict   DecisionValidationCode = "state_conflict"
	DecisionValidationNonceMissing    DecisionValidationCode = "nonce_missing"
	DecisionValidationNonceInvalid    DecisionValidationCode = "nonce_invalid"
	DecisionValidationManifestInvalid DecisionValidationCode = "manifest_invalid"
)

// DecisionActor describes who is attempting to resolve a pending approval.
// Regular approval-token holders are bound to the owning control session.
// Operator control-plane clients may act across sessions, but only within
// tenant scope when both sides carry tenant identity.
type DecisionActor struct {
	ControlSessionID string
	TenantID         string
	Operator         bool
}

type DecisionValidationError struct {
	Code   DecisionValidationCode
	Reason string
}

func (decisionValidationError *DecisionValidationError) Error() string {
	if decisionValidationError == nil {
		return ""
	}
	return decisionValidationError.Reason
}

// ValidateDecisionRequest enforces the pure approval-decision contract for a
// loaded pending approval record. Actor binding is checked before approval
// state so non-owners do not learn whether another session's approval is still
// pending or already resolved.
func ValidateDecisionRequest(
	pendingApproval PendingApproval,
	decisionRequest protocolpkg.ApprovalDecisionRequest,
	decisionActor DecisionActor,
) *DecisionValidationError {
	if decisionActor.Operator {
		if strings.TrimSpace(decisionActor.TenantID) != "" &&
			strings.TrimSpace(pendingApproval.ExecutionContext.TenantID) != "" &&
			decisionActor.TenantID != pendingApproval.ExecutionContext.TenantID {
			return &DecisionValidationError{
				Code:   DecisionValidationNotFound,
				Reason: "approval request not found",
			}
		}
	} else if strings.TrimSpace(decisionActor.ControlSessionID) != pendingApproval.ControlSessionID {
		return &DecisionValidationError{
			Code:   DecisionValidationOwnerMismatch,
			Reason: "approval token does not match approval owner",
		}
	}

	if pendingApproval.State != StatePending {
		denialCode := DecisionValidationStateInvalid
		if pendingApproval.State == StateConsumed || pendingApproval.State == StateExecutionFailed {
			denialCode = DecisionValidationStateConflict
		}
		return &DecisionValidationError{
			Code:   denialCode,
			Reason: "approval request is no longer pending",
		}
	}

	decisionNonce := strings.TrimSpace(decisionRequest.DecisionNonce)
	if decisionNonce == "" {
		return &DecisionValidationError{
			Code:   DecisionValidationNonceMissing,
			Reason: "approval decision nonce is required",
		}
	}
	if decisionNonce != pendingApproval.DecisionNonce {
		return &DecisionValidationError{
			Code:   DecisionValidationNonceInvalid,
			Reason: "approval decision nonce is invalid",
		}
	}

	submittedManifest := strings.TrimSpace(decisionRequest.ApprovalManifestSHA256)
	if decisionRequest.Approved && pendingApproval.ApprovalManifestSHA256 != "" {
		if submittedManifest == "" {
			return &DecisionValidationError{
				Code:   DecisionValidationManifestInvalid,
				Reason: "approval manifest sha256 is required for this approval",
			}
		}
		if submittedManifest != pendingApproval.ApprovalManifestSHA256 {
			return &DecisionValidationError{
				Code:   DecisionValidationManifestInvalid,
				Reason: "approval manifest sha256 does not match the pending approval",
			}
		}
	}

	return nil
}
