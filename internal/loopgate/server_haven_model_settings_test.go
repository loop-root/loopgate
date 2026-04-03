package loopgate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
