package identifiers

import "testing"

func TestValidateSafeIdentifier_AllowsSimpleLabels(t *testing.T) {
	for _, allowedValue := range []string{
		"loopgate",
		"session-20260307",
		"OPENAI_API_KEY",
		"user.name",
		"scope:model_inference",
	} {
		if err := ValidateSafeIdentifier("identifier", allowedValue); err != nil {
			t.Fatalf("expected %q to be allowed, got %v", allowedValue, err)
		}
	}
}

func TestValidateSafeIdentifier_DeniesTraversalAndShellPatterns(t *testing.T) {
	for _, deniedValue := range []string{
		"../../etc/passwd",
		"..\\..\\windows",
		"$rm",
		"$(whoami)",
		"`uname`",
		"user;cat",
		"user|cat",
		"user name",
		"~/user",
	} {
		if err := ValidateSafeIdentifier("identifier", deniedValue); err == nil {
			t.Fatalf("expected %q to be denied", deniedValue)
		}
	}
}
