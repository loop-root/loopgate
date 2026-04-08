package loopgate

import (
	"testing"

	"morph/internal/config"
	toolspkg "morph/internal/tools"
)

// Regression: default registry tools that match the unregistered secret-export name heuristic must
// declare classification via RawSecretExportProhibited or SecretExportNameHeuristicOptOut so
// registered-tool execution does not silently lose a defense-in-depth layer when names are added.
func TestDefaultRegistryToolsDeclareSecretExportClassificationWhenNameMatchesHeuristic(t *testing.T) {
	repoRoot := t.TempDir()
	registry, err := toolspkg.NewDefaultRegistry(repoRoot, config.Policy{})
	if err != nil {
		t.Fatalf("NewDefaultRegistry: %v", err)
	}
	for _, tool := range registry.All() {
		name := tool.Name()
		if !secretExportCapabilityNameHeuristic(name) {
			continue
		}
		_, hasProhibited := tool.(toolspkg.RawSecretExportProhibited)
		_, hasOptOut := tool.(toolspkg.SecretExportNameHeuristicOptOut)
		if !hasProhibited && !hasOptOut {
			t.Fatalf("tool %q matches secret-export name heuristic but implements neither RawSecretExportProhibited nor SecretExportNameHeuristicOptOut", name)
		}
	}
}
