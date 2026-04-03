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
}

type explicitMemoryPrefixRule struct {
	rawPrefix       string
	canonicalPrefix string
}

var explicitMemoryPrefixRules = []explicitMemoryPrefixRule{
	{rawPrefix: "user.", canonicalPrefix: "profile."},
	{rawPrefix: "profile.", canonicalPrefix: "profile."},
	{rawPrefix: "preference.", canonicalPrefix: "preference."},
	{rawPrefix: "routine.", canonicalPrefix: "routine."},
	{rawPrefix: "project.", canonicalPrefix: "project."},
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
	switch {
	case strings.Contains(analysisText, "dark mode"), strings.Contains(analysisText, "light mode"):
		return "ui_theme"
	case strings.Contains(analysisText, "morning"),
		strings.Contains(analysisText, "evening"),
		strings.Contains(analysisText, "afternoon"),
		strings.Contains(analysisText, "night"):
		return "time_of_day"
	default:
		return ""
	}
}
