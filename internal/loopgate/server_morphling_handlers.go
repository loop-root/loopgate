package loopgate

import (
	"errors"
	"net/http"

	"morph/internal/sandbox"
)

func (server *Server) handleMorphlingSpawn(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityMorphlingWrite) {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var spawnRequest MorphlingSpawnRequest
	if err := decodeJSONBytes(requestBodyBytes, &spawnRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if err := spawnRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	spawnResponse, err := server.spawnMorphling(tokenClaims, spawnRequest)
	if err != nil {
		server.writeJSON(writer, morphlingHTTPStatus(err), CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: redactMorphlingError(err),
			DenialCode:   morphlingDenialCode(err),
			Redacted:     true,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, spawnResponse)
}

func (server *Server) handleMorphlingStatus(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityMorphlingRead) {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var statusRequest MorphlingStatusRequest
	if err := decodeJSONBytes(requestBodyBytes, &statusRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if err := statusRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	statusResponse, err := server.morphlingStatus(tokenClaims, statusRequest)
	if err != nil {
		server.writeJSON(writer, morphlingHTTPStatus(err), CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: redactMorphlingError(err),
			DenialCode:   morphlingDenialCode(err),
			Redacted:     true,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, statusResponse)
}

func (server *Server) handleMorphlingTerminate(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityMorphlingWrite) {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var terminateRequest MorphlingTerminateRequest
	if err := decodeJSONBytes(requestBodyBytes, &terminateRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if err := terminateRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	terminateResponse, err := server.terminateMorphling(tokenClaims, terminateRequest)
	if err != nil {
		server.writeJSON(writer, morphlingHTTPStatus(err), CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: redactMorphlingError(err),
			DenialCode:   morphlingDenialCode(err),
			Redacted:     true,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, terminateResponse)
}

func morphlingDenialCode(err error) string {
	switch {
	case errors.Is(err, errMorphlingSpawnDisabled):
		return DenialCodeMorphlingSpawnDisabled
	case errors.Is(err, errMorphlingClassInvalid):
		return DenialCodeMorphlingClassInvalid
	case errors.Is(err, errMorphlingInputInvalid):
		return DenialCodeMorphlingInputInvalid
	case errors.Is(err, errMorphlingArtifactInvalid):
		return DenialCodeMorphlingArtifactInvalid
	case errors.Is(err, errMorphlingActiveLimitReached):
		return DenialCodeMorphlingActiveLimitReached
	case errors.Is(err, errMorphlingNotFound):
		return DenialCodeMorphlingNotFound
	case errors.Is(err, errMorphlingStateInvalid):
		return DenialCodeMorphlingStateInvalid
	case errors.Is(err, errMorphlingReviewInvalid):
		return DenialCodeMorphlingReviewInvalid
	case errors.Is(err, errMorphlingWorkerLaunchInvalid):
		return DenialCodeMorphlingWorkerLaunchInvalid
	case errors.Is(err, errMorphlingWorkerTokenInvalid):
		return DenialCodeMorphlingWorkerTokenInvalid
	case errors.Is(err, errMorphlingWorkerSessionsSaturated):
		return DenialCodeControlPlaneStateSaturated
	case errors.Is(err, sandbox.ErrSandboxPathInvalid), errors.Is(err, sandbox.ErrSandboxPathOutsideRoot), errors.Is(err, sandbox.ErrSandboxSourceUnavailable):
		return DenialCodeSandboxPathInvalid
	case errors.Is(err, sandbox.ErrSandboxDestinationExists):
		return DenialCodeSandboxDestinationExists
	case errors.Is(err, errMorphlingAuditUnavailable):
		return DenialCodeAuditUnavailable
	default:
		return DenialCodeExecutionFailed
	}
}

func morphlingHTTPStatus(err error) int {
	switch morphlingDenialCode(err) {
	case DenialCodeMorphlingSpawnDisabled:
		return http.StatusForbidden
	case DenialCodeMorphlingClassInvalid, DenialCodeMorphlingInputInvalid, DenialCodeMorphlingArtifactInvalid, DenialCodeSandboxPathInvalid:
		return http.StatusBadRequest
	case DenialCodeMorphlingActiveLimitReached, DenialCodeMorphlingStateInvalid, DenialCodeMorphlingReviewInvalid, DenialCodeSandboxDestinationExists:
		return http.StatusConflict
	case DenialCodeMorphlingNotFound:
		return http.StatusNotFound
	case DenialCodeMorphlingWorkerLaunchInvalid, DenialCodeMorphlingWorkerTokenInvalid, DenialCodeMorphlingWorkerTokenMissing:
		return http.StatusUnauthorized
	case DenialCodeControlPlaneStateSaturated:
		return http.StatusTooManyRequests
	case DenialCodeAuditUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func redactMorphlingError(err error) string {
	switch morphlingDenialCode(err) {
	case DenialCodeMorphlingSpawnDisabled:
		return "morphling spawn is disabled by policy"
	case DenialCodeMorphlingClassInvalid:
		return "morphling class is invalid"
	case DenialCodeMorphlingInputInvalid:
		return "morphling inputs must stay inside /morph/home"
	case DenialCodeMorphlingArtifactInvalid:
		return "morphling artifact paths must stay inside the morphling working directory"
	case DenialCodeMorphlingActiveLimitReached:
		return "morphling active limit reached"
	case DenialCodeMorphlingNotFound:
		return "morphling was not found"
	case DenialCodeMorphlingStateInvalid:
		return "morphling state transition is invalid"
	case DenialCodeMorphlingReviewInvalid:
		return "morphling review is invalid for the current state"
	case DenialCodeMorphlingWorkerLaunchInvalid:
		return "morphling worker launch token is invalid"
	case DenialCodeMorphlingWorkerTokenMissing:
		return "missing morphling worker token"
	case DenialCodeMorphlingWorkerTokenInvalid:
		return "morphling worker token is invalid"
	case DenialCodeSandboxPathInvalid:
		return "morphling sandbox paths must stay inside /morph/home"
	case DenialCodeSandboxDestinationExists:
		return "morphling working directory already exists"
	case DenialCodeAuditUnavailable:
		return "control-plane audit is unavailable"
	default:
		return "morphling operation failed"
	}
}
