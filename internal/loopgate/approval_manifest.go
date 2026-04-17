package loopgate

import (
	"loopgate/internal/loopgate/approval"
)

// approvalActionClassCapabilityExecute is the action class for capability execution approvals.
const approvalActionClassCapabilityExecute = "capability.execute"

// approvalSubjectClassCapability is the subject class for capability execution approvals.
const approvalSubjectClassCapability = "capability"

// approvalExecutionMethod is the HTTP method for capability execution.
const approvalExecutionMethod = "POST"

// approvalExecutionPath is the HTTP path for capability execution.
const approvalExecutionPath = "/v1/capabilities/execute"

// approvalScopeSingleUse is the single-use approval scope per AMP RFC 0005.
const approvalScopeSingleUse = "single-use"

// capabilityRequestBodySHA256 computes the SHA256 of the JSON-serialized capability request.
// This is computed at approval creation time and stored with the pending approval. At execution
// time (PR 1b), the live request body hash is verified against this stored value to ensure the
// method, path, and body being executed exactly match what was approved.
func capabilityRequestBodySHA256(capabilityRequest CapabilityRequest) (string, error) {
	return approval.RequestBodySHA256(capabilityRequest)
}

// cloneCapabilityRequest returns a deep copy so pending approval state cannot be mutated
// through a shared Arguments map held by the caller (or concurrent reuse of the same map).
func cloneCapabilityRequest(r CapabilityRequest) CapabilityRequest {
	out := r
	if r.Arguments != nil {
		out.Arguments = make(map[string]string, len(r.Arguments))
		for k, v := range r.Arguments {
			out.Arguments[k] = v
		}
	}
	return out
}

// buildCapabilityApprovalManifest computes all manifest fields for a capability execution approval.
// Returns the manifest SHA256, execution body SHA256, and expiry in milliseconds.
func buildCapabilityApprovalManifest(capabilityRequest CapabilityRequest, expiresAtMs int64) (manifestSHA256, bodySHA256 string, err error) {
	bodyHash, err := capabilityRequestBodySHA256(capabilityRequest)
	if err != nil {
		return "", "", err
	}
	subjectBinding := approval.CapabilitySubjectBinding(capabilityRequest.Capability)
	manifest := approval.ComputeManifestSHA256(
		approvalActionClassCapabilityExecute,
		approvalSubjectClassCapability,
		capabilityRequest.Capability,
		subjectBinding,
		approvalExecutionMethod,
		approvalExecutionPath,
		bodyHash,
		approvalScopeSingleUse,
		expiresAtMs,
	)
	return manifest, bodyHash, nil
}
