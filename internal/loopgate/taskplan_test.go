package loopgate

import (
	"testing"
)

func TestTaskPlanCanonicalHashDeterministic(t *testing.T) {
	steps := []TaskPlanStep{
		{StepIndex: 0, Capability: "echo.generate_summary", Arguments: map[string]string{"input_text": "hello"}},
	}
	hash1 := computeCanonicalHash("Summarize test input", steps)
	hash2 := computeCanonicalHash("Summarize test input", steps)
	if hash1 == "" {
		t.Fatal("expected non-empty hash")
	}
	if hash1 != hash2 {
		t.Fatalf("expected deterministic hash, got %s and %s", hash1, hash2)
	}
}

func TestTaskPlanCanonicalHashChangesWithGoal(t *testing.T) {
	steps := []TaskPlanStep{
		{StepIndex: 0, Capability: "echo.generate_summary", Arguments: map[string]string{"input_text": "hello"}},
	}
	hash1 := computeCanonicalHash("Goal A", steps)
	hash2 := computeCanonicalHash("Goal B", steps)
	if hash1 == hash2 {
		t.Fatal("expected different hashes for different goals")
	}
}

func TestTaskPlanCanonicalHashChangesWithArguments(t *testing.T) {
	steps1 := []TaskPlanStep{
		{StepIndex: 0, Capability: "echo.generate_summary", Arguments: map[string]string{"input_text": "hello"}},
	}
	steps2 := []TaskPlanStep{
		{StepIndex: 0, Capability: "echo.generate_summary", Arguments: map[string]string{"input_text": "world"}},
	}
	hash1 := computeCanonicalHash("Same goal", steps1)
	hash2 := computeCanonicalHash("Same goal", steps2)
	if hash1 == hash2 {
		t.Fatal("expected different hashes for different arguments")
	}
}

func TestTaskPlanValidTransitions(t *testing.T) {
	valid := [][2]string{
		{taskPlanStateSubmitted, taskPlanStateValidated},
		{taskPlanStateSubmitted, taskPlanStateDenied},
		{taskPlanStateValidated, taskPlanStateLeaseIssued},
		{taskPlanStateLeaseIssued, taskPlanStateExecuting},
		{taskPlanStateExecuting, taskPlanStateCompleted},
		{taskPlanStateExecuting, taskPlanStateFailed},
	}
	for _, pair := range valid {
		if !validTaskPlanTransition(pair[0], pair[1]) {
			t.Errorf("expected valid transition: %s → %s", pair[0], pair[1])
		}
	}
}

func TestTaskPlanInvalidTransitions(t *testing.T) {
	invalid := [][2]string{
		{taskPlanStateValidated, taskPlanStateSubmitted},
		{taskPlanStateDenied, taskPlanStateValidated},
		{taskPlanStateCompleted, taskPlanStateLeaseIssued},
		{taskPlanStateLeaseIssued, taskPlanStateValidated},
		{taskPlanStateFailed, taskPlanStateCompleted},
		{taskPlanStateValidated, taskPlanStateExecuting},   // skip lease_issued
		{taskPlanStateLeaseIssued, taskPlanStateCompleted}, // skip executing
	}
	for _, pair := range invalid {
		if validTaskPlanTransition(pair[0], pair[1]) {
			t.Errorf("expected invalid transition: %s → %s", pair[0], pair[1])
		}
	}
}

func TestTaskLeaseValidTransitions(t *testing.T) {
	valid := [][2]string{
		{taskLeaseStateIssued, taskLeaseStateExecuting},
		{taskLeaseStateIssued, taskLeaseStateExpired},
		{taskLeaseStateExecuting, taskLeaseStateConsumed},
		{taskLeaseStateExecuting, taskLeaseStateExpired},
	}
	for _, pair := range valid {
		if !validTaskLeaseTransition(pair[0], pair[1]) {
			t.Errorf("expected valid transition: %s → %s", pair[0], pair[1])
		}
	}
}

func TestTaskLeaseInvalidTransitions(t *testing.T) {
	invalid := [][2]string{
		{taskLeaseStateConsumed, taskLeaseStateIssued},
		{taskLeaseStateExpired, taskLeaseStateIssued},
		{taskLeaseStateConsumed, taskLeaseStateExpired},
		{taskLeaseStateIssued, taskLeaseStateConsumed}, // must go through executing
	}
	for _, pair := range invalid {
		if validTaskLeaseTransition(pair[0], pair[1]) {
			t.Errorf("expected invalid transition: %s → %s", pair[0], pair[1])
		}
	}
}

func TestTransitionTaskPlanStateFunction(t *testing.T) {
	plan := &taskPlanRecord{State: taskPlanStateSubmitted}
	if err := transitionTaskPlanState(plan, taskPlanStateValidated); err != nil {
		t.Fatalf("expected valid transition: %v", err)
	}
	if plan.State != taskPlanStateValidated {
		t.Fatalf("expected state validated, got %s", plan.State)
	}
	if err := transitionTaskPlanState(plan, taskPlanStateSubmitted); err == nil {
		t.Fatal("expected error for invalid backward transition")
	}
}

func TestTransitionTaskLeaseStateFunction(t *testing.T) {
	lease := &taskLeaseRecord{State: taskLeaseStateIssued}
	if err := transitionTaskLeaseState(lease, taskLeaseStateExecuting); err != nil {
		t.Fatalf("expected valid transition: %v", err)
	}
	if err := transitionTaskLeaseState(lease, taskLeaseStateConsumed); err != nil {
		t.Fatalf("expected valid transition: %v", err)
	}
	if err := transitionTaskLeaseState(lease, taskLeaseStateIssued); err == nil {
		t.Fatal("expected error for backward transition from consumed")
	}
}
