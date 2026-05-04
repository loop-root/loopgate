package loopgate

import (
	"context"
	"encoding/json"
	"errors"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"strings"
	"testing"
	"time"

	"loopgate/internal/ledger"
)

func TestCapabilityAuthDenialsAreAudited(t *testing.T) {
	testCases := []struct {
		name               string
		configure          func(t *testing.T, client *Client, server *Server) string
		wantDenialCode     string
		wantControlSession bool
	}{
		{
			name: "missing capability token",
			configure: func(t *testing.T, client *Client, server *Server) string {
				t.Helper()
				return ""
			},
			wantDenialCode: controlapipkg.DenialCodeCapabilityTokenMissing,
		},
		{
			name: "invalid capability token",
			configure: func(t *testing.T, client *Client, server *Server) string {
				t.Helper()
				return "invalid-capability-token"
			},
			wantDenialCode: controlapipkg.DenialCodeCapabilityTokenInvalid,
		},
		{
			name: "expired capability token",
			configure: func(t *testing.T, client *Client, server *Server) string {
				t.Helper()
				if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
					t.Fatalf("ensure capability token: %v", err)
				}
				server.mu.Lock()
				tokenClaims := server.sessionState.tokens[client.capabilityToken]
				tokenClaims.ExpiresAt = time.Now().UTC().Add(-1 * time.Minute)
				server.sessionState.tokens[client.capabilityToken] = tokenClaims
				activeSession := server.sessionState.sessions[tokenClaims.ControlSessionID]
				activeSession.ExpiresAt = time.Now().UTC().Add(-1 * time.Minute)
				server.sessionState.sessions[tokenClaims.ControlSessionID] = activeSession
				server.mu.Unlock()
				return client.capabilityToken
			},
			wantDenialCode:     controlapipkg.DenialCodeCapabilityTokenExpired,
			wantControlSession: true,
		},
		{
			name: "capability token peer binding mismatch",
			configure: func(t *testing.T, client *Client, server *Server) string {
				t.Helper()
				if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
					t.Fatalf("ensure capability token: %v", err)
				}
				server.mu.Lock()
				tokenClaims := server.sessionState.tokens[client.capabilityToken]
				tokenClaims.PeerIdentity.PID++
				server.sessionState.tokens[client.capabilityToken] = tokenClaims
				server.mu.Unlock()
				return client.capabilityToken
			},
			wantDenialCode:     controlapipkg.DenialCodeCapabilityTokenInvalid,
			wantControlSession: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			repoRoot := t.TempDir()
			client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

			capabilityToken := testCase.configure(t, client, server)

			rawClient := NewClient(client.socketPath)
			rawClient.mu.Lock()
			rawClient.delegatedSession = true
			rawClient.mu.Unlock()

			var statusResponse controlapipkg.StatusResponse
			err := rawClient.doJSON(context.Background(), http.MethodGet, "/v1/status", capabilityToken, nil, &statusResponse, nil)
			var denied RequestDeniedError
			if !errors.As(err, &denied) || denied.DenialCode != testCase.wantDenialCode {
				t.Fatalf("expected denied error %q, got %v", testCase.wantDenialCode, err)
			}

			authDeniedEvent := readLastAuditEventOfType(t, repoRoot, "auth.denied")
			if authDeniedEvent.Data["auth_kind"] != "capability_token" {
				t.Fatalf("expected auth_kind capability_token, got %#v", authDeniedEvent.Data["auth_kind"])
			}
			if authDeniedEvent.Data["denial_code"] != testCase.wantDenialCode {
				t.Fatalf("expected denial_code %q, got %#v", testCase.wantDenialCode, authDeniedEvent.Data["denial_code"])
			}
			if authDeniedEvent.Data["request_method"] != http.MethodGet {
				t.Fatalf("expected request_method GET, got %#v", authDeniedEvent.Data["request_method"])
			}
			if authDeniedEvent.Data["request_path"] != "/v1/status" {
				t.Fatalf("expected request_path /v1/status, got %#v", authDeniedEvent.Data["request_path"])
			}
			if _, ok := authDeniedEvent.Data["control_session_id"]; ok != testCase.wantControlSession {
				t.Fatalf("expected control_session_id presence %v, got %#v", testCase.wantControlSession, authDeniedEvent.Data)
			}
			if encodedEvent, err := json.Marshal(authDeniedEvent); err != nil {
				t.Fatalf("marshal auth denied event: %v", err)
			} else if capabilityToken != "" && strings.Contains(string(encodedEvent), capabilityToken) {
				t.Fatalf("auth denied audit event leaked raw capability token: %s", encodedEvent)
			}
		})
	}
}

func TestCapabilityAuthDenialFailsClosedWhenAuditUnavailable(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	originalAppendAuditEvent := server.appendAuditEvent
	server.appendAuditEvent = func(ledgerPath string, auditEvent ledger.Event) error {
		if auditEvent.Type == "auth.denied" {
			return errors.New("audit unavailable")
		}
		return originalAppendAuditEvent(ledgerPath, auditEvent)
	}

	rawClient := NewClient(client.socketPath)
	rawClient.mu.Lock()
	rawClient.delegatedSession = true
	rawClient.mu.Unlock()

	var statusResponse controlapipkg.StatusResponse
	err := rawClient.doJSON(context.Background(), http.MethodGet, "/v1/status", "invalid-capability-token", nil, &statusResponse, nil)
	var denied RequestDeniedError
	if !errors.As(err, &denied) || denied.DenialCode != controlapipkg.DenialCodeAuditUnavailable {
		t.Fatalf("expected audit unavailable denial, got %v", err)
	}
}

func TestCapabilityAuthDenialsSuppressRepeatedAuditWritesWithinBurstWindow(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	currentTime := time.Date(2026, time.April, 20, 12, 0, 0, 0, time.UTC)
	server.SetNowForTest(func() time.Time { return currentTime })

	rawClient := NewClient(client.socketPath)
	rawClient.mu.Lock()
	rawClient.delegatedSession = true
	rawClient.mu.Unlock()

	requestInvalidStatus := func() {
		t.Helper()
		var statusResponse controlapipkg.StatusResponse
		err := rawClient.doJSON(context.Background(), http.MethodGet, "/v1/status", "invalid-capability-token", nil, &statusResponse, nil)
		var denied RequestDeniedError
		if !errors.As(err, &denied) || denied.DenialCode != controlapipkg.DenialCodeCapabilityTokenInvalid {
			t.Fatalf("expected invalid capability token denial, got %v", err)
		}
	}

	requestInvalidStatus()
	requestInvalidStatus()

	authDeniedEvents := readAuditEventsOfType(t, repoRoot, "auth.denied")
	if len(authDeniedEvents) != 1 {
		t.Fatalf("expected exactly one must-persist auth.denied event in the burst window, got %d", len(authDeniedEvents))
	}

	currentTime = currentTime.Add(authDeniedAuditBurstWindow + time.Second)
	requestInvalidStatus()

	authDeniedEvents = readAuditEventsOfType(t, repoRoot, "auth.denied")
	if len(authDeniedEvents) != 2 {
		t.Fatalf("expected a second auth.denied event after the burst window rolled, got %d", len(authDeniedEvents))
	}

	suppressedEvents := readAuditEventsOfType(t, repoRoot, "auth.denied.suppressed")
	if len(suppressedEvents) != 1 {
		t.Fatalf("expected one auth.denied.suppressed aggregate event, got %d", len(suppressedEvents))
	}
	suppressedEvent := suppressedEvents[0]
	if suppressedEvent.Data["suppressed_count"] != float64(1) {
		t.Fatalf("expected suppressed_count 1, got %#v", suppressedEvent.Data["suppressed_count"])
	}
	if suppressedEvent.Data["denial_code"] != controlapipkg.DenialCodeCapabilityTokenInvalid {
		t.Fatalf("expected invalid capability token aggregate denial code, got %#v", suppressedEvent.Data["denial_code"])
	}
}

func TestCapabilityAuthDeniedSuppressionFailsClosedWhenAggregateAuditUnavailable(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))

	currentTime := time.Date(2026, time.April, 20, 12, 0, 0, 0, time.UTC)
	server.SetNowForTest(func() time.Time { return currentTime })

	originalAppendAuditEvent := server.appendAuditEvent
	server.appendAuditEvent = func(ledgerPath string, auditEvent ledger.Event) error {
		if auditEvent.Type == "auth.denied.suppressed" {
			return errors.New("aggregate audit unavailable")
		}
		return originalAppendAuditEvent(ledgerPath, auditEvent)
	}

	rawClient := NewClient(client.socketPath)
	rawClient.mu.Lock()
	rawClient.delegatedSession = true
	rawClient.mu.Unlock()

	var firstStatusResponse controlapipkg.StatusResponse
	firstErr := rawClient.doJSON(context.Background(), http.MethodGet, "/v1/status", "invalid-capability-token", nil, &firstStatusResponse, nil)
	var denied RequestDeniedError
	if !errors.As(firstErr, &denied) || denied.DenialCode != controlapipkg.DenialCodeCapabilityTokenInvalid {
		t.Fatalf("expected first invalid capability token denial, got %v", firstErr)
	}

	var secondStatusResponse controlapipkg.StatusResponse
	secondErr := rawClient.doJSON(context.Background(), http.MethodGet, "/v1/status", "invalid-capability-token", nil, &secondStatusResponse, nil)
	if !errors.As(secondErr, &denied) || denied.DenialCode != controlapipkg.DenialCodeCapabilityTokenInvalid {
		t.Fatalf("expected second invalid capability token denial in burst window, got %v", secondErr)
	}

	currentTime = currentTime.Add(authDeniedAuditBurstWindow + time.Second)

	var rolloverStatusResponse controlapipkg.StatusResponse
	rolloverErr := rawClient.doJSON(context.Background(), http.MethodGet, "/v1/status", "invalid-capability-token", nil, &rolloverStatusResponse, nil)
	if !errors.As(rolloverErr, &denied) || denied.DenialCode != controlapipkg.DenialCodeAuditUnavailable {
		t.Fatalf("expected aggregate audit failure to fail closed, got %v", rolloverErr)
	}
}
