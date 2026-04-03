package tcl

import "testing"

func TestValidateNode_AllowsAbsentDecision(t *testing.T) {
	node := TCLNode{
		ACT:  ActionStore,
		OBJ:  ObjectMemory,
		QUAL: []Qualifier{QualifierPrivate},
		STA:  StateActive,
		META: TCLMeta{
			ACTOR:  ObjectUser,
			TRUST:  TrustUserOriginated,
			CONF:   8,
			TS:     1700000000,
			SOURCE: "user_input",
		},
	}

	if err := ValidateNode(node); err != nil {
		t.Fatalf("expected node without decision to validate: %v", err)
	}
}

func TestValidateNode_AllowsValidDecision(t *testing.T) {
	node := TCLNode{
		ACT:  ActionStore,
		OBJ:  ObjectMemory,
		QUAL: []Qualifier{QualifierPrivate},
		STA:  StateReviewRequired,
		META: TCLMeta{
			ACTOR:  ObjectUser,
			TRUST:  TrustUserOriginated,
			CONF:   8,
			TS:     1700000000,
			SOURCE: "user_input",
		},
		DECISION: &TCLDecision{
			DISP:            DispositionReview,
			REVIEW_REQUIRED: true,
			RISKY:           true,
		},
	}

	if err := ValidateNode(node); err != nil {
		t.Fatalf("expected node with valid decision to validate: %v", err)
	}
}

func TestValidateNode_RejectsInvalidDecisionDisposition(t *testing.T) {
	node := TCLNode{
		ACT:  ActionStore,
		OBJ:  ObjectMemory,
		QUAL: []Qualifier{QualifierPrivate},
		STA:  StateReviewRequired,
		META: TCLMeta{
			ACTOR:  ObjectUser,
			TRUST:  TrustUserOriginated,
			CONF:   8,
			TS:     1700000000,
			SOURCE: "user_input",
		},
		DECISION: &TCLDecision{
			DISP: Disposition("BAD"),
		},
	}

	if err := ValidateNode(node); err == nil {
		t.Fatal("expected invalid decision disposition to fail validation")
	}
}
