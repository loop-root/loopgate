package loopgate

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	modelpkg "loopgate/internal/model"
	modelruntime "loopgate/internal/modelruntime"
)

type RequestDeniedError struct {
	DenialCode   string
	DenialReason string
}

func (deniedError RequestDeniedError) Error() string {
	if strings.TrimSpace(deniedError.DenialCode) != "" {
		return fmt.Sprintf("loopgate denied request (%s): %s", deniedError.DenialCode, deniedError.DenialReason)
	}
	return fmt.Sprintf("loopgate denied request: %s", deniedError.DenialReason)
}

type Client struct {
	socketPath string
	httpClient *http.Client
	baseURL    string

	defaultRequestTimeout time.Duration
	modelReplyTimeout     time.Duration

	mu                       sync.Mutex
	delegatedSession         bool
	actor                    string
	clientSessionID          string
	controlSessionID         string
	workspaceID              string
	operatorMountPaths       []string
	primaryOperatorMountPath string
	requestedCapabilities    []string
	capabilityToken          string
	approvalToken            string
	approvalDecisionNonce    map[string]string
	approvalManifestSHA256   map[string]string
	sessionMACKey            string
	tokenExpiresAt           time.Time
}

// DelegatedSessionConfig carries only the already-minted transport credentials.
// It does not carry tenant or user identity; those are stamped server-side at
// session open and enforced from the control session / capability token.
type DelegatedSessionConfig struct {
	ControlSessionID string
	CapabilityToken  string
	ApprovalToken    string
	SessionMACKey    string
	ExpiresAt        time.Time
}

var _ ControlPlaneClient = (*Client)(nil)

func NewClient(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			dialer := net.Dialer{}
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}

	return &Client{
		socketPath:             socketPath,
		httpClient:             &http.Client{Transport: transport},
		baseURL:                "http://loopgate",
		defaultRequestTimeout:  10 * time.Second,
		modelReplyTimeout:      2 * time.Minute,
		approvalDecisionNonce:  make(map[string]string),
		approvalManifestSHA256: make(map[string]string),
	}
}

func NewClientFromDelegatedSession(socketPath string, delegatedSession DelegatedSessionConfig) (*Client, error) {
	client := NewClient(socketPath)
	if err := client.UpdateDelegatedSession(delegatedSession); err != nil {
		return nil, err
	}
	return client, nil
}

// Health returns a minimal liveness response without authentication. Use for startup probes only;
// capability and policy inventory require Status after session open.
func (client *Client) Health(ctx context.Context) (HealthResponse, error) {
	var response HealthResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/health", "", nil, &response, nil); err != nil {
		return HealthResponse{}, err
	}
	return response, nil
}

func (client *Client) Status(ctx context.Context) (StatusResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return StatusResponse{}, err
	}
	var response StatusResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/status", capabilityToken, nil, &response, nil); err != nil {
		return StatusResponse{}, err
	}
	return response, nil
}

// FetchDiagnosticReport loads aggregated operator diagnostics (JSON). Requires an open control session
// over the same Unix peer binding as other privileged routes.
func (client *Client) FetchDiagnosticReport(ctx context.Context, responseBody interface{}) error {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return err
	}
	return client.doJSON(ctx, http.MethodGet, "/v1/diagnostic/report", capabilityToken, nil, responseBody, nil)
}

func (client *Client) ModelReply(ctx context.Context, request modelpkg.Request) (modelpkg.Response, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return modelpkg.Response{}, err
	}

	var response modelpkg.Response
	if err := client.doJSONWithTimeout(ctx, client.modelReplyTimeout, http.MethodPost, "/v1/model/reply", capabilityToken, request, &response, nil); err != nil {
		return modelpkg.Response{}, err
	}
	return response, nil
}

func (client *Client) ValidateModelConfig(ctx context.Context, runtimeConfig modelruntime.Config) (modelruntime.Config, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return modelruntime.Config{}, err
	}

	var response ModelValidateResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/model/validate", capabilityToken, ModelValidateRequest{
		RuntimeConfig: runtimeConfig,
	}, &response, nil); err != nil {
		return modelruntime.Config{}, err
	}
	return response.RuntimeConfig, nil
}

func (client *Client) StoreModelConnection(ctx context.Context, request ModelConnectionStoreRequest) (ModelConnectionStatus, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return ModelConnectionStatus{}, err
	}

	type modelConnectionStoreWire struct {
		ConnectionID string `json:"connection_id"`
		ProviderName string `json:"provider_name"`
		BaseURL      string `json:"base_url"`
		SecretValue  string `json:"secret_value"`
	}

	var response ModelConnectionStatus
	if err := client.doJSON(ctx, http.MethodPost, "/v1/model/connections/store", capabilityToken, modelConnectionStoreWire{
		ConnectionID: request.ConnectionID,
		ProviderName: request.ProviderName,
		BaseURL:      request.BaseURL,
		SecretValue:  request.SecretValue,
	}, &response, nil); err != nil {
		return ModelConnectionStatus{}, err
	}
	return response, nil
}

func (client *Client) ConnectionsStatus(ctx context.Context) ([]ConnectionStatus, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return nil, err
	}
	var response ConnectionsStatusResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/connections/status", capabilityToken, nil, &response, nil); err != nil {
		return nil, err
	}
	return response.Connections, nil
}

func (client *Client) ValidateConnection(ctx context.Context, provider string, subject string) (ConnectionStatus, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return ConnectionStatus{}, err
	}
	var response ConnectionStatus
	if err := client.doJSON(ctx, http.MethodPost, "/v1/connections/validate", capabilityToken, ConnectionKeyRequest{
		Provider: provider,
		Subject:  subject,
	}, &response, nil); err != nil {
		return ConnectionStatus{}, err
	}
	return response, nil
}

func (client *Client) StartPKCEConnection(ctx context.Context, request PKCEStartRequest) (PKCEStartResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return PKCEStartResponse{}, err
	}
	var response PKCEStartResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/connections/pkce/start", capabilityToken, request, &response, nil); err != nil {
		return PKCEStartResponse{}, err
	}
	return response, nil
}

func (client *Client) CompletePKCEConnection(ctx context.Context, request PKCECompleteRequest) (ConnectionStatus, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return ConnectionStatus{}, err
	}
	var response ConnectionStatus
	if err := client.doJSON(ctx, http.MethodPost, "/v1/connections/pkce/complete", capabilityToken, request, &response, nil); err != nil {
		return ConnectionStatus{}, err
	}
	return response, nil
}

func (client *Client) InspectSite(ctx context.Context, request SiteInspectionRequest) (SiteInspectionResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return SiteInspectionResponse{}, err
	}
	var response SiteInspectionResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/sites/inspect", capabilityToken, request, &response, nil); err != nil {
		return SiteInspectionResponse{}, err
	}
	return response, nil
}

func (client *Client) CreateTrustDraft(ctx context.Context, request SiteTrustDraftRequest) (SiteTrustDraftResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return SiteTrustDraftResponse{}, err
	}
	var response SiteTrustDraftResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/sites/trust-draft", capabilityToken, request, &response, nil); err != nil {
		return SiteTrustDraftResponse{}, err
	}
	return response, nil
}

func sameStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
