package loopgate

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestDelegatedSessionStreamRoundTrip(t *testing.T) {
	expiresAt := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	buffer := &bytes.Buffer{}

	writer := NewDelegatedSessionStreamWriter(buffer)
	writer.now = func() time.Time {
		return time.Date(2026, 3, 8, 11, 30, 0, 0, time.UTC)
	}

	expectedConfig := DelegatedSessionConfig{
		ControlSessionID: "abc123def456",
		CapabilityToken:  "cap-token",
		ApprovalToken:    "approval-token",
		SessionMACKey:    "mac-key",
		ExpiresAt:        expiresAt,
	}
	if err := writer.WriteCredentials(expectedConfig); err != nil {
		t.Fatalf("write credentials: %v", err)
	}

	reader := NewDelegatedSessionStreamReader(buffer)
	readConfig, err := reader.ReadCredentials()
	if err != nil {
		t.Fatalf("read credentials: %v", err)
	}
	if readConfig.ControlSessionID != expectedConfig.ControlSessionID ||
		readConfig.CapabilityToken != expectedConfig.CapabilityToken ||
		readConfig.ApprovalToken != expectedConfig.ApprovalToken ||
		readConfig.SessionMACKey != expectedConfig.SessionMACKey ||
		!readConfig.ExpiresAt.Equal(expectedConfig.ExpiresAt) {
		t.Fatalf("unexpected round-trip config: %#v", readConfig)
	}
}

func TestDelegatedSessionStreamRejectsUnknownFields(t *testing.T) {
	reader := NewDelegatedSessionStreamReader(strings.NewReader(`{"schema_version":"loopgate.delegated_session.v1","message_type":"credentials","sent_at_utc":"2026-03-08T11:30:00Z","credentials":{"control_session_id":"abc123def456","capability_token":"cap-token","approval_token":"approval-token","session_mac_key":"mac-key","expires_at_utc":"2026-03-08T12:00:00Z","extra":"forbidden"}}`))
	if _, err := reader.ReadCredentials(); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field rejection, got %v", err)
	}
}

func TestEvaluateDelegatedSessionState(t *testing.T) {
	now := time.Date(2026, 3, 8, 11, 0, 0, 0, time.UTC)

	if state := EvaluateDelegatedSessionState(now, now.Add(10*time.Minute)); state != DelegatedSessionStateHealthy {
		t.Fatalf("expected healthy state, got %s", state)
	}
	if state := EvaluateDelegatedSessionState(now, now.Add(90*time.Second)); state != DelegatedSessionStateRefreshSoon {
		t.Fatalf("expected refresh_soon state, got %s", state)
	}
	if state := EvaluateDelegatedSessionState(now, now.Add(-1*time.Second)); state != DelegatedSessionStateRefreshRequired {
		t.Fatalf("expected refresh_required state, got %s", state)
	}
}

func TestShouldRefreshDelegatedSession(t *testing.T) {
	now := time.Date(2026, 3, 8, 11, 0, 0, 0, time.UTC)
	if ShouldRefreshDelegatedSession(now, now.Add(15*time.Minute)) {
		t.Fatal("expected no refresh needed yet")
	}
	if !ShouldRefreshDelegatedSession(now, now.Add(30*time.Second)) {
		t.Fatal("expected refresh recommendation inside lead window")
	}
	if !ShouldRefreshDelegatedSession(now, now.Add(-30*time.Second)) {
		t.Fatal("expected refresh required for expired credentials")
	}
}

func TestClientDelegatedSessionHealth(t *testing.T) {
	now := time.Date(2026, 3, 8, 11, 0, 0, 0, time.UTC)
	client := &Client{
		delegatedSession: true,
		tokenExpiresAt:   now.Add(90 * time.Second),
	}

	state, expiresAt, ok := client.DelegatedSessionHealth(now)
	if !ok {
		t.Fatal("expected delegated session health to be available")
	}
	if state != DelegatedSessionStateRefreshSoon {
		t.Fatalf("expected refresh_soon state, got %s", state)
	}
	if !expiresAt.Equal(now.Add(90 * time.Second)) {
		t.Fatalf("unexpected expiry time %s", expiresAt)
	}
}
