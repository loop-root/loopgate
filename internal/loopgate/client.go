package loopgate

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	modelpkg "morph/internal/model"
	modelruntime "morph/internal/modelruntime"
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

	mu                     sync.Mutex
	delegatedSession       bool
	actor                  string
	clientSessionID        string
	controlSessionID       string
	workspaceID            string
	requestedCapabilities  []string
	capabilityToken        string
	approvalToken          string
	approvalDecisionNonce  map[string]string
	approvalManifestSHA256 map[string]string
	sessionMACKey          string
	tokenExpiresAt         time.Time
}

type DelegatedSessionConfig struct {
	ControlSessionID string
	CapabilityToken  string
	ApprovalToken    string
	SessionMACKey    string
	TenantID         string
	UserID           string
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

// SessionMACKeys returns previous, current, and next 12-hour epoch MAC material for this control session.
// Same transport requirements as Status (Bearer token + signed GET).
func (client *Client) SessionMACKeys(ctx context.Context) (SessionMACKeysResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return SessionMACKeysResponse{}, err
	}
	var response SessionMACKeysResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/session/mac-keys", capabilityToken, nil, &response, nil); err != nil {
		return SessionMACKeysResponse{}, err
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

	var response ModelConnectionStatus
	if err := client.doJSON(ctx, http.MethodPost, "/v1/model/connections/store", capabilityToken, request, &response, nil); err != nil {
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

func (client *Client) SandboxImport(ctx context.Context, request SandboxImportRequest) (SandboxOperationResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return SandboxOperationResponse{}, err
	}
	var response SandboxOperationResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/sandbox/import", capabilityToken, request, &response, nil); err != nil {
		return SandboxOperationResponse{}, err
	}
	return response, nil
}

func (client *Client) SandboxStage(ctx context.Context, request SandboxStageRequest) (SandboxOperationResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return SandboxOperationResponse{}, err
	}
	var response SandboxOperationResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/sandbox/stage", capabilityToken, request, &response, nil); err != nil {
		return SandboxOperationResponse{}, err
	}
	return response, nil
}

func (client *Client) SandboxMetadata(ctx context.Context, request SandboxMetadataRequest) (SandboxArtifactMetadataResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return SandboxArtifactMetadataResponse{}, err
	}
	var response SandboxArtifactMetadataResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/sandbox/metadata", capabilityToken, request, &response, nil); err != nil {
		return SandboxArtifactMetadataResponse{}, err
	}
	return response, nil
}

func (client *Client) SandboxList(ctx context.Context, request SandboxListRequest) (SandboxListResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return SandboxListResponse{}, err
	}
	var response SandboxListResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/sandbox/list", capabilityToken, request, &response, nil); err != nil {
		return SandboxListResponse{}, err
	}
	return response, nil
}

func (client *Client) SandboxExport(ctx context.Context, request SandboxExportRequest) (SandboxOperationResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return SandboxOperationResponse{}, err
	}
	var response SandboxOperationResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/sandbox/export", capabilityToken, request, &response, nil); err != nil {
		return SandboxOperationResponse{}, err
	}
	return response, nil
}

// SubmitHavenContinuityInspectionForThread loads a stored chat thread from Loopgate's threadstore
// and runs continuity inspection (proposal path). This is the supported client-facing continuity
// submission path so the client does not ship raw transcript payloads.
func (client *Client) SubmitHavenContinuityInspectionForThread(ctx context.Context, threadID string) (HavenContinuityInspectThreadResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return HavenContinuityInspectThreadResponse{}, err
	}
	var response HavenContinuityInspectThreadResponse
	req := HavenContinuityInspectThreadRequest{ThreadID: strings.TrimSpace(threadID)}
	if err := client.doJSON(ctx, http.MethodPost, "/v1/continuity/inspect-thread", capabilityToken, req, &response, nil); err != nil {
		return HavenContinuityInspectThreadResponse{}, err
	}
	return response, nil
}

func (client *Client) LoadMemoryWakeState(ctx context.Context) (MemoryWakeStateResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MemoryWakeStateResponse{}, err
	}
	var response MemoryWakeStateResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/memory/wake-state", capabilityToken, nil, &response, nil); err != nil {
		return MemoryWakeStateResponse{}, err
	}
	return response, nil
}

// LoadTasks returns Task Board items for local operator clients
// (control session auth; not capability execution).
func (client *Client) LoadTasks(ctx context.Context) (UITasksResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return UITasksResponse{}, err
	}
	var response UITasksResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/tasks", capabilityToken, nil, &response, nil); err != nil {
		return UITasksResponse{}, err
	}
	return response, nil
}

// SetExplicitTodoWorkflowStatus sets workflow status for an explicit todo item (todo vs in_progress).
func (client *Client) SetExplicitTodoWorkflowStatus(ctx context.Context, itemID string, status string) error {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return err
	}
	path := "/v1/tasks/" + itemID + "/status"
	return client.doJSON(ctx, http.MethodPut, path, capabilityToken, UITasksStatusUpdateRequest{Status: status}, nil, nil)
}

func (client *Client) LoadMemoryDiagnosticWake(ctx context.Context) (MemoryDiagnosticWakeResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MemoryDiagnosticWakeResponse{}, err
	}
	var response MemoryDiagnosticWakeResponse
	if err := client.doJSON(ctx, http.MethodGet, "/v1/memory/diagnostic-wake", capabilityToken, nil, &response, nil); err != nil {
		return MemoryDiagnosticWakeResponse{}, err
	}
	return response, nil
}

func (client *Client) DiscoverMemory(ctx context.Context, request MemoryDiscoverRequest) (MemoryDiscoverResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MemoryDiscoverResponse{}, err
	}
	var response MemoryDiscoverResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/memory/discover", capabilityToken, request, &response, nil); err != nil {
		return MemoryDiscoverResponse{}, err
	}
	return response, nil
}

func (client *Client) LookupMemoryArtifacts(ctx context.Context, request MemoryArtifactLookupRequest) (MemoryArtifactLookupResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MemoryArtifactLookupResponse{}, err
	}
	var response MemoryArtifactLookupResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/memory/artifacts/lookup", capabilityToken, request, &response, nil); err != nil {
		return MemoryArtifactLookupResponse{}, err
	}
	return response, nil
}

func (client *Client) RecallMemory(ctx context.Context, request MemoryRecallRequest) (MemoryRecallResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MemoryRecallResponse{}, err
	}
	var response MemoryRecallResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/memory/recall", capabilityToken, request, &response, nil); err != nil {
		return MemoryRecallResponse{}, err
	}
	return response, nil
}

func (client *Client) GetMemoryArtifacts(ctx context.Context, request MemoryArtifactGetRequest) (MemoryArtifactGetResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MemoryArtifactGetResponse{}, err
	}
	var response MemoryArtifactGetResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/memory/artifacts/get", capabilityToken, request, &response, nil); err != nil {
		return MemoryArtifactGetResponse{}, err
	}
	return response, nil
}

func (client *Client) RememberMemoryFact(ctx context.Context, request MemoryRememberRequest) (MemoryRememberResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MemoryRememberResponse{}, err
	}
	var response MemoryRememberResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/memory/remember", capabilityToken, request, &response, nil); err != nil {
		return MemoryRememberResponse{}, err
	}
	return response, nil
}

func (client *Client) ReviewMemoryInspection(ctx context.Context, inspectionID string, request MemoryInspectionReviewRequest) (MemoryInspectionGovernanceResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MemoryInspectionGovernanceResponse{}, err
	}
	var response MemoryInspectionGovernanceResponse
	path := fmt.Sprintf("/v1/memory/inspections/%s/review", inspectionID)
	if err := client.doJSON(ctx, http.MethodPost, path, capabilityToken, request, &response, nil); err != nil {
		return MemoryInspectionGovernanceResponse{}, err
	}
	return response, nil
}

func (client *Client) TombstoneMemoryInspection(ctx context.Context, inspectionID string, request MemoryInspectionLineageRequest) (MemoryInspectionGovernanceResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MemoryInspectionGovernanceResponse{}, err
	}
	var response MemoryInspectionGovernanceResponse
	path := fmt.Sprintf("/v1/memory/inspections/%s/tombstone", inspectionID)
	if err := client.doJSON(ctx, http.MethodPost, path, capabilityToken, request, &response, nil); err != nil {
		return MemoryInspectionGovernanceResponse{}, err
	}
	return response, nil
}

func (client *Client) PurgeMemoryInspection(ctx context.Context, inspectionID string, request MemoryInspectionLineageRequest) (MemoryInspectionGovernanceResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MemoryInspectionGovernanceResponse{}, err
	}
	var response MemoryInspectionGovernanceResponse
	path := fmt.Sprintf("/v1/memory/inspections/%s/purge", inspectionID)
	if err := client.doJSON(ctx, http.MethodPost, path, capabilityToken, request, &response, nil); err != nil {
		return MemoryInspectionGovernanceResponse{}, err
	}
	return response, nil
}

func (client *Client) SpawnMorphling(ctx context.Context, request MorphlingSpawnRequest) (MorphlingSpawnResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MorphlingSpawnResponse{}, err
	}
	var response MorphlingSpawnResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/morphlings/spawn", capabilityToken, request, &response, nil); err != nil {
		return MorphlingSpawnResponse{}, err
	}
	// Pending spawn approvals use the same manifest + nonce binding as capability approvals;
	// cache them so DecideApproval (HTTP) can submit /v1/approvals/.../decision.
	if response.Status == ResponseStatusPendingApproval && strings.TrimSpace(response.ApprovalID) != "" {
		client.mu.Lock()
		if strings.TrimSpace(response.ApprovalManifestSHA256) != "" {
			client.approvalManifestSHA256[response.ApprovalID] = strings.TrimSpace(response.ApprovalManifestSHA256)
		}
		if strings.TrimSpace(response.ApprovalDecisionNonce) != "" {
			client.approvalDecisionNonce[response.ApprovalID] = strings.TrimSpace(response.ApprovalDecisionNonce)
		}
		client.mu.Unlock()
	}
	return response, nil
}

func (client *Client) MorphlingStatus(ctx context.Context, request MorphlingStatusRequest) (MorphlingStatusResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MorphlingStatusResponse{}, err
	}
	var response MorphlingStatusResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/morphlings/status", capabilityToken, request, &response, nil); err != nil {
		return MorphlingStatusResponse{}, err
	}
	return response, nil
}

func (client *Client) TerminateMorphling(ctx context.Context, request MorphlingTerminateRequest) (MorphlingTerminateResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MorphlingTerminateResponse{}, err
	}
	var response MorphlingTerminateResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/morphlings/terminate", capabilityToken, request, &response, nil); err != nil {
		return MorphlingTerminateResponse{}, err
	}
	return response, nil
}

func (client *Client) LaunchMorphlingWorker(ctx context.Context, request MorphlingWorkerLaunchRequest) (MorphlingWorkerLaunchResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MorphlingWorkerLaunchResponse{}, err
	}
	var response MorphlingWorkerLaunchResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/morphlings/worker/launch", capabilityToken, request, &response, nil); err != nil {
		return MorphlingWorkerLaunchResponse{}, err
	}
	return response, nil
}

func (client *Client) ReviewMorphling(ctx context.Context, request MorphlingReviewRequest) (MorphlingReviewResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return MorphlingReviewResponse{}, err
	}
	var response MorphlingReviewResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/morphlings/review", capabilityToken, request, &response, nil); err != nil {
		return MorphlingReviewResponse{}, err
	}
	return response, nil
}

func (client *Client) QuarantineMetadata(ctx context.Context, quarantineRef string) (QuarantineMetadataResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return QuarantineMetadataResponse{}, err
	}
	var response QuarantineMetadataResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/quarantine/metadata", capabilityToken, QuarantineLookupRequest{
		QuarantineRef: quarantineRef,
	}, &response, nil); err != nil {
		return QuarantineMetadataResponse{}, err
	}
	return response, nil
}

func (client *Client) ViewQuarantinedPayload(ctx context.Context, quarantineRef string) (QuarantineViewResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return QuarantineViewResponse{}, err
	}
	var response QuarantineViewResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/quarantine/view", capabilityToken, QuarantineLookupRequest{
		QuarantineRef: quarantineRef,
	}, &response, nil); err != nil {
		return QuarantineViewResponse{}, err
	}
	return response, nil
}

func (client *Client) PruneQuarantinedPayload(ctx context.Context, quarantineRef string) (QuarantineMetadataResponse, error) {
	capabilityToken, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return QuarantineMetadataResponse{}, err
	}
	var response QuarantineMetadataResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/quarantine/prune", capabilityToken, QuarantineLookupRequest{
		QuarantineRef: quarantineRef,
	}, &response, nil); err != nil {
		return QuarantineMetadataResponse{}, err
	}
	return response, nil
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

// SetWorkspaceID sets the workspace identity that will be included in the
// session open request to Loopgate. The workspace ID should be a deterministic
// hash derived from the workspace root (e.g., SHA256 of absolute repo path),
// not a caller-chosen arbitrary string. This binds the Loopgate session to
// a specific workspace for audit and isolation purposes.
func (client *Client) SetWorkspaceID(workspaceID string) {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.workspaceID = workspaceID
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

func (client *Client) ExecuteCapability(ctx context.Context, capabilityRequest CapabilityRequest) (CapabilityResponse, error) {
	token, err := client.ensureCapabilityToken(ctx)
	if err != nil {
		return CapabilityResponse{}, err
	}

	client.mu.Lock()
	if strings.TrimSpace(capabilityRequest.Actor) == "" {
		capabilityRequest.Actor = client.actor
	}
	if strings.TrimSpace(capabilityRequest.SessionID) == "" {
		capabilityRequest.SessionID = client.clientSessionID
	}
	client.mu.Unlock()

	var response CapabilityResponse
	if err := client.doCapabilityJSON(ctx, client.defaultRequestTimeout, http.MethodPost, "/v1/capabilities/execute", token, capabilityRequest, &response, nil); err != nil {
		return CapabilityResponse{}, err
	}
	client.cacheApprovalDecisionNonce(response)
	return response, nil
}

func (client *Client) DecideApproval(ctx context.Context, approvalRequestID string, approved bool) (CapabilityResponse, error) {
	approvalToken, err := client.ensureApprovalToken(ctx)
	if err != nil {
		return CapabilityResponse{}, err
	}
	decisionNonce, err := client.lookupApprovalDecisionNonce(approvalRequestID)
	if err != nil {
		return CapabilityResponse{}, err
	}

	// Include the manifest SHA256 cached from the pending approval response. This binds the
	// decision to the exact method, path, and request body that was approved (AMP RFC 0005 §6).
	client.mu.Lock()
	manifestSHA256 := client.approvalManifestSHA256[approvalRequestID]
	client.mu.Unlock()

	var response CapabilityResponse
	path := fmt.Sprintf("/v1/approvals/%s/decision", approvalRequestID)
	if err := client.doCapabilityJSON(ctx, client.defaultRequestTimeout, http.MethodPost, path, "", ApprovalDecisionRequest{
		Approved:               approved,
		DecisionNonce:          decisionNonce,
		ApprovalManifestSHA256: manifestSHA256,
	}, &response, map[string]string{
		"X-Loopgate-Approval-Token": approvalToken,
	}); err != nil {
		return CapabilityResponse{}, err
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
	openRequest := OpenSessionRequest{
		Actor:                 client.actor,
		SessionID:             client.clientSessionID,
		RequestedCapabilities: append([]string(nil), client.requestedCapabilities...),
		WorkspaceID:           client.workspaceID,
	}
	client.mu.Unlock()

	var openResponse OpenSessionResponse
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

func (client *Client) cacheApprovalDecisionNonce(capabilityResponse CapabilityResponse) {
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

func (client *Client) doJSON(ctx context.Context, method string, path string, capabilityToken string, requestBody interface{}, responseBody interface{}, extraHeaders map[string]string) error {
	return client.doJSONWithTimeout(ctx, client.defaultRequestTimeout, method, path, capabilityToken, requestBody, responseBody, extraHeaders)
}

// doHavenChatSSE sends POST /v1/chat and reads the SSE response stream,
// assembling all events into a HavenChatResponse. Used in tests to exercise
// the SSE endpoint with the same request-signing path as the Swift client.
func (client *Client) doHavenChatSSE(ctx context.Context, capabilityToken string, requestBody havenChatRequest) (HavenChatResponse, error) {
	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return HavenChatResponse{}, fmt.Errorf("marshal request body: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, "POST", client.baseURL+"/v1/chat", bytes.NewReader(bodyBytes))
	if err != nil {
		return HavenChatResponse{}, fmt.Errorf("build request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(capabilityToken) != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+capabilityToken)
	}
	if err := client.attachRequestSignature(httpRequest, "/v1/chat", bodyBytes); err != nil {
		return HavenChatResponse{}, err
	}

	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return HavenChatResponse{}, fmt.Errorf("loopgate request failed: %w", err)
	}
	defer httpResponse.Body.Close()

	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		return HavenChatResponse{}, fmt.Errorf("loopgate returned status %d", httpResponse.StatusCode)
	}

	var result HavenChatResponse
	scanner := bufio.NewScanner(httpResponse.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		jsonData := line[len("data: "):]
		if strings.TrimSpace(jsonData) == "" {
			continue
		}
		var event havenSSEEvent
		if err := json.Unmarshal([]byte(jsonData), &event); err != nil {
			continue
		}
		switch event.Type {
		case "text_delta":
			result.AssistantText += event.Content
		case "approval_needed":
			result.Status = "approval_required"
			if event.ApprovalNeeded != nil {
				result.ApprovalID = event.ApprovalNeeded.ApprovalID
				result.ApprovalCapability = event.ApprovalNeeded.Capability
			}
		case "turn_complete":
			result.ThreadID = event.ThreadID
			result.UXSignals = event.UXSignals
			result.FinishReason = event.FinishReason
			result.InputTokens = event.InputTokens
			result.OutputTokens = event.OutputTokens
			result.TotalTokens = event.TotalTokens
			result.ProviderName = event.ProviderName
			result.ModelName = event.ModelName
		case "error":
			return result, fmt.Errorf("server error: %s", event.Error)
		}
	}
	if err := scanner.Err(); err != nil {
		return result, fmt.Errorf("read SSE stream: %w", err)
	}
	return result, nil
}

func (client *Client) doCapabilityJSON(ctx context.Context, requestTimeout time.Duration, method string, path string, capabilityToken string, requestBody interface{}, responseBody *CapabilityResponse, extraHeaders map[string]string) error {
	return client.doCapabilityJSONWithTimeoutRetry(ctx, requestTimeout, method, path, capabilityToken, requestBody, responseBody, extraHeaders, false)
}

func (client *Client) doJSONWithHeaders(ctx context.Context, method string, path string, capabilityToken string, requestBody interface{}, responseBody interface{}, extraHeaders map[string]string) error {
	return client.doJSONWithTimeout(ctx, client.defaultRequestTimeout, method, path, capabilityToken, requestBody, responseBody, extraHeaders)
}

func (client *Client) doJSONWithTimeout(ctx context.Context, requestTimeout time.Duration, method string, path string, capabilityToken string, requestBody interface{}, responseBody interface{}, extraHeaders map[string]string) error {
	return client.doJSONWithTimeoutRetry(ctx, requestTimeout, method, path, capabilityToken, requestBody, responseBody, extraHeaders, false)
}

func (client *Client) doJSONWithTimeoutRetry(ctx context.Context, requestTimeout time.Duration, method string, path string, capabilityToken string, requestBody interface{}, responseBody interface{}, extraHeaders map[string]string, retried bool) error {
	requestContext := ctx
	cancel := func() {}
	if _, hasDeadline := requestContext.Deadline(); !hasDeadline && requestTimeout > 0 {
		requestContext, cancel = context.WithTimeout(requestContext, requestTimeout)
	}
	defer cancel()

	var bodyBytes []byte
	if requestBody == nil {
		bodyBytes = nil
	} else {
		marshaledBytes, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyBytes = marshaledBytes
	}
	bodyReader := bytes.NewReader(bodyBytes)

	httpRequest, err := http.NewRequestWithContext(requestContext, method, client.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(capabilityToken) != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+capabilityToken)
	}
	for headerName, headerValue := range extraHeaders {
		httpRequest.Header.Set(headerName, headerValue)
	}
	if err := client.attachRequestSignature(httpRequest, path, bodyBytes); err != nil {
		return err
	}

	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return fmt.Errorf("loopgate request failed: %w", err)
	}
	defer httpResponse.Body.Close()

	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		var errorResponse CapabilityResponse
		if decodeErr := json.NewDecoder(httpResponse.Body).Decode(&errorResponse); decodeErr == nil && strings.TrimSpace(errorResponse.DenialReason) != "" {
			if !retried && strings.TrimSpace(capabilityToken) != "" && client.canRetryCapabilityToken(errorResponse.DenialCode) {
				if refreshedToken, retryErr := client.refreshCapabilityToken(requestContext); retryErr == nil {
					return client.doJSONWithTimeoutRetry(ctx, requestTimeout, method, path, refreshedToken, requestBody, responseBody, extraHeaders, true)
				}
			}
			return RequestDeniedError{
				DenialCode:   errorResponse.DenialCode,
				DenialReason: errorResponse.DenialReason,
			}
		}
		return fmt.Errorf("loopgate returned status %d", httpResponse.StatusCode)
	}

	if responseBody == nil {
		return nil
	}
	if err := json.NewDecoder(httpResponse.Body).Decode(responseBody); err != nil {
		return fmt.Errorf("decode response body: %w", err)
	}
	return nil
}

func (client *Client) doCapabilityJSONWithTimeoutRetry(ctx context.Context, requestTimeout time.Duration, method string, path string, capabilityToken string, requestBody interface{}, responseBody *CapabilityResponse, extraHeaders map[string]string, retried bool) error {
	requestContext := ctx
	cancel := func() {}
	if _, hasDeadline := requestContext.Deadline(); !hasDeadline && requestTimeout > 0 {
		requestContext, cancel = context.WithTimeout(requestContext, requestTimeout)
	}
	defer cancel()

	var bodyBytes []byte
	if requestBody != nil {
		marshaledBytes, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyBytes = marshaledBytes
	}

	httpRequest, err := http.NewRequestWithContext(requestContext, method, client.baseURL+path, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(capabilityToken) != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+capabilityToken)
	}
	for headerName, headerValue := range extraHeaders {
		httpRequest.Header.Set(headerName, headerValue)
	}
	if err := client.attachRequestSignature(httpRequest, path, bodyBytes); err != nil {
		return err
	}

	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return fmt.Errorf("loopgate request failed: %w", err)
	}
	defer httpResponse.Body.Close()

	if err := json.NewDecoder(httpResponse.Body).Decode(responseBody); err != nil {
		if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
			return fmt.Errorf("loopgate returned status %d", httpResponse.StatusCode)
		}
		return fmt.Errorf("decode response body: %w", err)
	}

	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		if !retried && strings.TrimSpace(capabilityToken) != "" && client.canRetryCapabilityToken(responseBody.DenialCode) {
			if refreshedToken, retryErr := client.refreshCapabilityToken(requestContext); retryErr == nil {
				return client.doCapabilityJSONWithTimeoutRetry(ctx, requestTimeout, method, path, refreshedToken, requestBody, responseBody, extraHeaders, true)
			}
		}
		return nil
	}
	return nil
}

func (client *Client) canRetryCapabilityToken(denialCode string) bool {
	if denialCode != DenialCodeCapabilityTokenInvalid && denialCode != DenialCodeCapabilityTokenExpired {
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

func (client *Client) attachRequestSignature(httpRequest *http.Request, path string, bodyBytes []byte) error {
	if strings.TrimSpace(httpRequest.Header.Get("X-Loopgate-Control-Session")) != "" ||
		strings.TrimSpace(httpRequest.Header.Get("X-Loopgate-Request-Timestamp")) != "" ||
		strings.TrimSpace(httpRequest.Header.Get("X-Loopgate-Request-Nonce")) != "" ||
		strings.TrimSpace(httpRequest.Header.Get("X-Loopgate-Request-Signature")) != "" {
		return nil
	}

	client.mu.Lock()
	controlSessionID := client.controlSessionID
	sessionMACKey := client.sessionMACKey
	client.mu.Unlock()

	if strings.TrimSpace(controlSessionID) == "" || strings.TrimSpace(sessionMACKey) == "" {
		return nil
	}

	requestNonce, err := clientRandomHex(12)
	if err != nil {
		return fmt.Errorf("generate request nonce: %w", err)
	}
	requestTimestamp := time.Now().UTC().Format(time.RFC3339Nano)
	requestSignature := computeRequestSignature(sessionMACKey, httpRequest.Method, path, controlSessionID, requestTimestamp, requestNonce, bodyBytes)

	httpRequest.Header.Set("X-Loopgate-Control-Session", controlSessionID)
	httpRequest.Header.Set("X-Loopgate-Request-Timestamp", requestTimestamp)
	httpRequest.Header.Set("X-Loopgate-Request-Nonce", requestNonce)
	httpRequest.Header.Set("X-Loopgate-Request-Signature", requestSignature)
	return nil
}

func computeRequestSignature(sessionMACKey string, method string, path string, controlSessionID string, requestTimestamp string, requestNonce string, bodyBytes []byte) string {
	bodyHash := sha256.Sum256(bodyBytes)
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

func clientRandomHex(byteCount int) (string, error) {
	randomBytes := make([]byte, byteCount)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(randomBytes), nil
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
