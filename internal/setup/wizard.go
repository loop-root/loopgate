package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"loopgate/internal/loopgate"
	modelruntime "loopgate/internal/modelruntime"
)

type Prompter interface {
	Ask(promptLabel string, defaultValue string) (string, error)
	AskSecret(promptLabel string) (string, error)
}

// SelectOption represents a choice in an interactive selection menu.
type SelectOption struct {
	Value string
	Label string
	Desc  string
}

// Selector is an optional interface a Prompter can implement to enable
// interactive arrow-key selection menus. When not implemented, the wizard
// falls back to text-based prompts.
type Selector interface {
	Select(title string, options []SelectOption, defaultIdx int) (string, error)
}

type Result struct {
	RuntimeConfig modelruntime.Config
	SavedPath     string
	Summary       string
}

type Validator func(context.Context, modelruntime.Config) (modelruntime.Config, error)
type ConnectionStorer func(context.Context, loopgate.ModelConnectionStoreRequest) (loopgate.ModelConnectionStatus, error)
type LoopbackModelProber func(context.Context, string) ([]string, error)

func RunModelWizard(ctx context.Context, repoRoot string, currentConfig modelruntime.Config, validator Validator, connectionStorer ConnectionStorer, loopbackModelProber LoopbackModelProber, prompter Prompter) (Result, error) {
	if prompter == nil {
		return Result{}, fmt.Errorf("setup wizard requires an interactive prompter")
	}
	if validator == nil {
		return Result{}, fmt.Errorf("setup wizard requires loopgate-backed model validation")
	}
	if connectionStorer == nil {
		return Result{}, fmt.Errorf("setup wizard requires loopgate-backed model connection storage")
	}

	normalizedCurrentConfig, err := modelruntime.NormalizeConfig(currentConfig)
	if err != nil {
		normalizedCurrentConfig = modelruntime.Config{
			ProviderName: "stub",
			ModelName:    "stub",
			Timeout:      30 * time.Second,
		}
	}

	providerName, err := askProviderName(prompter, normalizedCurrentConfig.ProviderName)
	if err != nil {
		return Result{}, err
	}

	configCandidate := normalizedCurrentConfig
	configCandidate.ProviderName = providerName

	switch providerName {
	case "stub":
		configCandidate = modelruntime.Config{
			ProviderName:    "stub",
			ModelName:       "stub",
			Temperature:     0,
			MaxOutputTokens: 1024,
			Timeout:         30 * time.Second,
		}
		confirmed, err := askConfirmation(prompter, "Save this configuration?")
		if err != nil {
			return Result{}, err
		}
		if !confirmed {
			return Result{}, fmt.Errorf("configuration was not saved")
		}

		validatedConfig, err := validator(ctx, configCandidate)
		if err != nil {
			return Result{}, err
		}

		savedPath := modelruntime.ConfigPath(repoRoot)
		if err := modelruntime.SavePersistedConfig(savedPath, validatedConfig); err != nil {
			return Result{}, err
		}

		return Result{
			RuntimeConfig: validatedConfig,
			SavedPath:     savedPath,
			Summary: strings.Join([]string{
				"Model runtime configuration saved.",
				"Validation authority: Loopgate",
				fmt.Sprintf("path: %s", savedPath),
				modelruntime.SummarizeConfig(validatedConfig),
			}, "\n"),
		}, nil
	case "openai_compatible", "anthropic":
		if normalizedCurrentConfig.ProviderName != providerName {
			configCandidate = modelruntime.Config{ProviderName: providerName}
		}
		if providerName == "anthropic" {
			configCandidate.ProviderName = "anthropic"
			baseURL, err := askNonEmpty(prompter, "API base URL > ", defaultAnthropicBaseURL(configCandidate))
			if err != nil {
				return Result{}, err
			}
			configCandidate.BaseURL = baseURL
			modelName, err := askNonEmpty(prompter, "Model name > ", defaultAnthropicModelName(configCandidate))
			if err != nil {
				return Result{}, err
			}
			profileName, err := askProfileName(prompter, defaultAnthropicProfile(configCandidate))
			if err != nil {
				return Result{}, err
			}
			configCandidate.ModelName = modelName
			configCandidate.ProfileName = profileName
			defaultingConfig := configCandidate
			if defaultingConfig.ModelConnectionID == "" && defaultingConfig.APIKeyEnvVar == "" {
				defaultingConfig.ModelConnectionID = "pending"
			}
			defaultedConfig, err := modelruntime.NormalizeConfig(defaultingConfig)
			if err != nil {
				return Result{}, err
			}
			temperatureValue, err := askFloat(prompter, "Temperature (0 = precise, 1 = creative) > ", defaultedConfig.Temperature)
			if err != nil {
				return Result{}, err
			}
			maxOutputTokens, err := askInt(prompter, "Max response length in tokens (typical: 1024–4096) > ", defaultedConfig.MaxOutputTokens)
			if err != nil {
				return Result{}, err
			}
			timeoutSeconds, err := askInt(prompter, "Request timeout in seconds > ", int(defaultedConfig.Timeout/time.Second))
			if err != nil {
				return Result{}, err
			}

			modelConnectionRequest, secretProvided, err := buildModelConnectionRequest(prompter, configCandidate)
			if err != nil {
				return Result{}, err
			}
			configCandidate.ModelConnectionID = modelConnectionRequest.ConnectionID
			configCandidate.APIKeyEnvVar = ""
			configCandidate.Temperature = temperatureValue
			configCandidate.MaxOutputTokens = maxOutputTokens
			configCandidate.Timeout = time.Duration(timeoutSeconds) * time.Second

			validatedShapeConfig, err := modelruntime.NormalizeConfig(configCandidate)
			if err != nil {
				return Result{}, err
			}
			configCandidate = validatedShapeConfig

			confirmationAnswer, err := askNonEmpty(prompter, "Save this configuration? (yes / no) > ", "no")
			if err != nil {
				return Result{}, err
			}
			if strings.ToLower(strings.TrimSpace(confirmationAnswer)) != "yes" {
				return Result{}, fmt.Errorf("configuration was not saved")
			}

			var storedModelConnection *loopgate.ModelConnectionStatus
			if secretProvided {
				modelConnectionStatus, err := connectionStorer(ctx, modelConnectionRequest)
				if err != nil {
					return Result{}, err
				}
				storedModelConnection = &modelConnectionStatus
			}

			validatedConfig, err := validator(ctx, configCandidate)
			if err != nil {
				if !secretProvided && strings.TrimSpace(configCandidate.ModelConnectionID) != "" {
					return Result{}, fmt.Errorf("setup could not reuse model connection %q: %w. Re-run /setup and enter the API key when prompted; secret input is hidden and will not echo", configCandidate.ModelConnectionID, err)
				}
				return Result{}, err
			}

			savedPath := modelruntime.ConfigPath(repoRoot)
			if err := modelruntime.SavePersistedConfig(savedPath, validatedConfig); err != nil {
				return Result{}, err
			}

			summaryLines := []string{
				"Model runtime configuration saved.",
				"Validation authority: Loopgate",
				fmt.Sprintf("path: %s", savedPath),
				modelruntime.SummarizeConfig(validatedConfig),
			}
			if storedModelConnection != nil {
				summaryLines = append(summaryLines,
					fmt.Sprintf("stored model connection: %s", storedModelConnection.ConnectionID),
					fmt.Sprintf("model secret ref: %s", storedModelConnection.SecureStoreRefID),
				)
			}

			return Result{
				RuntimeConfig: validatedConfig,
				SavedPath:     savedPath,
				Summary:       strings.Join(summaryLines, "\n"),
			}, nil
		}
		connectionMode, err := askOpenAICompatibleMode(prompter, defaultOpenAICompatibleMode(configCandidate))
		if err != nil {
			return Result{}, err
		}
		baseURL, err := askNonEmpty(prompter, "API base URL > ", defaultOpenAICompatibleBaseURL(configCandidate, connectionMode))
		if err != nil {
			return Result{}, err
		}
		configCandidate.BaseURL = baseURL
		discoveredModelNames := make([]string, 0)
		if connectionMode == openAICompatibleModeLoopback {
			if loopbackModelProber == nil {
				return Result{}, fmt.Errorf("loopback model probe is unavailable")
			}
			discoveredModelNames, err = loopbackModelProber(ctx, baseURL)
			if err != nil {
				return Result{}, fmt.Errorf("probe local loopback models: %w", err)
			}
		}
		modelName, err := askNonEmpty(prompter, loopbackModelPromptLabel(discoveredModelNames), defaultOpenAICompatibleModelName(configCandidate, discoveredModelNames, connectionMode))
		if err != nil {
			return Result{}, err
		}
		if connectionMode == openAICompatibleModeLoopback && !containsString(discoveredModelNames, modelName) {
			return Result{}, fmt.Errorf("loopback model %q was not found at %s; available models: %s", modelName, baseURL, strings.Join(discoveredModelNames, ", "))
		}
		profileName, err := askProfileName(prompter, defaultOpenAICompatibleProfile(configCandidate, connectionMode))
		if err != nil {
			return Result{}, err
		}
		configCandidate.ModelName = modelName
		configCandidate.ProfileName = profileName
		defaultingConfig := configCandidate
		if !modelruntime.IsLoopbackModelBaseURL(defaultingConfig.BaseURL) && defaultingConfig.ModelConnectionID == "" && defaultingConfig.APIKeyEnvVar == "" {
			defaultingConfig.ModelConnectionID = "pending"
		}
		defaultedConfig, err := modelruntime.NormalizeConfig(defaultingConfig)
		if err != nil {
			return Result{}, err
		}
		temperatureValue, err := askFloat(prompter, "Temperature (0 = precise, 1 = creative) > ", defaultedConfig.Temperature)
		if err != nil {
			return Result{}, err
		}
		maxOutputTokens, err := askInt(prompter, "Max response length in tokens (typical: 1024–4096) > ", defaultedConfig.MaxOutputTokens)
		if err != nil {
			return Result{}, err
		}
		timeoutSeconds, err := askInt(prompter, "Request timeout in seconds > ", int(defaultedConfig.Timeout/time.Second))
		if err != nil {
			return Result{}, err
		}

		modelConnectionRequest, secretProvided, err := buildModelConnectionRequest(prompter, configCandidate)
		if err != nil {
			return Result{}, err
		}
		configCandidate.ModelConnectionID = modelConnectionRequest.ConnectionID
		configCandidate.APIKeyEnvVar = ""
		configCandidate.Temperature = temperatureValue
		configCandidate.MaxOutputTokens = maxOutputTokens
		configCandidate.Timeout = time.Duration(timeoutSeconds) * time.Second

		validatedShapeConfig, err := modelruntime.NormalizeConfig(configCandidate)
		if err != nil {
			return Result{}, err
		}
		configCandidate = validatedShapeConfig

		confirmed, err := askConfirmation(prompter, "Save this configuration?")
		if err != nil {
			return Result{}, err
		}
		if !confirmed {
			return Result{}, fmt.Errorf("configuration was not saved")
		}

		var storedModelConnection *loopgate.ModelConnectionStatus
		if secretProvided {
			modelConnectionStatus, err := connectionStorer(ctx, modelConnectionRequest)
			if err != nil {
				return Result{}, err
			}
			storedModelConnection = &modelConnectionStatus
		}

		validatedConfig, err := validator(ctx, configCandidate)
		if err != nil {
			if !secretProvided && strings.TrimSpace(configCandidate.ModelConnectionID) != "" {
				return Result{}, fmt.Errorf("setup could not reuse model connection %q: %w. Re-run /setup and enter the API key when prompted; secret input is hidden and will not echo", configCandidate.ModelConnectionID, err)
			}
			return Result{}, err
		}

		savedPath := modelruntime.ConfigPath(repoRoot)
		if err := modelruntime.SavePersistedConfig(savedPath, validatedConfig); err != nil {
			return Result{}, err
		}

		summaryLines := []string{
			"Model runtime configuration saved.",
			"Validation authority: Loopgate",
			fmt.Sprintf("path: %s", savedPath),
			modelruntime.SummarizeConfig(validatedConfig),
		}
		if len(discoveredModelNames) > 0 {
			summaryLines = append(summaryLines, fmt.Sprintf("discovered_loopback_models: %s", strings.Join(discoveredModelNames, ", ")))
		}
		if storedModelConnection != nil {
			summaryLines = append(summaryLines,
				fmt.Sprintf("stored model connection: %s", storedModelConnection.ConnectionID),
				fmt.Sprintf("model secret ref: %s", storedModelConnection.SecureStoreRefID),
			)
		}

		return Result{
			RuntimeConfig: validatedConfig,
			SavedPath:     savedPath,
			Summary:       strings.Join(summaryLines, "\n"),
		}, nil
	default:
		return Result{}, fmt.Errorf("unsupported provider %q", providerName)
	}
}

const (
	openAICompatibleModeLoopback = "loopback"
	openAICompatibleModeRemote   = "remote"
)

type openAICompatibleModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

func ProbeOpenAICompatibleModels(ctx context.Context, baseURL string) ([]string, error) {
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("build loopback probe request: %w", err)
	}

	httpClient := &http.Client{Timeout: 5 * time.Second}
	httpResponse, err := httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("probe loopback models: %w", err)
	}
	defer httpResponse.Body.Close()

	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		responseBytes, _ := io.ReadAll(io.LimitReader(httpResponse.Body, 64*1024))
		return nil, fmt.Errorf("probe returned status %d: %s", httpResponse.StatusCode, strings.TrimSpace(string(responseBytes)))
	}

	responseBytes, err := io.ReadAll(io.LimitReader(httpResponse.Body, 256*1024))
	if err != nil {
		return nil, fmt.Errorf("read loopback probe response: %w", err)
	}

	var decodedResponse openAICompatibleModelsResponse
	if err := json.Unmarshal(responseBytes, &decodedResponse); err != nil {
		return nil, fmt.Errorf("decode loopback model list: %w", err)
	}
	if len(decodedResponse.Data) == 0 {
		return nil, fmt.Errorf("loopback model list was empty")
	}

	modelNames := make([]string, 0, len(decodedResponse.Data))
	for _, modelRecord := range decodedResponse.Data {
		trimmedModelID := strings.TrimSpace(modelRecord.ID)
		if trimmedModelID == "" {
			continue
		}
		modelNames = append(modelNames, trimmedModelID)
	}
	if len(modelNames) == 0 {
		return nil, fmt.Errorf("loopback model list did not contain any valid model IDs")
	}
	sort.Strings(modelNames)
	return modelNames, nil
}

func buildModelConnectionRequest(prompter Prompter, runtimeConfig modelruntime.Config) (loopgate.ModelConnectionStoreRequest, bool, error) {
	if modelruntime.IsLoopbackModelBaseURL(runtimeConfig.BaseURL) {
		return loopgate.ModelConnectionStoreRequest{}, false, nil
	}
	connectionID, err := askNonEmpty(prompter, "Connection label (a name for this credential, e.g. 'primary') > ", defaultString(runtimeConfig.ModelConnectionID, "primary"))
	if err != nil {
		return loopgate.ModelConnectionStoreRequest{}, false, err
	}
	fmt.Println("\n  Your API key is stored securely via Loopgate — never saved as plain text.")
	secretValue, err := prompter.AskSecret("API key (hidden input; leave blank to keep existing) > ")
	if err != nil {
		return loopgate.ModelConnectionStoreRequest{}, false, err
	}
	modelConnectionRequest := loopgate.ModelConnectionStoreRequest{
		ConnectionID: connectionID,
		ProviderName: runtimeConfig.ProviderName,
		BaseURL:      runtimeConfig.BaseURL,
		SecretValue:  secretValue,
	}
	if strings.TrimSpace(secretValue) == "" {
		return modelConnectionRequest, false, nil
	}
	return modelConnectionRequest, true, nil
}

func askProfileName(prompter Prompter, defaultValue string) (string, error) {
	if selector, ok := prompter.(Selector); ok {
		options := []SelectOption{
			{Value: "default", Label: "Default", Desc: "balanced settings for cloud models"},
			{Value: "fast_local", Label: "Fast Local", Desc: "optimized for local models (lower tokens, shorter timeout)"},
		}
		defaultIdx := 0
		if defaultValue == "fast_local" {
			defaultIdx = 1
		}
		answer, err := selector.Select("Response profile:", options, defaultIdx)
		if err != nil {
			return "", err
		}
		if answer == "default" {
			return "", nil
		}
		return answer, nil
	}

	profileAnswer, err := askNonEmpty(prompter, "Response profile? (default / fast_local) > ", defaultString(defaultValue, "default"))
	if err != nil {
		return "", err
	}
	switch profileAnswer {
	case "default":
		return "", nil
	case "fast_local":
		return profileAnswer, nil
	default:
		return "", fmt.Errorf("unsupported model profile %q", profileAnswer)
	}
}

func askOpenAICompatibleMode(prompter Prompter, defaultValue string) (string, error) {
	if selector, ok := prompter.(Selector); ok {
		options := []SelectOption{
			{Value: openAICompatibleModeLoopback, Label: "Local", Desc: "local models like Ollama, LM Studio"},
			{Value: openAICompatibleModeRemote, Label: "Remote", Desc: "cloud APIs like OpenAI, Together, etc."},
		}
		defaultIdx := 0
		if defaultValue == openAICompatibleModeRemote {
			defaultIdx = 1
		}
		return selector.Select("Connection mode:", options, defaultIdx)
	}

	modeAnswer, err := askNonEmpty(prompter, "Connection mode? (loopback for local models like Ollama, remote for cloud APIs) > ", defaultString(defaultValue, openAICompatibleModeLoopback))
	if err != nil {
		return "", err
	}
	switch modeAnswer {
	case openAICompatibleModeLoopback, openAICompatibleModeRemote:
		return modeAnswer, nil
	default:
		return "", fmt.Errorf("unsupported openai_compatible connection mode %q", modeAnswer)
	}
}

func askProviderName(prompter Prompter, defaultValue string) (string, error) {
	if selector, ok := prompter.(Selector); ok {
		defaultIdx := 0
		options := []SelectOption{
			{Value: "anthropic", Label: "Anthropic", Desc: "Claude models via the Anthropic API"},
			{Value: "openai_compatible", Label: "Local", Desc: "Ollama, LM Studio, OpenAI, and compatible APIs"},
			{Value: "stub", Label: "Stub", Desc: "offline test mode (no real model)"},
		}
		for i, opt := range options {
			if opt.Value == defaultValue {
				defaultIdx = i
				break
			}
		}
		return selector.Select("Select your AI provider:", options, defaultIdx)
	}

	// Fallback for non-interactive prompters (tests).
	fmt.Println("\nMorph works with:")
	fmt.Println("  anthropic — Claude models via the Anthropic API")
	fmt.Println("  local     — Ollama, LM Studio, OpenAI, and compatible APIs")
	fmt.Println("  stub      — offline test mode (no real model)")
	fmt.Println()
	providerName, err := askNonEmpty(prompter, "Which AI provider? (anthropic / local / stub) > ", defaultString(defaultValue, "stub"))
	if err != nil {
		return "", err
	}
	if providerName == "local" {
		providerName = "openai_compatible"
	}
	switch providerName {
	case "stub", "openai_compatible", "anthropic":
		return providerName, nil
	default:
		return "", fmt.Errorf("unsupported provider %q", providerName)
	}
}

func askNonEmpty(prompter Prompter, promptLabel string, defaultValue string) (string, error) {
	answer, err := prompter.Ask(promptLabel, defaultValue)
	if err != nil {
		return "", err
	}
	trimmedAnswer := strings.TrimSpace(answer)
	if trimmedAnswer == "" {
		trimmedAnswer = strings.TrimSpace(defaultValue)
	}
	if trimmedAnswer == "" {
		return "", fmt.Errorf("missing required value for %s", strings.TrimSpace(promptLabel))
	}
	return trimmedAnswer, nil
}

func askFloat(prompter Prompter, promptLabel string, defaultValue float64) (float64, error) {
	answer, err := askNonEmpty(prompter, promptLabel, fmt.Sprintf("%.2f", defaultValue))
	if err != nil {
		return 0, err
	}
	parsedValue, err := strconv.ParseFloat(answer, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid numeric value for %s: %w", strings.TrimSpace(promptLabel), err)
	}
	return parsedValue, nil
}

func askInt(prompter Prompter, promptLabel string, defaultValue int) (int, error) {
	answer, err := askNonEmpty(prompter, promptLabel, strconv.Itoa(defaultValue))
	if err != nil {
		return 0, err
	}
	parsedValue, err := strconv.Atoi(answer)
	if err != nil {
		return 0, fmt.Errorf("invalid integer value for %s: %w", strings.TrimSpace(promptLabel), err)
	}
	return parsedValue, nil
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func defaultOpenAICompatibleMode(runtimeConfig modelruntime.Config) string {
	if modelruntime.IsLoopbackModelBaseURL(runtimeConfig.BaseURL) {
		return openAICompatibleModeLoopback
	}
	if strings.TrimSpace(runtimeConfig.BaseURL) != "" {
		return openAICompatibleModeRemote
	}
	return openAICompatibleModeLoopback
}

func defaultOpenAICompatibleBaseURL(runtimeConfig modelruntime.Config, connectionMode string) string {
	trimmedBaseURL := strings.TrimSpace(runtimeConfig.BaseURL)
	if trimmedBaseURL != "" {
		if connectionMode == openAICompatibleModeLoopback && modelruntime.IsLoopbackModelBaseURL(trimmedBaseURL) {
			return trimmedBaseURL
		}
		if connectionMode == openAICompatibleModeRemote && !modelruntime.IsLoopbackModelBaseURL(trimmedBaseURL) {
			return trimmedBaseURL
		}
	}
	if connectionMode == openAICompatibleModeLoopback {
		return "http://127.0.0.1:11434/v1"
	}
	return "https://api.openai.com/v1"
}

func defaultOpenAICompatibleProfile(runtimeConfig modelruntime.Config, connectionMode string) string {
	trimmedProfileName := strings.TrimSpace(runtimeConfig.ProfileName)
	if trimmedProfileName != "" {
		if connectionMode == openAICompatibleModeLoopback {
			return trimmedProfileName
		}
		if trimmedProfileName != "fast_local" {
			return trimmedProfileName
		}
	}
	if connectionMode == openAICompatibleModeLoopback {
		return "fast_local"
	}
	return "default"
}

func defaultOpenAICompatibleModelName(runtimeConfig modelruntime.Config, discoveredModelNames []string, connectionMode string) string {
	if trimmedModelName := strings.TrimSpace(runtimeConfig.ModelName); trimmedModelName != "" && trimmedModelName != "stub" {
		return trimmedModelName
	}
	if connectionMode == openAICompatibleModeLoopback && len(discoveredModelNames) > 0 {
		return discoveredModelNames[0]
	}
	return "gpt-4o-mini"
}

func defaultAnthropicBaseURL(runtimeConfig modelruntime.Config) string {
	if trimmedBaseURL := strings.TrimSpace(runtimeConfig.BaseURL); trimmedBaseURL != "" {
		return trimmedBaseURL
	}
	return "https://api.anthropic.com/v1"
}

func defaultAnthropicModelName(runtimeConfig modelruntime.Config) string {
	if trimmedModelName := strings.TrimSpace(runtimeConfig.ModelName); trimmedModelName != "" && trimmedModelName != "stub" {
		return trimmedModelName
	}
	return "claude-sonnet-4-5"
}

func defaultAnthropicProfile(runtimeConfig modelruntime.Config) string {
	if trimmedProfileName := strings.TrimSpace(runtimeConfig.ProfileName); trimmedProfileName != "" && trimmedProfileName != "fast_local" {
		return trimmedProfileName
	}
	return "default"
}

func loopbackModelPromptLabel(discoveredModelNames []string) string {
	if len(discoveredModelNames) == 0 {
		return "Model name > "
	}
	return fmt.Sprintf("Model name [detected: %s] > ", strings.Join(discoveredModelNames, ", "))
}

func askConfirmation(prompter Prompter, label string) (bool, error) {
	if selector, ok := prompter.(Selector); ok {
		options := []SelectOption{
			{Value: "yes", Label: "Yes", Desc: "save and activate"},
			{Value: "no", Label: "No", Desc: "discard changes"},
		}
		answer, err := selector.Select(label, options, 0)
		if err != nil {
			return false, err
		}
		return answer == "yes", nil
	}
	answer, err := askNonEmpty(prompter, label+" (yes / no) > ", "no")
	if err != nil {
		return false, err
	}
	return strings.ToLower(strings.TrimSpace(answer)) == "yes", nil
}

func containsString(values []string, targetValue string) bool {
	for _, value := range values {
		if value == targetValue {
			return true
		}
	}
	return false
}
