package tcl

import "testing"

func TestCanonicalizeExplicitMemoryFactKey_UsesSharedAliases(t *testing.T) {
	testCases := []struct {
		rawKey           string
		wantCanonicalKey string
	}{
		{rawKey: "name", wantCanonicalKey: "name"},
		{rawKey: "user.name", wantCanonicalKey: "name"},
		{rawKey: "user_name", wantCanonicalKey: "name"},
		{rawKey: "my_name", wantCanonicalKey: "name"},
		{rawKey: "full_name", wantCanonicalKey: "name"},
		{rawKey: "preferred_name", wantCanonicalKey: "preferred_name"},
		{rawKey: "preferred-name", wantCanonicalKey: "preferred_name"},
		{rawKey: "user_preferred_name", wantCanonicalKey: "preferred_name"},
		{rawKey: "preference.theme", wantCanonicalKey: "preference.stated_preference"},
		{rawKey: "preference.ui_theme", wantCanonicalKey: "preference.stated_preference"},
		{rawKey: "Preference.Coffee_Order", wantCanonicalKey: "preference.coffee_order"},
		{rawKey: "routine.Friday_Gym", wantCanonicalKey: "routine.friday_gym"},
		{rawKey: "project.Current_Focus", wantCanonicalKey: "project.current_focus"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.rawKey, func(t *testing.T) {
			gotCanonicalKey := CanonicalizeExplicitMemoryFactKey(testCase.rawKey)
			if gotCanonicalKey != testCase.wantCanonicalKey {
				t.Fatalf("key %q: want %q, got %q", testCase.rawKey, testCase.wantCanonicalKey, gotCanonicalKey)
			}
		})
	}
}

func TestCanonicalizeExplicitMemoryFactKey_ProfileSettingsAliases(t *testing.T) {
	testCases := []struct {
		rawKey           string
		wantCanonicalKey string
	}{
		{rawKey: "timezone", wantCanonicalKey: "profile.timezone"},
		{rawKey: "user.timezone", wantCanonicalKey: "profile.timezone"},
		{rawKey: "profile.timezone", wantCanonicalKey: "profile.timezone"},
		{rawKey: "locale", wantCanonicalKey: "profile.locale"},
		{rawKey: "user.locale", wantCanonicalKey: "profile.locale"},
		{rawKey: "profile.locale", wantCanonicalKey: "profile.locale"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.rawKey, func(t *testing.T) {
			gotCanonicalKey := CanonicalizeExplicitMemoryFactKey(testCase.rawKey)
			if gotCanonicalKey != testCase.wantCanonicalKey {
				t.Fatalf("key %q: want %q, got %q", testCase.rawKey, testCase.wantCanonicalKey, gotCanonicalKey)
			}
		})
	}
}

func TestCanonicalizeExplicitMemoryFactKey_DeniesUnsupportedKeys(t *testing.T) {
	for _, rawKey := range []string{"", "nickname", "workspace.repo", "user.", "profile."} {
		t.Run(rawKey, func(t *testing.T) {
			if gotCanonicalKey := CanonicalizeExplicitMemoryFactKey(rawKey); gotCanonicalKey != "" {
				t.Fatalf("key %q: expected unsupported key to canonicalize to empty string, got %q", rawKey, gotCanonicalKey)
			}
		})
	}
}

func TestCanonicalizeExplicitMemoryFactKey_FamilyMatrix(t *testing.T) {
	testCases := []struct {
		name             string
		rawKey           string
		wantCanonicalKey string
	}{
		{
			name:             "goal family with valid suffix",
			rawKey:           "goal.current_sprint",
			wantCanonicalKey: "goal.current_sprint",
		},
		{
			name:             "work family with valid suffix",
			rawKey:           "work.focus_area",
			wantCanonicalKey: "work.focus_area",
		},
		{
			name:             "goal family with empty suffix",
			rawKey:           "goal.",
			wantCanonicalKey: "",
		},
		{
			name:             "work family with empty suffix",
			rawKey:           "work.",
			wantCanonicalKey: "",
		},
		{
			name:             "unsupported context family",
			rawKey:           "context.recent_topic",
			wantCanonicalKey: "",
		},
		{
			name:             "unsupported unknown family",
			rawKey:           "unknown.current_sprint",
			wantCanonicalKey: "",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			gotCanonicalKey := CanonicalizeExplicitMemoryFactKey(testCase.rawKey)
			if gotCanonicalKey != testCase.wantCanonicalKey {
				t.Fatalf("key %q: want canonical key %q, got %q", testCase.rawKey, testCase.wantCanonicalKey, gotCanonicalKey)
			}
		})
	}
}
