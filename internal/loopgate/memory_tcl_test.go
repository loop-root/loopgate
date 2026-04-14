package loopgate

import (
	"reflect"
	"testing"

	tclpkg "loopgate/internal/tcl"
)

func TestBuildValidatedMemoryRememberCandidate_CanonicalizesAliasAtAdapterBoundary(t *testing.T) {
	validatedRequest := MemoryRememberRequest{
		Scope:         memoryScopeGlobal,
		FactKey:       "user.name",
		FactValue:     "Ada",
		SourceChannel: memorySourceChannelUserInput,
	}

	candidateInput, err := memoryCandidateInputFromRememberRequest(validatedRequest)
	if err != nil {
		t.Fatalf("candidate input from remember request: %v", err)
	}
	expectedCandidate, err := tclpkg.BuildValidatedMemoryCandidate(candidateInput)
	if err != nil {
		t.Fatalf("build expected validated candidate: %v", err)
	}
	builtCandidate, err := buildValidatedMemoryRememberCandidate(validatedRequest)
	if err != nil {
		t.Fatalf("build validated memory remember candidate: %v", err)
	}

	if validatedRequest.FactKey != "user.name" {
		t.Fatalf("expected request alias to remain untouched before adapter, got %#v", validatedRequest)
	}
	if !reflect.DeepEqual(builtCandidate.ValidatedCandidate, expectedCandidate) {
		t.Fatalf("expected helper to return validated contract directly, got %#v want %#v", builtCandidate.ValidatedCandidate, expectedCandidate)
	}
	if builtCandidate.ValidatedCandidate.CanonicalKey != "name" {
		t.Fatalf("expected canonical key name, got %#v", builtCandidate.ValidatedCandidate)
	}
	if builtCandidate.ValidatedCandidate.AnchorTupleKey() != "v1:usr_profile:identity:fact:name" {
		t.Fatalf("expected canonical anchor tuple key, got %#v", builtCandidate.ValidatedCandidate)
	}
}

func TestMemoryRememberGovernanceDecision_KeepPersists(t *testing.T) {
	denialCode, safeReason, shouldPersist := memoryRememberGovernanceDecision(tclpkg.ValidatedMemoryDecision{
		Disposition: tclpkg.DispositionKeep,
		ReasonCode:  "candidate_signal_allowed",
	})
	if !shouldPersist {
		t.Fatalf("expected keep decision to persist, got code=%q reason=%q", denialCode, safeReason)
	}
}

func TestMemoryRememberGovernanceDecision_MapsReviewAndQuarantineOutcomes(t *testing.T) {
	testCases := []struct {
		name         string
		decision     tclpkg.ValidatedMemoryDecision
		expectedCode string
	}{
		{
			name: "review required",
			decision: tclpkg.ValidatedMemoryDecision{
				Disposition: tclpkg.DispositionReview,
				ReasonCode:  "requires_review",
			},
			expectedCode: DenialCodeMemoryCandidateReviewRequired,
		},
		{
			name: "quarantine required",
			decision: tclpkg.ValidatedMemoryDecision{
				Disposition: tclpkg.DispositionQuarantine,
				ReasonCode:  "requires_quarantine",
			},
			expectedCode: DenialCodeMemoryCandidateQuarantineRequired,
		},
		{
			name: "drop",
			decision: tclpkg.ValidatedMemoryDecision{
				Disposition: tclpkg.DispositionDrop,
				ReasonCode:  "drop_candidate",
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
