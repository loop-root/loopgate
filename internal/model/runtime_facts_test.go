package model

import "testing"

func TestConstrainedNativeToolsFromRuntimeFacts(t *testing.T) {
	if ConstrainedNativeToolsFromRuntimeFacts([]string{"hello"}) {
		t.Fatal("expected false without sentinel")
	}
	if !ConstrainedNativeToolsFromRuntimeFacts([]string{"x", ConstrainedNativeToolsRuntimeFact}) {
		t.Fatal("expected true with sentinel")
	}
	if !ConstrainedNativeToolsFromRuntimeFacts([]string{"  " + ConstrainedNativeToolsRuntimeFact + "\t"}) {
		t.Fatal("expected true with trimmed sentinel")
	}
}

func TestStripInternalRuntimeFacts(t *testing.T) {
	in := []string{"a", ConstrainedNativeToolsRuntimeFact, CompactNativeDispatchRuntimeFact, "b"}
	out := StripInternalRuntimeFacts(in)
	if len(out) != 2 || out[0] != "a" || out[1] != "b" {
		t.Fatalf("unexpected strip result: %#v", out)
	}
}
