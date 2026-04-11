package loopgate

import (
	"encoding/json"
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

func auditUnavailableCapabilityResponse(requestID string) CapabilityResponse {
	return CapabilityResponse{
		RequestID:    requestID,
		Status:       ResponseStatusError,
		DenialReason: "control-plane audit is unavailable",
		DenialCode:   DenialCodeAuditUnavailable,
	}
}

func httpStatusForResponse(response CapabilityResponse) int {
	switch response.Status {
	case ResponseStatusSuccess:
		return http.StatusOK
	case ResponseStatusPendingApproval:
		return http.StatusAccepted
	case ResponseStatusDenied:
		switch response.DenialCode {
		case DenialCodeCapabilityTokenMissing, DenialCodeCapabilityTokenInvalid, DenialCodeCapabilityTokenExpired,
			DenialCodeApprovalTokenMissing, DenialCodeApprovalTokenInvalid, DenialCodeApprovalTokenExpired,
			DenialCodeRequestSignatureMissing, DenialCodeRequestSignatureInvalid, DenialCodeRequestTimestampInvalid,
			DenialCodeRequestNonceReplayDetected, DenialCodeControlSessionBindingInvalid:
			return http.StatusUnauthorized
		case DenialCodeApprovalNotFound, DenialCodeMorphlingNotFound, DenialCodeContinuityInspectionNotFound:
			return http.StatusNotFound
		case DenialCodeRequestReplayDetected, DenialCodeCapabilityTokenReused, DenialCodeApprovalStateConflict,
			DenialCodeQuarantinePruneNotEligible:
			return http.StatusConflict
		case DenialCodeSessionOpenRateLimited, DenialCodeSessionActiveLimitReached, DenialCodeReplayStateSaturated, DenialCodePendingApprovalLimitReached, DenialCodeControlPlaneStateSaturated:
			return http.StatusTooManyRequests
		case DenialCodeMalformedRequest, DenialCodeInvalidCapabilityArguments, DenialCodeSiteURLInvalid,
			DenialCodeSiteInspectionUnsupportedType, DenialCodeSandboxPathInvalid, DenialCodeSandboxHostDestinationInvalid,
			DenialCodeMorphlingInputInvalid, DenialCodeMorphlingArtifactInvalid, DenialCodeMorphlingReviewInvalid,
			DenialCodeMorphlingWorkerLaunchInvalid, DenialCodeApprovalDecisionNonceMissing, DenialCodeApprovalDecisionNonceInvalid,
			DenialCodeApprovalManifestMismatch:
			return http.StatusBadRequest
		default:
			return http.StatusForbidden
		}
	case ResponseStatusError:
		switch response.DenialCode {
		case DenialCodeMalformedRequest, DenialCodeSiteURLInvalid, DenialCodeSiteInspectionUnsupportedType,
			DenialCodeSandboxPathInvalid, DenialCodeSandboxHostDestinationInvalid:
			return http.StatusBadRequest
		case DenialCodeAuditUnavailable:
			return http.StatusServiceUnavailable
		default:
			return http.StatusInternalServerError
		}
	default:
		return http.StatusInternalServerError
	}
}
