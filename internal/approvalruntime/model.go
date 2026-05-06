package approvalruntime

import (
	"strings"
	"time"

	protocolpkg "loopgate/internal/protocol"
)

const (
	actionClassCapabilityExecute = "capability.execute"
	subjectClassCapability       = "capability"
	executionMethodCapability    = "POST"
	executionPathCapability      = "/v1/capabilities/execute"
)

type ExecutionContext struct {
	ControlSessionID    string
	ActorLabel          string
	ClientSessionLabel  string
	AllowedCapabilities map[string]struct{}
	TenantID            string
	UserID              string
}

type PendingApproval struct {
	ID                     string
	Request                protocolpkg.CapabilityRequest
	CreatedAt              time.Time
	ExpiresAt              time.Time
	Metadata               map[string]interface{}
	Reason                 string
	ControlSessionID       string
	DecisionNonce          string
	DecisionSubmittedAt    time.Time
	ExecutedAt             time.Time
	ExecutionContext       ExecutionContext
	State                  string
	ApprovalManifestSHA256 string
	ExecutionBodySHA256    string
}

func BuildCapabilityApprovalManifest(capabilityRequest protocolpkg.CapabilityRequest, expiresAtMs int64) (manifestSHA256, bodySHA256 string, err error) {
	bodyHash, err := RequestBodySHA256(capabilityRequest)
	if err != nil {
		return "", "", err
	}
	return ComputeManifestSHA256(
		actionClassCapabilityExecute,
		subjectClassCapability,
		capabilityRequest.Capability,
		CapabilitySubjectBinding(capabilityRequest.Capability),
		executionMethodCapability,
		executionPathCapability,
		bodyHash,
		ScopeSingleUse,
		expiresAtMs,
	), bodyHash, nil
}

func BackfillPendingApprovalManifest(approvalRecords map[string]PendingApproval, approvalID string, approval PendingApproval) PendingApproval {
	if strings.TrimSpace(approval.ApprovalManifestSHA256) != "" {
		return approval
	}
	if strings.TrimSpace(approval.Request.Capability) == "" || approval.ExpiresAt.IsZero() {
		return approval
	}
	manifestSHA256, bodySHA256, err := BuildCapabilityApprovalManifest(approval.Request, approval.ExpiresAt.UTC().UnixMilli())
	if err != nil {
		return approval
	}
	approval.ApprovalManifestSHA256 = manifestSHA256
	if strings.TrimSpace(approval.ExecutionBodySHA256) == "" {
		approval.ExecutionBodySHA256 = bodySHA256
	}
	approvalRecords[approvalID] = approval
	return approval
}
