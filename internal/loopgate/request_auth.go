package loopgate

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

func (server *Server) authenticate(writer http.ResponseWriter, request *http.Request) (capabilityToken, bool) {
	requestPeerIdentity, ok := peerIdentityFromContext(request.Context())
	if !ok {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "missing authenticated peer identity",
			DenialCode:   DenialCodeCapabilityTokenInvalid,
		})
		return capabilityToken{}, false
	}

	authorizationHeader := strings.TrimSpace(request.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(authorizationHeader), "bearer ") {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "missing capability token",
			DenialCode:   DenialCodeCapabilityTokenMissing,
		})
		return capabilityToken{}, false
	}

	tokenString := strings.TrimSpace(authorizationHeader[len("Bearer "):])

	// Take a consistent now snapshot and perform all expiry checks inside a
	// single lock acquisition to eliminate the TOCTOU window between reading
	// token/session state and calling now() on the outside.
	server.mu.Lock()
	nowUTC := server.now().UTC()
	tokenClaims, found := server.tokens[tokenString]
	var activeSession controlSession
	var sessionFound bool
	var tokenExpired, sessionExpired bool
	if found {
		activeSession, sessionFound = server.sessions[tokenClaims.ControlSessionID]
		tokenExpired = nowUTC.After(tokenClaims.ExpiresAt)
		sessionExpired = sessionFound && nowUTC.After(activeSession.ExpiresAt)
		if tokenExpired {
			delete(server.tokens, tokenString)
		}
		if sessionExpired {
			delete(server.sessions, tokenClaims.ControlSessionID)
			delete(server.tokens, tokenString)
		}
	}
	server.mu.Unlock()

	if !found {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "invalid capability token",
			DenialCode:   DenialCodeCapabilityTokenInvalid,
		})
		return capabilityToken{}, false
	}
	if tokenExpired {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "expired capability token",
			DenialCode:   DenialCodeCapabilityTokenExpired,
		})
		return capabilityToken{}, false
	}
	if sessionExpired {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "expired capability token",
			DenialCode:   DenialCodeCapabilityTokenExpired,
		})
		return capabilityToken{}, false
	}
	if !sessionFound {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "invalid capability token",
			DenialCode:   DenialCodeCapabilityTokenInvalid,
		})
		return capabilityToken{}, false
	}
	if tokenClaims.PeerIdentity != requestPeerIdentity {
		server.writeJSON(writer, http.StatusUnauthorized, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "capability token peer binding mismatch",
			DenialCode:   DenialCodeCapabilityTokenInvalid,
		})
		return capabilityToken{}, false
	}

	return tokenClaims, true
}

// parseAndValidateSignedControlPlaneRequest checks signed-request headers and timestamp skew.
// It does not verify the HMAC. Callers supply expectedControlSessionID (for
// example, a scoped worker session id from a compatibility table); those ids
// are not necessarily rows in server.sessions.
func (server *Server) parseAndValidateSignedControlPlaneRequest(request *http.Request, expectedControlSessionID string) (requestTimestamp string, requestNonce string, requestSignature string, denial CapabilityResponse, ok bool) {
	controlSessionID := strings.TrimSpace(request.Header.Get("X-Loopgate-Control-Session"))
	requestTimestamp = strings.TrimSpace(request.Header.Get("X-Loopgate-Request-Timestamp"))
	requestNonce = strings.TrimSpace(request.Header.Get("X-Loopgate-Request-Nonce"))
	requestSignature = strings.TrimSpace(request.Header.Get("X-Loopgate-Request-Signature"))

	if controlSessionID == "" || requestTimestamp == "" || requestNonce == "" || requestSignature == "" {
		return "", "", "", CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "signed control-plane headers are required",
			DenialCode:   DenialCodeRequestSignatureMissing,
		}, false
	}
	if controlSessionID != expectedControlSessionID {
		return "", "", "", CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "control session binding is invalid",
			DenialCode:   DenialCodeControlSessionBindingInvalid,
		}, false
	}

	parsedTimestamp, err := time.Parse(time.RFC3339Nano, requestTimestamp)
	if err != nil {
		return "", "", "", CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "request timestamp is invalid",
			DenialCode:   DenialCodeRequestTimestampInvalid,
		}, false
	}
	if parsedTimestamp.Before(server.now().UTC().Add(-requestSignatureSkew)) || parsedTimestamp.After(server.now().UTC().Add(requestSignatureSkew)) {
		return "", "", "", CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "request timestamp is outside the allowed skew window",
			DenialCode:   DenialCodeRequestTimestampInvalid,
		}, false
	}

	return requestTimestamp, requestNonce, requestSignature, CapabilityResponse{}, true
}

func (server *Server) verifySignedRequest(request *http.Request, requestBodyBytes []byte, expectedControlSessionID string) (CapabilityResponse, bool) {
	controlSessionID := strings.TrimSpace(request.Header.Get("X-Loopgate-Control-Session"))
	requestTimestamp := strings.TrimSpace(request.Header.Get("X-Loopgate-Request-Timestamp"))
	requestNonce := strings.TrimSpace(request.Header.Get("X-Loopgate-Request-Nonce"))
	requestSignature := strings.TrimSpace(request.Header.Get("X-Loopgate-Request-Signature"))

	if controlSessionID == "" || requestTimestamp == "" || requestNonce == "" || requestSignature == "" {
		return CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "signed control-plane headers are required",
			DenialCode:   DenialCodeRequestSignatureMissing,
		}, false
	}
	if controlSessionID != expectedControlSessionID {
		return CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "control session binding is invalid",
			DenialCode:   DenialCodeControlSessionBindingInvalid,
		}, false
	}

	parsedTimestamp, err := time.Parse(time.RFC3339Nano, requestTimestamp)
	if err != nil {
		return CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "request timestamp is invalid",
			DenialCode:   DenialCodeRequestTimestampInvalid,
		}, false
	}
	if parsedTimestamp.Before(server.now().UTC().Add(-requestSignatureSkew)) || parsedTimestamp.After(server.now().UTC().Add(requestSignatureSkew)) {
		return CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "request timestamp is outside the allowed skew window",
			DenialCode:   DenialCodeRequestTimestampInvalid,
		}, false
	}

	server.mu.Lock()
	server.pruneExpiredLocked()
	activeSession, found := server.sessions[controlSessionID]
	server.mu.Unlock()
	if !found || strings.TrimSpace(activeSession.SessionMACKey) == "" {
		return CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "control session binding is invalid",
			DenialCode:   DenialCodeControlSessionBindingInvalid,
		}, false
	}

	if len(server.sessionMACRotationMaster) > 0 {
		return server.verifySignedRequestAgainstRotatingSessionMAC(request, requestBodyBytes, expectedControlSessionID, requestTimestamp, requestNonce, requestSignature)
	}
	return server.verifySignedRequestWithMACKey(request, requestBodyBytes, expectedControlSessionID, activeSession.SessionMACKey)
}

func (server *Server) verifySignedRequestWithMACKey(request *http.Request, requestBodyBytes []byte, expectedControlSessionID string, sessionMACKey string) (CapabilityResponse, bool) {
	controlSessionID := strings.TrimSpace(request.Header.Get("X-Loopgate-Control-Session"))
	requestTimestamp := strings.TrimSpace(request.Header.Get("X-Loopgate-Request-Timestamp"))
	requestNonce := strings.TrimSpace(request.Header.Get("X-Loopgate-Request-Nonce"))
	requestSignature := strings.TrimSpace(request.Header.Get("X-Loopgate-Request-Signature"))

	if controlSessionID == "" || requestTimestamp == "" || requestNonce == "" || requestSignature == "" {
		return CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "signed control-plane headers are required",
			DenialCode:   DenialCodeRequestSignatureMissing,
		}, false
	}
	if controlSessionID != expectedControlSessionID {
		return CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "control session binding is invalid",
			DenialCode:   DenialCodeControlSessionBindingInvalid,
		}, false
	}

	parsedTimestamp, err := time.Parse(time.RFC3339Nano, requestTimestamp)
	if err != nil {
		return CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "request timestamp is invalid",
			DenialCode:   DenialCodeRequestTimestampInvalid,
		}, false
	}
	if parsedTimestamp.Before(server.now().UTC().Add(-requestSignatureSkew)) || parsedTimestamp.After(server.now().UTC().Add(requestSignatureSkew)) {
		return CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "request timestamp is outside the allowed skew window",
			DenialCode:   DenialCodeRequestTimestampInvalid,
		}, false
	}

	expectedSignature := signRequest(sessionMACKey, request.Method, request.URL.Path, controlSessionID, requestTimestamp, requestNonce, requestBodyBytes)
	decodedRequestSignature, err := hex.DecodeString(requestSignature)
	if err != nil {
		return CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "request signature is invalid",
			DenialCode:   DenialCodeRequestSignatureInvalid,
		}, false
	}
	decodedExpectedSignature, err := hex.DecodeString(expectedSignature)
	if err != nil {
		return CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "request signature is invalid",
			DenialCode:   DenialCodeRequestSignatureInvalid,
		}, false
	}
	if !hmac.Equal(decodedExpectedSignature, decodedRequestSignature) {
		return CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "request signature is invalid",
			DenialCode:   DenialCodeRequestSignatureInvalid,
		}, false
	}

	if nonceDenial := server.recordAuthNonce(controlSessionID, requestNonce); nonceDenial != nil {
		return *nonceDenial, false
	}

	return CapabilityResponse{}, true
}

func peerIdentityFromContext(ctx context.Context) (peerIdentity, bool) {
	peerCreds, ok := ctx.Value(peerIdentityContextKey).(peerIdentity)
	return peerCreds, ok
}

func signedRequestHTTPStatus(denialCode string) int {
	switch denialCode {
	case DenialCodeRequestSignatureMissing, DenialCodeRequestSignatureInvalid, DenialCodeRequestTimestampInvalid, DenialCodeRequestNonceReplayDetected, DenialCodeControlSessionBindingInvalid:
		return http.StatusUnauthorized
	case DenialCodeReplayStateSaturated:
		return http.StatusTooManyRequests
	case DenialCodeAuditUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusBadRequest
	}
}

func signRequest(sessionMACKey string, method string, path string, controlSessionID string, requestTimestamp string, requestNonce string, requestBodyBytes []byte) string {
	bodyHash := sha256.Sum256(requestBodyBytes)
	signingPayload := strings.Join([]string{
		method,
		path,
		controlSessionID,
		requestTimestamp,
		requestNonce,
		hex.EncodeToString(bodyHash[:]),
	}, "\n")

	mac := hmac.New(sha256.New, []byte(sessionMACKey))
	_, _ = mac.Write([]byte(signingPayload))
	return hex.EncodeToString(mac.Sum(nil))
}
