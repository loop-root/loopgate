package loopgate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	modelruntime "morph/internal/modelruntime"
	"morph/internal/secrets"
)

const openAICompatibleModelsTimeout = 4 * time.Second

// OllamaTagsResponse lists model ids from an OpenAI-compatible /models endpoint.
type OllamaTagsResponse struct {
	Models []OllamaModelTag `json:"models"`
}

// OllamaModelTag is a single model entry surfaced to Haven.
type OllamaModelTag struct {
	Name string `json:"name"`
	Size int64  `json:"size,omitempty"`
}

func (server *Server) handleOllamaTags(writer http.ResponseWriter, request *http.Request) {
	server.handleOpenAICompatibleModels(writer, request)
}

func (server *Server) handleOpenAICompatibleModels(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireTrustedHavenSession(writer, tokenClaims, "model listing requires trusted Haven session") {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityConnectionWrite) {
		return
	}

	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	baseURL := strings.TrimSpace(request.URL.Query().Get("base_url"))
	if baseURL == "" {
		baseURL = "http://127.0.0.1:11434/v1"
	}

	models, err := server.fetchHavenOpenAICompatibleModels(request.Context(), baseURL)
	if err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: secrets.RedactText(err.Error()),
			DenialCode:   DenialCodeExecutionFailed,
			Redacted:     true,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, OllamaTagsResponse{Models: models})
}

func havenOllamaAPIRootFromModelBase(baseURL string) string {
	trimmedBaseURL := strings.TrimSpace(baseURL)
	if trimmedBaseURL == "" {
		return "http://127.0.0.1:11434/v1"
	}
	return trimmedBaseURL
}

func (server *Server) fetchHavenOpenAICompatibleModels(ctx context.Context, baseURL string) ([]OllamaModelTag, error) {
	trimmedBaseURL := strings.TrimSpace(baseURL)
	if trimmedBaseURL == "" {
		trimmedBaseURL = "http://127.0.0.1:11434/v1"
	}
	if modelruntime.IsLoopbackModelBaseURL(trimmedBaseURL) {
		return fetchOpenAICompatibleModelTags(ctx, trimmedBaseURL, "")
	}

	runtimeConfig, err := modelruntime.LoadPersistedConfig(modelruntime.ConfigPath(server.repoRoot))
	if err != nil {
		return nil, fmt.Errorf("load model config: %w", err)
	}
	if strings.TrimSpace(runtimeConfig.ProviderName) != "openai_compatible" {
		return nil, fmt.Errorf("remote model listing requires an openai-compatible provider in current model settings")
	}
	if !strings.EqualFold(strings.TrimSpace(runtimeConfig.BaseURL), trimmedBaseURL) {
		return nil, fmt.Errorf("base_url must match the saved openai-compatible endpoint")
	}
	if strings.TrimSpace(runtimeConfig.ModelConnectionID) == "" {
		return nil, fmt.Errorf("save the openai-compatible connection first so Loopgate can use its stored credential")
	}

	modelConnectionRecord, err := server.resolveModelConnection(runtimeConfig.ModelConnectionID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(modelConnectionRecord.ProviderName) != "openai_compatible" {
		return nil, fmt.Errorf("saved model connection provider %q does not support openai-compatible model listing", modelConnectionRecord.ProviderName)
	}
	if !strings.EqualFold(strings.TrimSpace(modelConnectionRecord.BaseURL), trimmedBaseURL) {
		return nil, fmt.Errorf("saved model connection endpoint does not match base_url")
	}

	secretStore, err := server.secretStoreForRef(modelConnectionRecord.Credential)
	if err != nil {
		return nil, err
	}
	apiKeyBytes, _, err := secretStore.Get(ctx, modelConnectionRecord.Credential)
	if err != nil {
		return nil, fmt.Errorf("resolve model api key: %w", err)
	}
	return fetchOpenAICompatibleModelTags(ctx, trimmedBaseURL, strings.TrimSpace(string(apiKeyBytes)))
}

// fetchOllamaModelTags preserves the older loopback-only helper shape for existing Haven settings code.
func fetchOllamaModelTags(ctx context.Context, baseURL string) []OllamaModelTag {
	models, err := fetchOpenAICompatibleModelTags(ctx, baseURL, "")
	if err != nil {
		return nil
	}
	return models
}

// fetchOpenAICompatibleModelTags returns model ids from an OpenAI-compatible /models endpoint.
func fetchOpenAICompatibleModelTags(ctx context.Context, baseURL string, bearerToken string) ([]OllamaModelTag, error) {
	trimmedBaseURL := strings.TrimSpace(baseURL)
	if trimmedBaseURL == "" {
		trimmedBaseURL = "http://127.0.0.1:11434/v1"
	}

	modelsURL, err := url.Parse(trimmedBaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse model base url: %w", err)
	}
	if modelsURL.Scheme != "http" && modelsURL.Scheme != "https" {
		return nil, fmt.Errorf("model base url must use http or https")
	}
	modelsURL.Path = strings.TrimSuffix(modelsURL.Path, "/") + "/models"

	ctx, cancel := context.WithTimeout(ctx, openAICompatibleModelsTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build models request: %w", err)
	}
	if strings.TrimSpace(bearerToken) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(bearerToken))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		responseBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return nil, fmt.Errorf("models endpoint returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBytes)))
	}

	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode model list: %w", err)
	}

	models := make([]OllamaModelTag, 0, len(parsed.Data))
	for _, modelRecord := range parsed.Data {
		trimmedModelID := strings.TrimSpace(modelRecord.ID)
		if trimmedModelID == "" {
			continue
		}
		models = append(models, OllamaModelTag{Name: trimmedModelID})
	}
	return models, nil
}
