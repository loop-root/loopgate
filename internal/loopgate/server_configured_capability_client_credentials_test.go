package loopgate

import (
	"context"
	"io"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"loopgate/internal/secrets"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfiguredClientCredentialsCapability_ExecutesThroughLoopgateOnly(t *testing.T) {
	repoRoot := t.TempDir()
	var tokenRequests int
	var apiRequests int

	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oauth/token":
			tokenRequests++
			if err := request.ParseForm(); err != nil {
				t.Fatalf("parse token form: %v", err)
			}
			if gotGrantType := request.Form.Get("grant_type"); gotGrantType != controlapipkg.GrantTypeClientCredentials {
				t.Fatalf("unexpected grant_type: %q", gotGrantType)
			}
			if gotClientID := request.Form.Get("client_id"); gotClientID != "example-client" {
				t.Fatalf("unexpected client_id: %q", gotClientID)
			}
			if gotClientSecret := request.Form.Get("client_secret"); gotClientSecret != "super-secret-client" {
				t.Fatalf("unexpected client_secret: %q", gotClientSecret)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"access_token":"provider-access-token","token_type":"Bearer","expires_in":300}`)
		case "/api/status":
			apiRequests++
			if gotAuthorization := request.Header.Get("Authorization"); gotAuthorization != "Bearer provider-access-token" {
				t.Fatalf("unexpected authorization header: %q", gotAuthorization)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"service":"example-api","healthy":true,"sensitive":"raw-body-only"}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer providerServer.Close()

	writeConfiguredConnectionYAML(t, repoRoot, providerServer.URL)
	t.Setenv("LOOPGATE_EXAMPLE_SECRET", "super-secret-client")

	client, status, server := startLoopgateServer(t, repoRoot, loopgateHTTPPolicyYAML(false))
	server.httpClient = providerServer.Client()

	if _, err := server.RegisterConnection(context.Background(), connectionRegistration{
		Provider:  "example",
		GrantType: controlapipkg.GrantTypeClientCredentials,
		Subject:   "service-bot",
		Scopes:    []string{"status.read"},
		Credential: secrets.SecretRef{
			ID:          "example-client-secret",
			Backend:     secrets.BackendEnv,
			AccountName: "LOOPGATE_EXAMPLE_SECRET",
			Scope:       "example.status_read",
		},
	}); err != nil {
		t.Fatalf("register configured connection: %v", err)
	}

	if !containsCapability(status.Capabilities, "example.status_get") {
		t.Fatalf("expected configured capability in status, got %#v", status.Capabilities)
	}

	firstResponse, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-example-1",
		Capability: "example.status_get",
	})
	if err != nil {
		t.Fatalf("execute configured capability: %v", err)
	}
	if firstResponse.Status != controlapipkg.ResponseStatusSuccess {
		t.Fatalf("unexpected configured capability response: %#v", firstResponse)
	}
	if firstResponse.StructuredResult["service"] != "example-api" {
		t.Fatalf("unexpected structured result: %#v", firstResponse.StructuredResult)
	}
	if _, found := firstResponse.StructuredResult["sensitive"]; found {
		t.Fatalf("raw unapproved field leaked into structured result: %#v", firstResponse.StructuredResult)
	}
	if firstResponse.QuarantineRef == "" {
		t.Fatalf("expected quarantined raw response, got %#v", firstResponse)
	}
	if promptEligible, ok := firstResponse.Metadata["prompt_eligible"].(bool); !ok || promptEligible {
		t.Fatalf("expected configured capability to be non-prompt-eligible, got %#v", firstResponse.Metadata)
	}
	if quarantined, ok := firstResponse.Metadata["quarantined"].(bool); !ok || !quarantined {
		t.Fatalf("expected configured capability to be quarantined, got %#v", firstResponse.Metadata)
	}
	if contentOrigin := firstResponse.Metadata["content_origin"]; contentOrigin != contentOriginRemote {
		t.Fatalf("expected remote content origin, got %#v", firstResponse.Metadata)
	}
	if contentClass := firstResponse.Metadata["content_class"]; contentClass != contentClassStructuredJSON {
		t.Fatalf("expected structured_json content class, got %#v", firstResponse.Metadata)
	}
	if contentType := firstResponse.Metadata["content_type"]; contentType != contentTypeApplicationJSON {
		t.Fatalf("expected application/json content type, got %#v", firstResponse.Metadata)
	}
	if extractor := firstResponse.Metadata["extractor"]; extractor != extractorJSONFieldAllowlist {
		t.Fatalf("expected json_field_allowlist extractor, got %#v", firstResponse.Metadata)
	}
	if fieldTrust := firstResponse.Metadata["field_trust"]; fieldTrust != fieldTrustDeterministic {
		t.Fatalf("expected deterministic field trust, got %#v", firstResponse.Metadata)
	}
	if derivedQuarantineRef := firstResponse.Metadata["derived_from_quarantine_ref"]; derivedQuarantineRef != firstResponse.QuarantineRef {
		t.Fatalf("expected derived quarantine ref to match response quarantine ref, got %#v", firstResponse.Metadata)
	}

	secondResponse, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-example-2",
		Capability: "example.status_get",
	})
	if err != nil {
		t.Fatalf("execute configured capability second time: %v", err)
	}
	if secondResponse.Status != controlapipkg.ResponseStatusSuccess {
		t.Fatalf("unexpected second response: %#v", secondResponse)
	}
	if tokenRequests != 1 {
		t.Fatalf("expected one token exchange due to in-memory cache, got %d", tokenRequests)
	}
	if apiRequests != 2 {
		t.Fatalf("expected two API requests, got %d", apiRequests)
	}
}

func TestConfiguredClientCredentialsTokenFetchIsSerializedPerConnection(t *testing.T) {
	repoRoot := t.TempDir()
	var tokenRequests atomic.Int32
	tokenRequestStarted := make(chan struct{}, 1)
	releaseTokenResponse := make(chan struct{})

	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oauth/token":
			if tokenRequests.Add(1) == 1 {
				tokenRequestStarted <- struct{}{}
			}
			<-releaseTokenResponse
			if err := request.ParseForm(); err != nil {
				t.Fatalf("parse token form: %v", err)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"access_token":"provider-access-token","token_type":"Bearer","expires_in":300}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer providerServer.Close()

	writeConfiguredConnectionYAML(t, repoRoot, providerServer.URL)
	t.Setenv("LOOPGATE_EXAMPLE_SECRET", "super-secret-client")

	_, _, server := startLoopgateServer(t, repoRoot, loopgateHTTPPolicyYAML(false))
	server.httpClient = providerServer.Client()

	if _, err := server.RegisterConnection(context.Background(), connectionRegistration{
		Provider:  "example",
		GrantType: controlapipkg.GrantTypeClientCredentials,
		Subject:   "service-bot",
		Scopes:    []string{"status.read"},
		Credential: secrets.SecretRef{
			ID:          "example-client-secret",
			Backend:     secrets.BackendEnv,
			AccountName: "LOOPGATE_EXAMPLE_SECRET",
			Scope:       "example.status_read",
		},
	}); err != nil {
		t.Fatalf("register configured connection: %v", err)
	}

	configuredConnectionDefinition, found := server.configuredConnectionSnapshot(connectionRecordKey("example", "service-bot"))
	if !found {
		t.Fatal("expected configured connection snapshot")
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
	case <-tokenRequestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for token request to start")
	}
	time.Sleep(25 * time.Millisecond)
	close(releaseTokenResponse)
	requestGroup.Wait()
	close(resultTokens)
	close(resultErrors)

	for err := range resultErrors {
		t.Fatalf("unexpected token fetch error: %v", err)
	}
	for accessToken := range resultTokens {
		if accessToken != "provider-access-token" {
			t.Fatalf("unexpected access token: %q", accessToken)
		}
	}
	if tokenRequests.Load() != 1 {
		t.Fatalf("expected one token request across concurrent callers, got %d", tokenRequests.Load())
	}
}
