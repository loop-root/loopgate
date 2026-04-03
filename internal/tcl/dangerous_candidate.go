package tcl

import "strings"

// This classifier stays separate from normalization so maintainers can change review heuristics
// without having to untangle them from anchor/key shaping in the same function.
func isDangerousCandidate(candidate MemoryCandidateInput) bool {
	analysisText := strings.ToLower(strings.Join([]string{
		strings.TrimSpace(candidate.RawSourceText),
		strings.TrimSpace(candidate.NormalizedFactValue),
	}, "\n"))
	containsSecretMaterial := containsAnyFold(analysisText,
		"secret",
		"token",
		"credential",
		"deployment key",
		"deployment credential",
		"access token",
		"api key",
		"password",
	)
	containsInstructionBypass := containsAnyFold(analysisText,
		"ignore previous safety",
		"ignore previous instructions",
		"ignore safety instructions",
		"ignore all safety rules",
		"hidden instruction",
		"skip safety review",
		"bypass safety review",
	) ||
		(containsAnyFold(analysisText, "set aside", "supersede", "higher priority", "outrank") &&
			containsAnyFold(analysisText, "control checks", "guardrails", "safety review", "safety rules", "instructions"))
	containsAuthoritySpoof := containsAnyFold(analysisText,
		"loopgate audit note",
		"system override",
		"policy override",
		"approval token",
		"override token",
		"control-plane note",
		"operator override memo",
		"approved override memo",
	)
	containsExfiltrationVerb := containsAnyFold(analysisText,
		"reveal",
		"export",
		"expose",
		"share with any caller",
		"hand over",
		"surface",
		"disclose",
	)
	return (containsSecretMaterial && containsInstructionBypass) ||
		(containsSecretMaterial && containsExfiltrationVerb) ||
		(containsAuthoritySpoof && (containsSecretMaterial || containsInstructionBypass || containsExfiltrationVerb))
}

func containsAnyFold(rawText string, wantedSubstrings ...string) bool {
	for _, wantedSubstring := range wantedSubstrings {
		if strings.Contains(rawText, wantedSubstring) {
			return true
		}
	}
	return false
}
