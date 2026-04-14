package loopgate

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

type providerAccessToken struct {
	ConnectionKey string
	AccessToken   string
	TokenType     string
	ExpiresAt     time.Time
}

const (
	contentOriginRemote          = "remote"
	contentClassStructuredJSON   = "structured_json"
	contentClassMarkdown         = "markdown"
	contentClassHTML             = "html"
	contentTypeApplicationJSON   = "application/json"
	contentTypeTextMarkdown      = "text/markdown"
	contentTypeTextHTML          = "text/html"
	extractorJSONFieldAllowlist  = "json_field_allowlist"
	extractorJSONNestedSelector  = "json_nested_selector"
	extractorJSONObjectList      = "json_object_list_selector"
	extractorMarkdownFrontmatter = "markdown_frontmatter_keys"
	extractorMarkdownSection     = "markdown_section_selector"
	extractorHTMLMetaAllowlist   = "html_meta_allowlist"
	fieldTrustDeterministic      = "deterministic"
)

func (server *Server) executeConfiguredCapability(ctx context.Context, capabilityName string, arguments map[string]string) (string, error) {
	_ = arguments

	configuredCapabilityDefinition, found := server.configuredCapabilities[capabilityName]
	if !found {
		return "", fmt.Errorf("configured capability %q not found", capabilityName)
	}
	configuredConnectionDefinition, found := server.configuredConnections[configuredCapabilityDefinition.ConnectionKey]
	if !found {
		return "", fmt.Errorf("configured connection for capability %q not found", capabilityName)
	}

	accessToken := ""
	if configuredConnectionDefinition.Registration.GrantType != GrantTypePublicRead {
		resolvedAccessToken, err := server.accessTokenForConfiguredConnection(ctx, configuredConnectionDefinition)
		if err != nil {
			return "", err
		}
		accessToken = resolvedAccessToken
	}

	apiURL := *configuredConnectionDefinition.APIBaseURL
	apiURL.Path = path.Join(configuredConnectionDefinition.APIBaseURL.Path, configuredCapabilityDefinition.Path)
	if !strings.HasPrefix(apiURL.Path, "/") {
		apiURL.Path = "/" + apiURL.Path
	}
	if _, allowed := configuredConnectionDefinition.AllowedHosts[apiURL.Hostname()]; !allowed {
		return "", fmt.Errorf("configured capability host %q is not allowed", apiURL.Hostname())
	}

	request, err := http.NewRequestWithContext(ctx, configuredCapabilityDefinition.Method, apiURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("create configured capability request: %w", err)
	}
	request.Header.Set("Accept", contentTypeForConfiguredContentClass(configuredCapabilityDefinition.ContentClass))
	if accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}

	response, err := server.currentPolicyRuntime().httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("execute configured capability request: %w", err)
	}
	defer response.Body.Close()

	rawBodyBytes, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBodyBytes))
	if err != nil {
		return "", fmt.Errorf("read configured capability response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("configured capability request failed with status %d", response.StatusCode)
	}
	if !contentTypeMatchesCapability(response.Header.Get("Content-Type"), configuredCapabilityDefinition) {
		return "", fmt.Errorf("configured capability response content type %q does not match declared content_class %q", response.Header.Get("Content-Type"), configuredCapabilityDefinition.ContentClass)
	}
	return string(rawBodyBytes), nil
}

func (server *Server) accessTokenForConfiguredConnection(ctx context.Context, configuredConnectionDefinition configuredConnection) (string, error) {
	connectionKey := connectionRecordKey(configuredConnectionDefinition.Registration.Provider, configuredConnectionDefinition.Registration.Subject)

	server.providerTokenMu.Lock()
	cachedToken, found := server.providerTokens[connectionKey]
	if found && strings.EqualFold(cachedToken.TokenType, "bearer") && server.now().UTC().Before(cachedToken.ExpiresAt.Add(-30*time.Second)) {
		accessToken := cachedToken.AccessToken
		server.providerTokenMu.Unlock()
		return accessToken, nil
	}
	server.providerTokenMu.Unlock()

	oauthToken, err := server.issueConnectionAccessToken(ctx, configuredConnectionDefinition)
	if err != nil {
		return "", err
	}
	expiresAt := server.now().UTC().Add(time.Duration(defaultInt(oauthToken.ExpiresIn, 300)) * time.Second)
	if err := server.logEvent("connection.token_issued", "", map[string]interface{}{
		"provider":         configuredConnectionDefinition.Registration.Provider,
		"subject":          configuredConnectionDefinition.Registration.Subject,
		"grant_type":       configuredConnectionDefinition.Registration.GrantType,
		"scope_count":      len(configuredConnectionDefinition.Registration.Scopes),
		"expires_at_utc":   expiresAt.Format(time.RFC3339Nano),
		"secure_store_ref": configuredConnectionDefinition.Registration.Credential.ID,
	}); err != nil {
		return "", err
	}

	server.providerTokenMu.Lock()
	server.providerTokens[connectionKey] = providerAccessToken{
		ConnectionKey: connectionKey,
		AccessToken:   oauthToken.AccessToken,
		TokenType:     defaultString(oauthToken.TokenType, "Bearer"),
		ExpiresAt:     expiresAt,
	}
	server.providerTokenMu.Unlock()
	return oauthToken.AccessToken, nil
}

func (server *Server) issueConnectionAccessToken(ctx context.Context, configuredConnectionDefinition configuredConnection) (oauthTokenResponse, error) {
	switch configuredConnectionDefinition.Registration.GrantType {
	case GrantTypePublicRead:
		return oauthTokenResponse{}, fmt.Errorf("public_read connections do not issue access tokens")
	case GrantTypeClientCredentials:
		rawClientSecret, _, _, err := server.ResolveConnectionSecret(ctx, configuredConnectionDefinition.Registration.Provider, configuredConnectionDefinition.Registration.Subject)
		if err != nil {
			return oauthTokenResponse{}, fmt.Errorf("resolve connection credential: %w", err)
		}
		formValues := url.Values{}
		formValues.Set("grant_type", GrantTypeClientCredentials)
		formValues.Set("client_id", configuredConnectionDefinition.ClientID)
		formValues.Set("client_secret", string(rawClientSecret))
		if len(configuredConnectionDefinition.Registration.Scopes) > 0 {
			formValues.Set("scope", strings.Join(configuredConnectionDefinition.Registration.Scopes, " "))
		}
		return server.exchangeOAuthToken(ctx, configuredConnectionDefinition, formValues)
	case GrantTypePKCE:
		rawRefreshToken, _, _, err := server.ResolveConnectionSecret(ctx, configuredConnectionDefinition.Registration.Provider, configuredConnectionDefinition.Registration.Subject)
		if err != nil {
			return oauthTokenResponse{}, fmt.Errorf("resolve connection refresh token: %w", err)
		}
		oauthToken, err := server.refreshPKCEAccessToken(ctx, configuredConnectionDefinition, rawRefreshToken)
		if err != nil {
			return oauthTokenResponse{}, err
		}
		if strings.TrimSpace(oauthToken.RefreshToken) != "" && oauthToken.RefreshToken != string(rawRefreshToken) {
			if _, err := server.RotateConnectionCredential(ctx, configuredConnectionDefinition.Registration, []byte(oauthToken.RefreshToken)); err != nil {
				return oauthTokenResponse{}, fmt.Errorf("rotate connection refresh token: %w", err)
			}
		}
		return oauthToken, nil
	default:
		return oauthTokenResponse{}, fmt.Errorf("unsupported grant_type %q", configuredConnectionDefinition.Registration.GrantType)
	}
}

func (server *Server) capabilityProvenanceMetadata(capabilityName string, quarantineRef string) map[string]interface{} {
	configuredCapabilityDefinition, found := server.configuredCapabilities[capabilityName]
	if !found {
		return nil
	}
	return map[string]interface{}{
		"content_origin":              contentOriginRemote,
		"content_class":               configuredCapabilityDefinition.ContentClass,
		"content_type":                contentTypeForConfiguredContentClass(configuredCapabilityDefinition.ContentClass),
		"extractor":                   configuredCapabilityDefinition.Extractor,
		"field_trust":                 fieldTrustDeterministic,
		"derived_from_quarantine_ref": quarantineRef,
	}
}

func contentTypeForConfiguredContentClass(contentClass string) string {
	switch contentClass {
	case contentClassStructuredJSONConfig:
		return contentTypeApplicationJSON
	case contentClassMarkdownConfig:
		return contentTypeTextMarkdown
	case contentClassHTMLConfig:
		return contentTypeTextHTML
	default:
		return "application/octet-stream"
	}
}

func validateConfiguredFieldsMetadata(structuredResult map[string]interface{}, fieldsMeta map[string]ResultFieldMetadata) error {
	capabilityResponse := CapabilityResponse{
		StructuredResult: structuredResult,
		FieldsMeta:       fieldsMeta,
	}
	if err := capabilityResponse.ValidateStructuredResultFields(); err != nil {
		return fmt.Errorf("validate configured fields metadata: %w", err)
	}
	return nil
}

func (server *Server) registerConfiguredCapabilities() error {
	return registerConfiguredCapabilitiesOnRegistry(server, server.currentPolicyRuntime().registry, server.currentConfiguredCapabilitiesSnapshot())
}

func sortedConfiguredCapabilityNames(configuredCapabilities map[string]configuredCapability) []string {
	capabilityNames := make([]string, 0, len(configuredCapabilities))
	for capabilityName := range configuredCapabilities {
		capabilityNames = append(capabilityNames, capabilityName)
	}
	sort.Strings(capabilityNames)
	return capabilityNames
}
