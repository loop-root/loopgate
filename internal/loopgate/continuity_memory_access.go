package loopgate

import (
	"context"
	"fmt"
	"strings"
)

func normalizeContinuityInspectRequest(rawRequest ContinuityInspectRequest) (ContinuityInspectRequest, error) {
	validatedRequest := rawRequest
	validatedRequest.Scope = strings.TrimSpace(validatedRequest.Scope)
	if validatedRequest.Scope == "" {
		validatedRequest.Scope = memoryScopeGlobal
	}
	validatedRequest.Tags = normalizeLoopgateMemoryTags(validatedRequest.Tags)
	for eventIndex, continuityEvent := range validatedRequest.Events {
		validatedRequest.Events[eventIndex].SessionID = strings.TrimSpace(continuityEvent.SessionID)
		validatedRequest.Events[eventIndex].ThreadID = strings.TrimSpace(continuityEvent.ThreadID)
		validatedRequest.Events[eventIndex].Scope = strings.TrimSpace(continuityEvent.Scope)
		validatedRequest.Events[eventIndex].Type = strings.TrimSpace(continuityEvent.Type)
		validatedRequest.Events[eventIndex].EpistemicFlavor = strings.TrimSpace(continuityEvent.EpistemicFlavor)
		validatedRequest.Events[eventIndex].EventHash = strings.TrimSpace(continuityEvent.EventHash)
		// Legacy synthetic continuity input is still used by tests and replay
		// compatibility. Caller-supplied source_refs are not authoritative
		// provenance, so drop them before the packet crosses into backend-owned
		// observed continuity state.
		validatedRequest.Events[eventIndex].SourceRefs = nil
	}
	if err := validatedRequest.Validate(); err != nil {
		return ContinuityInspectRequest{}, err
	}
	return validatedRequest, nil
}

func (server *Server) inspectObservedContinuity(tokenClaims capabilityToken, observedRequest ObservedContinuityInspectRequest) (ContinuityInspectResponse, error) {
	backend, err := server.memoryBackendForTenant(tokenClaims.TenantID)
	if err != nil {
		return ContinuityInspectResponse{}, err
	}
	return backend.InspectObservedContinuity(context.Background(), tokenClaims, observedRequest)
}

func buildContinuityInspectResponse(inspectionRecord continuityInspectionRecord) ContinuityInspectResponse {
	return ContinuityInspectResponse{
		InspectionID:          inspectionRecord.InspectionID,
		ThreadID:              inspectionRecord.ThreadID,
		Outcome:               inspectionRecord.DerivationOutcome,
		DerivationOutcome:     inspectionRecord.DerivationOutcome,
		ReviewStatus:          inspectionRecord.Review.Status,
		LineageStatus:         inspectionRecord.Lineage.Status,
		DerivedDistillateIDs:  append([]string(nil), inspectionRecord.DerivedDistillateIDs...),
		DerivedResonateKeyIDs: append([]string(nil), inspectionRecord.DerivedResonateKeyIDs...),
	}
}

func (server *Server) continuityThresholdReached(inspectionRecord continuityInspectionRecord) bool {
	if server.policy.Memory.SubmitPreviousMinEvents > 0 && inspectionRecord.EventCount >= server.policy.Memory.SubmitPreviousMinEvents {
		return true
	}
	if server.policy.Memory.SubmitPreviousMinPayloadBytes > 0 && inspectionRecord.ApproxPayloadBytes >= server.policy.Memory.SubmitPreviousMinPayloadBytes {
		return true
	}
	if server.policy.Memory.SubmitPreviousMinPromptTokens > 0 && inspectionRecord.ApproxPromptTokens >= server.policy.Memory.SubmitPreviousMinPromptTokens {
		return true
	}
	return false
}

func (server *Server) memoryBackendForTenant(rawTenantID string) (MemoryBackend, error) {
	server.memoryMu.Lock()
	defer server.memoryMu.Unlock()

	partition, err := server.ensureMemoryPartitionLocked(rawTenantID)
	if err != nil {
		return nil, err
	}
	if partition.backend == nil {
		return nil, fmt.Errorf("memory backend is not configured")
	}
	return partition.backend, nil
}

func (server *Server) discoverMemory(tenantID string, rawRequest MemoryDiscoverRequest) (MemoryDiscoverResponse, error) {
	validatedRequest := rawRequest
	if validatedRequest.Scope == "" {
		validatedRequest.Scope = memoryScopeGlobal
	}
	if validatedRequest.MaxItems == 0 {
		validatedRequest.MaxItems = 5
	}
	if err := validatedRequest.Validate(); err != nil {
		return MemoryDiscoverResponse{}, err
	}
	backend, err := server.memoryBackendForTenant(tenantID)
	if err != nil {
		return MemoryDiscoverResponse{}, err
	}
	return backend.Discover(context.Background(), validatedRequest)
}

func (server *Server) rememberMemoryFact(tokenClaims capabilityToken, rawRequest MemoryRememberRequest) (MemoryRememberResponse, error) {
	backend, err := server.memoryBackendForTenant(tokenClaims.TenantID)
	if err != nil {
		return MemoryRememberResponse{}, err
	}
	return backend.RememberFact(context.Background(), tokenClaims, rawRequest)
}

func (server *Server) recallMemory(tenantID string, rawRequest MemoryRecallRequest) (MemoryRecallResponse, error) {
	validatedRequest := rawRequest
	if validatedRequest.Scope == "" {
		validatedRequest.Scope = memoryScopeGlobal
	}
	if validatedRequest.MaxItems == 0 {
		validatedRequest.MaxItems = 10
	}
	if validatedRequest.MaxTokens == 0 {
		validatedRequest.MaxTokens = 2000
	}
	if err := validatedRequest.Validate(); err != nil {
		return MemoryRecallResponse{}, err
	}
	backend, err := server.memoryBackendForTenant(tenantID)
	if err != nil {
		return MemoryRecallResponse{}, err
	}
	return backend.Recall(context.Background(), validatedRequest)
}
