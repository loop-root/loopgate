package integration_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"morph/internal/ledger"
	"morph/internal/loopgate"
	"morph/internal/testutil"
)

type loopgateHarness struct {
	repoRoot   string
	socketPath string
	server     *loopgate.Server
	httpClient *http.Client
}

type sessionCredentials struct {
	ControlSessionID string
	CapabilityToken  string
	ApprovalToken    string
	SessionMACKey    string
}

const loopgateStartupReadyTimeout = 5 * time.Second

func newLoopgateHarness(t *testing.T, policyYAML string) *loopgateHarness {
	return newLoopgateHarnessWithSetup(t, policyYAML, nil)
}

func newLoopgateHarnessWithSetup(t *testing.T, policyYAML string, setupRepo func(string) error) *loopgateHarness {
	t.Helper()

	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, "runtime", "state"), 0o700); err != nil {
		t.Fatalf("mkdir runtime state dir: %v", err)
	}
	policySigner, err := testutil.NewPolicyTestSigner()
	if err != nil {
		t.Fatalf("new policy test signer: %v", err)
	}
	policySigner.ConfigureEnv(t.Setenv)
	if err := policySigner.WriteSignedPolicyYAML(repoRoot, policyYAML); err != nil {
		t.Fatalf("write signed policy: %v", err)
	}
	writeTestMorphlingClassPolicy(t, repoRoot)
	if setupRepo != nil {
		if err := setupRepo(repoRoot); err != nil {
			t.Fatalf("setup repo fixture: %v", err)
		}
	}

	socketFile, err := os.CreateTemp("", "loopgate-*.sock")
	if err != nil {
		t.Fatalf("create temp socket file: %v", err)
	}
	socketPath := socketFile.Name()
	if err := socketFile.Close(); err != nil {
		t.Fatalf("close temp socket file: %v", err)
	}
	if err := os.Remove(socketPath); err != nil {
		t.Fatalf("remove temp socket file placeholder: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(socketPath)
	})
	server, err := loopgate.NewServerForIntegrationHarness(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	serverContext, cancel := context.WithCancel(context.Background())
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		_ = server.Serve(serverContext)
	}()
	t.Cleanup(func() {
		cancel()
		<-serverDone
	})

	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				dialer := net.Dialer{}
				return dialer.DialContext(ctx, "unix", socketPath)
			},
		},
	}

	harness := &loopgateHarness{
		repoRoot:   repoRoot,
		socketPath: socketPath,
		server:     server,
		httpClient: httpClient,
	}
	harness.waitForHealth(t)
	return harness
}

func writeTestMorphlingClassPolicy(t *testing.T, repoRoot string) {
	t.Helper()

	classPolicyPath := filepath.Join(repoRoot, "core", "policy", "morphling_classes.yaml")
	if err := os.MkdirAll(filepath.Dir(classPolicyPath), 0o700); err != nil {
		t.Fatalf("mkdir morphling class policy dir: %v", err)
	}
	classPolicyBytes, err := os.ReadFile(filepath.Join("..", "..", "core", "policy", "morphling_classes.yaml"))
	if err != nil {
		t.Fatalf("read default morphling class policy: %v", err)
	}
	if err := os.WriteFile(classPolicyPath, classPolicyBytes, 0o600); err != nil {
		t.Fatalf("write morphling class policy: %v", err)
	}
}

func (harness *loopgateHarness) newClient(actor string, sessionID string, requestedCapabilities []string) *loopgate.Client {
	client := loopgate.NewClient(harness.socketPath)
	client.ConfigureSession(actor, sessionID, append([]string(nil), requestedCapabilities...))
	return client
}

func (harness *loopgateHarness) waitForHealth(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(loopgateStartupReadyTimeout)
	var lastCode int
	var lastErr error
	for time.Now().Before(deadline) {
		statusCode, responseBody, err := harness.doJSONBytesResult(http.MethodGet, "/v1/health", "", nil, nil)
		lastCode, lastErr = statusCode, err
		if err == nil && statusCode == http.StatusOK {
			var health loopgate.HealthResponse
			if json.Unmarshal(responseBody, &health) == nil && health.OK {
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for /v1/health (last code=%d err=%v)", lastCode, lastErr)
}

func (harness *loopgateHarness) getStatusAuthenticated(t *testing.T, creds sessionCredentials) loopgate.StatusResponse {
	t.Helper()
	requestTimestamp := time.Now().UTC().Format(time.RFC3339Nano)
	nonceBytes := make([]byte, 12)
	if _, err := rand.Read(nonceBytes); err != nil {
		t.Fatalf("request nonce: %v", err)
	}
	requestNonce := hex.EncodeToString(nonceBytes)
	statusCode, responseBody := harness.doSignedJSONBytes(t, http.MethodGet, "/v1/status", creds, requestTimestamp, requestNonce, nil)
	if statusCode != http.StatusOK {
		t.Fatalf("authenticated /v1/status: status=%d body=%s", statusCode, string(responseBody))
	}
	var status loopgate.StatusResponse
	decodeJSON(t, responseBody, &status)
	return status
}

func (harness *loopgateHarness) waitForStatus(t *testing.T) loopgate.StatusResponse {
	t.Helper()
	harness.waitForHealth(t)
	bootstrapCreds := harness.openSession(t, "integration-bootstrap", "integration-bootstrap-session", []string{"fs_list"})
	return harness.getStatusAuthenticated(t, bootstrapCreds)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestWaitForHealth_AllowsSlowServerStartupWithinTimeout(t *testing.T) {
	readyAt := time.Now().Add(2200 * time.Millisecond)
	harness := &loopgateHarness{
		httpClient: &http.Client{
			Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.Path != "/v1/health" {
					return nil, fmt.Errorf("unexpected path %s", request.URL.Path)
				}
				if time.Now().Before(readyAt) {
					return nil, fmt.Errorf("synthetic startup delay")
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"version":"test","ok":true}`)),
				}, nil
			}),
		},
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		statusCode, responseBody, err := harness.doJSONBytesResult(http.MethodGet, "/v1/health", "", nil, nil)
		if err == nil && statusCode == http.StatusOK {
			var health loopgate.HealthResponse
			if json.Unmarshal(responseBody, &health) == nil && health.OK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected health polling to tolerate slow startup, last code=%d err=%v", statusCode, err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func (harness *loopgateHarness) openSession(t *testing.T, actor string, sessionID string, requestedCapabilities []string) sessionCredentials {
	t.Helper()

	statusCode, responseBody := harness.doJSON(t, http.MethodPost, "/v1/session/open", "", nil, loopgate.OpenSessionRequest{
		Actor:                 actor,
		SessionID:             sessionID,
		RequestedCapabilities: append([]string(nil), requestedCapabilities...),
	})
	if statusCode != http.StatusOK {
		t.Fatalf("open session failed: status=%d body=%s", statusCode, string(responseBody))
	}

	var openResponse loopgate.OpenSessionResponse
	decodeJSON(t, responseBody, &openResponse)
	return sessionCredentials{
		ControlSessionID: openResponse.ControlSessionID,
		CapabilityToken:  openResponse.CapabilityToken,
		ApprovalToken:    openResponse.ApprovalToken,
		SessionMACKey:    openResponse.SessionMACKey,
	}
}

func (harness *loopgateHarness) doJSON(t *testing.T, method string, path string, bearerToken string, headers map[string]string, requestBody interface{}) (int, []byte) {
	t.Helper()

	var bodyBytes []byte
	if requestBody != nil {
		bodyBytes = mustJSON(t, requestBody)
	}
	return harness.doJSONBytes(t, method, path, bearerToken, headers, bodyBytes)
}

func (harness *loopgateHarness) doJSONBytes(t *testing.T, method string, path string, bearerToken string, headers map[string]string, bodyBytes []byte) (int, []byte) {
	t.Helper()

	statusCode, responseBody, err := harness.doJSONBytesResult(method, path, bearerToken, headers, bodyBytes)
	if err != nil {
		t.Fatalf("perform request: %v", err)
	}
	return statusCode, responseBody
}

func (harness *loopgateHarness) doJSONBytesResult(method string, path string, bearerToken string, headers map[string]string, bodyBytes []byte) (int, []byte, error) {
	request, err := http.NewRequestWithContext(context.Background(), method, "http://loopgate"+path, bytes.NewReader(bodyBytes))
	if err != nil {
		return 0, nil, fmt.Errorf("build request: %w", err)
	}
	if bodyBytes != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(bearerToken) != "" {
		request.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	for headerName, headerValue := range headers {
		request.Header.Set(headerName, headerValue)
	}

	response, err := harness.httpClient.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("read response body: %w", err)
	}
	return response.StatusCode, responseBody, nil
}

func (harness *loopgateHarness) doSignedJSONBytes(
	t *testing.T,
	method string,
	path string,
	credentials sessionCredentials,
	requestTimestamp string,
	requestNonce string,
	bodyBytes []byte,
) (int, []byte) {
	t.Helper()

	requestSignature := computeRequestSignature(
		credentials.SessionMACKey,
		method,
		path,
		credentials.ControlSessionID,
		requestTimestamp,
		requestNonce,
		bodyBytes,
	)
	return harness.doJSONBytes(t, method, path, credentials.CapabilityToken, map[string]string{
		"X-Loopgate-Control-Session":   credentials.ControlSessionID,
		"X-Loopgate-Request-Timestamp": requestTimestamp,
		"X-Loopgate-Request-Nonce":     requestNonce,
		"X-Loopgate-Request-Signature": requestSignature,
	}, bodyBytes)
}

func (harness *loopgateHarness) auditPath() string {
	return filepath.Join(harness.repoRoot, "runtime", "state", "loopgate_events.jsonl")
}

func (harness *loopgateHarness) readAuditEvents(t *testing.T) ([]ledger.Event, []byte) {
	t.Helper()

	auditBytes, err := os.ReadFile(harness.auditPath())
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	lines := bytes.Split(bytes.TrimSpace(auditBytes), []byte("\n"))
	events := make([]ledger.Event, 0, len(lines))
	for _, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		event, ok := ledger.ParseEvent(line)
		if !ok {
			t.Fatalf("parse audit line: %s", string(line))
		}
		events = append(events, event)
	}
	return events, auditBytes
}

func (harness *loopgateHarness) verifyAuditChain(t *testing.T) int64 {
	t.Helper()

	auditFile, err := os.Open(harness.auditPath())
	if err != nil {
		t.Fatalf("open audit file: %v", err)
	}
	defer auditFile.Close()

	lastSequence, _, err := ledger.ReadVerifiedChainState(auditFile, "audit_sequence")
	if err != nil {
		t.Fatalf("verify audit chain: %v", err)
	}
	return lastSequence
}

func capabilityNames(capabilities []loopgate.CapabilitySummary) []string {
	names := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		names = append(names, capability.Name)
	}
	return names
}

func advertisedSessionCapabilityNames(status loopgate.StatusResponse) []string {
	advertisedCapabilities := capabilityNames(status.Capabilities)
	advertisedCapabilities = append(advertisedCapabilities, capabilityNames(status.ControlCapabilities)...)
	return advertisedCapabilities
}

func decodeJSON(t *testing.T, bodyBytes []byte, target interface{}) {
	t.Helper()
	if err := json.Unmarshal(bodyBytes, target); err != nil {
		t.Fatalf("decode json %s: %v", string(bodyBytes), err)
	}
}

func mustJSON(t *testing.T, value interface{}) []byte {
	t.Helper()
	encodedBytes, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return encodedBytes
}

func computeRequestSignature(sessionMACKey string, method string, path string, controlSessionID string, requestTimestamp string, requestNonce string, bodyBytes []byte) string {
	bodyHash := sha256.Sum256(bodyBytes)
	signingPayload := strings.Join([]string{
		method,
		path,
		controlSessionID,
		requestTimestamp,
		requestNonce,
		hex.EncodeToString(bodyHash[:]),
	}, "\n")

	mac := hmac.New(sha256.New, []byte(sessionMACKey))
	_, _ = mac.Write([]byte(signingPayload))
	return hex.EncodeToString(mac.Sum(nil))
}

func mutateHexSignature(signature string) string {
	if signature == "" {
		return "0"
	}
	lastByte := signature[len(signature)-1]
	replacement := byte('0')
	if lastByte == '0' {
		replacement = '1'
	}
	return signature[:len(signature)-1] + string(replacement)
}

func eventSequence(t *testing.T, auditEvent ledger.Event) int64 {
	t.Helper()

	rawSequence, found := auditEvent.Data["audit_sequence"]
	if !found {
		t.Fatalf("audit event missing audit_sequence: %#v", auditEvent)
	}
	switch typedSequence := rawSequence.(type) {
	case float64:
		return int64(typedSequence)
	case int64:
		return typedSequence
	case int:
		return int64(typedSequence)
	default:
		t.Fatalf("unexpected audit_sequence type %T in event %#v", rawSequence, auditEvent)
		return 0
	}
}

func findAuditEvent(events []ledger.Event, eventType string, requestID string) (ledger.Event, bool) {
	for _, auditEvent := range events {
		if auditEvent.Type != eventType {
			continue
		}
		if requestID == "" {
			return auditEvent, true
		}
		eventRequestID, _ := auditEvent.Data["request_id"].(string)
		if eventRequestID == requestID {
			return auditEvent, true
		}
	}
	return ledger.Event{}, false
}

func integrationPolicyYAML(writeEnabled bool) string {
	writeEnabledValue := "false"
	if writeEnabled {
		writeEnabledValue = "true"
	}

	return fmt.Sprintf(
		"version: 0.1.0\n\n"+
			"tools:\n"+
			"  filesystem:\n"+
			"    allowed_roots:\n"+
			"      - \".\"\n"+
			"    denied_paths:\n"+
			"      - \"core/policy\"\n"+
			"    read_enabled: true\n"+
			"    write_enabled: %s\n"+
			"    write_requires_approval: false\n"+
			"  http:\n"+
			"    enabled: false\n"+
			"    allowed_domains: []\n"+
			"    requires_approval: true\n"+
			"    timeout_seconds: 10\n"+
			"  shell:\n"+
			"    enabled: false\n"+
			"    allowed_commands: []\n"+
			"    requires_approval: true\n"+
			"  morphlings:\n"+
			"    spawn_enabled: false\n"+
			"    max_active: 5\n"+
			"    require_template: true\n"+
			"logging:\n"+
			"  log_commands: true\n"+
			"  log_tool_calls: true\n"+
			"  log_memory_promotions: true\n"+
			"memory:\n"+
			"  auto_distillate: true\n"+
			"  require_promotion_approval: true\n"+
			"safety:\n"+
			"  allow_persona_modification: false\n"+
			"  allow_policy_modification: false\n"+
			"  haven_trusted_sandbox_auto_allow: true\n",
		writeEnabledValue,
	)
}
