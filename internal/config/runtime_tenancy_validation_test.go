package config

import "testing"

func TestValidateOptionalDeploymentIdentity_RejectsControlChars(t *testing.T) {
	if err := validateOptionalDeploymentIdentity("tenancy.deployment_tenant_id", "ok-tenant"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if err := validateOptionalDeploymentIdentity("tenancy.deployment_tenant_id", ""); err != nil {
		t.Fatalf("empty should be allowed: %v", err)
	}
	if err := validateOptionalDeploymentIdentity("tenancy.deployment_tenant_id", "bad\ninfix"); err == nil {
		t.Fatal("expected error for embedded newline")
	}
}
