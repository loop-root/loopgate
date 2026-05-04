package loopgate

import (
	"context"
	"io"
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfiguredPublicHTMLMetaCapability_ExecutesWithoutSecretResolution(t *testing.T) {
	repoRoot := t.TempDir()

	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if gotAuthorization := strings.TrimSpace(request.Header.Get("Authorization")); gotAuthorization != "" {
			t.Fatalf("expected no authorization header for public_read capability, got %q", gotAuthorization)
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(writer, "<html><head><title>Stripe Status</title><meta name=\"description\" content=\"No active incidents.\"></head><body><p>ignored</p></body></html>")
	}))
	defer providerServer.Close()

	writeConfiguredPublicHTMLMetaYAML(t, repoRoot, providerServer.URL)

	client, status, server := startLoopgateServer(t, repoRoot, loopgateHTTPPolicyYAML(false))
	server.httpClient = providerServer.Client()

	if len(status.Connections) != 1 || status.Connections[0].GrantType != controlapipkg.GrantTypePublicRead {
		t.Fatalf("expected public_read connection summary, got %#v", status.Connections)
	}

	response, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-status-html",
		Capability: "statuspage.summary_get",
	})
	if err != nil {
		t.Fatalf("execute public html capability: %v", err)
	}
	if response.Status != controlapipkg.ResponseStatusSuccess {
		t.Fatalf("expected successful public html response, got %#v", response)
	}
	if response.StructuredResult["page_title"] != "Stripe Status" {
		t.Fatalf("unexpected page title extraction: %#v", response.StructuredResult)
	}
	if response.StructuredResult["description"] != "No active incidents." {
		t.Fatalf("unexpected description extraction: %#v", response.StructuredResult)
	}
	if response.Metadata["extractor"] != extractorHTMLMetaAllowlist {
		t.Fatalf("unexpected metadata for public html response: %#v", response.Metadata)
	}
}

func TestConfiguredPublicJSONNestedCapability_ExecutesWithoutSecretResolution(t *testing.T) {
	repoRoot := t.TempDir()

	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if gotAuthorization := strings.TrimSpace(request.Header.Get("Authorization")); gotAuthorization != "" {
			t.Fatalf("expected no authorization header for public_read capability, got %q", gotAuthorization)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"page":{"title":"GitHub Status"},"status":{"description":"All Systems Operational","indicator":"none"},"ignored":{"nested":"nope"}}`)
	}))
	defer providerServer.Close()

	writeConfiguredPublicJSONNestedYAML(t, repoRoot, providerServer.URL)

	client, status, server := startLoopgateServer(t, repoRoot, loopgateHTTPPolicyYAML(false))
	server.httpClient = providerServer.Client()

	if len(status.Connections) != 1 || status.Connections[0].GrantType != controlapipkg.GrantTypePublicRead {
		t.Fatalf("expected public_read connection summary, got %#v", status.Connections)
	}

	response, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-status-json-nested",
		Capability: "statuspage.summary_get",
	})
	if err != nil {
		t.Fatalf("execute public nested json capability: %v", err)
	}
	if response.Status != controlapipkg.ResponseStatusSuccess {
		t.Fatalf("expected successful public nested json response, got %#v", response)
	}
	if response.StructuredResult["status_description"] != "All Systems Operational" {
		t.Fatalf("unexpected status description extraction: %#v", response.StructuredResult)
	}
	if response.StructuredResult["status_indicator"] != "none" {
		t.Fatalf("unexpected status indicator extraction: %#v", response.StructuredResult)
	}
	if response.Metadata["extractor"] != extractorJSONNestedSelector {
		t.Fatalf("unexpected metadata for public nested json response: %#v", response.Metadata)
	}
}

func TestConfiguredPublicJSONIssueListCapability_ExecutesWithoutSecretResolution(t *testing.T) {
	repoRoot := t.TempDir()

	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if gotAuthorization := strings.TrimSpace(request.Header.Get("Authorization")); gotAuthorization != "" {
			t.Fatalf("expected no authorization header for public_read capability, got %q", gotAuthorization)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"issues":{"items":[{"number":101,"title":"First issue","state":"open","updated_at":"2026-03-08T10:00:00Z","html_url":"https://example.test/issues/101"},{"number":102,"title":"Second issue","state":"open","updated_at":"2026-03-07T09:30:00Z","html_url":"https://example.test/issues/102"},{"number":103,"title":"Third issue","state":"open","updated_at":"2026-03-06T08:15:00Z","html_url":"https://example.test/issues/103"}]}}`)
	}))
	defer providerServer.Close()

	writeConfiguredPublicJSONIssueListYAML(t, repoRoot, providerServer.URL)

	client, status, server := startLoopgateServer(t, repoRoot, loopgateHTTPPolicyYAML(false))
	server.httpClient = providerServer.Client()

	if len(status.Connections) != 1 || status.Connections[0].GrantType != controlapipkg.GrantTypePublicRead {
		t.Fatalf("expected public_read connection summary, got %#v", status.Connections)
	}

	response, err := client.ExecuteCapability(context.Background(), controlapipkg.CapabilityRequest{
		RequestID:  "req-repo-issues",
		Capability: "repo.issues_list",
	})
	if err != nil {
		t.Fatalf("execute public issue list capability: %v", err)
	}
	if response.Status != controlapipkg.ResponseStatusSuccess {
		t.Fatalf("expected successful public issue list response, got %#v", response)
	}
	rawIssuesValue, found := response.StructuredResult["issues"]
	if !found {
		t.Fatalf("expected issues field, got %#v", response.StructuredResult)
	}
	issueItems, ok := rawIssuesValue.([]interface{})
	if !ok {
		t.Fatalf("expected issues array, got %#v", response.StructuredResult)
	}
	if len(issueItems) != 2 {
		t.Fatalf("expected bounded issue list of 2 items, got %#v", issueItems)
	}
	issuesFieldMeta := response.FieldsMeta["issues"]
	if issuesFieldMeta.Kind != controlapipkg.ResultFieldKindArray || issuesFieldMeta.PromptEligible {
		t.Fatalf("unexpected issues field metadata: %#v", issuesFieldMeta)
	}
	if response.Metadata["extractor"] != extractorJSONObjectList {
		t.Fatalf("unexpected metadata for public issue list response: %#v", response.Metadata)
	}
}
