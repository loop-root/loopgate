package memorybench

import (
	"fmt"
	"strings"
)

func mustAnnotateFixtureContinuityParitySeedSpec(fixture ScenarioFixture) ScenarioFixture {
	annotatedFixture := fixture
	annotatedFixture.ContinuityParitySeedSpec = defaultContinuityParitySeedSpec(fixture)
	if err := ValidateContinuityParitySeedSpec(annotatedFixture); err != nil {
		panic(fmt.Sprintf("invalid continuity parity seed spec for fixture %q: %v", fixture.Metadata.ScenarioID, err))
	}
	return annotatedFixture
}

func ValidateContinuityParitySeedSpec(fixture ScenarioFixture) error {
	if fixture.ContinuityParitySeedSpec == nil {
		return nil
	}
	validatedSeedSpec := fixture.ContinuityParitySeedSpec
	if strings.TrimSpace(validatedSeedSpec.DistractorPath) == ContinuitySeedPathRememberMemoryFact {
		return fmt.Errorf("preview and distractor seeds must not use %q", ContinuitySeedPathRememberMemoryFact)
	}
	currentUsesValidatedWrite := strings.TrimSpace(validatedSeedSpec.CurrentPath) == ContinuitySeedPathRememberMemoryFact
	suppressedUsesValidatedWrite := strings.TrimSpace(validatedSeedSpec.SuppressedPath) == ContinuitySeedPathRememberMemoryFact
	trimmedCanonicalFactKey := strings.TrimSpace(validatedSeedSpec.CanonicalFactKey)
	if currentUsesValidatedWrite || suppressedUsesValidatedWrite {
		if trimmedCanonicalFactKey == "" {
			return fmt.Errorf("canonical fact key is required when current or suppressed path uses %q", ContinuitySeedPathRememberMemoryFact)
		}
		return nil
	}
	if trimmedCanonicalFactKey != "" {
		return fmt.Errorf("canonical fact key is only allowed when current or suppressed path uses %q", ContinuitySeedPathRememberMemoryFact)
	}
	return nil
}

func defaultContinuityParitySeedSpec(fixture ScenarioFixture) *ContinuityParitySeedSpec {
	switch fixture.Metadata.Category {
	case CategoryMemoryContradiction:
		return defaultContradictionContinuityParitySeedSpec(fixture)
	case CategoryTaskResumption:
		return &ContinuityParitySeedSpec{
			CurrentPath:    ContinuitySeedPathFixtureIngest,
			SuppressedPath: ContinuitySeedPathFixtureIngest,
		}
	default:
		return nil
	}
}

func defaultContradictionContinuityParitySeedSpec(fixture ScenarioFixture) *ContinuityParitySeedSpec {
	canonicalFactKey := contradictionValidatedWriteCanonicalFactKey(fixture)
	if canonicalFactKey == "" {
		return &ContinuityParitySeedSpec{
			CurrentPath:    ContinuitySeedPathFixtureIngest,
			SuppressedPath: ContinuitySeedPathFixtureIngest,
			DistractorPath: ContinuitySeedPathFixtureIngest,
		}
	}
	return &ContinuityParitySeedSpec{
		CurrentPath:      ContinuitySeedPathRememberMemoryFact,
		SuppressedPath:   ContinuitySeedPathRememberMemoryFact,
		DistractorPath:   ContinuitySeedPathFixtureIngest,
		CanonicalFactKey: canonicalFactKey,
	}
}

func contradictionValidatedWriteCanonicalFactKey(fixture ScenarioFixture) string {
	if fixture.ContradictionExpectation == nil {
		return ""
	}
	normalizedScenarioID := strings.ToLower(strings.TrimSpace(fixture.Metadata.ScenarioID))
	if strings.Contains(normalizedScenarioID, "preview_only_control") {
		return ""
	}
	normalizedSignatureHint := strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(fixture.ContradictionExpectation.CurrentSignatureHint))), " ")
	switch {
	case strings.Contains(normalizedSignatureHint, "identity timezone slot timezone"):
		return "profile.timezone"
	case strings.Contains(normalizedSignatureHint, "identity locale slot locale"):
		return "profile.locale"
	case strings.Contains(normalizedSignatureHint, "identity name slot preferred_name"):
		return "preferred_name"
	case strings.Contains(normalizedSignatureHint, "identity name slot name"):
		return "name"
	case strings.HasPrefix(normalizedScenarioID, "contradiction.preference_"):
		return "preference.stated_preference"
	default:
		return ""
	}
}
