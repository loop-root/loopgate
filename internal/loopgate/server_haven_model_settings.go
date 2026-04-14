package loopgate

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	modelruntime "morph/internal/modelruntime"
	"morph/internal/secrets"
)

const maxHavenModelSettingsBodyBytes = 32 * 1024

const (
	havenModelSettingsLoadFailureText       = "failed to load model settings"
	havenModelSettingsUpdateFailureText     = "failed to update model settings"
	havenModelSettingsValidationFailureText = "failed to validate model settings"
)

// HavenModelSettingsResponse is the JSON for GET/POST /v1/model/settings.
type HavenModelSettingsResponse struct {
	CurrentModel       string           `json:"current_model"`
	ProviderName       string           `json:"provider_name"`
	BaseURL            string           `json:"base_url"`
	AvailableModels    []OllamaModelTag `json:"available_models"`
	Mode               string           `json:"mode"`
	HasCloudCredential bool             `json:"has_cloud_credential"`
	LocalBaseURL       string           `json:"local_base_url"`
}

type havenModelSettingsPostRequest struct {
	Mode            string `json:"mode"`
	ModelName       string `json:"model_name"`
	LocalBaseURL    string `json:"local_base_url"`
	AnthropicAPIKey string `json:"anthropic_api_key"`
	OpenAIAPIKey    string `json:"openai_api_key"`
}

func (server *Server) handleHavenModelSettings(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		server.handleHavenModelSettingsGet(writer, request)
	case http.MethodPost:
		server.handleHavenModelSettingsPost(writer, request)
	default:
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (server *Server) handleHavenModelSettingsGet(writer http.ResponseWriter, request *http.Request) {
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireTrustedHavenSession(writer, tokenClaims, "model settings require trusted Haven session") {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityModelSettingsRead) {
		return
	}
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	configPath := modelruntime.ConfigPath(server.repoRoot)
	runtimeConfig, err := modelruntime.LoadPersistedConfig(configPath)
	if err != nil {
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: havenModelSettingsLoadFailureText,
			DenialCode:   DenialCodeExecutionFailed,
			Redacted:     true,
		})
		return
	}

	baseURL := runtimeConfig.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	ollamaRoot := havenOllamaAPIRootFromModelBase(baseURL)
	models := havenNonNilModelTagSlice(fetchOllamaModelTags(request.Context(), ollamaRoot))

	mode := havenDeriveModelSettingsMode(runtimeConfig)
	localBase := runtimeConfig.BaseURL
	if localBase == "" || mode == "anthropic" {
		localBase = "http://localhost:11434/v1"
	}
	hasCloud := strings.TrimSpace(runtimeConfig.ModelConnectionID) != "" && (runtimeConfig.ProviderName == "anthropic" || runtimeConfig.ProviderName == "openai_compatible")

	currentModel := strings.TrimSpace(runtimeConfig.ModelName)
	if currentModel == "" {
		currentModel = "unknown"
	}
	providerName := strings.TrimSpace(runtimeConfig.ProviderName)
	if providerName == "" {
		providerName = "unknown"
	}

	server.writeJSON(writer, http.StatusOK, HavenModelSettingsResponse{
		CurrentModel:       currentModel,
		ProviderName:       providerName,
		BaseURL:            runtimeConfig.BaseURL,
		AvailableModels:    models,
		Mode:               mode,
		HasCloudCredential: hasCloud,
		LocalBaseURL:       localBase,
	})
}

func (server *Server) handleHavenModelSettingsPost(writer http.ResponseWriter, request *http.Request) {
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireTrustedHavenSession(writer, tokenClaims, "model settings require trusted Haven session") {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityModelSettingsWrite) {
		return
	}

	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxHavenModelSettingsBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var req havenModelSettingsPostRequest
	if err := decodeJSONBytes(requestBodyBytes, &req); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	modelName := strings.TrimSpace(req.ModelName)
	if modelName == "" {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: "model_name is required",
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	configPath := modelruntime.ConfigPath(server.repoRoot)
	existing, err := modelruntime.LoadPersistedConfig(configPath)
	if err != nil {
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: havenModelSettingsLoadFailureText,
			DenialCode:   DenialCodeExecutionFailed,
			Redacted:     true,
		})
		return
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
			Temperature:       havenPickModelTemperature(existing.Temperature, 0.7),
			MaxOutputTokens:   havenPickMaxOutputTokens(existing.MaxOutputTokens, 4096),
			Timeout:           havenPickModelTimeout(existing.Timeout, 120*time.Second),
		}
	case "openai_compatible":
		openAIBaseURL := strings.TrimSpace(req.LocalBaseURL)
		openAIAPIKey := strings.TrimSpace(req.OpenAIAPIKey)
		if openAIBaseURL == "" {
			server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
				Status:       ResponseStatusDenied,
				DenialReason: "local_base_url is required for openai_compatible mode",
				DenialCode:   DenialCodeMalformedRequest,
			})
			return
		}
		next = modelruntime.Config{
			ProviderName:    "openai_compatible",
			ModelName:       modelName,
			BaseURL:         openAIBaseURL,
			Temperature:     havenPickModelTemperature(existing.Temperature, 0.7),
			MaxOutputTokens: havenPickMaxOutputTokens(existing.MaxOutputTokens, 4096),
			Timeout:         havenPickModelTimeout(existing.Timeout, 120*time.Second),
		}
		if modelruntime.IsLoopbackModelBaseURL(openAIBaseURL) {
			next.ModelConnectionID = ""
			next.APIKeyEnvVar = ""
		} else if openAIAPIKey != "" {
			if !server.requireControlCapability(writer, tokenClaims, controlCapabilityConnectionWrite) {
				return
			}
			next.ModelConnectionID = fmt.Sprintf("openai_compatible:%d", time.Now().UTC().UnixNano())
			if _, err := server.StoreModelConnection(request.Context(), ModelConnectionStoreRequest{
				ConnectionID: next.ModelConnectionID,
				ProviderName: "openai_compatible",
				BaseURL:      next.BaseURL,
				SecretValue:  openAIAPIKey,
			}); err != nil {
				server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
					Status:       ResponseStatusError,
					DenialReason: secrets.RedactText(err.Error()),
					DenialCode:   DenialCodeExecutionFailed,
					Redacted:     true,
				})
				return
			}
		} else if strings.TrimSpace(existing.ModelConnectionID) != "" && existing.ProviderName == "openai_compatible" && strings.EqualFold(strings.TrimSpace(existing.BaseURL), openAIBaseURL) {
			next.ModelConnectionID = existing.ModelConnectionID
		} else {
			server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
				Status:       ResponseStatusDenied,
				DenialReason: "openai-compatible API key is required for a non-loopback endpoint (paste once; Loopgate stores it securely)",
				DenialCode:   DenialCodeMalformedRequest,
			})
			return
		}
	case "anthropic":
		apiKey := strings.TrimSpace(req.AnthropicAPIKey)
		next = modelruntime.Config{
			ProviderName:    "anthropic",
			ModelName:       modelName,
			BaseURL:         havenPickAnthropicBaseURL(existing),
			Temperature:     havenPickModelTemperature(existing.Temperature, 0.7),
			MaxOutputTokens: havenPickMaxOutputTokens(existing.MaxOutputTokens, 4096),
			Timeout:         havenPickModelTimeout(existing.Timeout, 120*time.Second),
		}
		if apiKey != "" {
			if !server.requireControlCapability(writer, tokenClaims, controlCapabilityConnectionWrite) {
				return
			}
			next.ModelConnectionID = fmt.Sprintf("anthropic:%d", time.Now().UTC().UnixNano())
			if _, err := server.StoreModelConnection(request.Context(), ModelConnectionStoreRequest{
				ConnectionID: next.ModelConnectionID,
				ProviderName: "anthropic",
				BaseURL:      next.BaseURL,
				SecretValue:  apiKey,
			}); err != nil {
				server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
					Status:       ResponseStatusError,
					DenialReason: secrets.RedactText(err.Error()),
					DenialCode:   DenialCodeExecutionFailed,
					Redacted:     true,
				})
				return
			}
		} else if strings.TrimSpace(existing.ModelConnectionID) != "" && existing.ProviderName == "anthropic" {
			next.ModelConnectionID = existing.ModelConnectionID
			if strings.TrimSpace(next.BaseURL) == "" {
				next.BaseURL = existing.BaseURL
			}
		} else {
			server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
				Status:       ResponseStatusDenied,
				DenialReason: "anthropic API key is required (paste once; Loopgate stores it securely)",
				DenialCode:   DenialCodeMalformedRequest,
			})
			return
		}
	default:
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusDenied,
			DenialReason: fmt.Sprintf("unsupported mode %q (use local, openai_compatible, or anthropic)", mode),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	normalizedConfig, err := modelruntime.NormalizeConfig(next)
	if err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeExecutionFailed,
		})
		return
	}
	validatedConfig, err := server.validateModelConfig(request.Context(), normalizedConfig)
	if err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: havenModelSettingsValidationFailureText,
			DenialCode:   DenialCodeExecutionFailed,
			Redacted:     true,
		})
		return
	}

	if err := modelruntime.SavePersistedConfig(configPath, validatedConfig); err != nil {
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: havenModelSettingsUpdateFailureText,
			DenialCode:   DenialCodeExecutionFailed,
			Redacted:     true,
		})
		return
	}

	if auditErr := server.logEvent("haven.model_settings", tokenClaims.ControlSessionID, map[string]interface{}{
		"mode":               mode,
		"provider":           validatedConfig.ProviderName,
		"model":              validatedConfig.ModelName,
		"control_session_id": tokenClaims.ControlSessionID,
	}); auditErr != nil {
		server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "control-plane audit is unavailable",
			DenialCode:   DenialCodeAuditUnavailable,
		})
		return
	}

	// Return refreshed view (same shape as GET).
	ollamaRoot := havenOllamaAPIRootFromModelBase(havenPickLocalOllamaRootForTags(validatedConfig))
	models := havenNonNilModelTagSlice(fetchOllamaModelTags(request.Context(), ollamaRoot))
	respMode := havenDeriveModelSettingsMode(validatedConfig)
	localBase := validatedConfig.BaseURL
	if localBase == "" || respMode == "anthropic" {
		localBase = "http://localhost:11434/v1"
	}
	hasCloud := strings.TrimSpace(validatedConfig.ModelConnectionID) != "" && (validatedConfig.ProviderName == "anthropic" || validatedConfig.ProviderName == "openai_compatible")

	server.writeJSON(writer, http.StatusOK, HavenModelSettingsResponse{
		CurrentModel:       validatedConfig.ModelName,
		ProviderName:       validatedConfig.ProviderName,
		BaseURL:            validatedConfig.BaseURL,
		AvailableModels:    models,
		Mode:               respMode,
		HasCloudCredential: hasCloud,
		LocalBaseURL:       localBase,
	})
}

// havenNonNilModelTagSlice ensures JSON encodes "available_models" as [] not null.
// Swift's Haven client decodes available_models as a non-optional array; null breaks decoding.
func havenNonNilModelTagSlice(tags []OllamaModelTag) []OllamaModelTag {
	if tags == nil {
		return []OllamaModelTag{}
	}
	return tags
}

func havenDeriveModelSettingsMode(cfg modelruntime.Config) string {
	if cfg.ProviderName == "anthropic" {
		return "anthropic"
	}
	if cfg.ProviderName == "openai_compatible" && !modelruntime.IsLoopbackModelBaseURL(strings.TrimSpace(cfg.BaseURL)) {
		return "openai_compatible"
	}
	return "local"
}

func havenPickAnthropicBaseURL(existing modelruntime.Config) string {
	if strings.TrimSpace(existing.BaseURL) != "" && existing.ProviderName == "anthropic" {
		return strings.TrimSpace(existing.BaseURL)
	}
	return "https://api.anthropic.com/v1"
}

func havenPickLocalOllamaRootForTags(cfg modelruntime.Config) string {
	b := strings.TrimSpace(cfg.BaseURL)
	if b == "" {
		return "http://localhost:11434/v1"
	}
	return havenOllamaAPIRootFromModelBase(b)
}

func havenPickModelTemperature(current float64, def float64) float64 {
	if current > 0 {
		return current
	}
	return def
}

func havenPickMaxOutputTokens(current int, def int) int {
	if current > 0 {
		return current
	}
	return def
}

func havenPickModelTimeout(current time.Duration, def time.Duration) time.Duration {
	if current > 0 {
		return current
	}
	return def
}
