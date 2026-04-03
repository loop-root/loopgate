package mcpserve

import (
	"os"
	"testing"
	"time"
)

func TestDelegatedConfigFromEnv_MissingExpiry(t *testing.T) {
	t.Setenv(envControlSessionID, "a1b2c3d4e5f6789012345678abcdef01")
	t.Setenv(envCapabilityToken, "cap-token")
	t.Setenv(envApprovalToken, "appr-token")
	t.Setenv(envSessionMACKey, "0102030405060708090a0b0c0d0e0f10")
	_ = os.Unsetenv(envExpiresAt)

	_, _, _, err := DelegatedConfigFromEnv()
	if err == nil {
		t.Fatal("expected error for missing expiry")
	}
}

func TestDelegatedConfigFromEnv_OK(t *testing.T) {
	exp := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	t.Setenv(envControlSessionID, "a1b2c3d4e5f6789012345678abcdef01")
	t.Setenv(envCapabilityToken, "cap-token")
	t.Setenv(envApprovalToken, "appr-token")
	t.Setenv(envSessionMACKey, "0102030405060708090a0b0c0d0e0f10")
	t.Setenv(envExpiresAt, exp)
	_ = os.Unsetenv(envActor)
	_ = os.Unsetenv(envClientSession)

	cfg, actor, clientSess, err := DelegatedConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ControlSessionID != "a1b2c3d4e5f6789012345678abcdef01" || cfg.CapabilityToken != "cap-token" {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
	if actor != DefaultActor || clientSess != DefaultClientSession {
		t.Fatalf("actor=%q client=%q", actor, clientSess)
	}
}
