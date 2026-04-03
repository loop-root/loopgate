package model

import "strings"

// HavenConstrainedNativeToolsRuntimeFact is appended to model.Request.RuntimeFacts by Haven
// so the prompt compiler can select narrower native TOOL USE text without a separate JSON field
// (Loopgate decodes model requests with DisallowUnknownFields).
const HavenConstrainedNativeToolsRuntimeFact = "haven_constrained_native_tools:v1"

// HavenCompactNativeDispatchRuntimeFact marks that Haven sends only invoke_capability in NativeToolDefs;
// orchestrator expands to real capability names before Loopgate execution.
const HavenCompactNativeDispatchRuntimeFact = "haven_compact_native_dispatch:v1"

// ConstrainedNativeToolsFromRuntimeFacts reports whether Haven asked for constrained native tool policy.
func ConstrainedNativeToolsFromRuntimeFacts(runtimeFacts []string) bool {
	for _, fact := range runtimeFacts {
		if strings.TrimSpace(fact) == HavenConstrainedNativeToolsRuntimeFact {
			return true
		}
	}
	return false
}

// StripHavenInternalRuntimeFacts removes machine-only markers from facts shown in the model system prompt.
func StripHavenInternalRuntimeFacts(runtimeFacts []string) []string {
	if len(runtimeFacts) == 0 {
		return nil
	}
	out := make([]string, 0, len(runtimeFacts))
	for _, fact := range runtimeFacts {
		trimmed := strings.TrimSpace(fact)
		if trimmed == HavenConstrainedNativeToolsRuntimeFact || trimmed == HavenCompactNativeDispatchRuntimeFact {
			continue
		}
		out = append(out, fact)
	}
	return out
}
