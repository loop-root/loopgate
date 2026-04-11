package loopgate

import (
	"context"
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
