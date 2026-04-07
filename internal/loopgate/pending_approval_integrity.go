package loopgate

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"morph/internal/secrets"
	toolspkg "morph/internal/tools"
)

// verifyPendingApprovalStoredExecutionBody checks that pending.Request still serializes to the
// same SHA256 recorded at approval creation. Skips when ExecutionBodySHA256 is empty (legacy
// backfill-only records).
func (server *Server) verifyPendingApprovalStoredExecutionBody(pending pendingApproval) (CapabilityResponse, bool) {
	if strings.TrimSpace(pending.ExecutionBodySHA256) == "" {
		return CapabilityResponse{}, true
	}
	current, err := capabilityRequestBodySHA256(pending.Request)
	if err != nil {
		return CapabilityResponse{
			RequestID:    pending.Request.RequestID,
			Status:       ResponseStatusError,
			DenialReason: "control-plane approval execution body check failed",
			DenialCode:   DenialCodeExecutionFailed,
		}, false
	}
	// Constant-time compare on hex digests: same length from SHA256 hex encoding.
	if len(current) != len(pending.ExecutionBodySHA256) {
		return CapabilityResponse{
			RequestID:    pending.Request.RequestID,
			Status:       ResponseStatusError,
			DenialReason: "stored approval request does not match execution body hash",
			DenialCode:   DenialCodeApprovalExecutionBodyMismatch,
		}, false
	}
	if subtle.ConstantTimeCompare([]byte(current), []byte(pending.ExecutionBodySHA256)) != 1 {
		return CapabilityResponse{
			RequestID:    pending.Request.RequestID,
			Status:       ResponseStatusError,
			DenialReason: "stored approval request does not match execution body hash",
			DenialCode:   DenialCodeApprovalExecutionBodyMismatch,
		}, false
	}
	return CapabilityResponse{}, true
}

// writePendingApprovalExecutionIntegrityDenial mirrors other approval denial paths: audit, then JSON.
func (server *Server) writePendingApprovalExecutionIntegrityDenial(
	writer http.ResponseWriter,
	controlSession controlSession,
	approvalID string,
	pendingApproval pendingApproval,
	denial CapabilityResponse,
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
		server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
			RequestID:         denial.RequestID,
			Status:            ResponseStatusError,
			DenialReason:      "control-plane audit is unavailable",
			DenialCode:        DenialCodeAuditUnavailable,
			ApprovalRequestID: approvalID,
		})
		return
	}
	server.writeJSON(writer, httpStatusForResponse(denial), denial)
}

// capabilityProhibitsRawSecretExport uses registry metadata when implemented, with the legacy
// name heuristic as defense in depth when not (see AGENTS.md).
func (server *Server) capabilityProhibitsRawSecretExport(tool toolspkg.Tool, capabilityName string) bool {
	if tool != nil {
		if explicit, ok := tool.(toolspkg.RawSecretExportProhibited); ok && explicit.RawSecretExportProhibited() {
			return true
		}
		if optOut, ok := tool.(toolspkg.SecretExportNameHeuristicOptOut); ok && optOut.SecretExportNameHeuristicOptOut() {
			return false
		}
	}
	return isSecretExportCapabilityHeuristic(capabilityName)
}
