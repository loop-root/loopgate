package relationhints

import "testing"

func TestBuildLookupQuery_CarriesOnlyBoundedStateHints(t *testing.T) {
	lookupQuery := BuildLookupQuery("find the design thread", []string{
		"Current blocker: renewal still looks current even when memory.write is missing.",
		"",
		"Next step: keep projected status visibly non-authoritative.",
	})
	expectedQuery := "find the design thread\nRelated current state:\nCurrent blocker: renewal still looks current even when memory.write is missing.\nNext step: keep projected status visibly non-authoritative."
	if lookupQuery != expectedQuery {
		t.Fatalf("unexpected lookup query: %q", lookupQuery)
	}
}

func TestEvidenceSearchPoolSize_StaysBounded(t *testing.T) {
	if EvidenceSearchPoolSize(0) != 12 {
		t.Fatalf("expected default evidence pool size 12, got %d", EvidenceSearchPoolSize(0))
	}
	if EvidenceSearchPoolSize(2) != 12 {
		t.Fatalf("expected two-item evidence pool size 12, got %d", EvidenceSearchPoolSize(2))
	}
	if EvidenceSearchPoolSize(5) != 24 {
		t.Fatalf("expected bounded max evidence pool size 24, got %d", EvidenceSearchPoolSize(5))
	}
}

func TestRerankCandidates_PrefersPhraseSpecificSiblingNotes(t *testing.T) {
	rerankedCandidates := RerankCandidates([]Candidate{
		{
			StableID:   "wrong_demo_note",
			Text:       "Dashboard demo note: the auto-refresh card renewed the preview lease during demos so the status always looked current.",
			MatchCount: 5,
		},
		{
			StableID:   "right_explicit_renew",
			Text:       "Design note: operator mount write grant renewal moved into the explicit UI renew action so audit append failures still block authority mutation.",
			MatchCount: 4,
		},
		{
			StableID:   "right_projected_status",
			Text:       "Design follow-up: status cards remain projected convenience views and must never imply a renewed grant on their own.",
			MatchCount: 4,
		},
	}, "Find the design thread about why write access only advances during an explicit operator refresh instead of a self-updating dashboard card.", []string{
		"Current blocker: renewal flow still lets projected status cards look renewed even when memory.write is missing.",
		"Next step: thread the explicit renew action through the governed memory.write check and keep projected status cards visibly non-authoritative.",
	}, 2)

	if len(rerankedCandidates) != 2 {
		t.Fatalf("expected two reranked candidates, got %#v", rerankedCandidates)
	}
	if rerankedCandidates[0].StableID != "right_explicit_renew" || rerankedCandidates[1].StableID != "right_projected_status" {
		t.Fatalf("expected phrase-specific sibling notes to win, got %#v", rerankedCandidates)
	}
}

func TestRerankCandidates_PrefersComplementaryFilesystemEvidence(t *testing.T) {
	rerankedCandidates := RerankCandidates([]Candidate{
		{
			StableID:   "wrong_workspace_visibility",
			Text:       "Demo workspace note: keep the friendly workspace path visible in operator surfaces so people are not staring at private runtime directories.",
			MatchCount: 5,
		},
		{
			StableID:   "right_resolved_target",
			Text:       "Filesystem hardening note: policy checks must run on the final resolved target path, not the caller's raw relative path.",
			MatchCount: 4,
		},
		{
			StableID:   "right_virtual_projection",
			Text:       "Projection note: operator surfaces should show virtual sandbox paths while resolved runtime paths stay private and server-side.",
			MatchCount: 4,
		},
	}, "Find the path-handling discussion about why we validate the resolved target but still render virtual paths in operator surfaces.", []string{
		"Current follow-up: keep operator-visible sandbox paths virtual while denying any write that resolves outside the allowed root.",
		"Next step: thread final-target resolution through the local file preview path before expanding more workspace shortcuts.",
	}, 2)

	if len(rerankedCandidates) != 2 {
		t.Fatalf("expected two reranked candidates, got %#v", rerankedCandidates)
	}
	selectedCandidateIDs := map[string]bool{}
	for _, rerankedCandidate := range rerankedCandidates {
		selectedCandidateIDs[rerankedCandidate.StableID] = true
	}
	if !selectedCandidateIDs["right_resolved_target"] || !selectedCandidateIDs["right_virtual_projection"] || selectedCandidateIDs["wrong_workspace_visibility"] {
		t.Fatalf("expected complementary filesystem rationale to win, got %#v", rerankedCandidates)
	}
}
