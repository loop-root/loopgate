package model

import "testing"

func TestConstrainedNativeToolsFromRuntimeFacts(t *testing.T) {
	if ConstrainedNativeToolsFromRuntimeFacts([]string{"hello"}) {
		t.Fatal("expected false without sentinel")
	}
	if !ConstrainedNativeToolsFromRuntimeFacts([]string{"x", HavenConstrainedNativeToolsRuntimeFact}) {
		t.Fatal("expected true with sentinel")
	}
	if !ConstrainedNativeToolsFromRuntimeFacts([]string{"  " + HavenConstrainedNativeToolsRuntimeFact + "\t"}) {
		t.Fatal("expected true with trimmed sentinel")
	}
}

func TestStripHavenInternalRuntimeFacts(t *testing.T) {
	in := []string{"a", HavenConstrainedNativeToolsRuntimeFact, HavenCompactNativeDispatchRuntimeFact, "b"}
	out := StripHavenInternalRuntimeFacts(in)
	if len(out) != 2 || out[0] != "a" || out[1] != "b" {
		t.Fatalf("unexpected strip result: %#v", out)
	}
}
