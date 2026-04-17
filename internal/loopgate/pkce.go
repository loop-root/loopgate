package loopgate

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const pkceSessionTTL = 10 * time.Minute

type pendingPKCESession struct {
	State         string
	ConnectionKey string
	CodeVerifier  string
	CreatedAt     time.Time
	ExpiresAt     time.Time
}

type oauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

func (server *Server) startPKCEConnection(ctx context.Context, tokenClaims capabilityToken, request PKCEStartRequest) (PKCEStartResponse, error) {
	if err := request.Validate(); err != nil {
		return PKCEStartResponse{}, err
	}
	configuredConnectionDefinition, connectionKey, err := server.lookupConfiguredConnection(request.Provider, request.Subject, GrantTypePKCE)
	if err != nil {
		return PKCEStartResponse{}, err
	}

	codeVerifier, err := randomHex(32)
	if err != nil {
		return PKCEStartResponse{}, fmt.Errorf("generate pkce code verifier: %w", err)
	}
	state, err := randomHex(16)
	if err != nil {
		return PKCEStartResponse{}, fmt.Errorf("generate pkce state: %w", err)
	}

	server.pkceRuntime.mu.Lock()
	server.pruneExpiredPKCESessionsLocked()
	for _, existingSession := range server.pkceRuntime.sessions {
		if existingSession.ConnectionKey == connectionKey && server.now().UTC().Before(existingSession.ExpiresAt) {
			server.pkceRuntime.mu.Unlock()
			return PKCEStartResponse{}, fmt.Errorf("pkce session already pending for provider %q subject %q", request.Provider, request.Subject)
		}
	}
	expiresAt := server.now().UTC().Add(pkceSessionTTL)
	server.pkceRuntime.sessions[state] = pendingPKCESession{
		State:         state,
		ConnectionKey: connectionKey,
		CodeVerifier:  codeVerifier,
		CreatedAt:     server.now().UTC(),
		ExpiresAt:     expiresAt,
	}
	server.pkceRuntime.mu.Unlock()

	authorizationURL := *configuredConnectionDefinition.AuthorizationURL
	queryValues := authorizationURL.Query()
	queryValues.Set("response_type", "code")
	queryValues.Set("client_id", configuredConnectionDefinition.ClientID)
	queryValues.Set("redirect_uri", configuredConnectionDefinition.RedirectURL)
	if len(configuredConnectionDefinition.Registration.Scopes) > 0 {
		queryValues.Set("scope", strings.Join(configuredConnectionDefinition.Registration.Scopes, " "))
	}
	queryValues.Set("state", state)
	queryValues.Set("code_challenge_method", "S256")
	queryValues.Set("code_challenge", pkceCodeChallenge(codeVerifier))
	authorizationURL.RawQuery = queryValues.Encode()

	if err := server.logEvent("connection.pkce_started", tokenClaims.ControlSessionID, map[string]interface{}{
		"provider":             request.Provider,
		"subject":              request.Subject,
		"grant_type":           GrantTypePKCE,
		"control_session_id":   tokenClaims.ControlSessionID,
		"actor_label":          tokenClaims.ActorLabel,
		"client_session_label": tokenClaims.ClientSessionLabel,
		"expires_at_utc":       expiresAt.Format(time.RFC3339Nano),
	}); err != nil {
		server.pkceRuntime.mu.Lock()
		delete(server.pkceRuntime.sessions, state)
		server.pkceRuntime.mu.Unlock()
		return PKCEStartResponse{}, err
	}

	return PKCEStartResponse{
		Provider:         request.Provider,
		Subject:          request.Subject,
		AuthorizationURL: authorizationURL.String(),
		State:            state,
		ExpiresAtUTC:     expiresAt.Format(time.RFC3339Nano),
	}, nil
}

func (server *Server) completePKCEConnection(ctx context.Context, tokenClaims capabilityToken, request PKCECompleteRequest) (ConnectionStatus, error) {
	if err := request.Validate(); err != nil {
		return ConnectionStatus{}, err
	}
	configuredConnectionDefinition, connectionKey, err := server.lookupConfiguredConnection(request.Provider, request.Subject, GrantTypePKCE)
	if err != nil {
		return ConnectionStatus{}, err
	}

	server.pkceRuntime.mu.Lock()
	server.pruneExpiredPKCESessionsLocked()
	pendingSession, found := server.pkceRuntime.sessions[request.State]
	server.pkceRuntime.mu.Unlock()
	if !found {
		return ConnectionStatus{}, fmt.Errorf("pkce session not found for state %q", request.State)
	}
	if pendingSession.ConnectionKey != connectionKey {
		return ConnectionStatus{}, fmt.Errorf("pkce session connection mismatch")
	}

	tokenResponse, err := server.exchangePKCEAuthorizationCode(ctx, configuredConnectionDefinition, pendingSession.CodeVerifier, request.Code)
	if err != nil {
		return ConnectionStatus{}, err
	}
	if strings.TrimSpace(tokenResponse.RefreshToken) == "" {
		return ConnectionStatus{}, fmt.Errorf("pkce token response did not include refresh_token")
	}

	connectionStatus, err := server.UpsertConnectionCredential(ctx, configuredConnectionDefinition.Registration, []byte(tokenResponse.RefreshToken))
	if err != nil {
		return ConnectionStatus{}, err
	}
	expiresAt := server.now().UTC().Add(time.Duration(defaultInt(tokenResponse.ExpiresIn, 300)) * time.Second)
	server.providerRuntime.mu.Lock()
	server.providerRuntime.tokens[connectionKey] = providerAccessToken{
		ConnectionKey: connectionKey,
		AccessToken:   tokenResponse.AccessToken,
		TokenType:     defaultString(tokenResponse.TokenType, "Bearer"),
		ExpiresAt:     expiresAt,
	}
	server.providerRuntime.mu.Unlock()

	server.pkceRuntime.mu.Lock()
	delete(server.pkceRuntime.sessions, request.State)
	server.pkceRuntime.mu.Unlock()

	if err := server.logEvent("connection.pkce_completed", tokenClaims.ControlSessionID, map[string]interface{}{
		"provider":             request.Provider,
		"subject":              request.Subject,
		"grant_type":           GrantTypePKCE,
		"control_session_id":   tokenClaims.ControlSessionID,
		"actor_label":          tokenClaims.ActorLabel,
		"client_session_label": tokenClaims.ClientSessionLabel,
		"status":               connectionStatus.Status,
		"expires_at_utc":       expiresAt.Format(time.RFC3339Nano),
	}); err != nil {
		return ConnectionStatus{}, err
	}

	return connectionStatus, nil
}

func (server *Server) exchangePKCEAuthorizationCode(ctx context.Context, configuredConnectionDefinition configuredConnection, codeVerifier string, code string) (oauthTokenResponse, error) {
	formValues := url.Values{}
	formValues.Set("grant_type", GrantTypeAuthorizationCode)
	formValues.Set("client_id", configuredConnectionDefinition.ClientID)
	formValues.Set("code", code)
	formValues.Set("code_verifier", codeVerifier)
	formValues.Set("redirect_uri", configuredConnectionDefinition.RedirectURL)
	return server.exchangeOAuthToken(ctx, configuredConnectionDefinition, formValues)
}

func (server *Server) refreshPKCEAccessToken(ctx context.Context, configuredConnectionDefinition configuredConnection, rawRefreshToken []byte) (oauthTokenResponse, error) {
	formValues := url.Values{}
	formValues.Set("grant_type", "refresh_token")
	formValues.Set("client_id", configuredConnectionDefinition.ClientID)
	formValues.Set("refresh_token", string(rawRefreshToken))
	return server.exchangeOAuthToken(ctx, configuredConnectionDefinition, formValues)
}

func (server *Server) exchangeOAuthToken(ctx context.Context, configuredConnectionDefinition configuredConnection, formValues url.Values) (oauthTokenResponse, error) {
	policyRuntime := server.currentPolicyRuntime()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, configuredConnectionDefinition.TokenURL.String(), strings.NewReader(formValues.Encode()))
	if err != nil {
		return oauthTokenResponse{}, fmt.Errorf("create oauth token request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")

	response, err := policyRuntime.httpClient.Do(request)
	if err != nil {
		return oauthTokenResponse{}, fmt.Errorf("exchange oauth token: %w", err)
	}
	defer response.Body.Close()

	rawBodyBytes, err := io.ReadAll(io.LimitReader(response.Body, maxTokenBodyBytes))
	if err != nil {
		return oauthTokenResponse{}, fmt.Errorf("read oauth token response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return oauthTokenResponse{}, fmt.Errorf("token endpoint returned status %d", response.StatusCode)
	}

	var parsedTokenResponse oauthTokenResponse
	if err := json.Unmarshal(rawBodyBytes, &parsedTokenResponse); err != nil {
		return oauthTokenResponse{}, fmt.Errorf("decode oauth token response: %w", err)
	}
	if strings.TrimSpace(parsedTokenResponse.AccessToken) == "" {
		return oauthTokenResponse{}, fmt.Errorf("oauth token response did not include access_token")
	}
	if !strings.EqualFold(defaultString(parsedTokenResponse.TokenType, "Bearer"), "Bearer") {
		return oauthTokenResponse{}, fmt.Errorf("unsupported token_type %q", parsedTokenResponse.TokenType)
	}
	return parsedTokenResponse, nil
}

func (server *Server) pruneExpiredPKCESessionsLocked() {
	nowUTC := server.now().UTC()
	for state, pendingSession := range server.pkceRuntime.sessions {
		if nowUTC.After(pendingSession.ExpiresAt) {
			delete(server.pkceRuntime.sessions, state)
		}
	}
}

func (server *Server) lookupConfiguredConnection(provider string, subject string, expectedGrantType string) (configuredConnection, string, error) {
	connectionKey := connectionRecordKey(strings.TrimSpace(provider), strings.TrimSpace(subject))
	configuredConnectionDefinition, found := server.providerRuntime.configuredConnections[connectionKey]
	if !found {
		return configuredConnection{}, "", fmt.Errorf("configured connection not found for provider %q subject %q", provider, subject)
	}
	if configuredConnectionDefinition.Registration.GrantType != expectedGrantType {
		return configuredConnection{}, "", fmt.Errorf("configured connection grant_type %q does not match expected %q", configuredConnectionDefinition.Registration.GrantType, expectedGrantType)
	}
	return configuredConnectionDefinition, connectionKey, nil
}

func pkceCodeChallenge(codeVerifier string) string {
	challengeSum := sha256.Sum256([]byte(codeVerifier))
	return base64.RawURLEncoding.EncodeToString(challengeSum[:])
}

func defaultString(rawValue string, fallback string) string {
	if strings.TrimSpace(rawValue) == "" {
		return fallback
	}
	return rawValue
}

func defaultInt(rawValue int, fallback int) int {
	if rawValue <= 0 {
		return fallback
	}
	return rawValue
}
