package loopgate

import (
	"net/http"
	"strings"

	"morph/internal/identifiers"
)

func (server *Server) handleTasksCollection(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/v1/tasks" {
		http.NotFound(writer, request)
		return
	}
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	server.memoryMu.Lock()
	partition, partitionErr := server.ensureMemoryPartitionLocked(tokenClaims.TenantID)
	var response UITasksResponse
	if partitionErr == nil {
		response = buildUITasksResponseFromContinuityState(partition.state)
	}
	server.memoryMu.Unlock()
	if partitionErr != nil {
		server.writeMemoryOperationError(writer, partitionErr)
		return
	}

	server.writeJSON(writer, http.StatusOK, response)
}

func (server *Server) handleTasksSubpaths(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPut {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	remainder := strings.TrimPrefix(request.URL.Path, "/v1/tasks/")
	remainder = strings.Trim(remainder, "/")
	pathSegments := strings.Split(remainder, "/")
	if len(pathSegments) != 2 || pathSegments[1] != "status" {
		http.NotFound(writer, request)
		return
	}
	itemID := strings.TrimSpace(pathSegments[0])
	if err := identifiers.ValidateSafeIdentifier("item_id", itemID); err != nil {
		http.NotFound(writer, request)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var statusRequest UITasksStatusUpdateRequest
	if err := decodeJSONBytes(requestBodyBytes, &statusRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if err := validatePutExplicitTodoWorkflowStatus(statusRequest.Status); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	if err := server.setExplicitTodoItemWorkflowStatus(tokenClaims, itemID, statusRequest.Status); err != nil {
		server.writeMemoryOperationError(writer, err)
		return
	}

	writer.WriteHeader(http.StatusNoContent)
}
