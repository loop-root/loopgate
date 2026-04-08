package config

import (
	"testing"
)

func TestPolicy_HavenTrustedSandboxAutoAllowEnabled_DefaultFalseWhenOmitted(t *testing.T) {
	p := Policy{}
	if p.HavenTrustedSandboxAutoAllowEnabled() {
		t.Fatalf("expected omitted haven_trusted_sandbox_auto_allow to default to disabled")
	}
	off := false
	p.Safety.HavenTrustedSandboxAutoAllow = &off
	if p.HavenTrustedSandboxAutoAllowEnabled() {
		t.Fatalf("expected explicit false to stay disabled")
	}
	on := true
	p.Safety.HavenTrustedSandboxAutoAllow = &on
	if !p.HavenTrustedSandboxAutoAllowEnabled() {
		t.Fatalf("expected explicit true to enable")
	}
}

func TestPolicy_HavenTrustedSandboxAutoAllowMatchesCapability(t *testing.T) {
	p := Policy{}
	if !p.HavenTrustedSandboxAutoAllowMatchesCapability("notes.write") {
		t.Fatalf("expected omitted allowlist to match any capability")
	}
	empty := []string{}
	p.Safety.HavenTrustedSandboxAutoAllowCapabilities = &empty
	if p.HavenTrustedSandboxAutoAllowMatchesCapability("notes.write") {
		t.Fatalf("expected empty allowlist to match nothing")
	}
	only := []string{"notes.read"}
	p.Safety.HavenTrustedSandboxAutoAllowCapabilities = &only
	if p.HavenTrustedSandboxAutoAllowMatchesCapability("notes.write") {
		t.Fatalf("expected allowlist to exclude unlisted capability")
	}
	if !p.HavenTrustedSandboxAutoAllowMatchesCapability("notes.read") {
		t.Fatalf("expected allowlist to include listed capability")
	}
}
