package tcl

import "testing"

func TestParseCompactExpression_RoundTripsCanonicalMemoryWrite(t *testing.T) {
	rawExpression := "STR(MEM:PRI)->WRT[REV]%(9)"

	parsedNode, err := ParseCompactExpression(rawExpression)
	if err != nil {
		t.Fatalf("parse compact expression: %v", err)
	}

	if parsedNode.ACT != ActionStore {
		t.Fatalf("expected ACT %q, got %q", ActionStore, parsedNode.ACT)
	}
	if parsedNode.OBJ != ObjectMemory {
		t.Fatalf("expected OBJ %q, got %q", ObjectMemory, parsedNode.OBJ)
	}
	if len(parsedNode.QUAL) != 1 || parsedNode.QUAL[0] != QualifierPrivate {
		t.Fatalf("expected one private qualifier, got %#v", parsedNode.QUAL)
	}
	if parsedNode.OUT != ActionWrite {
		t.Fatalf("expected OUT %q, got %q", ActionWrite, parsedNode.OUT)
	}
	if parsedNode.STA != StateReviewRequired {
		t.Fatalf("expected STA %q, got %q", StateReviewRequired, parsedNode.STA)
	}
	if parsedNode.META.CONF != 9 {
		t.Fatalf("expected confidence 9, got %d", parsedNode.META.CONF)
	}

	renderedExpression := MustCompactExpression(parsedNode)
	if renderedExpression != rawExpression {
		t.Fatalf("expected round trip %q, got %q", rawExpression, renderedExpression)
	}
}

func TestValidateNode_RejectsDuplicateQualifiers(t *testing.T) {
	node := TCLNode{
		ID:   "M7",
		ACT:  ActionAnalyze,
		OBJ:  ObjectRepository,
		QUAL: []Qualifier{QualifierSecuritySensitive, QualifierSecuritySensitive},
		STA:  StateActive,
		META: TCLMeta{
			ACTOR: ObjectUser,
			TRUST: TrustUserOriginated,
			CONF:  8,
			TS:    1700000000,
			SOURCE:"user_input",
		},
	}

	err := ValidateNode(node)
	if err == nil {
		t.Fatal("expected duplicate qualifier validation failure")
	}
}
