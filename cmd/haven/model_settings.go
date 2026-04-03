package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"morph/internal/loopgate"
	modelruntime "morph/internal/modelruntime"
)

// OllamaModel represents a model available in the local Ollama instance.
type OllamaModel struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// ModelSettingsResponse is returned by GetModelSettings.
type ModelSettingsResponse struct {
	CurrentModel    string        `json:"current_model"`
	ProviderName    string        `json:"provider_name"`
	BaseURL         string        `json:"base_url"`
	AvailableModels []OllamaModel `json:"available_models"`
	// Mode is "local" (Ollama / OpenAI-compatible loopback) or "anthropic" (cloud).
	Mode string `json:"mode"`
	// HasCloudCredential is true when Anthropic is configured with a Loopgate-stored API key.
	HasCloudCredential bool `json:"has_cloud_credential"`
	// LocalBaseURL is the OpenAI-compatible base URL shown for local mode (includes /v1 when set).
	LocalBaseURL string `json:"local_base_url"`
}

// SaveModelRequest is the request payload for SaveModelSelection.
type SaveModelRequest struct {
	ModelName string `json:"model_name"`
}

// SaveModelProviderRequest switches between local Ollama (loopback) and Anthropic cloud.
//
// API keys MUST NOT be written to repo-local JSON or logs. When Mode is "anthropic" and
// AnthropicAPIKey is non-empty, Haven sends the key only to Loopgate's StoreModelConnection
// RPC (see internal/loopgate/model_connections.go): Loopgate copies the secret into the OS
// secure store (e.g. macOS Keychain for BackendSecure), clears the plaintext from the request,
// and persists only a SecretRef + connection metadata. model_runtime.json stores at most
// model_connection_id, never the raw key.
type SaveModelProviderRequest struct {
	Mode string `json:"mode"` // "local" | "anthropic"
	// ModelName is required: Ollama tag for local, or Anthropic model id (e.g. claude-sonnet-4-5) for cloud.
	ModelName string `json:"model_name"`
	// LocalBaseURL is the OpenAI-compatible endpoint for Ollama (e.g. http://localhost:11434/v1).
	LocalBaseURL string `json:"local_base_url"`
	// AnthropicAPIKey is required when switching to Anthropic or replacing the key; omit to keep existing credential.
	AnthropicAPIKey string `json:"anthropic_api_key"`
}

// GetModelSettings returns the current model config and available Ollama models.
func (app *HavenApp) GetModelSettings() ModelSettingsResponse {
	configPath := modelruntime.ConfigPath(app.setupRepoRoot())
	runtimeConfig, err := modelruntime.LoadPersistedConfig(configPath)
	if err != nil {
		return ModelSettingsResponse{CurrentModel: "unknown", ProviderName: "unknown"}
	}

	baseURL := runtimeConfig.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}

	// Strip /v1 suffix for Ollama API calls.
	ollamaBase := baseURL
	if len(ollamaBase) > 3 && ollamaBase[len(ollamaBase)-3:] == "/v1" {
		ollamaBase = ollamaBase[:len(ollamaBase)-3]
	}

	models, _ := listOllamaModels(ollamaBase)

	mode := deriveModelSettingsMode(runtimeConfig)
	// OpenAI-compatible endpoint for local mode. When the active provider is Anthropic,
	// BaseURL points at Anthropic — do not surface that as the "local Ollama" default
	// (Settings uses this to prefill "switch back to local").
	localBase := runtimeConfig.BaseURL
	if localBase == "" || mode == "anthropic" {
		localBase = "http://localhost:11434/v1"
	}
	hasCloud := runtimeConfig.ProviderName == "anthropic" && strings.TrimSpace(runtimeConfig.ModelConnectionID) != ""

	return ModelSettingsResponse{
		CurrentModel:       runtimeConfig.ModelName,
		ProviderName:       runtimeConfig.ProviderName,
		BaseURL:            runtimeConfig.BaseURL,
		AvailableModels:    models,
		Mode:               mode,
		HasCloudCredential: hasCloud,
		LocalBaseURL:       localBase,
	}
}

func deriveModelSettingsMode(cfg modelruntime.Config) string {
	if cfg.ProviderName == "anthropic" {
		return "anthropic"
	}
	return "local"
}

// SaveModelSelection updates the model_runtime.json with the selected model.
func (app *HavenApp) SaveModelSelection(req SaveModelRequest) SaveSettingsResult {
	configPath := modelruntime.ConfigPath(app.setupRepoRoot())
	runtimeConfig, err := modelruntime.LoadPersistedConfig(configPath)
	if err != nil {
		return SaveSettingsResult{Error: fmt.Sprintf("load model config: %v", err)}
	}

	runtimeConfig.ModelName = strings.TrimSpace(req.ModelName)
	if runtimeConfig.ModelName == "" {
		return SaveSettingsResult{Error: "model name is required"}
	}

	normalizedConfig, err := modelruntime.NormalizeConfig(runtimeConfig)
	if err != nil {
		return SaveSettingsResult{Error: fmt.Sprintf("normalize model config: %v", err)}
	}
	validatedConfig, err := app.loopgateClient.ValidateModelConfig(context.Background(), normalizedConfig)
	if err != nil {
		return SaveSettingsResult{Error: fmt.Sprintf("validate model config: %v", err)}
	}

	if err := modelruntime.SavePersistedConfig(configPath, validatedConfig); err != nil {
		return SaveSettingsResult{Error: fmt.Sprintf("save model config: %v", err)}
	}

	return SaveSettingsResult{Success: true}
}

// SaveModelProviderSettings switches local vs Anthropic and persists credentials through Loopgate.
func (app *HavenApp) SaveModelProviderSettings(req SaveModelProviderRequest) SaveSettingsResult {
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	modelName := strings.TrimSpace(req.ModelName)
	if modelName == "" {
		return SaveSettingsResult{Error: "model name is required"}
	}

	configPath := modelruntime.ConfigPath(app.setupRepoRoot())
	existing, err := modelruntime.LoadPersistedConfig(configPath)
	if err != nil {
		return SaveSettingsResult{Error: fmt.Sprintf("load model config: %v", err)}
	}

	var next modelruntime.Config

	switch mode {
	case "local":
		localBase := strings.TrimSpace(req.LocalBaseURL)
		if localBase == "" {
			localBase = "http://localhost:11434/v1"
		}
		next = modelruntime.Config{
			ProviderName:      "openai_compatible",
			ModelName:         modelName,
			BaseURL:           localBase,
			ModelConnectionID: "",
			APIKeyEnvVar:      "",
			Temperature:       pickModelTemperature(existing.Temperature, 0.7),
			MaxOutputTokens:   pickMaxOutputTokens(existing.MaxOutputTokens, 4096),
			Timeout:           pickModelTimeout(existing.Timeout, 120*time.Second),
		}
	case "anthropic":
		apiKey := strings.TrimSpace(req.AnthropicAPIKey)
		next = modelruntime.Config{
			ProviderName:    "anthropic",
			ModelName:       modelName,
			BaseURL:         pickAnthropicBaseURL(existing),
			Temperature:     pickModelTemperature(existing.Temperature, 0.7),
			MaxOutputTokens: pickMaxOutputTokens(existing.MaxOutputTokens, 4096),
			Timeout:         pickModelTimeout(existing.Timeout, 120*time.Second),
		}
		if apiKey != "" {
			next.ModelConnectionID = setupModelConnectionID("anthropic")
			// Loopgate StoreModelConnection persists the key via secrets.BackendSecure (OS keychain
			// on macOS), clears SecretValue on the server, and only records a SecretRef — never the raw key in JSON.
			if _, err := app.loopgateClient.StoreModelConnection(context.Background(), loopgate.ModelConnectionStoreRequest{
				ConnectionID: next.ModelConnectionID,
				ProviderName: "anthropic",
				BaseURL:      next.BaseURL,
				SecretValue:  apiKey,
			}); err != nil {
				return SaveSettingsResult{Error: fmt.Sprintf("store anthropic credential: %v", err)}
			}
		} else if strings.TrimSpace(existing.ModelConnectionID) != "" && existing.ProviderName == "anthropic" {
			next.ModelConnectionID = existing.ModelConnectionID
			if strings.TrimSpace(next.BaseURL) == "" {
				next.BaseURL = existing.BaseURL
			}
		} else {
			return SaveSettingsResult{Error: "anthropic API key is required (paste once; it is stored in Loopgate, not in a project file)"}
		}
	default:
		return SaveSettingsResult{Error: fmt.Sprintf("unsupported mode %q (use local or anthropic)", req.Mode)}
	}

	normalizedConfig, err := modelruntime.NormalizeConfig(next)
	if err != nil {
		return SaveSettingsResult{Error: fmt.Sprintf("normalize model config: %v", err)}
	}
	validatedConfig, err := app.loopgateClient.ValidateModelConfig(context.Background(), normalizedConfig)
	if err != nil {
		return SaveSettingsResult{Error: fmt.Sprintf("validate model config: %v", err)}
	}

	if err := modelruntime.SavePersistedConfig(configPath, validatedConfig); err != nil {
		return SaveSettingsResult{Error: fmt.Sprintf("save model config: %v", err)}
	}

	return SaveSettingsResult{Success: true}
}

func pickAnthropicBaseURL(existing modelruntime.Config) string {
	if strings.TrimSpace(existing.BaseURL) != "" && existing.ProviderName == "anthropic" {
		return strings.TrimSpace(existing.BaseURL)
	}
	return "https://api.anthropic.com/v1"
}

func pickModelTemperature(current float64, def float64) float64 {
	if current > 0 {
		return current
	}
	return def
}

func pickMaxOutputTokens(current int, def int) int {
	if current > 0 {
		return current
	}
	return def
}

func pickModelTimeout(current time.Duration, def time.Duration) time.Duration {
	if current > 0 {
		return current
	}
	return def
}

// listOllamaModels queries the Ollama /api/tags endpoint for available models.
// Returns an empty slice (not an error) if Ollama is unreachable.
func listOllamaModels(ollamaBaseURL string) ([]OllamaModel, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ollamaBaseURL+"/api/tags", nil)
	if err != nil {
		return nil, nil
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil // Ollama not running — not an error
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	var result struct {
		Models []OllamaModel `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, nil
	}

	return result.Models, nil
}
