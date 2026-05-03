package loopgate

import (
	"context"
	"io"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"loopgate/internal/secrets"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfiguredCapabilityRejectsRedirectToHostOutsideAllowedHosts(t *testing.T) {
	repoRoot := t.TempDir()
	var tokenRequests int
	var apiRequests int
	var redirectedAPIRequests int

	redirectTargetServer := newLocalhostTestServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		redirectedAPIRequests++
		http.NotFound(writer, request)
	}))
	redirectTargetURL := localhostRedirectURL(t, redirectTargetServer.URL, "/api/status")

	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oauth/token":
			tokenRequests++
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"access_token":"provider-access-token","token_type":"Bearer","expires_in":300}`)
		case "/api/status":
			apiRequests++
			http.Redirect(writer, request, redirectTargetURL, http.StatusTemporaryRedirect)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer providerServer.Close()

	writeConfiguredConnectionYAML(t, repoRoot, providerServer.URL)
	t.Setenv("LOOPGATE_EXAMPLE_SECRET", "super-secret-client")

	client, _, server := startLoopgateServer(t, repoRoot, loopgateHTTPPolicyYAML(false))
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

	response, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-example-redirect-api",
		Capability: "example.status_get",
	})
	if err != nil {
		t.Fatalf("execute configured capability with redirect denial: %v", err)
	}
	if response.Status != controlapipkg.ResponseStatusError {
		t.Fatalf("expected error response for disallowed redirect, got %#v", response)
	}
	if !strings.Contains(response.DenialReason, `redirect host "localhost" is not in allowed_hosts`) {
		t.Fatalf("expected allowed_hosts redirect denial, got %#v", response)
	}
	if tokenRequests != 1 {
		t.Fatalf("expected one token request, got %d", tokenRequests)
	}
	if apiRequests != 1 {
		t.Fatalf("expected one api request before redirect denial, got %d", apiRequests)
	}
	if redirectedAPIRequests != 0 {
		t.Fatalf("expected redirect target not to be reached, got %d requests", redirectedAPIRequests)
	}
}

func TestConfiguredTokenExchangeRejectsRedirectToHostOutsideAllowedHosts(t *testing.T) {
	repoRoot := t.TempDir()
	var apiRequests int
	var redirectedTokenRequests int

	redirectTargetServer := newLocalhostTestServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		redirectedTokenRequests++
		http.NotFound(writer, request)
	}))
	redirectTargetURL := localhostRedirectURL(t, redirectTargetServer.URL, "/oauth/token")

	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oauth/token":
			http.Redirect(writer, request, redirectTargetURL, http.StatusTemporaryRedirect)
		case "/api/status":
			apiRequests++
			http.NotFound(writer, request)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer providerServer.Close()

	writeConfiguredConnectionYAML(t, repoRoot, providerServer.URL)
	t.Setenv("LOOPGATE_EXAMPLE_SECRET", "super-secret-client")

	client, _, server := startLoopgateServer(t, repoRoot, loopgateHTTPPolicyYAML(false))
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

	response, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-example-redirect-token",
		Capability: "example.status_get",
	})
	if err != nil {
		t.Fatalf("execute configured capability with token redirect denial: %v", err)
	}
	if response.Status != controlapipkg.ResponseStatusError {
		t.Fatalf("expected error response for disallowed token redirect, got %#v", response)
	}
	if !strings.Contains(response.DenialReason, `redirect host "localhost" is not in allowed_hosts`) {
		t.Fatalf("expected allowed_hosts token redirect denial, got %#v", response)
	}
	if redirectedTokenRequests != 0 {
		t.Fatalf("expected redirected token target not to be reached, got %d requests", redirectedTokenRequests)
	}
	if apiRequests != 0 {
		t.Fatalf("expected api request to be skipped after token redirect denial, got %d", apiRequests)
	}
}
