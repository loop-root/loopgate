package approval

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const (
	StatePending         = "pending"
	StateGranted         = "granted"
	StateDenied          = "denied"
	StateExpired         = "expired"
	StateCancelled       = "cancelled"
	StateConsumed        = "consumed"
	StateExecutionFailed = "execution_failed"
)

// TokenHash returns the SHA-256 hex digest used for approval-token indexing.
func TokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ComputeManifestSHA256 computes the canonical approval manifest hash per AMP RFC 0005 §6.
func ComputeManifestSHA256(
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
	sum := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(sum[:])
}

// CapabilitySubjectBinding returns the stable object binding used for capability approvals.
func CapabilitySubjectBinding(capabilityName string) string {
	sum := sha256.Sum256([]byte("capability-name:" + capabilityName))
	return "object-sha256:" + hex.EncodeToString(sum[:])
}

// RequestBodySHA256 computes the SHA256 of a JSON-serialized approval-bound request body.
func RequestBodySHA256(requestBody any) (string, error) {
	requestBytes, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("marshal approval request body: %w", err)
	}
	sum := sha256.Sum256(requestBytes)
	return hex.EncodeToString(sum[:]), nil
}
