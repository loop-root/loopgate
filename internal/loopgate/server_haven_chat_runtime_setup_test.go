package loopgate

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	modelpkg "morph/internal/model"
	modelruntime "morph/internal/modelruntime"
)

func TestHavenChatRuntimeConfigLoadDoesNotExposeConfigPath(t *testing.T) {
	repoRoot := t.TempDir()
	configPath := modelruntime.ConfigPath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.Mkdir(configPath, 0o700); err != nil {
		t.Fatalf("mkdir config path as directory: %v", err)
	}

	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	pinTestProcessAsExpectedClient(t, server)
	server.resolveUserHomeDir = func() (string, error) { return repoRoot, nil }

	client.ConfigureSession("haven", "haven-chat-runtime-load-redaction", advertisedSessionCapabilityNames(status))
	capabilityToken, err := client.ensureCapabilityToken(context.Background())
	if err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	var response CapabilityResponse
	if err := client.doCapabilityJSON(context.Background(), client.defaultRequestTimeout, http.MethodPost, "/v1/chat", capabilityToken, havenChatRequest{
		Message: "hello",
	}, &response, nil); err != nil {
		t.Fatalf("POST /v1/chat: %v", err)
	}

	if response.DenialReason != havenChatRuntimeLoadFailureText {
		t.Fatalf("expected stable runtime load failure text, got %#v", response)
	}
	if !response.Redacted {
		t.Fatalf("expected redacted runtime load failure, got %#v", response)
	}
	if strings.Contains(response.DenialReason, configPath) || strings.Contains(response.DenialReason, filepath.Base(configPath)) {
		t.Fatalf("expected config path to stay redacted, got %#v", response)
	}
}

func TestHavenChatRuntimeInitDoesNotExposeBackendDetails(t *testing.T) {
	repoRoot := t.TempDir()
	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	pinTestProcessAsExpectedClient(t, server)
	server.resolveUserHomeDir = func() (string, error) { return repoRoot, nil }

	const backendDetail = "model_connection_id anthropic:stale failed for /tmp/runtime/state/model.json"
	server.newModelClientFromConfig = func(runtimeConfig modelruntime.Config) (*modelpkg.Client, modelruntime.Config, error) {
		return nil, modelruntime.Config{}, errors.New(backendDetail)
	}

	client.ConfigureSession("haven", "haven-chat-runtime-init-redaction", advertisedSessionCapabilityNames(status))
	capabilityToken, err := client.ensureCapabilityToken(context.Background())
	if err != nil {
		t.Fatalf("ensure capability token: %v", err)
	}

	var response CapabilityResponse
	if err := client.doCapabilityJSON(context.Background(), client.defaultRequestTimeout, http.MethodPost, "/v1/chat", capabilityToken, havenChatRequest{
		Message: "hello",
	}, &response, nil); err != nil {
		t.Fatalf("POST /v1/chat: %v", err)
	}

	if response.DenialReason != havenChatRuntimeInitFailureText {
		t.Fatalf("expected stable runtime init failure text, got %#v", response)
	}
	if !response.Redacted {
		t.Fatalf("expected redacted runtime init failure, got %#v", response)
	}
	if strings.Contains(response.DenialReason, backendDetail) || strings.Contains(response.DenialReason, "anthropic:stale") {
		t.Fatalf("expected backend detail to stay redacted, got %#v", response)
	}
}

func TestHavenChatTimeoutWindow_LocalProvidersGetLongerWindow(t *testing.T) {
	if got := havenChatTimeoutWindow(modelruntime.Config{ProviderName: "openai_compatible", BaseURL: "http://localhost:11434/v1"}); got != 5*time.Minute {
		t.Fatalf("loopback openai-compatible timeout = %v, want %v", got, 5*time.Minute)
	}
	if got := havenChatTimeoutWindow(modelruntime.Config{ProviderName: "anthropic", BaseURL: "https://api.anthropic.com/v1"}); got != 60*time.Second {
		t.Fatalf("remote anthropic timeout = %v, want %v", got, 60*time.Second)
	}
}
