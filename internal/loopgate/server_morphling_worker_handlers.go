package loopgate

import (
	"io"
	"net/http"
	"strings"
)

func (server *Server) handleMorphlingWorkerLaunch(writer http.ResponseWriter, request *http.Request) {
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

	var launchRequest MorphlingWorkerLaunchRequest
	if err := decodeJSONBytes(requestBodyBytes, &launchRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if err := launchRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	launchResponse, err := server.createMorphlingWorkerLaunch(tokenClaims, launchRequest)
	if err != nil {
		server.writeJSON(writer, morphlingHTTPStatus(err), CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: redactMorphlingError(err),
			DenialCode:   morphlingDenialCode(err),
			Redacted:     true,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, launchResponse)
}

func (server *Server) handleMorphlingWorkerOpen(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requestPeerIdentity, ok := peerIdentityFromContext(request.Context())
	if !ok {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "missing authenticated peer identity",
			DenialCode:   DenialCodeMorphlingWorkerTokenInvalid,
		})
		return
	}

	var openRequest MorphlingWorkerOpenRequest
	if err := server.decodeJSONBody(writer, request, maxCapabilityBodyBytes, &openRequest); err != nil {
		return
	}
	if err := openRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	sessionResponse, err := server.openMorphlingWorkerSession(requestPeerIdentity, openRequest)
	if err != nil {
		server.writeJSON(writer, morphlingHTTPStatus(err), CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: redactMorphlingError(err),
			DenialCode:   morphlingDenialCode(err),
			Redacted:     true,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, sessionResponse)
}

func (server *Server) handleMorphlingWorkerStart(writer http.ResponseWriter, request *http.Request) {
	server.handleMorphlingWorkerAction(writer, request, func(workerSession morphlingWorkerSession, requestBodyBytes []byte) (interface{}, error) {
		var startRequest MorphlingWorkerStartRequest
		if err := decodeJSONBytes(requestBodyBytes, &startRequest); err != nil {
			return nil, err
		}
		if err := startRequest.Validate(); err != nil {
			return nil, err
		}
		return server.startMorphlingWorker(workerSession, startRequest)
	})
}

func (server *Server) handleMorphlingWorkerUpdate(writer http.ResponseWriter, request *http.Request) {
	server.handleMorphlingWorkerAction(writer, request, func(workerSession morphlingWorkerSession, requestBodyBytes []byte) (interface{}, error) {
		var updateRequest MorphlingWorkerUpdateRequest
		if err := decodeJSONBytes(requestBodyBytes, &updateRequest); err != nil {
			return nil, err
		}
		if err := updateRequest.Validate(); err != nil {
			return nil, err
		}
		return server.updateMorphlingWorker(workerSession, updateRequest)
	})
}

func (server *Server) handleMorphlingWorkerComplete(writer http.ResponseWriter, request *http.Request) {
	server.handleMorphlingWorkerAction(writer, request, func(workerSession morphlingWorkerSession, requestBodyBytes []byte) (interface{}, error) {
		var completeRequest MorphlingWorkerCompleteRequest
		if err := decodeJSONBytes(requestBodyBytes, &completeRequest); err != nil {
			return nil, err
		}
		if err := completeRequest.Validate(); err != nil {
			return nil, err
		}
		return server.completeMorphlingWorker(workerSession, completeRequest)
	})
}

func (server *Server) handleMorphlingReview(writer http.ResponseWriter, request *http.Request) {
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

	var reviewRequest MorphlingReviewRequest
	if err := decodeJSONBytes(requestBodyBytes, &reviewRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if err := reviewRequest.Validate(); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	reviewResponse, err := server.reviewMorphling(tokenClaims, reviewRequest)
	if err != nil {
		server.writeJSON(writer, morphlingHTTPStatus(err), CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: redactMorphlingError(err),
			DenialCode:   morphlingDenialCode(err),
			Redacted:     true,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, reviewResponse)
}

func (server *Server) handleMorphlingWorkerAction(writer http.ResponseWriter, request *http.Request, runAction func(morphlingWorkerSession, []byte) (interface{}, error)) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workerSession, ok := server.authenticateMorphlingWorker(writer, request)
	if !ok {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifyMorphlingWorkerSignedBody(writer, request, maxCapabilityBodyBytes, workerSession)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	actionResponse, err := runAction(workerSession, requestBodyBytes)
	if err != nil {
		if strings.Contains(err.Error(), "invalid request body") || strings.Contains(err.Error(), "request body must contain a single JSON object") || strings.Contains(err.Error(), "exceeds maximum length") {
			server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
				Status:       ResponseStatusError,
				DenialReason: err.Error(),
				DenialCode:   DenialCodeMalformedRequest,
			})
			return
		}
		server.writeJSON(writer, morphlingHTTPStatus(err), CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: redactMorphlingError(err),
			DenialCode:   morphlingDenialCode(err),
			Redacted:     true,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, actionResponse)
}

func (server *Server) authenticateMorphlingWorker(writer http.ResponseWriter, request *http.Request) (morphlingWorkerSession, bool) {
	requestPeerIdentity, ok := peerIdentityFromContext(request.Context())
	if !ok {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "missing authenticated peer identity",
			DenialCode:   DenialCodeMorphlingWorkerTokenInvalid,
		})
		return morphlingWorkerSession{}, false
	}

	workerToken := strings.TrimSpace(request.Header.Get("X-Loopgate-Morphling-Worker-Token"))
	if workerToken == "" {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "missing morphling worker token",
			DenialCode:   DenialCodeMorphlingWorkerTokenMissing,
		})
		return morphlingWorkerSession{}, false
	}

	server.mu.Lock()
	server.pruneExpiredLocked()
	workerSession, found := server.morphlingWorkerSessions[workerToken]
	server.mu.Unlock()
	if !found {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "invalid morphling worker token",
			DenialCode:   DenialCodeMorphlingWorkerTokenInvalid,
		})
		return morphlingWorkerSession{}, false
	}
	if workerSession.PeerIdentity != requestPeerIdentity {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "morphling worker token peer binding mismatch",
			DenialCode:   DenialCodeMorphlingWorkerTokenInvalid,
		})
		return morphlingWorkerSession{}, false
	}
	return workerSession, true
}

func (server *Server) readAndVerifyMorphlingWorkerSignedBody(writer http.ResponseWriter, request *http.Request, maxBodyBytes int64, workerSession morphlingWorkerSession) ([]byte, CapabilityResponse, bool) {
	request.Body = http.MaxBytesReader(writer, request.Body, maxBodyBytes)
	requestBodyBytes, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "invalid request body: " + err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		}, false
	}
	requestTimestamp, requestNonce, requestSignature, denial, ok := server.parseAndValidateSignedControlPlaneRequest(request, workerSession.ControlSessionID)
	if !ok {
		return nil, denial, false
	}
	if verificationResponse, ok := server.verifySignedRequestAgainstRotatingSessionMAC(request, requestBodyBytes, workerSession.ControlSessionID, requestTimestamp, requestNonce, requestSignature); !ok {
		return nil, verificationResponse, false
	}
	return requestBodyBytes, CapabilityResponse{}, true
}
