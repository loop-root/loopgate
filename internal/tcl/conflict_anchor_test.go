package tcl

import "testing"

func TestConflictAnchorCanonicalKey_V1(t *testing.T) {
	anchor := ConflictAnchor{
		Version:  "v1",
		Domain:   "usr_profile",
		Entity:   "identity",
		SlotKind: "fact",
		SlotName: "name",
	}
	if got := anchor.CanonicalKey(); got != "usr_profile:identity:fact:name" {
		t.Fatalf("expected canonical key usr_profile:identity:fact:name, got %q", got)
	}
}

func TestConflictAnchorCanonicalKey_IncludesFacet(t *testing.T) {
	anchor := ConflictAnchor{
		Version:  "v1",
		Domain:   "usr_preference",
		Entity:   "stated",
		SlotKind: "fact",
		SlotName: "preference",
		Facet:    "ui_theme",
	}
	if got := anchor.CanonicalKey(); got != "usr_preference:stated:fact:preference:ui_theme" {
		t.Fatalf("unexpected canonical key %q", got)
	}
}

func TestValidateNode_RejectsInvalidConflictAnchor(t *testing.T) {
	node := TCLNode{
		ACT: ActionStore,
		OBJ: ObjectMemory,
		STA: StateActive,
		META: TCLMeta{
			ACTOR:  ObjectUser,
			TRUST:  TrustUserOriginated,
			CONF:   8,
			TS:     1,
			SOURCE: "user_input",
		},
		ANCHOR: &ConflictAnchor{
			Version:  "v1",
			Domain:   "usr:profile",
			Entity:   "identity",
			SlotKind: "fact",
			SlotName: "name",
		},
	}
	if err := ValidateNode(node); err == nil {
		t.Fatal("expected invalid delimiter-bearing anchor to fail validation")
	}
}
