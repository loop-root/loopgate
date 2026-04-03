package loopgate

import (
	"testing"

	tclpkg "morph/internal/tcl"
)

func TestMemoryRememberGovernanceDecision_KeepPersists(t *testing.T) {
	denialCode, safeReason, shouldPersist := memoryRememberGovernanceDecision(tclpkg.PolicyDecision{
		TCLDecision: tclpkg.TCLDecision{
			DISP:   tclpkg.DispositionKeep,
			REASON: "candidate_signal_allowed",
		},
	})
	if !shouldPersist {
		t.Fatalf("expected keep decision to persist, got code=%q reason=%q", denialCode, safeReason)
	}
}

func TestMemoryRememberGovernanceDecision_MapsReviewAndQuarantineOutcomes(t *testing.T) {
	testCases := []struct {
		name         string
		decision      tclpkg.PolicyDecision
		expectedCode string
	}{
		{
			name: "review required",
			decision: tclpkg.PolicyDecision{
				TCLDecision: tclpkg.TCLDecision{DISP: tclpkg.DispositionReview, REASON: "requires_review"},
			},
			expectedCode: DenialCodeMemoryCandidateReviewRequired,
		},
		{
			name: "quarantine required",
			decision: tclpkg.PolicyDecision{
				TCLDecision: tclpkg.TCLDecision{DISP: tclpkg.DispositionQuarantine, REASON: "requires_quarantine"},
			},
			expectedCode: DenialCodeMemoryCandidateQuarantineRequired,
		},
		{
			name: "drop",
			decision: tclpkg.PolicyDecision{
				TCLDecision: tclpkg.TCLDecision{DISP: tclpkg.DispositionDrop, REASON: "drop_candidate"},
			},
			expectedCode: DenialCodeMemoryCandidateDropped,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			denialCode, safeReason, shouldPersist := memoryRememberGovernanceDecision(testCase.decision)
			if shouldPersist {
				t.Fatalf("expected %s to deny persistence", testCase.name)
			}
			if denialCode != testCase.expectedCode {
				t.Fatalf("expected denial code %q, got %q", testCase.expectedCode, denialCode)
			}
			if safeReason == "" {
				t.Fatalf("expected safe reason for %s", testCase.name)
			}
		})
	}
}
