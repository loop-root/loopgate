package loopgate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	modelruntime "morph/internal/modelruntime"
)

func TestHavenDeriveModelSettingsMode(t *testing.T) {
	testCases := []struct {
		name string
		cfg  modelruntime.Config
		want string
	}{
		{
			name: "anthropic stays anthropic",
			cfg:  modelruntime.Config{ProviderName: "anthropic", BaseURL: "https://api.anthropic.com/v1"},
			want: "anthropic",
		},
		{
			name: "loopback openai-compatible is local",
			cfg:  modelruntime.Config{ProviderName: "openai_compatible", BaseURL: "http://127.0.0.1:11434/v1"},
			want: "local",
		},
		{
			name: "remote openai-compatible keeps explicit mode",
			cfg:  modelruntime.Config{ProviderName: "openai_compatible", BaseURL: "https://api.openai.com/v1"},
			want: "openai_compatible",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := havenDeriveModelSettingsMode(testCase.cfg); got != testCase.want {
				t.Fatalf("unexpected mode: got %q want %q", got, testCase.want)
			}
		})
	}
}

func TestFetchOllamaModelTags_ReadsOpenAICompatibleModelsEndpoint(t *testing.T) {
	modelServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" {
			http.NotFound(writer, request)
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "llama3.3-70b-instruct"},
				{"id": "qwen2.5-coder"},
			},
		})
	}))
	defer modelServer.Close()

	models := fetchOllamaModelTags(context.Background(), modelServer.URL+"/v1")
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].Name != "llama3.3-70b-instruct" {
		t.Fatalf("unexpected first model %q", models[0].Name)
	}
	if models[1].Name != "qwen2.5-coder" {
		t.Fatalf("unexpected second model %q", models[1].Name)
	}
}

func TestHavenNonNilModelTagSlice_JSONEncodesEmptyArrayNotNull(t *testing.T) {
	t.Parallel()
	payload, err := json.Marshal(map[string]any{
		"available_models": havenNonNilModelTagSlice(nil),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(payload) != `{"available_models":[]}` {
		t.Fatalf("expected empty JSON array for nil slice, got %s", payload)
	}
}

func TestHavenModelSettingsGetDoesNotExposeConfigPath(t *testing.T) {
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
	havenClient := NewClient(client.socketPath)
	havenClient.ConfigureSession("haven", "model-settings-load-redaction", advertisedSessionCapabilityNames(status))
	token, err := havenClient.ensureCapabilityToken(context.Background())
	if err != nil {
		t.Fatalf("ensure haven token: %v", err)
	}

	var response HavenModelSettingsResponse
	err = havenClient.doJSON(context.Background(), http.MethodGet, "/v1/model/settings", token, nil, &response, nil)
	if err == nil {
		t.Fatal("expected model settings load to fail when config path is unreadable")
	}
	if !strings.Contains(err.Error(), havenModelSettingsLoadFailureText) {
		t.Fatalf("expected stable load failure text, got %v", err)
	}
	if strings.Contains(err.Error(), configPath) || strings.Contains(err.Error(), filepath.Base(configPath)) {
		t.Fatalf("expected config path to stay redacted, got %v", err)
	}
}

func TestHavenModelSettingsPostValidationDoesNotExposeConnectionID(t *testing.T) {
	repoRoot := t.TempDir()
	const staleConnectionID = "anthropic:stale"
	const anthropicBaseURL = "https://api.anthropic.com/v1"
	if err := modelruntime.SavePersistedConfig(modelruntime.ConfigPath(repoRoot), modelruntime.Config{
		ProviderName:      "anthropic",
		ModelName:         "claude-sonnet-4-5",
		BaseURL:           anthropicBaseURL,
		ModelConnectionID: staleConnectionID,
		Temperature:       0.7,
		MaxOutputTokens:   4096,
		Timeout:           120 * time.Second,
	}); err != nil {
		t.Fatalf("seed persisted model config: %v", err)
	}

	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	pinTestProcessAsExpectedClient(t, server)
	havenClient := NewClient(client.socketPath)
	havenClient.ConfigureSession("haven", "model-settings-validation-redaction", advertisedSessionCapabilityNames(status))
	token, err := havenClient.ensureCapabilityToken(context.Background())
	if err != nil {
		t.Fatalf("ensure haven token: %v", err)
	}

	err = havenClient.doJSON(context.Background(), http.MethodPost, "/v1/model/settings", token, havenModelSettingsPostRequest{
		Mode:      "anthropic",
		ModelName: "claude-sonnet-4-5",
	}, &HavenModelSettingsResponse{}, nil)
	if err == nil {
		t.Fatal("expected model settings update to fail when stored connection ref is missing")
	}
	if !strings.Contains(err.Error(), havenModelSettingsValidationFailureText) {
		t.Fatalf("expected stable validation failure text, got %v", err)
	}
	if strings.Contains(err.Error(), staleConnectionID) || strings.Contains(err.Error(), anthropicBaseURL) {
		t.Fatalf("expected model connection details to stay redacted, got %v", err)
	}
}

func TestHavenModelSettingsPostSaveDoesNotExposeConfigPath(t *testing.T) {
	repoRoot := t.TempDir()
	configPath := modelruntime.ConfigPath(repoRoot)
	if err := modelruntime.SavePersistedConfig(configPath, modelruntime.Config{
		ProviderName:    "openai_compatible",
		ModelName:       "phi4",
		BaseURL:         "http://localhost:11434/v1",
		Temperature:     0.7,
		MaxOutputTokens: 4096,
		Timeout:         120 * time.Second,
	}); err != nil {
		t.Fatalf("seed persisted model config: %v", err)
	}
	configDir := filepath.Dir(configPath)

	client, status, server := startLoopgateServer(t, repoRoot, loopgatePolicyYAML(false))
	pinTestProcessAsExpectedClient(t, server)
	havenClient := NewClient(client.socketPath)
	havenClient.ConfigureSession("haven", "model-settings-save-redaction", advertisedSessionCapabilityNames(status))
	token, err := havenClient.ensureCapabilityToken(context.Background())
	if err != nil {
		t.Fatalf("ensure haven token: %v", err)
	}
	if err := os.Chmod(configDir, 0o500); err != nil {
		t.Fatalf("chmod config dir read-only: %v", err)
	}
	defer func() {
		_ = os.Chmod(configDir, 0o700)
	}()

	err = havenClient.doJSON(context.Background(), http.MethodPost, "/v1/model/settings", token, havenModelSettingsPostRequest{
		Mode:         "local",
		ModelName:    "phi4-mini",
		LocalBaseURL: "http://localhost:11434/v1",
	}, &HavenModelSettingsResponse{}, nil)
	if err == nil {
		t.Fatal("expected model settings update to fail when config dir is read-only")
	}
	if !strings.Contains(err.Error(), havenModelSettingsUpdateFailureText) {
		t.Fatalf("expected stable update failure text, got %v", err)
	}
	if strings.Contains(err.Error(), configPath) || strings.Contains(err.Error(), filepath.Base(configPath)) {
		t.Fatalf("expected config path to stay redacted, got %v", err)
	}
}
