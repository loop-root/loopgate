package loopgate

import (
	"encoding/json"
	"fmt"
	"strings"

	"loopgate/internal/identifiers"
)

type ModelConnectionStoreRequest struct {
	ConnectionID string `json:"connection_id"`
	ProviderName string `json:"provider_name"`
	BaseURL      string `json:"base_url"`
	SecretValue  string `json:"secret_value"`
}

// MarshalJSON omits raw secret material so accidental json.Marshal of this request
// (logging, debug echoes) cannot leak credentials — mirrors CapabilityRequest.
func (request ModelConnectionStoreRequest) MarshalJSON() ([]byte, error) {
	type modelConnectionStoreWire struct {
		ConnectionID string `json:"connection_id"`
		ProviderName string `json:"provider_name"`
		BaseURL      string `json:"base_url"`
	}
	return json.Marshal(modelConnectionStoreWire{
		ConnectionID: request.ConnectionID,
		ProviderName: request.ProviderName,
		BaseURL:      request.BaseURL,
	})
}

type ModelConnectionStatus struct {
	ConnectionID       string `json:"connection_id"`
	ProviderName       string `json:"provider_name"`
	BaseURL            string `json:"base_url"`
	Status             string `json:"status"`
	SecureStoreRefID   string `json:"secure_store_ref_id"`
	LastValidatedAtUTC string `json:"last_validated_at_utc,omitempty"`
	LastUsedAtUTC      string `json:"last_used_at_utc,omitempty"`
	LastRotatedAtUTC   string `json:"last_rotated_at_utc,omitempty"`
}

type ConnectionStatus struct {
	Provider           string   `json:"provider"`
	GrantType          string   `json:"grant_type"`
	Subject            string   `json:"subject"`
	Scopes             []string `json:"scopes"`
	Status             string   `json:"status"`
	SecureStoreRefID   string   `json:"secure_store_ref_id"`
	LastValidatedAtUTC string   `json:"last_validated_at_utc,omitempty"`
	LastUsedAtUTC      string   `json:"last_used_at_utc,omitempty"`
	LastRotatedAtUTC   string   `json:"last_rotated_at_utc,omitempty"`
}

type ConnectionsStatusResponse struct {
	Connections []ConnectionStatus `json:"connections"`
}

type ConnectionKeyRequest struct {
	Provider string `json:"provider"`
	Subject  string `json:"subject"`
}

type PKCEStartRequest struct {
	Provider string `json:"provider"`
	Subject  string `json:"subject"`
}

type PKCEStartResponse struct {
	Provider         string `json:"provider"`
	Subject          string `json:"subject"`
	AuthorizationURL string `json:"authorization_url"`
	State            string `json:"state"`
	ExpiresAtUTC     string `json:"expires_at_utc"`
}

type PKCECompleteRequest struct {
	Provider string `json:"provider"`
	Subject  string `json:"subject"`
	State    string `json:"state"`
	Code     string `json:"code"`
}

type SiteInspectionRequest struct {
	URL string `json:"url"`
}

type SiteCertificateInfo struct {
	Subject           string   `json:"subject"`
	Issuer            string   `json:"issuer"`
	DNSNames          []string `json:"dns_names,omitempty"`
	NotBeforeUTC      string   `json:"not_before_utc,omitempty"`
	NotAfterUTC       string   `json:"not_after_utc,omitempty"`
	FingerprintSHA256 string   `json:"fingerprint_sha256,omitempty"`
}

type SiteDraftField struct {
	Name                 string `json:"name"`
	Sensitivity          string `json:"sensitivity"`
	MaxInlineBytes       int    `json:"max_inline_bytes"`
	AllowBlobRefFallback bool   `json:"allow_blob_ref_fallback"`
	JSONPath             string `json:"json_path,omitempty"`
	HTMLTitle            bool   `json:"html_title,omitempty"`
	MetaName             string `json:"meta_name,omitempty"`
	MetaProperty         string `json:"meta_property,omitempty"`
}

type SiteTrustDraftSuggestion struct {
	Provider       string           `json:"provider"`
	Subject        string           `json:"subject"`
	CapabilityName string           `json:"capability_name"`
	ContentClass   string           `json:"content_class"`
	Extractor      string           `json:"extractor"`
	CapabilityPath string           `json:"capability_path"`
	Fields         []SiteDraftField `json:"fields"`
}

type SiteInspectionResponse struct {
	NormalizedURL     string                    `json:"normalized_url"`
	Scheme            string                    `json:"scheme"`
	Host              string                    `json:"host"`
	Path              string                    `json:"path"`
	HTTPStatusCode    int                       `json:"http_status_code"`
	ContentType       string                    `json:"content_type"`
	HTTPS             bool                      `json:"https"`
	TLSValid          bool                      `json:"tls_valid"`
	Certificate       *SiteCertificateInfo      `json:"certificate,omitempty"`
	TrustDraftAllowed bool                      `json:"trust_draft_allowed"`
	DraftSuggestion   *SiteTrustDraftSuggestion `json:"draft_suggestion,omitempty"`
}

type SiteTrustDraftRequest struct {
	URL string `json:"url"`
}

type SiteTrustDraftResponse struct {
	NormalizedURL string                   `json:"normalized_url"`
	DraftPath     string                   `json:"draft_path"`
	Suggestion    SiteTrustDraftSuggestion `json:"suggestion"`
}

func (connectionStatus ConnectionStatus) Validate() error {
	if err := identifiers.ValidateSafeIdentifier("connection provider", connectionStatus.Provider); err != nil {
		return err
	}
	if strings.TrimSpace(connectionStatus.GrantType) != "" {
		if err := ValidateGrantType(connectionStatus.GrantType); err != nil {
			return err
		}
	}
	if strings.TrimSpace(connectionStatus.Subject) != "" {
		if err := identifiers.ValidateSafeIdentifier("connection subject", connectionStatus.Subject); err != nil {
			return err
		}
	}
	for _, rawScope := range connectionStatus.Scopes {
		if err := identifiers.ValidateSafeIdentifier("connection scope", rawScope); err != nil {
			return err
		}
	}
	if strings.TrimSpace(connectionStatus.Status) != "" {
		if err := identifiers.ValidateSafeIdentifier("connection status", connectionStatus.Status); err != nil {
			return err
		}
	}
	if strings.TrimSpace(connectionStatus.SecureStoreRefID) != "" {
		if err := identifiers.ValidateSafeIdentifier("connection secure store ref id", connectionStatus.SecureStoreRefID); err != nil {
			return err
		}
	}
	return nil
}

func (connectionKeyRequest ConnectionKeyRequest) Validate() error {
	if err := identifiers.ValidateSafeIdentifier("connection provider", strings.TrimSpace(connectionKeyRequest.Provider)); err != nil {
		return err
	}
	if strings.TrimSpace(connectionKeyRequest.Subject) != "" {
		if err := identifiers.ValidateSafeIdentifier("connection subject", strings.TrimSpace(connectionKeyRequest.Subject)); err != nil {
			return err
		}
	}
	return nil
}

func (pkceStartRequest PKCEStartRequest) Validate() error {
	return ConnectionKeyRequest(pkceStartRequest).Validate()
}

func (pkceCompleteRequest PKCECompleteRequest) Validate() error {
	if err := (ConnectionKeyRequest{Provider: pkceCompleteRequest.Provider, Subject: pkceCompleteRequest.Subject}).Validate(); err != nil {
		return err
	}
	if err := identifiers.ValidateSafeIdentifier("pkce state", strings.TrimSpace(pkceCompleteRequest.State)); err != nil {
		return err
	}
	if err := identifiers.ValidateSafeIdentifier("pkce code", strings.TrimSpace(pkceCompleteRequest.Code)); err != nil {
		return err
	}
	return nil
}

func (siteInspectionRequest SiteInspectionRequest) Validate() error {
	if strings.TrimSpace(siteInspectionRequest.URL) == "" {
		return fmt.Errorf("url is required")
	}
	return nil
}

func (siteTrustDraftRequest SiteTrustDraftRequest) Validate() error {
	if strings.TrimSpace(siteTrustDraftRequest.URL) == "" {
		return fmt.Errorf("url is required")
	}
	return nil
}
