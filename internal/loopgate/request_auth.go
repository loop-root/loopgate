package loopgate

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"strings"
	"time"

	"loopgate/internal/secrets"
)

type authDeniedAuditOptions struct {
	authKind           string
	controlSessionID   string
	actorLabel         string
	clientSessionLabel string
	tenantID           string
	userID             string
	requestPeer        *peerIdentity
}

type signedControlPlaneHeaders struct {
	ControlSessionID string
	// RawRequestTimestamp is the exact trimmed header value that participates in
	// the signed request payload. Do not rebuild it from ParsedRequestTimestampUTC.
	RawRequestTimestamp string
	// ParsedRequestTimestampUTC is the validated UTC timestamp parsed from
	// RawRequestTimestamp for skew enforcement and any future time comparisons.
	ParsedRequestTimestampUTC time.Time
	RequestNonce              string
	RequestSignature          string
}

// parseCapabilityTokenAuthorizationHeader accepts RFC-agnostic but explicit
// bearer forms used by local clients. It tolerates mixed-case scheme names and
// arbitrary ASCII whitespace between scheme and token, but it rejects malformed
// "Bearer..." headers before token lookup so operators get a precise denial.
func parseCapabilityTokenAuthorizationHeader(authorizationHeader string) (string, controlapipkg.CapabilityResponse, bool) {
	trimmedAuthorizationHeader := strings.TrimSpace(authorizationHeader)
	if trimmedAuthorizationHeader == "" {
		return "", controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "missing capability token",
			DenialCode:   controlapipkg.DenialCodeCapabilityTokenMissing,
		}, false
	}

	normalizedAuthorizationHeader := strings.ToLower(trimmedAuthorizationHeader)
	if strings.HasPrefix(normalizedAuthorizationHeader, "bearer") &&
		!strings.HasPrefix(normalizedAuthorizationHeader, "bearer ") &&
		!strings.HasPrefix(normalizedAuthorizationHeader, "bearer\t") {
		return "", controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "malformed capability token authorization header",
			DenialCode:   controlapipkg.DenialCodeCapabilityTokenInvalid,
		}, false
	}

	authorizationFields := strings.Fields(trimmedAuthorizationHeader)
	if len(authorizationFields) != 2 {
		if len(authorizationFields) > 0 && strings.EqualFold(authorizationFields[0], "Bearer") {
			return "", controlapipkg.CapabilityResponse{
				Status:       controlapipkg.ResponseStatusDenied,
				DenialReason: "malformed capability token authorization header",
				DenialCode:   controlapipkg.DenialCodeCapabilityTokenInvalid,
			}, false
		}
		return "", controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "missing capability token",
			DenialCode:   controlapipkg.DenialCodeCapabilityTokenMissing,
		}, false
	}
	if !strings.EqualFold(authorizationFields[0], "Bearer") {
		return "", controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "missing capability token",
			DenialCode:   controlapipkg.DenialCodeCapabilityTokenMissing,
		}, false
	}

	return authorizationFields[1], controlapipkg.CapabilityResponse{}, true
}

func (server *Server) writeAuditedAuthDenial(writer http.ResponseWriter, request *http.Request, denial controlapipkg.CapabilityResponse, options authDeniedAuditOptions) bool {
	auditData := map[string]interface{}{
		"auth_kind":      options.authKind,
		"denial_code":    denial.DenialCode,
		"reason":         secrets.RedactText(denial.DenialReason),
		"request_method": request.Method,
		"request_path":   request.URL.Path,
	}
	if strings.TrimSpace(options.controlSessionID) != "" {
		auditData["control_session_id"] = options.controlSessionID
	}
	if strings.TrimSpace(options.actorLabel) != "" {
		auditData["actor_label"] = options.actorLabel
	}
	if strings.TrimSpace(options.clientSessionLabel) != "" {
		auditData["client_session_label"] = options.clientSessionLabel
	}
	if strings.TrimSpace(options.tenantID) != "" {
		auditData["tenant_id"] = options.tenantID
	}
	if strings.TrimSpace(options.userID) != "" {
		auditData["user_id"] = options.userID
	}
	if options.requestPeer != nil {
		auditData["peer_uid"] = options.requestPeer.UID
		auditData["peer_pid"] = options.requestPeer.PID
		auditData["peer_epid"] = options.requestPeer.EPID
	}

	if err := server.logEvent("auth.denied", options.controlSessionID, auditData); err != nil {
		server.writeJSON(writer, http.StatusServiceUnavailable, auditUnavailableCapabilityResponse(""))
		return false
	}
	server.writeJSON(writer, httpStatusForResponse(denial), denial)
	return false
}

func (server *Server) authenticate(writer http.ResponseWriter, request *http.Request) (capabilityToken, bool) {
	requestPeerIdentity, ok := peerIdentityFromContext(request.Context())
	if !ok {
		server.writeAuditedAuthDenial(writer, request, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "missing authenticated peer identity",
			DenialCode:   controlapipkg.DenialCodeCapabilityTokenInvalid,
		}, authDeniedAuditOptions{authKind: "capability_token"})
		return capabilityToken{}, false
	}

	tokenString, denialResponse, ok := parseCapabilityTokenAuthorizationHeader(request.Header.Get("Authorization"))
	if !ok {
		server.writeAuditedAuthDenial(writer, request, denialResponse, authDeniedAuditOptions{
			authKind:    "capability_token",
			requestPeer: &requestPeerIdentity,
		})
		return capabilityToken{}, false
	}

	// Take a consistent now snapshot and perform all expiry checks inside a
	// single lock acquisition to eliminate the TOCTOU window between reading
	// token/session state and calling now() on the outside.
	server.mu.Lock()
	nowUTC := server.now().UTC()
	tokenClaims, found := server.sessionState.tokens[tokenString]
	var activeSession controlSession
	var sessionFound bool
	var tokenExpired, sessionExpired bool
	if found {
		activeSession, sessionFound = server.sessionState.sessions[tokenClaims.ControlSessionID]
		tokenExpired = nowUTC.After(tokenClaims.ExpiresAt)
		sessionExpired = sessionFound && nowUTC.After(activeSession.ExpiresAt)
		if tokenExpired {
			delete(server.sessionState.tokens, tokenString)
		}
		if sessionExpired {
			delete(server.sessionState.sessions, tokenClaims.ControlSessionID)
			delete(server.sessionState.tokens, tokenString)
		}
	}
	server.mu.Unlock()

	if !found {
		server.writeAuditedAuthDenial(writer, request, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "invalid capability token",
			DenialCode:   controlapipkg.DenialCodeCapabilityTokenInvalid,
		}, authDeniedAuditOptions{
			authKind:    "capability_token",
			requestPeer: &requestPeerIdentity,
		})
		return capabilityToken{}, false
	}
	if tokenExpired {
		server.writeAuditedAuthDenial(writer, request, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "expired capability token",
			DenialCode:   controlapipkg.DenialCodeCapabilityTokenExpired,
		}, authDeniedAuditOptions{
			authKind:           "capability_token",
			controlSessionID:   tokenClaims.ControlSessionID,
			actorLabel:         tokenClaims.ActorLabel,
			clientSessionLabel: tokenClaims.ClientSessionLabel,
			tenantID:           tokenClaims.TenantID,
			userID:             tokenClaims.UserID,
			requestPeer:        &requestPeerIdentity,
		})
		return capabilityToken{}, false
	}
	if sessionExpired {
		server.writeAuditedAuthDenial(writer, request, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "expired capability token",
			DenialCode:   controlapipkg.DenialCodeCapabilityTokenExpired,
		}, authDeniedAuditOptions{
			authKind:           "capability_token",
			controlSessionID:   tokenClaims.ControlSessionID,
			actorLabel:         tokenClaims.ActorLabel,
			clientSessionLabel: tokenClaims.ClientSessionLabel,
			tenantID:           tokenClaims.TenantID,
			userID:             tokenClaims.UserID,
			requestPeer:        &requestPeerIdentity,
		})
		return capabilityToken{}, false
	}
	if !sessionFound {
		server.writeAuditedAuthDenial(writer, request, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "invalid capability token",
			DenialCode:   controlapipkg.DenialCodeCapabilityTokenInvalid,
		}, authDeniedAuditOptions{
			authKind:           "capability_token",
			controlSessionID:   tokenClaims.ControlSessionID,
			actorLabel:         tokenClaims.ActorLabel,
			clientSessionLabel: tokenClaims.ClientSessionLabel,
			tenantID:           tokenClaims.TenantID,
			userID:             tokenClaims.UserID,
			requestPeer:        &requestPeerIdentity,
		})
		return capabilityToken{}, false
	}
	if tokenClaims.PeerIdentity != requestPeerIdentity {
		server.writeAuditedAuthDenial(writer, request, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "capability token peer binding mismatch",
			DenialCode:   controlapipkg.DenialCodeCapabilityTokenInvalid,
		}, authDeniedAuditOptions{
			authKind:           "capability_token",
			controlSessionID:   tokenClaims.ControlSessionID,
			actorLabel:         tokenClaims.ActorLabel,
			clientSessionLabel: tokenClaims.ClientSessionLabel,
			tenantID:           tokenClaims.TenantID,
			userID:             tokenClaims.UserID,
			requestPeer:        &requestPeerIdentity,
		})
		return capabilityToken{}, false
	}

	return tokenClaims, true
}

// parseSignedControlPlaneHeaders checks signed-request headers and timestamp skew.
// It does not verify the HMAC. Callers supply expectedControlSessionID (for
// example, a scoped worker session id from a compatibility table); those ids
// are not necessarily rows in server.sessionState.sessions.
func (server *Server) parseSignedControlPlaneHeaders(request *http.Request, expectedControlSessionID string) (signedControlPlaneHeaders, controlapipkg.CapabilityResponse, bool) {
	headers := signedControlPlaneHeaders{
		ControlSessionID:    strings.TrimSpace(request.Header.Get("X-Loopgate-Control-Session")),
		RawRequestTimestamp: strings.TrimSpace(request.Header.Get("X-Loopgate-Request-Timestamp")),
		RequestNonce:        strings.TrimSpace(request.Header.Get("X-Loopgate-Request-Nonce")),
		RequestSignature:    strings.TrimSpace(request.Header.Get("X-Loopgate-Request-Signature")),
	}

	if headers.ControlSessionID == "" || headers.RawRequestTimestamp == "" || headers.RequestNonce == "" || headers.RequestSignature == "" {
		return signedControlPlaneHeaders{}, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "signed control-plane headers are required",
			DenialCode:   controlapipkg.DenialCodeRequestSignatureMissing,
		}, false
	}
	if headers.ControlSessionID != expectedControlSessionID {
		return signedControlPlaneHeaders{}, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "control session binding is invalid",
			DenialCode:   controlapipkg.DenialCodeControlSessionBindingInvalid,
		}, false
	}

	parsedTimestamp, err := time.Parse(time.RFC3339Nano, headers.RawRequestTimestamp)
	if err != nil {
		return signedControlPlaneHeaders{}, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "request timestamp is invalid",
			DenialCode:   controlapipkg.DenialCodeRequestTimestampInvalid,
		}, false
	}
	nowUTC := server.now().UTC()
	if parsedTimestamp.Before(nowUTC.Add(-requestSignatureSkew)) || parsedTimestamp.After(nowUTC.Add(requestSignatureSkew)) {
		return signedControlPlaneHeaders{}, controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "request timestamp is outside the allowed skew window",
			DenialCode:   controlapipkg.DenialCodeRequestTimestampInvalid,
		}, false
	}
	headers.ParsedRequestTimestampUTC = parsedTimestamp.UTC()

	return headers, controlapipkg.CapabilityResponse{}, true
}

func (server *Server) verifySignedRequest(request *http.Request, requestBodyBytes []byte, expectedControlSessionID string) (controlapipkg.CapabilityResponse, bool) {
	headers, denial, ok := server.parseSignedControlPlaneHeaders(request, expectedControlSessionID)
	if !ok {
		return denial, false
	}

	server.mu.Lock()
	server.pruneExpiredLocked()
	activeSession, found := server.sessionState.sessions[headers.ControlSessionID]
	sessionMACRotationMaster := append([]byte(nil), server.sessionMACRotationMaster...)
	server.mu.Unlock()
	if !found || strings.TrimSpace(activeSession.SessionMACKey) == "" {
		return controlapipkg.CapabilityResponse{
			Status:       controlapipkg.ResponseStatusDenied,
			DenialReason: "control session binding is invalid",
			DenialCode:   controlapipkg.DenialCodeControlSessionBindingInvalid,
		}, false
	}

	if len(sessionMACRotationMaster) > 0 {
		return server.verifySignedRequestAgainstRotatingSessionMAC(request, requestBodyBytes, headers, sessionMACRotationMaster)
	}
	return server.verifySignedRequestWithMACKey(request, requestBodyBytes, headers, activeSession.SessionMACKey)
}

func invalidRequestSignatureResponse() controlapipkg.CapabilityResponse {
	return controlapipkg.CapabilityResponse{
		Status:       controlapipkg.ResponseStatusDenied,
		DenialReason: "request signature is invalid",
		DenialCode:   controlapipkg.DenialCodeRequestSignatureInvalid,
	}
}

func (server *Server) verifySignedRequestAgainstMACKeys(request *http.Request, requestBodyBytes []byte, headers signedControlPlaneHeaders, candidateMACKeys []string) (controlapipkg.CapabilityResponse, bool) {
	for _, sessionMACKey := range candidateMACKeys {
		if sessionMACKey == "" {
			continue
		}
		if !requestSignatureBytesMatchMACKey(headers.RequestSignature, request.Method, request.URL.Path, headers.ControlSessionID, headers.RawRequestTimestamp, headers.RequestNonce, requestBodyBytes, sessionMACKey) {
			continue
		}
		if nonceDenial := server.recordAuthNonce(headers.ControlSessionID, headers.RequestNonce); nonceDenial != nil {
			return *nonceDenial, false
		}
		return controlapipkg.CapabilityResponse{}, true
	}
	return invalidRequestSignatureResponse(), false
}

func (server *Server) verifySignedRequestWithMACKey(request *http.Request, requestBodyBytes []byte, headers signedControlPlaneHeaders, sessionMACKey string) (controlapipkg.CapabilityResponse, bool) {
	return server.verifySignedRequestAgainstMACKeys(request, requestBodyBytes, headers, []string{sessionMACKey})
}

func peerIdentityFromContext(ctx context.Context) (peerIdentity, bool) {
	peerCreds, ok := ctx.Value(peerIdentityContextKey).(peerIdentity)
	return peerCreds, ok
}

func signedRequestHTTPStatus(denialCode string) int {
	switch denialCode {
	case controlapipkg.DenialCodeRequestSignatureMissing, controlapipkg.DenialCodeRequestSignatureInvalid, controlapipkg.DenialCodeRequestTimestampInvalid, controlapipkg.DenialCodeRequestNonceReplayDetected, controlapipkg.DenialCodeControlSessionBindingInvalid:
		return http.StatusUnauthorized
	case controlapipkg.DenialCodeReplayStateSaturated:
		return http.StatusTooManyRequests
	case controlapipkg.DenialCodeAuditUnavailable:
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
