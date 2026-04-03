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
