package loopgate

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

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
