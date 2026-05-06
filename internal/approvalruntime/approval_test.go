package approvalruntime

import "testing"

func TestTokenHash_IsStable(t *testing.T) {
	const token = "approval-token"
	first := TokenHash(token)
	second := TokenHash(token)
	if first == "" || first != second {
		t.Fatalf("expected stable token hash, got %q and %q", first, second)
	}
}

func TestRequestBodySHA256_IsStableForEquivalentBodies(t *testing.T) {
	first, err := RequestBodySHA256(map[string]any{
		"capability": "fs_write",
		"arguments": map[string]string{
			"path": "notes.txt",
		},
	})
	if err != nil {
		t.Fatalf("first request hash: %v", err)
	}
	second, err := RequestBodySHA256(map[string]any{
		"capability": "fs_write",
		"arguments": map[string]string{
			"path": "notes.txt",
		},
	})
	if err != nil {
		t.Fatalf("second request hash: %v", err)
	}
	if first == "" || first != second {
		t.Fatalf("expected stable request body hash, got %q and %q", first, second)
	}
}

func TestValidateStateTransition_AllowsExpectedTransitions(t *testing.T) {
	testCases := []struct {
		name         string
		currentState string
		nextState    string
	}{
		{name: "pending to granted", currentState: StatePending, nextState: StateGranted},
		{name: "pending to denied", currentState: StatePending, nextState: StateDenied},
		{name: "pending to expired", currentState: StatePending, nextState: StateExpired},
		{name: "pending to cancelled", currentState: StatePending, nextState: StateCancelled},
		{name: "pending to consumed", currentState: StatePending, nextState: StateConsumed},
		{name: "granted to consumed", currentState: StateGranted, nextState: StateConsumed},
		{name: "consumed to execution failed", currentState: StateConsumed, nextState: StateExecutionFailed},
		{name: "idempotent same state", currentState: StateDenied, nextState: StateDenied},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := ValidateStateTransition(testCase.currentState, testCase.nextState); err != nil {
				t.Fatalf("expected transition %q -> %q to be allowed: %v", testCase.currentState, testCase.nextState, err)
			}
		})
	}
}

func TestValidateStateTransition_DeniesUnexpectedTransitions(t *testing.T) {
	testCases := []struct {
		name         string
		currentState string
		nextState    string
	}{
		{name: "granted to denied", currentState: StateGranted, nextState: StateDenied},
		{name: "consumed to denied", currentState: StateConsumed, nextState: StateDenied},
		{name: "execution failed to consumed", currentState: StateExecutionFailed, nextState: StateConsumed},
		{name: "expired to pending", currentState: StateExpired, nextState: StatePending},
		{name: "cancelled to denied", currentState: StateCancelled, nextState: StateDenied},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := ValidateStateTransition(testCase.currentState, testCase.nextState); err == nil {
				t.Fatalf("expected transition %q -> %q to be denied", testCase.currentState, testCase.nextState)
			}
		})
	}
}
