package loopgate

import (
	"crypto/subtle"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"strings"

	approvalpkg "loopgate/internal/loopgate/approval"
	"loopgate/internal/secrets"
	toolspkg "loopgate/internal/tools"
)

// verifyPendingApprovalStoredExecutionBody checks that pending.Request still serializes to the
// same SHA256 recorded at approval creation. Skips when ExecutionBodySHA256 is empty (legacy
// backfill-only records).
func (server *Server) verifyPendingApprovalStoredExecutionBody(pending pendingApproval) (controlapipkg.CapabilityResponse, bool) {
	if strings.TrimSpace(pending.ExecutionBodySHA256) == "" {
		return controlapipkg.CapabilityResponse{}, true
	}
	current, err := approvalpkg.RequestBodySHA256(pending.Request)
	if err != nil {
		return controlapipkg.CapabilityResponse{
			RequestID:    pending.Request.RequestID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "control-plane approval execution body check failed",
			DenialCode:   controlapipkg.DenialCodeExecutionFailed,
		}, false
	}
	// Constant-time compare on hex digests: same length from SHA256 hex encoding.
	if len(current) != len(pending.ExecutionBodySHA256) {
		return controlapipkg.CapabilityResponse{
			RequestID:    pending.Request.RequestID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "stored approval request does not match execution body hash",
			DenialCode:   controlapipkg.DenialCodeApprovalExecutionBodyMismatch,
		}, false
	}
	if subtle.ConstantTimeCompare([]byte(current), []byte(pending.ExecutionBodySHA256)) != 1 {
		return controlapipkg.CapabilityResponse{
			RequestID:    pending.Request.RequestID,
			Status:       controlapipkg.ResponseStatusError,
			DenialReason: "stored approval request does not match execution body hash",
			DenialCode:   controlapipkg.DenialCodeApprovalExecutionBodyMismatch,
		}, false
	}
	return controlapipkg.CapabilityResponse{}, true
}

// writePendingApprovalExecutionIntegrityDenial mirrors other approval denial paths: audit, then JSON.
func (server *Server) writePendingApprovalExecutionIntegrityDenial(
	writer http.ResponseWriter,
	controlSession controlSession,
	approvalID string,
	pendingApproval pendingApproval,
	denial controlapipkg.CapabilityResponse,
) {
	approvalDeniedAuditData := map[string]interface{}{
		"approval_request_id":  approvalID,
		"approval_class":       pendingApproval.Metadata["approval_class"],
		"reason":               secrets.RedactText(denial.DenialReason),
		"denial_code":          denial.DenialCode,
		"control_session_id":   controlSession.ID,
		"actor_label":          controlSession.ActorLabel,
		"client_session_label": controlSession.ClientSessionLabel,
	}
	if approvalClass, okClass := pendingApproval.Metadata["approval_class"].(string); okClass && strings.TrimSpace(approvalClass) != "" {
		approvalDeniedAuditData["approval_class"] = approvalClass
	}
	if err := server.logEvent("approval.denied", controlSession.ID, approvalDeniedAuditData); err != nil {
		server.writeJSON(writer, http.StatusServiceUnavailable, controlapipkg.CapabilityResponse{
			RequestID:         denial.RequestID,
			Status:            controlapipkg.ResponseStatusError,
			DenialReason:      "control-plane audit is unavailable",
			DenialCode:        controlapipkg.DenialCodeAuditUnavailable,
			ApprovalRequestID: approvalID,
		})
		return
	}
	server.writeJSON(writer, httpStatusForResponse(denial), denial)
}

// capabilityProhibitsRawSecretExport: registered tools are classified only via optional interfaces
// (no name-based fallback for arbitrary registered Tool implementations). Unregistered capability
// names still use the legacy name heuristic before the unknown-capability path. Configured HTTP
// capabilities register as *configuredCapabilityTool, which implements RawSecretExportProhibited
// using the same heuristic on the configured name so YAML-defined integrations stay covered.
func (server *Server) capabilityProhibitsRawSecretExport(tool toolspkg.Tool, capabilityName string) bool {
	if tool != nil {
		if explicit, ok := tool.(toolspkg.RawSecretExportProhibited); ok && explicit.RawSecretExportProhibited() {
			return true
		}
		if optOut, ok := tool.(toolspkg.SecretExportNameHeuristicOptOut); ok && optOut.SecretExportNameHeuristicOptOut() {
			return false
		}
		return false
	}
	return secretExportCapabilityNameHeuristic(capabilityName)
}
