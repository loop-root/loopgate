package main

import "testing"

func TestNewRootFlagSet_DoesNotExposeAdminFlag(t *testing.T) {
	rootFlags, _ := newRootFlagSet()
	if rootFlags.Lookup("admin") != nil {
		t.Fatalf("expected root loopgate flags to omit deprecated admin surface, got admin=%#v", rootFlags.Lookup("admin"))
	}
	if rootFlags.Lookup("accept-policy") == nil {
		t.Fatal("expected root loopgate flags to keep accept-policy")
	}
}
