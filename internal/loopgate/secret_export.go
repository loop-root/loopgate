package loopgate

import "strings"

// secretExportCapabilityNameHeuristic classifies capability names that are treated as raw secret-export
// surfaces when no registered tool is found (unregistered path). Registered tools do not fall back to
// this heuristic; they must implement tool.RawSecretExportProhibited and/or
// tool.SecretExportNameHeuristicOptOut (see capabilityProhibitsRawSecretExport). Configured HTTP
// capabilities use configuredCapabilityTool.RawSecretExportProhibited, which delegates here.
func secretExportCapabilityNameHeuristic(capability string) bool {
	lowerCapability := strings.ToLower(strings.TrimSpace(capability))
	if lowerCapability == "" {
		return false
	}

	sensitivePrefixes := []string{
		"secret.",
		"token.",
		"credential.",
		"credentials.",
		"key.",
	}
	for _, sensitivePrefix := range sensitivePrefixes {
		if strings.HasPrefix(lowerCapability, sensitivePrefix) {
			return true
		}
	}

	if strings.Contains(lowerCapability, "export") && (strings.Contains(lowerCapability, "token") || strings.Contains(lowerCapability, "secret") || strings.Contains(lowerCapability, "credential") || strings.Contains(lowerCapability, "key")) {
		return true
	}
	return false
}
