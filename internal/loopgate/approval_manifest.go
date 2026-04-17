package loopgate

import (
	approvalpkg "loopgate/internal/loopgate/approval"
	protocolpkg "loopgate/internal/loopgate/protocol"
)

// capabilityRequestBodySHA256 computes the SHA256 of the JSON-serialized capability request.
// This is computed at approval creation time and stored with the pending approval. At execution
// time (PR 1b), the live request body hash is verified against this stored value to ensure the
// method, path, and body being executed exactly match what was approved.
func capabilityRequestBodySHA256(capabilityRequest CapabilityRequest) (string, error) {
	return protocolpkg.RequestBodySHA256(capabilityRequest)
}

// cloneCapabilityRequest returns a deep copy so pending approval state cannot be mutated
// through a shared Arguments map held by the caller (or concurrent reuse of the same map).
func cloneCapabilityRequest(r CapabilityRequest) CapabilityRequest {
	return protocolpkg.CloneCapabilityRequest(r)
}

// buildCapabilityApprovalManifest computes all manifest fields for a capability execution approval.
// Returns the manifest SHA256, execution body SHA256, and expiry in milliseconds.
func buildCapabilityApprovalManifest(capabilityRequest CapabilityRequest, expiresAtMs int64) (manifestSHA256, bodySHA256 string, err error) {
	return approvalpkg.BuildCapabilityApprovalManifest(capabilityRequest, expiresAtMs)
}
