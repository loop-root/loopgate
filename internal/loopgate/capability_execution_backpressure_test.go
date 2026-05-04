package loopgate

import (
	"context"
	"strings"
	"testing"

	controlapipkg "loopgate/internal/loopgate/controlapi"
)

func TestCapabilityExecutionBackpressureRejectsWhenSaturated(t *testing.T) {
	server := &Server{
		capabilityExecutionSlots: make(chan struct{}, 1),
	}
	server.capabilityExecutionSlots <- struct{}{}

	response := server.executeCapabilityRequest(context.Background(), capabilityToken{
		ControlSessionID:   "session-a",
		ActorLabel:         "agent",
		ClientSessionLabel: "client-a",
	}, controlapipkg.CapabilityRequest{
		RequestID:  "req-saturated",
		Capability: "fs_read",
		Arguments:  map[string]string{"path": "README.md"},
	}, false)

	if response.Status != controlapipkg.ResponseStatusDenied {
		t.Fatalf("expected denied response, got %q", response.Status)
	}
	if response.DenialCode != controlapipkg.DenialCodeControlPlaneStateSaturated {
		t.Fatalf("expected denial code %q, got %q", controlapipkg.DenialCodeControlPlaneStateSaturated, response.DenialCode)
	}
	if !strings.Contains(response.DenialReason, "capability execution concurrency limit") {
		t.Fatalf("expected agent-readable saturation reason, got %q", response.DenialReason)
	}
	if response.Metadata["overload_scope"] != capabilityExecutionOverloadScope {
		t.Fatalf("expected overload scope metadata, got %#v", response.Metadata)
	}
	if response.Metadata["retryable"] != true {
		t.Fatalf("expected retryable metadata, got %#v", response.Metadata)
	}
}

func TestCapabilityExecutionSlotCanBeReleasedAndReacquired(t *testing.T) {
	server := &Server{}
	server.configureCapabilityExecutionSlots(1)

	release, ok := server.tryAcquireCapabilityExecutionSlot()
	if !ok {
		t.Fatal("expected first acquisition to succeed")
	}
	if _, ok := server.tryAcquireCapabilityExecutionSlot(); ok {
		t.Fatal("expected second acquisition to fail while saturated")
	}

	release()
	release, ok = server.tryAcquireCapabilityExecutionSlot()
	if !ok {
		t.Fatal("expected acquisition to succeed after release")
	}
	release()
}
