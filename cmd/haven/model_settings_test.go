package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListAvailableModels_ReturnsModelsFromOllama(t *testing.T) {
	ollamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"models": []map[string]interface{}{
				{"name": "qwen2.5:7b", "size": 4700000000},
				{"name": "llama3:8b", "size": 4800000000},
			},
		})
	}))
	defer ollamaServer.Close()

	models, err := listOllamaModels(ollamaServer.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].Name != "qwen2.5:7b" {
		t.Errorf("expected first model qwen2.5:7b, got %s", models[0].Name)
	}
}

func TestListAvailableModels_HandlesOllamaDown(t *testing.T) {
	models, err := listOllamaModels("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("should not error, got: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("expected 0 models when Ollama is down, got %d", len(models))
	}
}
