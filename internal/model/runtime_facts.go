package model

import "strings"

// ConstrainedNativeToolsRuntimeFact is a legacy runtime marker used to request
// narrower native tool-use prompt text without a separate JSON field.
// The string value stays stable for compatibility with older local clients.
// (Loopgate decodes model requests with DisallowUnknownFields).
const ConstrainedNativeToolsRuntimeFact = "haven_constrained_native_tools:v1"

// CompactNativeDispatchRuntimeFact is a legacy runtime marker indicating the
// client sent only invoke_capability in NativeToolDefs as a compact provider
// schema rather than enumerating every governed capability separately.
const CompactNativeDispatchRuntimeFact = "haven_compact_native_dispatch:v1"

// ConstrainedNativeToolsFromRuntimeFacts reports whether the client asked for
// constrained native tool policy.
func ConstrainedNativeToolsFromRuntimeFacts(runtimeFacts []string) bool {
	for _, fact := range runtimeFacts {
		if strings.TrimSpace(fact) == ConstrainedNativeToolsRuntimeFact {
			return true
		}
	}
	return false
}

// StripInternalRuntimeFacts removes machine-only markers from facts shown in
// the model system prompt.
func StripInternalRuntimeFacts(runtimeFacts []string) []string {
	if len(runtimeFacts) == 0 {
		return nil
	}
	out := make([]string, 0, len(runtimeFacts))
	for _, fact := range runtimeFacts {
		trimmed := strings.TrimSpace(fact)
		if trimmed == ConstrainedNativeToolsRuntimeFact || trimmed == CompactNativeDispatchRuntimeFact {
			continue
		}
		out = append(out, fact)
	}
	return out
}
