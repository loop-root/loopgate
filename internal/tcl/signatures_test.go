package tcl

import "testing"

func TestDeriveSignatureSet_StableForEquivalentSemanticNode(t *testing.T) {
	firstNode := TCLNode{
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
	secondNode := TCLNode{
		ACT:  ActionStore,
		OBJ:  ObjectMemory,
		QUAL: []Qualifier{QualifierPrivate},
		STA:  StateActive,
		META: TCLMeta{
			ACTOR:  ObjectUser,
			TRUST:  TrustUserOriginated,
			CONF:   8,
			TS:     1700000999,
			SOURCE: "user_input",
		},
	}

	firstSignatureSet, err := DeriveSignatureSet(firstNode)
	if err != nil {
		t.Fatalf("derive first signature set: %v", err)
	}
	secondSignatureSet, err := DeriveSignatureSet(secondNode)
	if err != nil {
		t.Fatalf("derive second signature set: %v", err)
	}

	if firstSignatureSet.Exact == "" {
		t.Fatal("expected exact signature")
	}
	if firstSignatureSet.Exact != secondSignatureSet.Exact {
		t.Fatalf("expected stable exact signature, got %q and %q", firstSignatureSet.Exact, secondSignatureSet.Exact)
	}
	if firstSignatureSet.Family == "" {
		t.Fatal("expected family signature")
	}
}

func TestDeriveSignatureSet_RiskMotifForPrivateExternalMemoryWrite(t *testing.T) {
	dangerousNode := TCLNode{
		ACT:  ActionStore,
		OBJ:  ObjectMemory,
		QUAL: []Qualifier{QualifierPrivate, QualifierExternal},
		OUT:  ActionWrite,
		STA:  StateReviewRequired,
		META: TCLMeta{
			ACTOR:  ObjectUser,
			TRUST:  TrustUserOriginated,
			CONF:   9,
			TS:     1700000000,
			SOURCE: "user_input",
		},
	}

	signatureSet, err := DeriveSignatureSet(dangerousNode)
	if err != nil {
		t.Fatalf("derive dangerous signature set: %v", err)
	}

	if len(signatureSet.RiskMotifs) == 0 {
		t.Fatal("expected at least one risk motif")
	}
	if signatureSet.RiskMotifs[0] != RiskMotifPrivateExternalMemoryWrite {
		t.Fatalf("expected private external memory write motif, got %#v", signatureSet.RiskMotifs)
	}
}
