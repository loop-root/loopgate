package tcl

import "strings"

var explicitMemoryExactKeyAliases = map[string]string{
	"name":                "name",
	"user.name":           "name",
	"user_name":           "name",
	"my_name":             "name",
	"full_name":           "name",
	"preferred_name":      "preferred_name",
	"preferred-name":      "preferred_name",
	"user_preferred_name": "preferred_name",
	"timezone":            "profile.timezone",
	"locale":              "profile.locale",
}

type explicitMemoryPrefixRule struct {
	rawPrefix       string
	canonicalPrefix string
}

// The explicit-memory registry stays compiled and conservative because its key families,
// anchor behavior, and runtime hints are still coupled in code. Until we have a signed
// admin-distributed registry path with the same fail-closed semantics, unknown families deny.
var explicitMemoryPrefixRules = []explicitMemoryPrefixRule{
	{rawPrefix: "user.", canonicalPrefix: "profile."},
	{rawPrefix: "profile.", canonicalPrefix: "profile."},
	{rawPrefix: "preference.", canonicalPrefix: "preference."},
	{rawPrefix: "routine.", canonicalPrefix: "routine."},
	{rawPrefix: "project.", canonicalPrefix: "project."},
	{rawPrefix: "goal.", canonicalPrefix: "goal."},
	{rawPrefix: "work.", canonicalPrefix: "work."},
}

// CanonicalizeExplicitMemoryFactKey applies the shared explicit-memory key registry used by
// Loopgate request validation and TCL normalization. Unsupported keys return the empty string.
func CanonicalizeExplicitMemoryFactKey(rawFactKey string) string {
	normalizedFactKey := strings.ToLower(strings.TrimSpace(rawFactKey))
	if canonicalFactKey, found := explicitMemoryExactKeyAliases[normalizedFactKey]; found {
		return canonicalFactKey
	}

	switch normalizedFactKey {
	case "preference.theme", "preference.ui_theme":
		return "preference.stated_preference"
	}

	for _, prefixRule := range explicitMemoryPrefixRules {
		if !strings.HasPrefix(normalizedFactKey, prefixRule.rawPrefix) {
			continue
		}

		trimmedSuffix := strings.TrimPrefix(normalizedFactKey, prefixRule.rawPrefix)
		// Bare namespace writes are denied because they would create ambiguous slots whose
		// meaning could drift as the registry evolves. Callers must name a concrete field so
		// persisted facts stay stable across future registry changes.
		if strings.TrimSpace(trimmedSuffix) == "" {
			return ""
		}
		return prefixRule.canonicalPrefix + trimmedSuffix
	}

	return ""
}

// DeriveExplicitMemoryConflictAnchor derives the stable contradiction slot for a canonical
// explicit-memory fact key. The caller must pass a key already normalized through
// CanonicalizeExplicitMemoryFactKey.
func DeriveExplicitMemoryConflictAnchor(canonicalFactKey string, normalizedFactValue string) *ConflictAnchor {
	switch {
	case canonicalFactKey == "name":
		return &ConflictAnchor{
			Version:  "v1",
			Domain:   "usr_profile",
			Entity:   "identity",
			SlotKind: "fact",
			SlotName: "name",
		}
	case canonicalFactKey == "preferred_name":
		return &ConflictAnchor{
			Version:  "v1",
			Domain:   "usr_profile",
			Entity:   "identity",
			SlotKind: "fact",
			SlotName: "preferred_name",
		}
	case canonicalFactKey == "profile.timezone":
		return &ConflictAnchor{
			Version:  "v1",
			Domain:   "usr_profile",
			Entity:   "settings",
			SlotKind: "fact",
			SlotName: "timezone",
		}
	case canonicalFactKey == "profile.locale":
		return &ConflictAnchor{
			Version:  "v1",
			Domain:   "usr_profile",
			Entity:   "settings",
			SlotKind: "fact",
			SlotName: "locale",
		}
	case strings.HasPrefix(canonicalFactKey, "preference.favorite_"):
		return &ConflictAnchor{
			Version:  "v1",
			Domain:   "usr_preference",
			Entity:   "favorite",
			SlotKind: "fact",
			SlotName: strings.TrimPrefix(canonicalFactKey, "preference."),
		}
	case canonicalFactKey == "preference.stated_preference":
		facet := deriveExplicitPreferenceFacet(normalizedFactValue)
		if facet == "" {
			return nil
		}
		return &ConflictAnchor{
			Version:  "v1",
			Domain:   "usr_preference",
			Entity:   "stated",
			SlotKind: "fact",
			SlotName: "preference",
			Facet:    facet,
		}
	default:
		return nil
	}
}

func deriveExplicitPreferenceFacet(normalizedFactValue string) string {
	analysisText := strings.ToLower(strings.TrimSpace(normalizedFactValue))
	// These phrase-level facets are a secondary fallback while validated TCL candidates are
	// not yet the authoritative normalization path for all explicit preference writes. Keep
	// this table narrow so supersession expands only through explicit, test-backed rules.
	switch {
	case strings.Contains(analysisText, "dark mode"), strings.Contains(analysisText, "light mode"):
		return "ui_theme"
	case strings.Contains(analysisText, "sepia mode"),
		strings.Contains(analysisText, "theme preference"):
		return "ui_theme"
	case strings.Contains(analysisText, "bullet"),
		strings.Contains(analysisText, "list"),
		strings.Contains(analysisText, "numbered"):
		return "response_format"
	case strings.Contains(analysisText, "tabs for indentation"),
		strings.Contains(analysisText, "spaces for indentation"),
		strings.Contains(analysisText, "indentation in code blocks"):
		return "indentation"
	case strings.Contains(analysisText, "concise"),
		strings.Contains(analysisText, "brief"),
		strings.Contains(analysisText, "short"),
		strings.Contains(analysisText, "terse"),
		strings.Contains(analysisText, "detailed"),
		strings.Contains(analysisText, "verbose"),
		strings.Contains(analysisText, "thorough"):
		return "verbosity"
	case strings.Contains(analysisText, "formal"),
		strings.Contains(analysisText, "professional"),
		strings.Contains(analysisText, "casual"),
		strings.Contains(analysisText, "informal"):
		return "tone"
	case strings.Contains(analysisText, "one question"),
		strings.Contains(analysisText, "single question"):
		return "question_style"
	case strings.Contains(analysisText, "morning"),
		strings.Contains(analysisText, "evening"),
		strings.Contains(analysisText, "afternoon"),
		strings.Contains(analysisText, "night"):
		return "time_of_day"
	default:
		// Unknown preference language stays unanchored on purpose so we do not silently widen
		// supersession semantics before TCL candidate generation owns this path.
		return ""
	}
}
