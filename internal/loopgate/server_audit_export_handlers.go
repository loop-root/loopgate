package loopgate

import (
	"context"
	"net/http"
	"strings"

	"loopgate/internal/secrets"
)

func (server *Server) handleAuditExportFlush(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityAuditExport) {
		return
	}
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	requestSuffix, err := randomHex(8)
	if err != nil {
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "audit export flush request id generation failed",
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}
	flushRequestID := "audit_export." + requestSuffix

	baseAuditData := map[string]interface{}{
		"request_id":           flushRequestID,
		"destination_kind":     strings.TrimSpace(server.runtimeConfig.Logging.AuditExport.DestinationKind),
		"destination_label":    strings.TrimSpace(server.runtimeConfig.Logging.AuditExport.DestinationLabel),
		"actor_label":          tokenClaims.ActorLabel,
		"client_session_label": tokenClaims.ClientSessionLabel,
		"control_session_id":   tokenClaims.ControlSessionID,
	}
	if err := server.logEvent("audit_export.requested", tokenClaims.ControlSessionID, baseAuditData); err != nil {
		server.writeJSON(writer, http.StatusServiceUnavailable, auditUnavailableCapabilityResponse(flushRequestID))
		return
	}

	flushResponse, flushErr := server.performAuditExportFlush(request.Context(), flushRequestID)
	if flushErr != nil {
		failureAuditData := copyInterfaceMap(baseAuditData)
		failureAuditData["operator_error_class"] = secrets.LoopgateOperatorErrorClass(flushErr)
		failureAuditData["reason"] = "audit export flush failed"
		if err := server.logEvent("audit_export.failed", tokenClaims.ControlSessionID, failureAuditData); err != nil {
			server.writeJSON(writer, http.StatusServiceUnavailable, auditUnavailableCapabilityResponse(flushRequestID))
			return
		}
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			RequestID:    flushRequestID,
			Status:       ResponseStatusError,
			DenialReason: "audit export flush failed",
			DenialCode:   DenialCodeExecutionFailed,
			Redacted:     true,
		})
		return
	}

	outcomeAuditData := copyInterfaceMap(baseAuditData)
	outcomeAuditData["status"] = flushResponse.Status
	outcomeAuditData["event_count"] = flushResponse.EventCount
	if flushResponse.ApproxBytes > 0 {
		outcomeAuditData["approx_bytes"] = flushResponse.ApproxBytes
	}
	if flushResponse.FromAuditSequence > 0 {
		outcomeAuditData["from_audit_sequence"] = flushResponse.FromAuditSequence
	}
	if flushResponse.ThroughAuditSequence > 0 {
		outcomeAuditData["through_audit_sequence"] = flushResponse.ThroughAuditSequence
	}
	if strings.TrimSpace(flushResponse.ThroughEventHash) != "" {
		outcomeAuditData["through_event_hash"] = strings.TrimSpace(flushResponse.ThroughEventHash)
	}
	outcomeEventType := "audit_export.completed"
	if flushResponse.Status == "noop" || flushResponse.Status == "disabled" {
		outcomeEventType = "audit_export.noop"
	}
	if err := server.logEvent(outcomeEventType, tokenClaims.ControlSessionID, outcomeAuditData); err != nil {
		server.writeJSON(writer, http.StatusServiceUnavailable, auditUnavailableCapabilityResponse(flushRequestID))
		return
	}

	server.writeJSON(writer, http.StatusOK, flushResponse)
}

func (server *Server) performAuditExportFlush(ctx context.Context, flushRequestID string) (AuditExportFlushResponse, error) {
	response := AuditExportFlushResponse{
		FlushRequestID:   strings.TrimSpace(flushRequestID),
		DestinationKind:  strings.TrimSpace(server.runtimeConfig.Logging.AuditExport.DestinationKind),
		DestinationLabel: strings.TrimSpace(server.runtimeConfig.Logging.AuditExport.DestinationLabel),
	}
	if !server.runtimeConfig.Logging.AuditExport.Enabled {
		response.Status = "disabled"
		return response, nil
	}

	exportBatch, err := server.prepareNextAuditExportBatch()
	if err != nil {
		return response, err
	}
	response.EventCount = exportBatch.EventCount
	response.ApproxBytes = exportBatch.ApproxBytes
	response.FromAuditSequence = exportBatch.FromAuditSequence
	response.ThroughAuditSequence = exportBatch.ThroughAuditSequence
	response.ThroughEventHash = strings.TrimSpace(exportBatch.ThroughEventHash)
	if exportBatch.EventCount == 0 {
		response.Status = "noop"
		return response, nil
	}

	if err := server.flushAuditExportBatchToConfiguredDestination(ctx, exportBatch); err != nil {
		return response, err
	}
	response.Status = "flushed"
	return response, nil
}
