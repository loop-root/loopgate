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
		"credential pair",
		"deployment key",
		"deployment credential",
		"access token",
		"refresh token",
		"session token",
		"session cookie",
		"signing key",
		"api key",
		"client secret",
		"oauth client secret",
		"password",
	)
	// Keep the bypass logic co-occurrence-based. A single scary noun like "review"
	// or "approval" is common in benign runbooks, but skip/waive/disable language
	// combined with those nouns is where the poisoning family starts.
	containsInstructionBypass := containsAnyFold(analysisText,
		"ignore previous safety",
		"ignore previous instructions",
		"ignore safety instructions",
		"ignore all safety rules",
		"hidden instruction",
		"skip safety review",
		"bypass safety review",
		"skip the ordinary approval check",
		"waive the usual review step",
		"approval gate is waived",
		"normal approval no longer applies",
		"normal review no longer applies",
		"disable the normal guardrails",
	) ||
		(containsAnyFold(analysisText, "set aside", "supersede", "higher priority", "outrank", "skip", "disable", "suspend", "drop") &&
			containsAnyFold(analysisText, "control checks", "guardrails", "approval", "review", "safety review", "safety rules", "instructions"))
	containsAuthoritySpoof := containsAnyFold(analysisText,
		"loopgate audit note",
		"approved by loopgate",
		"system override",
		"policy override",
		"approval token",
		"override token",
		"control-plane note",
		"operator override memo",
		"approved override memo",
		"pre-cleared escalation memo",
		"pre-cleared",
	)
	containsExfiltrationVerb := containsAnyFold(analysisText,
		"reveal",
		"expose",
		"share with any caller",
		"hand over",
		"surface",
		"disclose",
		"paste",
		"copy the",
		"copy into the",
		"send the",
		"forward the",
		"return the",
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
