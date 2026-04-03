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

func TestCanonicalizeExplicitMemoryFactKey_DeniesUnsupportedKeys(t *testing.T) {
	for _, rawKey := range []string{"", "nickname", "workspace.repo", "user.", "profile."} {
		t.Run(rawKey, func(t *testing.T) {
			if gotCanonicalKey := CanonicalizeExplicitMemoryFactKey(rawKey); gotCanonicalKey != "" {
				t.Fatalf("key %q: expected unsupported key to canonicalize to empty string, got %q", rawKey, gotCanonicalKey)
			}
		})
	}
}
