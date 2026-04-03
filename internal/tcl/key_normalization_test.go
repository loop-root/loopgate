package tcl

import "testing"

func TestNormalizeExplicitFactKey_NameAliasesCollapseToName(t *testing.T) {
	base := MemoryCandidate{
		Source:              CandidateSourceExplicitFact,
		SourceChannel:       "user_input",
		NormalizedFactValue: "Ada",
		Trust:               TrustUserOriginated,
		Actor:               ObjectUser,
	}
	wantKey := "usr_profile:identity:fact:name"

	for _, aliasKey := range []string{"user.name", "user_name", "my_name", "full_name"} {
		t.Run(aliasKey, func(t *testing.T) {
			c := base
			c.NormalizedFactKey = aliasKey
			node, err := NormalizeMemoryCandidate(c)
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			if node.ANCHOR == nil || node.ANCHOR.CanonicalKey() != wantKey {
				t.Fatalf("key %q: want anchor %q, got %#v", aliasKey, wantKey, node.ANCHOR)
			}
		})
	}
}

func TestNormalizeExplicitFactKey_PreferredNameAliasesCollapse(t *testing.T) {
	base := MemoryCandidate{
		Source:              CandidateSourceExplicitFact,
		SourceChannel:       "user_input",
		NormalizedFactValue: "Alex",
		Trust:               TrustUserOriginated,
		Actor:               ObjectUser,
	}
	wantKey := "usr_profile:identity:fact:preferred_name"

	for _, key := range []string{"user_preferred_name", "preferred_name", "preferred-name"} {
		t.Run(key, func(t *testing.T) {
			c := base
			c.NormalizedFactKey = key
			node, err := NormalizeMemoryCandidate(c)
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			if node.ANCHOR == nil || node.ANCHOR.CanonicalKey() != wantKey {
				t.Fatalf("key %q: want anchor %q, got %#v", key, wantKey, node.ANCHOR)
			}
		})
	}
}

func TestNormalizeExplicitFactKey_BoundedNamespaceRemainsAllowed(t *testing.T) {
	node, err := NormalizeMemoryCandidate(MemoryCandidate{
		Source:              CandidateSourceExplicitFact,
		SourceChannel:       "user_input",
		NormalizedFactKey:   "Preference.Coffee_Order",
		NormalizedFactValue: "oat milk cappuccino",
		Trust:               TrustUserOriginated,
		Actor:               ObjectUser,
	})
	if err != nil {
		t.Fatalf("normalize namespaced key: %v", err)
	}
	if node.ANCHOR != nil {
		t.Fatalf("expected generic bounded namespace key to remain unanchored, got %#v", node.ANCHOR)
	}
}

func TestNormalizeExplicitFactKey_PreferenceAliasesCollapseToStableFacet(t *testing.T) {
	wantKey := "usr_preference:stated:fact:preference:ui_theme"
	value := "dark mode"

	for _, key := range []string{"preference.theme", "preference.ui_theme", "preference.stated_preference"} {
		t.Run(key, func(t *testing.T) {
			node, err := NormalizeMemoryCandidate(MemoryCandidate{
				Source:              CandidateSourceExplicitFact,
				SourceChannel:       "user_input",
				NormalizedFactKey:   key,
				NormalizedFactValue: value,
				Trust:               TrustUserOriginated,
				Actor:               ObjectUser,
			})
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			if node.ANCHOR == nil || node.ANCHOR.CanonicalKey() != wantKey {
				t.Fatalf("key %q: want anchor %q, got %#v", key, wantKey, node.ANCHOR)
			}
		})
	}
}

func TestNormalizeExplicitFactKey_PreferenceFallbackFacetAnchors(t *testing.T) {
	testCases := []struct {
		name          string
		factValue     string
		wantAnchorKey string
	}{
		{
			name:          "concise answers maps to verbosity",
			factValue:     "I prefer concise answers",
			wantAnchorKey: "usr_preference:stated:fact:preference:verbosity",
		},
		{
			name:          "brief maps to verbosity",
			factValue:     "please be brief",
			wantAnchorKey: "usr_preference:stated:fact:preference:verbosity",
		},
		{
			name:          "detailed maps to verbosity",
			factValue:     "be detailed",
			wantAnchorKey: "usr_preference:stated:fact:preference:verbosity",
		},
		{
			name:          "bullet points map to response format",
			factValue:     "use bullet points",
			wantAnchorKey: "usr_preference:stated:fact:preference:response_format",
		},
		{
			name:          "numbered lists map to response format",
			factValue:     "numbered lists please",
			wantAnchorKey: "usr_preference:stated:fact:preference:response_format",
		},
		{
			name:          "formal tone maps to tone",
			factValue:     "use a formal tone",
			wantAnchorKey: "usr_preference:stated:fact:preference:tone",
		},
		{
			name:          "casual maps to tone",
			factValue:     "be casual",
			wantAnchorKey: "usr_preference:stated:fact:preference:tone",
		},
		{
			name:          "one question maps to question style",
			factValue:     "ask one question at a time",
			wantAnchorKey: "usr_preference:stated:fact:preference:question_style",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			node, err := NormalizeMemoryCandidate(MemoryCandidate{
				Source:              CandidateSourceExplicitFact,
				SourceChannel:       "user_input",
				NormalizedFactKey:   "preference.stated_preference",
				NormalizedFactValue: testCase.factValue,
				Trust:               TrustUserOriginated,
				Actor:               ObjectUser,
			})
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			if node.ANCHOR == nil {
				t.Fatalf("expected anchor %q, got nil", testCase.wantAnchorKey)
			}
			if node.ANCHOR.Version != "v1" {
				t.Fatalf("expected anchor version v1, got %#v", node.ANCHOR)
			}
			if node.ANCHOR.CanonicalKey() != testCase.wantAnchorKey {
				t.Fatalf("value %q: want anchor %q, got %#v", testCase.factValue, testCase.wantAnchorKey, node.ANCHOR)
			}
		})
	}
}

func TestNormalizeExplicitFactKey_UnknownPreferenceDoesNotAnchor(t *testing.T) {
	for _, factValue := range []string{
		"I like things better this way",
		"that style works for me",
		"do what feels right",
	} {
		t.Run(factValue, func(t *testing.T) {
			node, err := NormalizeMemoryCandidate(MemoryCandidate{
				Source:              CandidateSourceExplicitFact,
				SourceChannel:       "user_input",
				NormalizedFactKey:   "preference.stated_preference",
				NormalizedFactValue: factValue,
				Trust:               TrustUserOriginated,
				Actor:               ObjectUser,
			})
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			if node.ANCHOR != nil {
				t.Fatalf("value %q: expected no anchor, got %#v", factValue, node.ANCHOR)
			}
		})
	}
}
