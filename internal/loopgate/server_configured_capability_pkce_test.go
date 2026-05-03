package loopgate

import (
	"context"
	"errors"
	"io"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"loopgate/internal/secrets"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfiguredPKCECapability_ExchangesAndRefreshesInsideLoopgate(t *testing.T) {
	repoRoot := t.TempDir()
	var authorizationRequests int
	var tokenRequests int
	var refreshRequests int
	var apiRequests int

	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oauth/authorize":
			authorizationRequests++
			writer.WriteHeader(http.StatusNoContent)
		case "/oauth/token":
			tokenRequests++
			if err := request.ParseForm(); err != nil {
				t.Fatalf("parse pkce token form: %v", err)
			}
			switch request.Form.Get("grant_type") {
			case controlapipkg.GrantTypeAuthorizationCode:
				if request.Form.Get("client_id") != "pkce-client" {
					t.Fatalf("unexpected pkce client_id: %q", request.Form.Get("client_id"))
				}
				if request.Form.Get("code") != "auth-code-1" {
					t.Fatalf("unexpected pkce code: %q", request.Form.Get("code"))
				}
				if request.Form.Get("redirect_uri") != "http://127.0.0.1/callback" {
					t.Fatalf("unexpected redirect_uri: %q", request.Form.Get("redirect_uri"))
				}
				if strings.TrimSpace(request.Form.Get("code_verifier")) == "" {
					t.Fatal("expected code_verifier")
				}
				writer.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(writer, `{"access_token":"pkce-access-1","token_type":"Bearer","expires_in":300,"refresh_token":"pkce-refresh-1"}`)
			case "refresh_token":
				refreshRequests++
				if request.Form.Get("refresh_token") != "pkce-refresh-1" {
					t.Fatalf("unexpected refresh_token: %q", request.Form.Get("refresh_token"))
				}
				writer.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(writer, `{"access_token":"pkce-access-2","token_type":"Bearer","expires_in":300,"refresh_token":"pkce-refresh-2"}`)
			default:
				t.Fatalf("unexpected oauth grant_type: %q", request.Form.Get("grant_type"))
			}
		case "/api/status":
			apiRequests++
			writer.Header().Set("Content-Type", "application/json")
			if request.Header.Get("Authorization") == "Bearer pkce-access-1" {
				_, _ = io.WriteString(writer, `{"service":"pkce-api","healthy":true,"generation":1,"secret":"raw-only"}`)
				return
			}
			if request.Header.Get("Authorization") == "Bearer pkce-access-2" {
				_, _ = io.WriteString(writer, `{"service":"pkce-api","healthy":true,"generation":2,"secret":"raw-only"}`)
				return
			}
			t.Fatalf("unexpected authorization header: %q", request.Header.Get("Authorization"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer providerServer.Close()

	writeConfiguredPKCEYAML(t, repoRoot, providerServer.URL)
	client, _, server := startLoopgateServer(t, repoRoot, loopgateHTTPPolicyYAML(false))
	server.httpClient = providerServer.Client()
	fakeStore := &fakeConnectionSecretStore{}
	server.resolveSecretStore = func(validatedRef secrets.SecretRef) (secrets.SecretStore, error) {
		return fakeStore, nil
	}

	startResponse, err := client.StartPKCEConnection(context.Background(), controlapipkg.PKCEStartRequest{
		Provider: "examplepkce",
		Subject:  "workspace-user",
	})
	if err != nil {
		t.Fatalf("start pkce: %v", err)
	}
	authURL, err := url.Parse(startResponse.AuthorizationURL)
	if err != nil {
		t.Fatalf("parse authorization url: %v", err)
	}
	if gotClientID := authURL.Query().Get("client_id"); gotClientID != "pkce-client" {
		t.Fatalf("unexpected auth client_id: %q", gotClientID)
	}
	if gotState := authURL.Query().Get("state"); gotState != startResponse.State {
		t.Fatalf("unexpected auth state: %q vs %q", gotState, startResponse.State)
	}
	if strings.TrimSpace(authURL.Query().Get("code_challenge")) == "" {
		t.Fatal("expected code_challenge in auth URL")
	}

	connectionStatus, err := client.CompletePKCEConnection(context.Background(), controlapipkg.PKCECompleteRequest{
		Provider: "examplepkce",
		Subject:  "workspace-user",
		State:    startResponse.State,
		Code:     "auth-code-1",
	})
	if err != nil {
		t.Fatalf("complete pkce: %v", err)
	}
	if connectionStatus.Status != "stored" {
		t.Fatalf("unexpected connection status after pkce complete: %#v", connectionStatus)
	}
	if storedRefreshToken := string(fakeStore.storedSecret["pkce-refresh-token"]); storedRefreshToken != "pkce-refresh-1" {
		t.Fatalf("expected refresh token in secure backend only, got %q", storedRefreshToken)
	}

	firstResponse, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-pkce-1",
		Capability: "examplepkce.status_get",
	})
	if err != nil {
		t.Fatalf("execute pkce capability: %v", err)
	}
	if firstResponse.StructuredResult["generation"] != float64(1) {
		t.Fatalf("unexpected first pkce structured result: %#v", firstResponse.StructuredResult)
	}
	if _, found := firstResponse.StructuredResult["secret"]; found {
		t.Fatalf("expected raw secret field to remain quarantined, got %#v", firstResponse.StructuredResult)
	}
	if contentOrigin := firstResponse.Metadata["content_origin"]; contentOrigin != contentOriginRemote {
		t.Fatalf("expected remote content origin, got %#v", firstResponse.Metadata)
	}
	if extractor := firstResponse.Metadata["extractor"]; extractor != extractorJSONFieldAllowlist {
		t.Fatalf("expected json_field_allowlist extractor, got %#v", firstResponse.Metadata)
	}
	if derivedQuarantineRef := firstResponse.Metadata["derived_from_quarantine_ref"]; derivedQuarantineRef != firstResponse.QuarantineRef {
		t.Fatalf("expected derived quarantine ref to match response quarantine ref, got %#v", firstResponse.Metadata)
	}

	server.providerRuntime.mu.Lock()
	connectionKey := connectionRecordKey("examplepkce", "workspace-user")
	cachedToken := server.providerRuntime.tokens[connectionKey]
	cachedToken.ExpiresAt = time.Now().UTC().Add(-1 * time.Minute)
	server.providerRuntime.tokens[connectionKey] = cachedToken
	server.providerRuntime.mu.Unlock()

	secondResponse, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-pkce-2",
		Capability: "examplepkce.status_get",
	})
	if err != nil {
		t.Fatalf("execute pkce capability after expiry: %v", err)
	}
	if secondResponse.StructuredResult["generation"] != float64(2) {
		t.Fatalf("unexpected second pkce structured result: %#v", secondResponse.StructuredResult)
	}
	if storedRefreshToken := string(fakeStore.storedSecret["pkce-refresh-token"]); storedRefreshToken != "pkce-refresh-2" {
		t.Fatalf("expected rotated refresh token in secure backend only, got %q", storedRefreshToken)
	}
	if tokenRequests != 2 {
		t.Fatalf("expected authorization-code exchange and refresh-token exchange, got %d token requests", tokenRequests)
	}
	if refreshRequests != 1 {
		t.Fatalf("expected one refresh-token request, got %d", refreshRequests)
	}
	if apiRequests != 2 {
		t.Fatalf("expected two API requests, got %d", apiRequests)
	}
	if authorizationRequests != 0 {
		t.Fatalf("authorization endpoint should not be called by Loopgate start flow, got %d", authorizationRequests)
	}
}

func TestConfiguredPKCERefreshIsSerializedPerConnection(t *testing.T) {
	repoRoot := t.TempDir()
	var refreshRequests atomic.Int32
	refreshRequestStarted := make(chan struct{}, 1)
	releaseRefreshResponse := make(chan struct{})

	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oauth/token":
			if err := request.ParseForm(); err != nil {
				t.Fatalf("parse pkce refresh form: %v", err)
			}
			if request.Form.Get("grant_type") != "refresh_token" {
				t.Fatalf("unexpected oauth grant_type: %q", request.Form.Get("grant_type"))
			}
			if request.Form.Get("refresh_token") != "pkce-refresh-1" {
				t.Fatalf("unexpected refresh_token: %q", request.Form.Get("refresh_token"))
			}
			if refreshRequests.Add(1) == 1 {
				refreshRequestStarted <- struct{}{}
			}
			<-releaseRefreshResponse
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"access_token":"pkce-access-2","token_type":"Bearer","expires_in":300,"refresh_token":"pkce-refresh-2"}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer providerServer.Close()

	writeConfiguredPKCEYAML(t, repoRoot, providerServer.URL)
	_, _, server := startLoopgateServer(t, repoRoot, loopgateHTTPPolicyYAML(false))
	server.httpClient = providerServer.Client()
	fakeStore := &fakeConnectionSecretStore{}
	server.resolveSecretStore = func(validatedRef secrets.SecretRef) (secrets.SecretStore, error) {
		return fakeStore, nil
	}

	if _, err := server.UpsertConnectionCredential(context.Background(), connectionRegistration{
		Provider:  "examplepkce",
		GrantType: controlapipkg.GrantTypePKCE,
		Subject:   "workspace-user",
		Scopes:    []string{"status.read"},
		Credential: secrets.SecretRef{
			ID:          "pkce-refresh-token",
			Backend:     secrets.BackendSecure,
			AccountName: "loopgate.examplepkce.workspace-user",
			Scope:       "examplepkce.status_read",
		},
	}, []byte("pkce-refresh-1")); err != nil {
		t.Fatalf("seed pkce refresh token: %v", err)
	}

	configuredConnectionDefinition, found := server.configuredConnectionSnapshot(connectionRecordKey("examplepkce", "workspace-user"))
	if !found {
		t.Fatal("expected configured pkce connection snapshot")
	}

	const concurrentRequests = 8
	startRequests := make(chan struct{})
	resultTokens := make(chan string, concurrentRequests)
	resultErrors := make(chan error, concurrentRequests)
	var requestGroup sync.WaitGroup
	for requestIndex := 0; requestIndex < concurrentRequests; requestIndex++ {
		requestGroup.Add(1)
		go func() {
			defer requestGroup.Done()
			<-startRequests
			accessToken, err := server.accessTokenForConfiguredConnection(context.Background(), configuredConnectionDefinition)
			if err != nil {
				resultErrors <- err
				return
			}
			resultTokens <- accessToken
		}()
	}

	close(startRequests)
	select {
	case <-refreshRequestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for refresh request to start")
	}
	time.Sleep(25 * time.Millisecond)
	close(releaseRefreshResponse)
	requestGroup.Wait()
	close(resultTokens)
	close(resultErrors)

	for err := range resultErrors {
		t.Fatalf("unexpected refresh error: %v", err)
	}
	for accessToken := range resultTokens {
		if accessToken != "pkce-access-2" {
			t.Fatalf("unexpected refreshed access token: %q", accessToken)
		}
	}
	if refreshRequests.Load() != 1 {
		t.Fatalf("expected one refresh-token request across concurrent callers, got %d", refreshRequests.Load())
	}
	if fakeStore.putCalls != 2 {
		t.Fatalf("expected one initial store and one rotated refresh-token store, got %d puts", fakeStore.putCalls)
	}
	if storedRefreshToken := string(fakeStore.storedSecret["pkce-refresh-token"]); storedRefreshToken != "pkce-refresh-2" {
		t.Fatalf("expected rotated refresh token in secure backend only, got %q", storedRefreshToken)
	}
}

func TestConfiguredPKCECapability_CompletionRequiresInitiatingControlSession(t *testing.T) {
	repoRoot := t.TempDir()
	var tokenExchangeRequests int32
	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oauth/authorize":
			http.Error(writer, "authorization endpoint should not be called directly in tests", http.StatusBadRequest)
		case "/oauth/token":
			atomic.AddInt32(&tokenExchangeRequests, 1)
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"access_token":"pkce-access-1","token_type":"Bearer","expires_in":300,"refresh_token":"pkce-refresh-1"}`)
		case "/api/status":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"healthy":"yes","generation":1,"secret":"remote-secret-1"}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer providerServer.Close()

	writeConfiguredPKCEYAML(t, repoRoot, providerServer.URL)
	client, status, server := startLoopgateServer(t, repoRoot, loopgateHTTPPolicyYAML(false))
	server.httpClient = providerServer.Client()
	fakeStore := &fakeConnectionSecretStore{}
	server.resolveSecretStore = func(validatedRef secrets.SecretRef) (secrets.SecretStore, error) {
		return fakeStore, nil
	}

	startResponse, err := client.StartPKCEConnection(context.Background(), controlapipkg.PKCEStartRequest{
		Provider: "examplepkce",
		Subject:  "workspace-user",
	})
	if err != nil {
		t.Fatalf("start pkce: %v", err)
	}

	otherClient := NewClient(client.socketPath)
	otherClient.ConfigureSession("other-actor", "other-session", advertisedSessionCapabilityNames(status))
	if _, err := otherClient.ensureCapabilityToken(context.Background()); err != nil {
		t.Fatalf("ensure other client capability token: %v", err)
	}

	_, err = otherClient.CompletePKCEConnection(context.Background(), controlapipkg.PKCECompleteRequest{
		Provider: "examplepkce",
		Subject:  "workspace-user",
		State:    startResponse.State,
		Code:     "auth-code-cross-session",
	})
	var denied RequestDeniedError
	if !errors.As(err, &denied) || denied.DenialCode != controlapipkg.DenialCodeExecutionFailed {
		t.Fatalf("expected pkce completion denial, got %v", err)
	}
	if !strings.Contains(denied.DenialReason, "different control session") {
		t.Fatalf("expected control-session binding reason, got %v", denied)
	}
	if got := atomic.LoadInt32(&tokenExchangeRequests); got != 0 {
		t.Fatalf("expected no token exchange for cross-session completion, got %d", got)
	}

	server.pkceRuntime.mu.Lock()
	pendingSession, found := server.pkceRuntime.sessions[startResponse.State]
	server.pkceRuntime.mu.Unlock()
	if !found {
		t.Fatalf("expected pending PKCE session to remain after denied completion")
	}
	if pendingSession.ControlSessionID != client.controlSessionID {
		t.Fatalf("expected pending PKCE session to remain bound to original control session, got %#v", pendingSession)
	}

	connectionStatus, err := client.CompletePKCEConnection(context.Background(), controlapipkg.PKCECompleteRequest{
		Provider: "examplepkce",
		Subject:  "workspace-user",
		State:    startResponse.State,
		Code:     "auth-code-1",
	})
	if err != nil {
		t.Fatalf("complete pkce with original client: %v", err)
	}
	if connectionStatus.Status != "stored" {
		t.Fatalf("unexpected connection status after original completion: %#v", connectionStatus)
	}
	if got := atomic.LoadInt32(&tokenExchangeRequests); got != 1 {
		t.Fatalf("expected exactly one token exchange after original completion, got %d", got)
	}
	if storedRefreshToken := string(fakeStore.storedSecret["pkce-refresh-token"]); storedRefreshToken != "pkce-refresh-1" {
		t.Fatalf("expected refresh token in secure backend only, got %q", storedRefreshToken)
	}
}
