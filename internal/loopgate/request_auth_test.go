package loopgate

import (
	"context"
	"encoding/json"
	"errors"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseCapabilityTokenAuthorizationHeader(t *testing.T) {
	testCases := []struct {
		name           string
		header         string
		wantToken      string
		wantDenialCode string
		wantReason     string
	}{
		{
			name:           "missing header",
			header:         "",
			wantDenialCode: controlapipkg.DenialCodeCapabilityTokenMissing,
			wantReason:     "missing capability token",
		},
		{
			name:           "wrong scheme",
			header:         "Basic abc123",
			wantDenialCode: controlapipkg.DenialCodeCapabilityTokenMissing,
			wantReason:     "missing capability token",
		},
		{
			name:      "lowercase bearer",
			header:    "bearer token-123",
			wantToken: "token-123",
		},
		{
			name:      "uppercase bearer",
			header:    "BEARER token-123",
			wantToken: "token-123",
		},
		{
			name:      "multiple spaces",
			header:    "Bearer    token-123",
			wantToken: "token-123",
		},
		{
			name:      "tab separator",
			header:    "Bearer\ttoken-123",
			wantToken: "token-123",
		},
		{
			name:           "missing whitespace after scheme",
			header:         "BEARERtoken-123",
			wantDenialCode: controlapipkg.DenialCodeCapabilityTokenInvalid,
			wantReason:     "malformed capability token authorization header",
		},
		{
			name:           "scheme only",
			header:         "Bearer",
			wantDenialCode: controlapipkg.DenialCodeCapabilityTokenInvalid,
			wantReason:     "malformed capability token authorization header",
		},
		{
			name:           "too many fields",
			header:         "Bearer token-123 extra",
			wantDenialCode: controlapipkg.DenialCodeCapabilityTokenInvalid,
			wantReason:     "malformed capability token authorization header",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			gotToken, denialResponse, ok := parseCapabilityTokenAuthorizationHeader(testCase.header)
			if testCase.wantToken != "" {
				if !ok {
					t.Fatalf("expected success, got denial %#v", denialResponse)
				}
				if gotToken != testCase.wantToken {
					t.Fatalf("token = %q, want %q", gotToken, testCase.wantToken)
				}
				return
			}

			if ok {
				t.Fatalf("expected denial, got token %q", gotToken)
			}
			if denialResponse.DenialCode != testCase.wantDenialCode {
				t.Fatalf("denial code = %q, want %q", denialResponse.DenialCode, testCase.wantDenialCode)
			}
			if denialResponse.DenialReason != testCase.wantReason {
				t.Fatalf("denial reason = %q, want %q", denialResponse.DenialReason, testCase.wantReason)
			}
		})
	}
}

func TestParseSignedControlPlaneHeaders(t *testing.T) {
	fixedNowUTC := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	server := &Server{
		now: func() time.Time {
			return fixedNowUTC
		},
	}

	testCases := []struct {
		name              string
		headers           map[string]string
		expectedSessionID string
		wantHeaders       signedControlPlaneHeaders
		wantDenialCode    string
		wantDenialReason  string
	}{
		{
			name:              "valid signed headers",
			expectedSessionID: "control-123",
			headers: map[string]string{
				"X-Loopgate-Control-Session":   " control-123 ",
				"X-Loopgate-Request-Timestamp": fixedNowUTC.Format(time.RFC3339Nano),
				"X-Loopgate-Request-Nonce":     " nonce-123 ",
				"X-Loopgate-Request-Signature": " deadbeef ",
			},
			wantHeaders: signedControlPlaneHeaders{
				ControlSessionID:          "control-123",
				RawRequestTimestamp:       fixedNowUTC.Format(time.RFC3339Nano),
				ParsedRequestTimestampUTC: fixedNowUTC,
				RequestNonce:              "nonce-123",
				RequestSignature:          "deadbeef",
			},
		},
		{
			name:              "valid signed headers preserve raw offset timestamp",
			expectedSessionID: "control-123",
			headers: map[string]string{
				"X-Loopgate-Control-Session":   "control-123",
				"X-Loopgate-Request-Timestamp": fixedNowUTC.In(time.FixedZone("mdt", -6*60*60)).Format(time.RFC3339Nano),
				"X-Loopgate-Request-Nonce":     "nonce-456",
				"X-Loopgate-Request-Signature": "beadfeed",
			},
			wantHeaders: signedControlPlaneHeaders{
				ControlSessionID:          "control-123",
				RawRequestTimestamp:       fixedNowUTC.In(time.FixedZone("mdt", -6*60*60)).Format(time.RFC3339Nano),
				ParsedRequestTimestampUTC: fixedNowUTC,
				RequestNonce:              "nonce-456",
				RequestSignature:          "beadfeed",
			},
		},
		{
			name:              "missing signed header",
			expectedSessionID: "control-123",
			headers: map[string]string{
				"X-Loopgate-Control-Session":   "control-123",
				"X-Loopgate-Request-Timestamp": fixedNowUTC.Format(time.RFC3339Nano),
				"X-Loopgate-Request-Nonce":     "nonce-123",
			},
			wantDenialCode:   controlapipkg.DenialCodeRequestSignatureMissing,
			wantDenialReason: "signed control-plane headers are required",
		},
		{
			name:              "control session binding mismatch",
			expectedSessionID: "control-123",
			headers: map[string]string{
				"X-Loopgate-Control-Session":   "other-control",
				"X-Loopgate-Request-Timestamp": fixedNowUTC.Format(time.RFC3339Nano),
				"X-Loopgate-Request-Nonce":     "nonce-123",
				"X-Loopgate-Request-Signature": "deadbeef",
			},
			wantDenialCode:   controlapipkg.DenialCodeControlSessionBindingInvalid,
			wantDenialReason: "control session binding is invalid",
		},
		{
			name:              "invalid timestamp format",
			expectedSessionID: "control-123",
			headers: map[string]string{
				"X-Loopgate-Control-Session":   "control-123",
				"X-Loopgate-Request-Timestamp": "not-a-timestamp",
				"X-Loopgate-Request-Nonce":     "nonce-123",
				"X-Loopgate-Request-Signature": "deadbeef",
			},
			wantDenialCode:   controlapipkg.DenialCodeRequestTimestampInvalid,
			wantDenialReason: "request timestamp is invalid",
		},
		{
			name:              "timestamp outside skew window",
			expectedSessionID: "control-123",
			headers: map[string]string{
				"X-Loopgate-Control-Session":   "control-123",
				"X-Loopgate-Request-Timestamp": fixedNowUTC.Add(-(requestSignatureSkew + time.Second)).Format(time.RFC3339Nano),
				"X-Loopgate-Request-Nonce":     "nonce-123",
				"X-Loopgate-Request-Signature": "deadbeef",
			},
			wantDenialCode:   controlapipkg.DenialCodeRequestTimestampInvalid,
			wantDenialReason: "request timestamp is outside the allowed skew window",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/v1/capabilities/execute", nil)
			for headerName, headerValue := range testCase.headers {
				request.Header.Set(headerName, headerValue)
			}

			gotHeaders, denialResponse, ok := server.parseSignedControlPlaneHeaders(request, testCase.expectedSessionID)
			if testCase.wantDenialCode != "" {
				if ok {
					t.Fatalf("expected denial, got headers %#v", gotHeaders)
				}
				if denialResponse.DenialCode != testCase.wantDenialCode {
					t.Fatalf("denial code = %q, want %q", denialResponse.DenialCode, testCase.wantDenialCode)
				}
				if denialResponse.DenialReason != testCase.wantDenialReason {
					t.Fatalf("denial reason = %q, want %q", denialResponse.DenialReason, testCase.wantDenialReason)
				}
				return
			}

			if !ok {
				t.Fatalf("expected success, got denial %#v", denialResponse)
			}
			if gotHeaders != testCase.wantHeaders {
				t.Fatalf("headers = %#v, want %#v", gotHeaders, testCase.wantHeaders)
			}
		})
	}
}

func TestStatusAcceptsCaseInsensitiveBearerAuthorization(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	testCases := []struct {
		name   string
		header string
	}{
		{name: "lowercase scheme", header: "bearer " + client.capabilityToken},
		{name: "uppercase scheme", header: "BEARER " + client.capabilityToken},
		{name: "tab separator", header: "Bearer\t" + client.capabilityToken},
		{name: "multiple spaces", header: "Bearer    " + client.capabilityToken},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var statusResponse controlapipkg.StatusResponse
			if err := client.doJSON(context.Background(), http.MethodGet, "/v1/status", "", nil, &statusResponse, map[string]string{
				"Authorization": testCase.header,
			}); err != nil {
				t.Fatalf("status with %s: %v", testCase.name, err)
			}
			if strings.TrimSpace(statusResponse.Version) == "" {
				t.Fatalf("expected populated status response, got %#v", statusResponse)
			}
		})
	}
}

func TestStatusRejectsMalformedBearerAuthorizationHeaderAndAuditsDenial(t *testing.T) {
	repoRoot := t.TempDir()
	client, _, _ := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	if _, err := client.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	var statusResponse controlapipkg.StatusResponse
	err := client.doJSON(context.Background(), http.MethodGet, "/v1/status", "", nil, &statusResponse, map[string]string{
		"Authorization": "Bearer " + client.capabilityToken + " extra",
	})
	var denied RequestDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected denied error, got %v", err)
	}
	if denied.DenialCode != controlapipkg.DenialCodeCapabilityTokenInvalid {
		t.Fatalf("denial code = %q, want %q", denied.DenialCode, controlapipkg.DenialCodeCapabilityTokenInvalid)
	}
	if denied.DenialReason != "malformed capability token authorization header" {
		t.Fatalf("denial reason = %q", denied.DenialReason)
	}

	authDeniedEvent := readLastAuditEventOfType(t, repoRoot, "auth.denied")
	if authDeniedEvent.Data["auth_kind"] != "capability_token" {
		t.Fatalf("expected auth_kind capability_token, got %#v", authDeniedEvent.Data["auth_kind"])
	}
	if authDeniedEvent.Data["denial_code"] != controlapipkg.DenialCodeCapabilityTokenInvalid {
		t.Fatalf("expected denial_code %q, got %#v", controlapipkg.DenialCodeCapabilityTokenInvalid, authDeniedEvent.Data["denial_code"])
	}
	if authDeniedEvent.Data["reason"] != "malformed capability token authorization header" {
		t.Fatalf("expected malformed-header audit reason, got %#v", authDeniedEvent.Data["reason"])
	}
	if encodedEvent, marshalErr := json.Marshal(authDeniedEvent); marshalErr != nil {
		t.Fatalf("marshal auth denied event: %v", marshalErr)
	} else if strings.Contains(string(encodedEvent), client.capabilityToken) {
		t.Fatalf("auth denied audit event leaked capability token: %s", encodedEvent)
	}
}
