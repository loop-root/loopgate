package loopgate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

// computeApprovalManifestSHA256 computes the canonical approval manifest hash per AMP RFC 0005 §6.
//
// The manifest binds an approval to the exact action class, subject, execution method, path,
// request body hash, scope, and expiry. Submitting this hash with an approval decision proves
// the operator reviewed the exact action being approved, not a spoofed or substituted request.
//
// Both the server (at creation time) and the operator (via the decision submission) compute
// this value independently. The server verifies they match before accepting a decision.
func computeApprovalManifestSHA256(
	actionClass, subjectClass, subjectRef, subjectBinding,
	executionMethod, executionPath, executionBodySHA256,
	approvalScope string,
	expiresAtMs int64,
) string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "amp-approval-manifest-v1\n")
	fmt.Fprintf(&buf, "action-class:%s\n", actionClass)
	fmt.Fprintf(&buf, "subject-class:%s\n", subjectClass)
	fmt.Fprintf(&buf, "subject-ref:%s\n", subjectRef)
	fmt.Fprintf(&buf, "subject-binding:%s\n", subjectBinding)
	fmt.Fprintf(&buf, "execution-method:%s\n", executionMethod)
	fmt.Fprintf(&buf, "execution-path:%s\n", executionPath)
	fmt.Fprintf(&buf, "execution-body-sha256:%s\n", executionBodySHA256)
	fmt.Fprintf(&buf, "approval-scope:%s\n", approvalScope)
	fmt.Fprintf(&buf, "expires-at-ms:%d\n", expiresAtMs)
	h := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(h[:])
}

// capabilityRequestBodySHA256 computes the SHA256 of the JSON-serialized capability request.
// This is computed at approval creation time and stored with the pending approval. At execution
// time (PR 1b), the live request body hash is verified against this stored value to ensure the
// method, path, and body being executed exactly match what was approved.
func capabilityRequestBodySHA256(capabilityRequest CapabilityRequest) (string, error) {
	requestBytes, err := json.Marshal(capabilityRequest)
	if err != nil {
		return "", fmt.Errorf("marshal capability request: %w", err)
	}
	h := sha256.Sum256(requestBytes)
	return hex.EncodeToString(h[:]), nil
}

// capabilitySubjectBinding computes the subject binding for a capability approval manifest.
// The binding is a type-prefixed SHA256 of the capability name, providing a stable object-level
// binding without requiring a full capability version hash at this stage.
func capabilitySubjectBinding(capabilityName string) string {
	h := sha256.Sum256([]byte("capability-name:" + capabilityName))
	return "object-sha256:" + hex.EncodeToString(h[:])
}

// buildCapabilityApprovalManifest computes all manifest fields for a capability execution approval.
// Returns the manifest SHA256, execution body SHA256, and expiry in milliseconds.
func buildCapabilityApprovalManifest(capabilityRequest CapabilityRequest, expiresAtMs int64) (manifestSHA256, bodySHA256 string, err error) {
	bodyHash, err := capabilityRequestBodySHA256(capabilityRequest)
	if err != nil {
		return "", "", err
	}
	subjectBinding := capabilitySubjectBinding(capabilityRequest.Capability)
	manifest := computeApprovalManifestSHA256(
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
