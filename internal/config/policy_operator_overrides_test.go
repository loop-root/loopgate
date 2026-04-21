package config

import (
	"strings"
	"testing"
)

func TestApplyPolicyDefaults_NormalizesOperatorOverrideDelegation(t *testing.T) {
	policy := Policy{}
	policy.OperatorOverrides.Classes = map[string]OperatorOverrideClassPolicy{
		OperatorOverrideClassRepoEditSafe: {},
	}

	if err := applyPolicyDefaults(&policy); err != nil {
		t.Fatalf("apply defaults: %v", err)
	}

	if got := policy.OperatorOverrideMaxDelegation(OperatorOverrideClassRepoEditSafe); got != OperatorOverrideDelegationNone {
		t.Fatalf("expected default max delegation none, got %q", got)
	}
}

func TestApplyPolicyDefaults_RejectsUnknownOperatorOverrideClass(t *testing.T) {
	policy := Policy{}
	policy.OperatorOverrides.Classes = map[string]OperatorOverrideClassPolicy{
		"freeform": {MaxDelegation: OperatorOverrideDelegationPersistent},
	}

	err := applyPolicyDefaults(&policy)
	if err == nil {
		t.Fatal("expected unsupported operator override class to be rejected")
	}
	if !strings.Contains(err.Error(), "unsupported class") {
		t.Fatalf("expected unsupported class error, got %v", err)
	}
}

func TestApplyPolicyDefaults_RejectsUnknownOperatorOverrideDelegation(t *testing.T) {
	policy := Policy{}
	policy.OperatorOverrides.Classes = map[string]OperatorOverrideClassPolicy{
		OperatorOverrideClassRepoEditSafe: {MaxDelegation: "forever"},
	}

	err := applyPolicyDefaults(&policy)
	if err == nil {
		t.Fatal("expected unsupported operator override delegation to be rejected")
	}
	if !strings.Contains(err.Error(), "max_delegation") {
		t.Fatalf("expected max_delegation validation error, got %v", err)
	}
}

func TestPolicy_ClaudeCodeToolOperatorOverride(t *testing.T) {
	policy := Policy{}
	policy.OperatorOverrides.Classes = map[string]OperatorOverrideClassPolicy{
		OperatorOverrideClassRepoEditSafe: {MaxDelegation: OperatorOverrideDelegationPersistent},
		OperatorOverrideClassRepoBashSafe: {MaxDelegation: OperatorOverrideDelegationSession},
	}

	if err := applyPolicyDefaults(&policy); err != nil {
		t.Fatalf("apply defaults: %v", err)
	}

	overrideClass, maxDelegation, found := policy.ClaudeCodeToolOperatorOverride("Edit")
	if !found {
		t.Fatal("expected Edit to map to an operator override class")
	}
	if overrideClass != OperatorOverrideClassRepoEditSafe {
		t.Fatalf("expected repo_edit_safe override class, got %q", overrideClass)
	}
	if maxDelegation != OperatorOverrideDelegationPersistent {
		t.Fatalf("expected persistent max delegation, got %q", maxDelegation)
	}

	_, bashDelegation, found := policy.ClaudeCodeToolOperatorOverride("Bash")
	if !found {
		t.Fatal("expected Bash to map to an operator override class")
	}
	if bashDelegation != OperatorOverrideDelegationSession {
		t.Fatalf("expected session max delegation, got %q", bashDelegation)
	}

	_, readDelegation, found := policy.ClaudeCodeToolOperatorOverride("Read")
	if !found {
		t.Fatal("expected Read to map to an operator override class")
	}
	if readDelegation != OperatorOverrideDelegationNone {
		t.Fatalf("expected unconfigured Read delegation to default none, got %q", readDelegation)
	}
}
