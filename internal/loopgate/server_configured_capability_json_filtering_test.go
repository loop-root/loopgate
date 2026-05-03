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

func TestConfiguredCapability_DeniesUnexpectedResponseContentType(t *testing.T) {
	repoRoot := t.TempDir()

	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oauth/token":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"access_token":"provider-access-token","token_type":"Bearer","expires_in":300}`)
		case "/api/status":
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(writer, `<html><body>not-json</body></html>`)
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
		RequestID:  "req-example-bad-content-type",
		Capability: "example.status_get",
	})
	if err != nil {
		t.Fatalf("execute configured capability: %v", err)
	}
	if response.Status != controlapipkg.ResponseStatusError {
		t.Fatalf("expected error response for content-type mismatch, got %#v", response)
	}
	if !strings.Contains(response.DenialReason, "content type") {
		t.Fatalf("expected content-type mismatch error, got %#v", response)
	}
}

func TestConfiguredCapability_DeniesOversizedInlineField(t *testing.T) {
	repoRoot := t.TempDir()

	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oauth/token":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"access_token":"provider-access-token","token_type":"Bearer","expires_in":300}`)
		case "/api/status":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"service":"`+strings.Repeat("a", 300)+`","healthy":true}`)
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
		RequestID:  "req-example-oversized-field",
		Capability: "example.status_get",
	})
	if err != nil {
		t.Fatalf("execute configured capability: %v", err)
	}
	if response.Status != controlapipkg.ResponseStatusError {
		t.Fatalf("expected error response for oversized field, got %#v", response)
	}
	if !strings.Contains(response.DenialReason, "max_inline_bytes") {
		t.Fatalf("expected max_inline_bytes error, got %#v", response)
	}
}

func TestConfiguredCapability_UsesBlobRefForOversizedFieldWhenAllowed(t *testing.T) {
	repoRoot := t.TempDir()

	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oauth/token":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"access_token":"provider-access-token","token_type":"Bearer","expires_in":300}`)
		case "/api/status":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"service":"`+strings.Repeat("a", 300)+`","healthy":true}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer providerServer.Close()

	writeConfiguredConnectionYAMLWithBlobFallback(t, repoRoot, providerServer.URL)
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
		RequestID:  "req-example-oversized-field-blob",
		Capability: "example.status_get",
	})
	if err != nil {
		t.Fatalf("execute configured capability: %v", err)
	}
	if response.Status != controlapipkg.ResponseStatusSuccess {
		t.Fatalf("expected successful response with blob_ref fallback, got %#v", response)
	}
	serviceField, found := response.StructuredResult["service"]
	if !found {
		t.Fatalf("expected service field in structured result, got %#v", response.StructuredResult)
	}
	serviceBlobRef, ok := serviceField.(map[string]interface{})
	if !ok {
		t.Fatalf("expected service field blob_ref object, got %#v", serviceField)
	}
	if serviceBlobRef["kind"] != controlapipkg.ResultFieldKindBlobRef {
		t.Fatalf("expected blob_ref kind, got %#v", serviceBlobRef)
	}
	if serviceBlobRef["quarantine_ref"] != response.QuarantineRef {
		t.Fatalf("expected blob_ref to reference response quarantine ref, got %#v", serviceBlobRef)
	}
	if serviceBlobRef["field_path"] != "service" {
		t.Fatalf("expected blob_ref field path, got %#v", serviceBlobRef)
	}
	if serviceBlobRef["storage_state"] != quarantineStorageStateBlobPresent {
		t.Fatalf("expected blob_present state in blob_ref, got %#v", serviceBlobRef)
	}
	serviceFieldMeta, found := response.FieldsMeta["service"]
	if !found {
		t.Fatalf("expected fields_meta for blob_ref field, got %#v", response.FieldsMeta)
	}
	if serviceFieldMeta.Kind != controlapipkg.ResultFieldKindBlobRef {
		t.Fatalf("expected blob_ref field kind metadata, got %#v", serviceFieldMeta)
	}
	if serviceFieldMeta.PromptEligible {
		t.Fatalf("expected blob_ref field to remain non-prompt eligible, got %#v", serviceFieldMeta)
	}
}

func TestConfiguredCapability_DeniesNonScalarField(t *testing.T) {
	repoRoot := t.TempDir()

	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oauth/token":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"access_token":"provider-access-token","token_type":"Bearer","expires_in":300}`)
		case "/api/status":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"service":["bad","array"],"healthy":true}`)
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
		RequestID:  "req-example-array-field",
		Capability: "example.status_get",
	})
	if err != nil {
		t.Fatalf("execute configured capability: %v", err)
	}
	if response.Status != controlapipkg.ResponseStatusError {
		t.Fatalf("expected error response for non-scalar field, got %#v", response)
	}
	if !strings.Contains(response.DenialReason, "must be scalar") {
		t.Fatalf("expected scalar-kind error, got %#v", response)
	}
}
