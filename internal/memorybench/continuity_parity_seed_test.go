package memorybench

import "testing"

func TestContinuityParitySeedSpec_RequiresCanonicalKeyOnlyForRememberPaths(t *testing.T) {
	if err := ValidateContinuityParitySeedSpec(ScenarioFixture{
		Metadata: ScenarioMetadata{ScenarioID: "contradiction.profile_timezone_same_entity_wrong_current_probe.v1"},
		ContinuityParitySeedSpec: &ContinuityParitySeedSpec{
			CurrentPath: ContinuitySeedPathRememberMemoryFact,
		},
	}); err == nil {
		t.Fatal("expected remember-memory seed spec without canonical key to fail")
	}

	if err := ValidateContinuityParitySeedSpec(ScenarioFixture{
		Metadata: ScenarioMetadata{ScenarioID: "contradiction.profile_timezone_preview_only_control.v1"},
		ContinuityParitySeedSpec: &ContinuityParitySeedSpec{
			CurrentPath:      ContinuitySeedPathFixtureIngest,
			SuppressedPath:   ContinuitySeedPathFixtureIngest,
			DistractorPath:   ContinuitySeedPathFixtureIngest,
			CanonicalFactKey: "profile.timezone",
		},
	}); err == nil {
		t.Fatal("expected non-remember seed spec with canonical key to fail")
	}
}

func TestContinuityParitySeedSpec_RejectsPreviewDistractorRememberPath(t *testing.T) {
	if err := ValidateContinuityParitySeedSpec(ScenarioFixture{
		Metadata: ScenarioMetadata{ScenarioID: "contradiction.profile_timezone_same_entity_wrong_current_probe.v1"},
		ContinuityParitySeedSpec: &ContinuityParitySeedSpec{
			CurrentPath:      ContinuitySeedPathRememberMemoryFact,
			SuppressedPath:   ContinuitySeedPathRememberMemoryFact,
			DistractorPath:   ContinuitySeedPathRememberMemoryFact,
			CanonicalFactKey: "profile.timezone",
		},
	}); err == nil {
		t.Fatal("expected preview/distractor remember path to fail")
	}
}

func TestDefaultScenarioFixtures_ProfileSlotParitySeedSpecsStayDeterministic(t *testing.T) {
	defaultFixtures := DefaultScenarioFixtures()
	var timezoneFixture ScenarioFixture
	var previewOnlyFixture ScenarioFixture
	foundTimezoneFixture := false
	foundPreviewOnlyFixture := false
	for _, defaultFixture := range defaultFixtures {
		switch defaultFixture.Metadata.ScenarioID {
		case "contradiction.profile_timezone_same_entity_wrong_current_probe.v1":
			timezoneFixture = defaultFixture
			foundTimezoneFixture = true
		case "contradiction.profile_timezone_preview_only_control.v1":
			previewOnlyFixture = defaultFixture
			foundPreviewOnlyFixture = true
		}
	}
	if !foundTimezoneFixture || !foundPreviewOnlyFixture {
		t.Fatalf("expected targeted continuity parity fixtures, got timezone=%t previewOnly=%t", foundTimezoneFixture, foundPreviewOnlyFixture)
	}
	if timezoneFixture.ContinuityParitySeedSpec == nil {
		t.Fatal("expected timezone fixture to carry continuity parity seed spec")
	}
	if timezoneFixture.ContinuityParitySeedSpec.CurrentPath != ContinuitySeedPathRememberMemoryFact ||
		timezoneFixture.ContinuityParitySeedSpec.SuppressedPath != ContinuitySeedPathRememberMemoryFact ||
		timezoneFixture.ContinuityParitySeedSpec.DistractorPath != ContinuitySeedPathFixtureIngest ||
		timezoneFixture.ContinuityParitySeedSpec.CanonicalFactKey != "profile.timezone" {
		t.Fatalf("unexpected timezone continuity parity seed spec: %#v", timezoneFixture.ContinuityParitySeedSpec)
	}
	if previewOnlyFixture.ContinuityParitySeedSpec == nil {
		t.Fatal("expected preview-only control fixture to carry continuity parity seed spec")
	}
	if previewOnlyFixture.ContinuityParitySeedSpec.CurrentPath != ContinuitySeedPathFixtureIngest ||
		previewOnlyFixture.ContinuityParitySeedSpec.DistractorPath != ContinuitySeedPathFixtureIngest ||
		previewOnlyFixture.ContinuityParitySeedSpec.CanonicalFactKey != "" {
		t.Fatalf("unexpected preview-only continuity parity seed spec: %#v", previewOnlyFixture.ContinuityParitySeedSpec)
	}
}
