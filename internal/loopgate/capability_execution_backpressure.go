package loopgate

import (
	controlapipkg "loopgate/internal/loopgate/controlapi"
)

const capabilityExecutionOverloadScope = "capability_execution"

func (server *Server) tryAcquireCapabilityExecutionSlot() (func(), bool) {
	if server == nil {
		return func() {}, true
	}
	server.capabilityExecutionSlotsMu.RLock()
	slots := server.capabilityExecutionSlots
	server.capabilityExecutionSlotsMu.RUnlock()
	if slots == nil {
		return func() {}, true
	}
	select {
	case slots <- struct{}{}:
		return func() {
			<-slots
		}, true
	default:
		return nil, false
	}
}

func (server *Server) configureCapabilityExecutionSlots(maxInFlightExecutions int) {
	if server == nil || maxInFlightExecutions <= 0 {
		return
	}
	server.capabilityExecutionSlotsMu.Lock()
	server.capabilityExecutionSlots = make(chan struct{}, maxInFlightExecutions)
	server.capabilityExecutionSlotsMu.Unlock()
}

func capabilityExecutionSaturatedResponse(requestID string) controlapipkg.CapabilityResponse {
	return controlapipkg.CapabilityResponse{
		RequestID:    requestID,
		Status:       controlapipkg.ResponseStatusDenied,
		DenialReason: "control plane is saturated: capability execution concurrency limit reached; retry after in-flight operations complete",
		DenialCode:   controlapipkg.DenialCodeControlPlaneStateSaturated,
		Metadata: map[string]interface{}{
			"overload_scope": capabilityExecutionOverloadScope,
			"retryable":      true,
		},
	}
}
