package loopgate

import (
	"errors"
	"net/http"
	"strings"

	"morph/internal/identifiers"
)

func (server *Server) handleMemoryWakeState(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityMemoryRead) {
		return
	}
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	wakeStateResponse, err := server.loadMemoryWakeState(tokenClaims.TenantID)
	if err != nil {
		server.writeMemoryOperationError(writer, err)
		return
	}
	server.writeJSON(writer, http.StatusOK, wakeStateResponse)
}

func (server *Server) handleMemoryDiagnosticWake(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityMemoryRead) {
		return
	}
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	server.writeJSON(writer, http.StatusOK, server.loadMemoryDiagnosticWake(tokenClaims.TenantID))
}

func (server *Server) handleMemoryDiscover(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityMemoryRead) {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var discoverRequest MemoryDiscoverRequest
	if err := decodeJSONBytes(requestBodyBytes, &discoverRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	discoverResponse, err := server.discoverMemory(tokenClaims.TenantID, discoverRequest)
	if err != nil {
		server.writeMemoryOperationError(writer, err)
		return
	}
	server.writeJSON(writer, http.StatusOK, discoverResponse)
}

func (server *Server) handleMemoryRecall(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityMemoryRead) {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var recallRequest MemoryRecallRequest
	if err := decodeJSONBytes(requestBodyBytes, &recallRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	recallResponse, err := server.recallMemory(tokenClaims.TenantID, recallRequest)
	if err != nil {
		server.writeMemoryOperationError(writer, err)
		return
	}
	server.writeJSON(writer, http.StatusOK, recallResponse)
}

func (server *Server) handleMemoryRemember(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityMemoryWrite) {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var rememberRequest MemoryRememberRequest
	if err := decodeJSONBytes(requestBodyBytes, &rememberRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	rememberResponse, err := server.rememberMemoryFact(tokenClaims, rememberRequest)
	if err != nil {
		server.writeMemoryOperationError(writer, err)
		return
	}
	server.writeJSON(writer, http.StatusOK, rememberResponse)
}

func (server *Server) handleMemoryInspectionGovernance(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}

	memoryPath := strings.TrimPrefix(request.URL.Path, "/v1/memory/inspections/")
	pathParts := strings.Split(memoryPath, "/")
	if len(pathParts) != 2 || strings.TrimSpace(pathParts[0]) == "" || strings.TrimSpace(pathParts[1]) == "" {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "invalid inspection governance path",
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	inspectionID := strings.TrimSpace(pathParts[0])
	action := strings.TrimSpace(pathParts[1])
	if err := identifiers.ValidateSafeIdentifier("inspection_id", inspectionID); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	requiredControlCapability, err := memoryInspectionControlCapability(action)
	if err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, requiredControlCapability) {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	switch action {
	case "review":
		var reviewRequest MemoryInspectionReviewRequest
		if err := decodeJSONBytes(requestBodyBytes, &reviewRequest); err != nil {
			server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
				Status:       ResponseStatusError,
				DenialReason: err.Error(),
				DenialCode:   DenialCodeMalformedRequest,
			})
			return
		}
		governanceResponse, err := server.reviewContinuityInspection(tokenClaims, inspectionID, reviewRequest)
		if err != nil {
			server.writeMemoryOperationError(writer, err)
			return
		}
		server.writeJSON(writer, http.StatusOK, governanceResponse)
	case "tombstone":
		var lineageRequest MemoryInspectionLineageRequest
		if err := decodeJSONBytes(requestBodyBytes, &lineageRequest); err != nil {
			server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
				Status:       ResponseStatusError,
				DenialReason: err.Error(),
				DenialCode:   DenialCodeMalformedRequest,
			})
			return
		}
		governanceResponse, err := server.tombstoneContinuityInspection(tokenClaims, inspectionID, lineageRequest)
		if err != nil {
			server.writeMemoryOperationError(writer, err)
			return
		}
		server.writeJSON(writer, http.StatusOK, governanceResponse)
	case "purge":
		var lineageRequest MemoryInspectionLineageRequest
		if err := decodeJSONBytes(requestBodyBytes, &lineageRequest); err != nil {
			server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
				Status:       ResponseStatusError,
				DenialReason: err.Error(),
				DenialCode:   DenialCodeMalformedRequest,
			})
			return
		}
		governanceResponse, err := server.purgeContinuityInspection(tokenClaims, inspectionID, lineageRequest)
		if err != nil {
			server.writeMemoryOperationError(writer, err)
			return
		}
		server.writeJSON(writer, http.StatusOK, governanceResponse)
	default:
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "unknown continuity governance action",
			DenialCode:   DenialCodeMalformedRequest,
		})
	}
}

func memoryInspectionControlCapability(action string) (string, error) {
	switch action {
	case "review":
		return controlCapabilityMemoryReview, nil
	case "tombstone", "purge":
		return controlCapabilityMemoryLineage, nil
	default:
		return "", errors.New("unknown continuity governance action")
	}
}

func (server *Server) writeMemoryOperationError(writer http.ResponseWriter, operationError error) {
	var governanceError continuityGovernanceError
	if errors.As(operationError, &governanceError) {
		server.writeJSON(writer, governanceError.httpStatus, CapabilityResponse{
			Status:       governanceError.responseStatus,
			DenialReason: governanceError.reason,
			DenialCode:   governanceError.denialCode,
		})
		return
	}
	server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
		Status:       ResponseStatusError,
		DenialReason: operationError.Error(),
		DenialCode:   DenialCodeMalformedRequest,
	})
}
