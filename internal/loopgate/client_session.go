package loopgate

import (
	"context"
	"fmt"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"strings"
	"time"
)

// SessionMACKeys returns previous, current, and next 12-hour epoch MAC material for this control session.
// Same transport requirements as Status (Bearer token + signed GET).
func (client *Client) SessionMACKeys(ctx context.Context) (controlapipkg.SessionMACKeysResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.SessionMACKeysResponse{}, err
	}
	var response controlapipkg.SessionMACKeysResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/session/mac-keys", capabilityToken, nil, &response, nil); err != nil {
		return controlapipkg.SessionMACKeysResponse{}, err
	}
	return response, nil
}

// RefreshSessionMACKeyFromServer replaces the in-memory session_mac_key using the server's current
// rotation slot (GET /v1/session/mac-keys). Call after session open or when signatures fail across epochs.
func (client *Client) RefreshSessionMACKeyFromServer(ctx context.Context) error {
	keys, err := client.SessionMACKeys(ctx)
	if err != nil {
		return fmt.Errorf("refresh session mac key: %w", err)
	}
	derived := strings.TrimSpace(keys.Current.DerivedSessionMACKey)
	if derived == "" {
		return fmt.Errorf("refresh session mac key: empty current.derived_session_mac_key from server")
	}
	client.mu.Lock()
	client.sessionMACKey = derived
	client.mu.Unlock()
	return nil
}

func (client *Client) ConfigureSession(actor string, sessionID string, requestedCapabilities []string) {
	client.mu.Lock()
	defer client.mu.Unlock()

	if client.actor != actor || client.clientSessionID != sessionID || !sameStrings(client.requestedCapabilities, requestedCapabilities) {
		client.resetSessionCredentialsLocked()
	}
	client.actor = actor
	client.clientSessionID = sessionID
	client.requestedCapabilities = append([]string(nil), requestedCapabilities...)
}

func (client *Client) CloseSession(ctx context.Context) error {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return err
	}

	var response controlapipkg.CloseSessionResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/session/close", capabilityToken, nil, &response, nil); err != nil {
		return err
	}

	client.mu.Lock()
	client.resetSessionCredentialsLocked()
	client.mu.Unlock()
	return nil
}

// SetWorkspaceID sets the workspace identity hint that will be included in the
// session open request to Loopgate. The server derives the authoritative
// workspace binding from repoRoot and rejects mismatches, so this remains a
// compatibility field rather than a client-owned authority input.
func (client *Client) SetWorkspaceID(workspaceID string) {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.workspaceID = workspaceID
}

// SetOperatorMountPaths includes operator-approved host directories on the next control-session
// open request. Loopgate only honors these bindings when the server pins the expected operator
// executable at session open; export still requires a separate write grant for the matched root.
func (client *Client) SetOperatorMountPaths(operatorMountPaths []string, primaryOperatorMountPath string) {
	client.mu.Lock()
	defer client.mu.Unlock()

	if !sameStrings(client.operatorMountPaths, operatorMountPaths) || client.primaryOperatorMountPath != primaryOperatorMountPath {
		client.resetSessionCredentialsLocked()
	}
	client.operatorMountPaths = append([]string(nil), operatorMountPaths...)
	client.primaryOperatorMountPath = primaryOperatorMountPath
}

// CloseIdleConnections releases idle HTTP connections held by the local client.
func (client *Client) CloseIdleConnections() {
	if client.httpClient != nil {
		client.httpClient.CloseIdleConnections()
	}
}

func (client *Client) UpdateDelegatedSession(delegatedSession DelegatedSessionConfig) error {
	if err := validateDelegatedSessionConfig(delegatedSession, time.Now()); err != nil {
		return err
	}

	client.mu.Lock()
	defer client.mu.Unlock()

	client.delegatedSession = true
	client.controlSessionID = strings.TrimSpace(delegatedSession.ControlSessionID)
	client.capabilityToken = strings.TrimSpace(delegatedSession.CapabilityToken)
	client.approvalToken = strings.TrimSpace(delegatedSession.ApprovalToken)
	client.sessionMACKey = strings.TrimSpace(delegatedSession.SessionMACKey)
	client.tokenExpiresAt = delegatedSession.ExpiresAt.UTC()
	client.approvalDecisionNonce = make(map[string]string)
	client.approvalManifestSHA256 = make(map[string]string)
	return nil
}

func (client *Client) DelegatedSessionHealth(now time.Time) (DelegatedSessionState, time.Time, bool) {
	client.mu.Lock()
	defer client.mu.Unlock()

	if !client.delegatedSession {
		return DelegatedSessionStateRefreshRequired, time.Time{}, false
	}
	return EvaluateDelegatedSessionState(now, client.tokenExpiresAt), client.tokenExpiresAt, true
}

func (client *Client) ExecuteCapability(ctx context.Context, capabilityRequest controlapipkg.CapabilityRequest) (controlapipkg.CapabilityResponse, error) {
	token, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return controlapipkg.CapabilityResponse{}, err
	}

	client.mu.Lock()
	if strings.TrimSpace(capabilityRequest.Actor) == "" {
		capabilityRequest.Actor = client.actor
	}
	if strings.TrimSpace(capabilityRequest.SessionID) == "" {
		capabilityRequest.SessionID = client.clientSessionID
	}
	client.mu.Unlock()

	var response controlapipkg.CapabilityResponse
	if err := client.doCapabilityJSON(ctx, client.defaultRequestTimeout, http.MethodPost, "/v1/capabilities/execute", token, capabilityRequest, &response, nil); err != nil {
		return controlapipkg.CapabilityResponse{}, err
	}
	client.cacheApprovalDecisionNonce(response)
	return response, nil
}

func (client *Client) DecideApproval(ctx context.Context, approvalRequestID string, approved bool) (controlapipkg.CapabilityResponse, error) {
	return client.DecideApprovalWithReason(ctx, approvalRequestID, approved, "")
}

func (client *Client) DecideApprovalWithReason(ctx context.Context, approvalRequestID string, approved bool, reason string) (controlapipkg.CapabilityResponse, error) {
	approvalToken, err := client.ensureApprovalToken(ctx)
	if err != nil {
		return controlapipkg.CapabilityResponse{}, err
	}
	decisionNonce, err := client.lookupApprovalDecisionNonce(approvalRequestID)
	if err != nil {
		return controlapipkg.CapabilityResponse{}, err
	}

	// Include the manifest SHA256 cached from the pending approval response. This binds the
	// decision to the exact method, path, and request body that was approved (AMP RFC 0005 §6).
	client.mu.Lock()
	manifestSHA256 := client.approvalManifestSHA256[approvalRequestID]
	client.mu.Unlock()

	var response controlapipkg.CapabilityResponse
	path := fmt.Sprintf("/v1/approvals/%s/decision", approvalRequestID)
	if err := client.doCapabilityJSON(ctx, client.defaultRequestTimeout, http.MethodPost, path, "", controlapipkg.ApprovalDecisionRequest{
		Approved:               approved,
		Reason:                 strings.TrimSpace(reason),
		DecisionNonce:          decisionNonce,
		ApprovalManifestSHA256: manifestSHA256,
	}, &response, map[string]string{
		"X-Loopgate-Approval-Token": approvalToken,
	}); err != nil {
		return controlapipkg.CapabilityResponse{}, err
	}
	client.mu.Lock()
	delete(client.approvalDecisionNonce, approvalRequestID)
	delete(client.approvalManifestSHA256, approvalRequestID)
	client.mu.Unlock()
	return response, nil
}

func (client *Client) ensureApprovalToken(ctx context.Context) (string, error) {
	if _, err := client.ensureCapabilityToken(ctx); err != nil {
		return "", err
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if strings.TrimSpace(client.approvalToken) == "" {
		return "", fmt.Errorf("loopgate approval token is not configured")
	}
	return client.approvalToken, nil
}

func (client *Client) ensureCapabilityToken(ctx context.Context) (string, error) {
	client.mu.Lock()
	nowUTC := time.Now().UTC()
	if client.delegatedSession {
		if strings.TrimSpace(client.capabilityToken) != "" && EvaluateDelegatedSessionState(nowUTC, client.tokenExpiresAt) != DelegatedSessionStateRefreshRequired {
			token := client.capabilityToken
			client.mu.Unlock()
			return token, nil
		}
		client.mu.Unlock()
		return "", ErrDelegatedSessionRefreshRequired
	}
	if client.capabilityToken != "" && nowUTC.Before(client.tokenExpiresAt.Add(-30*time.Second)) {
		token := client.capabilityToken
		client.mu.Unlock()
		return token, nil
	}
	openRequest := controlapipkg.OpenSessionRequest{
		Actor:                    client.actor,
		SessionID:                client.clientSessionID,
		RequestedCapabilities:    append([]string(nil), client.requestedCapabilities...),
		WorkspaceID:              client.workspaceID,
		OperatorMountPaths:       append([]string(nil), client.operatorMountPaths...),
		PrimaryOperatorMountPath: client.primaryOperatorMountPath,
	}
	client.mu.Unlock()

	var openResponse controlapipkg.OpenSessionResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/session/open", "", openRequest, &openResponse, nil); err != nil {
		return "", err
	}

	expiresAt, err := time.Parse(time.RFC3339Nano, openResponse.ExpiresAtUTC)
	if err != nil {
		return "", fmt.Errorf("parse loopgate token expiry: %w", err)
	}

	client.mu.Lock()
	client.controlSessionID = openResponse.ControlSessionID
	client.capabilityToken = openResponse.CapabilityToken
	client.approvalToken = openResponse.ApprovalToken
	client.sessionMACKey = openResponse.SessionMACKey
	client.tokenExpiresAt = expiresAt
	token := client.capabilityToken
	client.mu.Unlock()
	return token, nil
}

func (client *Client) resetSessionCredentialsLocked() {
	client.delegatedSession = false
	client.controlSessionID = ""
	client.capabilityToken = ""
	client.approvalToken = ""
	client.approvalDecisionNonce = make(map[string]string)
	client.approvalManifestSHA256 = make(map[string]string)
	client.sessionMACKey = ""
	client.tokenExpiresAt = time.Time{}
}

func (client *Client) cacheApprovalDecisionNonce(capabilityResponse controlapipkg.CapabilityResponse) {
	if !capabilityResponse.ApprovalRequired || strings.TrimSpace(capabilityResponse.ApprovalRequestID) == "" {
		return
	}
	// Cache the manifest SHA256 alongside the decision nonce so DecideApproval can include it.
	// Prefer the explicit top-level field, but fall back to metadata to tolerate older or mixed
	// response shapes while the approval-manifest path settles in.
	manifestSHA256 := strings.TrimSpace(capabilityResponse.ApprovalManifestSHA256)
	if manifestSHA256 == "" {
		if rawManifestSHA256, found := capabilityResponse.Metadata["approval_manifest_sha256"]; found {
			if metadataManifestSHA256, ok := rawManifestSHA256.(string); ok {
				manifestSHA256 = strings.TrimSpace(metadataManifestSHA256)
			}
		}
	}
	if manifestSHA256 != "" {
		client.mu.Lock()
		client.approvalManifestSHA256[capabilityResponse.ApprovalRequestID] = manifestSHA256
		client.mu.Unlock()
	}
	rawDecisionNonce, found := capabilityResponse.Metadata["approval_decision_nonce"]
	if !found {
		return
	}
	decisionNonce, ok := rawDecisionNonce.(string)
	if !ok || strings.TrimSpace(decisionNonce) == "" {
		return
	}

	client.mu.Lock()
	client.approvalDecisionNonce[capabilityResponse.ApprovalRequestID] = decisionNonce
	client.mu.Unlock()
}

func (client *Client) lookupApprovalDecisionNonce(approvalRequestID string) (string, error) {
	client.mu.Lock()
	defer client.mu.Unlock()

	decisionNonce := strings.TrimSpace(client.approvalDecisionNonce[approvalRequestID])
	if decisionNonce == "" {
		return "", fmt.Errorf("loopgate approval decision nonce is missing for %s", approvalRequestID)
	}
	return decisionNonce, nil
}

func (client *Client) canRetryCapabilityToken(denialCode string) bool {
	if denialCode != controlapipkg.DenialCodeCapabilityTokenInvalid && denialCode != controlapipkg.DenialCodeCapabilityTokenExpired {
		return false
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	return !client.delegatedSession
}

func (client *Client) refreshCapabilityToken(ctx context.Context) (string, error) {
	client.mu.Lock()
	client.resetSessionCredentialsLocked()
	client.mu.Unlock()
	return client.ensureCapabilityToken(ctx)
}
