package loopgate

import (
	"encoding/json"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
)

func (server *Server) writeJSON(writer http.ResponseWriter, statusCode int, payload interface{}) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	if err := encodeJSONResponse(writer, payload); err != nil && server.reportResponseWriteError != nil {
		server.reportResponseWriteError(statusCode, err)
	}
}

func encodeJSONResponse(writer http.ResponseWriter, payload interface{}) error {
	return json.NewEncoder(writer).Encode(payload)
}

func auditUnavailableCapabilityResponse(requestID string) controlapipkg.CapabilityResponse {
	return controlapipkg.CapabilityResponse{
		RequestID:    requestID,
		Status:       controlapipkg.ResponseStatusError,
		DenialReason: "control-plane audit is unavailable",
		DenialCode:   controlapipkg.DenialCodeAuditUnavailable,
	}
}

func httpStatusForResponse(response controlapipkg.CapabilityResponse) int {
	switch response.Status {
	case controlapipkg.ResponseStatusSuccess:
		return http.StatusOK
	case controlapipkg.ResponseStatusPendingApproval:
		return http.StatusAccepted
	case controlapipkg.ResponseStatusDenied:
		switch response.DenialCode {
		case controlapipkg.DenialCodeCapabilityTokenMissing, controlapipkg.DenialCodeCapabilityTokenInvalid, controlapipkg.DenialCodeCapabilityTokenExpired,
			controlapipkg.DenialCodeApprovalTokenMissing, controlapipkg.DenialCodeApprovalTokenInvalid, controlapipkg.DenialCodeApprovalTokenExpired,
			controlapipkg.DenialCodeRequestSignatureMissing, controlapipkg.DenialCodeRequestSignatureInvalid, controlapipkg.DenialCodeRequestTimestampInvalid,
			controlapipkg.DenialCodeRequestNonceReplayDetected, controlapipkg.DenialCodeControlSessionBindingInvalid:
			return http.StatusUnauthorized
		case controlapipkg.DenialCodeApprovalNotFound:
			return http.StatusNotFound
		case controlapipkg.DenialCodeRequestReplayDetected, controlapipkg.DenialCodeCapabilityTokenReused, controlapipkg.DenialCodeApprovalStateConflict,
			controlapipkg.DenialCodeQuarantinePruneNotEligible:
			return http.StatusConflict
		case controlapipkg.DenialCodeSessionOpenRateLimited, controlapipkg.DenialCodeSessionActiveLimitReached, controlapipkg.DenialCodeReplayStateSaturated, controlapipkg.DenialCodePendingApprovalLimitReached, controlapipkg.DenialCodeControlPlaneStateSaturated:
			return http.StatusTooManyRequests
		case controlapipkg.DenialCodeMalformedRequest, controlapipkg.DenialCodeInvalidCapabilityArguments, controlapipkg.DenialCodeSiteURLInvalid,
			controlapipkg.DenialCodeSiteInspectionUnsupportedType, controlapipkg.DenialCodeSandboxPathInvalid, controlapipkg.DenialCodeSandboxHostDestinationInvalid,
			controlapipkg.DenialCodeApprovalDecisionNonceMissing, controlapipkg.DenialCodeApprovalDecisionNonceInvalid,
			controlapipkg.DenialCodeApprovalManifestMismatch:
			return http.StatusBadRequest
		default:
			return http.StatusForbidden
		}
	case controlapipkg.ResponseStatusError:
		switch response.DenialCode {
		case controlapipkg.DenialCodeMalformedRequest, controlapipkg.DenialCodeSiteURLInvalid, controlapipkg.DenialCodeSiteInspectionUnsupportedType,
			controlapipkg.DenialCodeSandboxPathInvalid, controlapipkg.DenialCodeSandboxHostDestinationInvalid:
			return http.StatusBadRequest
		case controlapipkg.DenialCodeAuditUnavailable:
			return http.StatusServiceUnavailable
		default:
			return http.StatusInternalServerError
		}
	default:
		return http.StatusInternalServerError
	}
}
