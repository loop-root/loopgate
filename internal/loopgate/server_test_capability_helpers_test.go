package loopgate

import (
	"encoding/json"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"testing"
)

func mustJSON(t *testing.T, value interface{}) []byte {
	t.Helper()
	encodedBytes, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal test json: %v", err)
	}
	return encodedBytes
}

func capabilityNames(capabilities []controlapipkg.CapabilitySummary) []string {
	names := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		names = append(names, capability.Name)
	}
	return names
}

func advertisedSessionCapabilityNames(status controlapipkg.StatusResponse) []string {
	advertisedCapabilities := capabilityNames(status.Capabilities)
	advertisedCapabilities = append(advertisedCapabilities, capabilityNames(status.ControlCapabilities)...)
	return advertisedCapabilities
}

func containsCapability(capabilities []controlapipkg.CapabilitySummary, capabilityName string) bool {
	for _, capability := range capabilities {
		if capability.Name == capabilityName {
			return true
		}
	}
	return false
}
