package loopgate

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestRememberMemoryFact_SupersedesOnlyWhenAnchorTupleMatches checks that explicit remembers
// persist TCL-owned (version, canonical key) tuples and that a second remember targeting the
// same tuple supersedes the prior fact (master plan Phase 1 Task 4).
func TestRememberMemoryFact_SupersedesOnlyWhenAnchorTupleMatches(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	firstResponse, err := client.RememberMemoryFact(context.Background(), MemoryRememberRequest{
		FactKey:   "name",
		FactValue: "Ada",
	})
	if err != nil {
		t.Fatalf("remember explicit name: %v", err)
	}

	firstDistillate, found := testDefaultMemoryState(t, server).Distillates[firstResponse.DistillateID]
	if !found {
		t.Fatalf("expected persisted distillate %q", firstResponse.DistillateID)
	}
	if len(firstDistillate.Facts) != 1 {
		t.Fatalf("expected one remembered fact, got %#v", firstDistillate.Facts)
	}
	// Canonical in-memory state keeps anchor tuples on SemanticProjection; legacy
	// conflict_key* wire fields are normalized away when projection is present
	// (continuityFactAnchorTuple matches production resolution for supersession
	// and wake-state).
	anchorVersion, anchorKey := continuityFactAnchorTuple(firstDistillate.Facts[0])
	if anchorVersion != "v1" {
		t.Fatalf("expected TCL anchor version v1, got %q (fact %#v)", anchorVersion, firstDistillate.Facts[0])
	}
	if anchorKey != "usr_profile:identity:fact:name" {
		t.Fatalf("expected TCL anchor key usr_profile:identity:fact:name, got %q (fact %#v)", anchorKey, firstDistillate.Facts[0])
	}

	secondResponse, err := client.RememberMemoryFact(context.Background(), MemoryRememberRequest{
		FactKey:   "name",
		FactValue: "Grace",
	})
	if err != nil {
		t.Fatalf("remember superseding name: %v", err)
	}
	if !secondResponse.UpdatedExisting {
		t.Fatalf("expected same anchor tuple to supersede earlier fact, got %#v", secondResponse)
	}
	if secondResponse.FactValue != "Grace" {
		t.Fatalf("expected updated fact value Grace, got %q", secondResponse.FactValue)
	}

	firstInspection := testDefaultMemoryState(t, server).Inspections[firstResponse.InspectionID]
	if firstInspection.Lineage.Status != continuityLineageStatusTombstoned {
		t.Fatalf("expected first inspection tombstoned after supersession, got %#v", firstInspection.Lineage)
	}
}

func TestRememberMemoryFact_CoexistsWhenTCLReturnsNoAnchor(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	if _, err := client.RememberMemoryFact(context.Background(), MemoryRememberRequest{
		FactKey:   "preference.stated_preference",
		FactValue: "I like things better this way",
	}); err != nil {
		t.Fatalf("remember first unstable preference: %v", err)
	}
	secondResponse, err := client.RememberMemoryFact(context.Background(), MemoryRememberRequest{
		FactKey:   "preference.stated_preference",
		FactValue: "I like things better that way",
	})
	if err != nil {
		t.Fatalf("remember second unstable preference: %v", err)
	}
	if secondResponse.UpdatedExisting {
		t.Fatalf("expected unstable preference to coexist without anchor, got %#v", secondResponse)
	}
	if len(testDefaultMemoryState(t, server).Inspections) != 2 {
		t.Fatalf("expected two remembered inspections, got %#v", testDefaultMemoryState(t, server).Inspections)
	}
}

func TestRememberMemoryFact_FailsClosedWhenTCLValidationFails(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	server.analyzeExplicitMemoryCandidate = func(MemoryRememberRequest) (memoryTCLAnalysis, error) {
		return memoryTCLAnalysis{}, fmt.Errorf("synthetic TCL validation failure")
	}

	_, err := client.RememberMemoryFact(context.Background(), MemoryRememberRequest{
		FactKey:   "name",
		FactValue: "Ada",
	})
	if err == nil || !strings.Contains(err.Error(), DenialCodeMemoryCandidateInvalid) {
		t.Fatalf("expected explicit remember to fail closed on TCL validation error, got %v", err)
	}
	if len(testDefaultMemoryState(t, server).Distillates) != 0 || len(testDefaultMemoryState(t, server).Inspections) != 0 {
		t.Fatalf("expected no durable memory writes after TCL validation failure, got %#v", testDefaultMemoryState(t, server))
	}
}
