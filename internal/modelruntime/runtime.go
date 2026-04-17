package modelruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"loopgate/internal/identifiers"
	"loopgate/internal/model"
	anthropicprovider "loopgate/internal/model/anthropic"
	openai "loopgate/internal/model/openai"
	"loopgate/internal/secrets"
)

const runtimeConfigRelativePath = "runtime/state/model_runtime.json"

var envVarNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

const (
	loopgateModelProviderEnv        = "LOOPGATE_MODEL_PROVIDER"
	legacyMorphModelProviderEnv     = "MORPH_MODEL_PROVIDER"
	loopgateModelProfileEnv         = "LOOPGATE_MODEL_PROFILE"
	legacyMorphModelProfileEnv      = "MORPH_MODEL_PROFILE"
	loopgateModelNameEnv            = "LOOPGATE_MODEL_NAME"
	legacyMorphModelNameEnv         = "MORPH_MODEL_NAME"
	loopgateModelBaseURLEnv         = "LOOPGATE_MODEL_BASE_URL"
	legacyMorphModelBaseURLEnv      = "MORPH_MODEL_BASE_URL"
	loopgateModelConnectionIDEnv    = "LOOPGATE_MODEL_CONNECTION_ID"
	legacyMorphModelConnectionIDEnv = "MORPH_MODEL_CONNECTION_ID"
	loopgateModelAPIKeyEnvEnv       = "LOOPGATE_MODEL_API_KEY_ENV"
	legacyMorphModelAPIKeyEnvEnv    = "MORPH_MODEL_API_KEY_ENV"
	loopgateModelTemperatureEnv     = "LOOPGATE_MODEL_TEMPERATURE"
	legacyMorphModelTemperatureEnv  = "MORPH_MODEL_TEMPERATURE"
	loopgateModelMaxTokensEnv       = "LOOPGATE_MODEL_MAX_OUTPUT_TOKENS"
	legacyMorphModelMaxTokensEnv    = "MORPH_MODEL_MAX_OUTPUT_TOKENS"
	loopgateModelTimeoutEnv         = "LOOPGATE_MODEL_TIMEOUT_SECONDS"
	legacyMorphModelTimeoutEnv      = "MORPH_MODEL_TIMEOUT_SECONDS"
)

type Config struct {
	ProviderName      string        `json:"provider_name"`
	ProfileName       string        `json:"profile_name"`
	ModelName         string        `json:"model_name"`
	BaseURL           string        `json:"base_url"`
	ModelConnectionID string        `json:"model_connection_id"`
	APIKeyEnvVar      string        `json:"api_key_env_var"`
	Temperature       float64       `json:"temperature"`
	MaxOutputTokens   int           `json:"max_output_tokens"`
	Timeout           time.Duration `json:"timeout"`
}

type persistedConfigFile struct {
	ProviderName      string  `json:"provider_name"`
	ProfileName       string  `json:"profile_name"`
	ModelName         string  `json:"model_name"`
	BaseURL           string  `json:"base_url"`
	ModelConnectionID string  `json:"model_connection_id"`
	APIKeyEnvVar      string  `json:"api_key_env_var"`
	Temperature       float64 `json:"temperature"`
	MaxOutputTokens   int     `json:"max_output_tokens"`
	TimeoutSeconds    int     `json:"timeout_seconds"`
}

type envOverrides struct {
	ProviderName      *string
	ProfileName       *string
	ModelName         *string
	BaseURL           *string
	ModelConnectionID *string
	APIKeyEnvVar      *string
	Temperature       *float64
	MaxOutputTokens   *int
	Timeout           *time.Duration
}

func ConfigPath(repoRoot string) string {
	return filepath.Join(repoRoot, runtimeConfigRelativePath)
}

func NewClientFromRepo(repoRoot string) (*model.Client, Config, error) {
	runtimeConfig, err := LoadConfig(repoRoot)
	if err != nil {
		return nil, Config{}, err
	}
	return NewClientFromConfig(runtimeConfig)
}

func NewClientFromEnv() (*model.Client, Config, error) {
	runtimeConfig, err := LoadConfigFromEnv()
	if err != nil {
		return nil, Config{}, err
	}
	return NewClientFromConfig(runtimeConfig)
}

func NewClientFromConfig(runtimeConfig Config) (*model.Client, Config, error) {
	validatedConfig, err := ValidateConfig(context.Background(), runtimeConfig)
	if err != nil {
		return nil, Config{}, err
	}

	switch validatedConfig.ProviderName {
	case "stub":
		return model.NewClient(model.NewStubProvider()), validatedConfig, nil
	case "anthropic":
		if validatedConfig.ModelConnectionID != "" {
			return nil, Config{}, fmt.Errorf("anthropic model connection %q must be resolved through Loopgate", validatedConfig.ModelConnectionID)
		}
		secretStore := secrets.NewEnvSecretStore()
		secretRef := secrets.SecretRef{
			ID:          "model-api-key",
			Backend:     secrets.BackendEnv,
			AccountName: validatedConfig.APIKeyEnvVar,
			Scope:       "model_inference",
		}
		provider, err := anthropicprovider.NewProvider(anthropicprovider.Config{
			BaseURL:         validatedConfig.BaseURL,
			ModelName:       validatedConfig.ModelName,
			Temperature:     validatedConfig.Temperature,
			MaxOutputTokens: validatedConfig.MaxOutputTokens,
			Timeout:         validatedConfig.Timeout,
			APIKeyRef:       secretRef,
			SecretStore:     secretStore,
		})
		if err != nil {
			return nil, Config{}, err
		}
		return model.NewClient(provider), validatedConfig, nil
	case "openai_compatible":
		if validatedConfig.ModelConnectionID != "" {
			return nil, Config{}, fmt.Errorf("openai_compatible model connection %q must be resolved through Loopgate", validatedConfig.ModelConnectionID)
		}
		if validatedConfig.APIKeyEnvVar == "" && IsLoopbackModelBaseURL(validatedConfig.BaseURL) {
			provider, err := openai.NewProvider(openai.Config{
				BaseURL:         validatedConfig.BaseURL,
				ModelName:       validatedConfig.ModelName,
				Temperature:     validatedConfig.Temperature,
				MaxOutputTokens: validatedConfig.MaxOutputTokens,
				Timeout:         validatedConfig.Timeout,
				NoAuth:          true,
			})
			if err != nil {
				return nil, Config{}, err
			}
			return model.NewClient(provider), validatedConfig, nil
		}
		secretStore := secrets.NewEnvSecretStore()
		secretRef := secrets.SecretRef{
			ID:          "model-api-key",
			Backend:     secrets.BackendEnv,
			AccountName: validatedConfig.APIKeyEnvVar,
			Scope:       "model_inference",
		}
		provider, err := openai.NewProvider(openai.Config{
			BaseURL:         validatedConfig.BaseURL,
			ModelName:       validatedConfig.ModelName,
			Temperature:     validatedConfig.Temperature,
			MaxOutputTokens: validatedConfig.MaxOutputTokens,
			Timeout:         validatedConfig.Timeout,
			APIKeyRef:       secretRef,
			SecretStore:     secretStore,
		})
		if err != nil {
			return nil, Config{}, err
		}
		return model.NewClient(provider), validatedConfig, nil
	default:
		return nil, Config{}, fmt.Errorf("unsupported model provider %q", validatedConfig.ProviderName)
	}
}

func LoadConfig(repoRoot string) (Config, error) {
	persistedConfig, err := LoadPersistedConfig(ConfigPath(repoRoot))
	if err != nil {
		return Config{}, err
	}

	overrides, err := loadEnvOverrides()
	if err != nil {
		return Config{}, err
	}

	return NormalizeConfig(applyEnvOverrides(persistedConfig, overrides))
}

func LoadConfigFromEnv() (Config, error) {
	overrides, err := loadEnvOverrides()
	if err != nil {
		return Config{}, err
	}
	return NormalizeConfig(applyEnvOverrides(Config{}, overrides))
}

func LoadPersistedConfig(path string) (Config, error) {
	rawConfigBytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("read model runtime config: %w", err)
	}

	var persistedConfig persistedConfigFile
	decoder := json.NewDecoder(bytes.NewReader(rawConfigBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&persistedConfig); err != nil {
		return Config{}, fmt.Errorf("decode model runtime config: %w", err)
	}

	return Config{
		ProviderName:      strings.TrimSpace(persistedConfig.ProviderName),
		ProfileName:       strings.TrimSpace(persistedConfig.ProfileName),
		ModelName:         strings.TrimSpace(persistedConfig.ModelName),
		BaseURL:           strings.TrimSpace(persistedConfig.BaseURL),
		ModelConnectionID: strings.TrimSpace(persistedConfig.ModelConnectionID),
		APIKeyEnvVar:      strings.TrimSpace(persistedConfig.APIKeyEnvVar),
		Temperature:       persistedConfig.Temperature,
		MaxOutputTokens:   persistedConfig.MaxOutputTokens,
		Timeout:           time.Duration(persistedConfig.TimeoutSeconds) * time.Second,
	}, nil
}

func SavePersistedConfig(path string, runtimeConfig Config) error {
	normalizedConfig, err := NormalizeConfig(runtimeConfig)
	if err != nil {
		return err
	}

	fileConfig := persistedConfigFile{
		ProviderName:      normalizedConfig.ProviderName,
		ProfileName:       normalizedConfig.ProfileName,
		ModelName:         normalizedConfig.ModelName,
		BaseURL:           normalizedConfig.BaseURL,
		ModelConnectionID: normalizedConfig.ModelConnectionID,
		APIKeyEnvVar:      normalizedConfig.APIKeyEnvVar,
		Temperature:       normalizedConfig.Temperature,
		MaxOutputTokens:   normalizedConfig.MaxOutputTokens,
		TimeoutSeconds:    int(normalizedConfig.Timeout / time.Second),
	}

	jsonBytes, err := json.MarshalIndent(fileConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal model runtime config: %w", err)
	}
	if len(jsonBytes) == 0 || jsonBytes[len(jsonBytes)-1] != '\n' {
		jsonBytes = append(jsonBytes, '\n')
	}

	configDir := filepath.Dir(path)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return fmt.Errorf("create model runtime config dir: %w", err)
	}

	tempPath := path + ".tmp"
	configFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open temp model runtime config: %w", err)
	}

	defer func() {
		_ = configFile.Close()
	}()

	if _, err := configFile.Write(jsonBytes); err != nil {
		return fmt.Errorf("write temp model runtime config: %w", err)
	}
	if err := configFile.Sync(); err != nil {
		return fmt.Errorf("sync temp model runtime config: %w", err)
	}
	if err := configFile.Close(); err != nil {
		return fmt.Errorf("close temp model runtime config: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("rename model runtime config: %w", err)
	}

	if configDirHandle, err := os.Open(configDir); err == nil {
		_ = configDirHandle.Sync()
		_ = configDirHandle.Close()
	}
	_ = os.Chmod(path, 0o600)

	return nil
}

func ValidateConfig(ctx context.Context, runtimeConfig Config) (Config, error) {
	normalizedConfig, err := NormalizeConfig(runtimeConfig)
	if err != nil {
		return Config{}, err
	}

	if normalizedConfig.ProviderName != "openai_compatible" && normalizedConfig.ProviderName != "anthropic" {
		return normalizedConfig, nil
	}
	if normalizedConfig.ModelConnectionID != "" {
		return normalizedConfig, nil
	}
	if normalizedConfig.ProviderName == "openai_compatible" && IsLoopbackModelBaseURL(normalizedConfig.BaseURL) {
		return normalizedConfig, nil
	}

	secretRef := secrets.SecretRef{
		ID:          "model-api-key",
		Backend:     secrets.BackendEnv,
		AccountName: normalizedConfig.APIKeyEnvVar,
		Scope:       "model_inference",
	}
	if _, err := secrets.NewEnvSecretStore().Metadata(ctx, secretRef); err != nil {
		return Config{}, fmt.Errorf("validate model api key reference: %w", err)
	}

	return normalizedConfig, nil
}

func NormalizeConfig(runtimeConfig Config) (Config, error) {
	normalizedConfig := Config{
		ProviderName:      strings.TrimSpace(runtimeConfig.ProviderName),
		ProfileName:       strings.TrimSpace(runtimeConfig.ProfileName),
		ModelName:         strings.TrimSpace(runtimeConfig.ModelName),
		BaseURL:           strings.TrimSpace(runtimeConfig.BaseURL),
		ModelConnectionID: strings.TrimSpace(runtimeConfig.ModelConnectionID),
		APIKeyEnvVar:      strings.TrimSpace(runtimeConfig.APIKeyEnvVar),
		Temperature:       runtimeConfig.Temperature,
		MaxOutputTokens:   runtimeConfig.MaxOutputTokens,
		Timeout:           runtimeConfig.Timeout,
	}

	if normalizedConfig.ProviderName == "" {
		normalizedConfig.ProviderName = "stub"
	}
	if err := identifiers.ValidateSafeIdentifier("model provider", normalizedConfig.ProviderName); err != nil {
		return Config{}, err
	}
	if normalizedConfig.ProfileName != "" && normalizedConfig.ProfileName != "fast_local" {
		return Config{}, fmt.Errorf("unsupported model profile %q", normalizedConfig.ProfileName)
	}
	if normalizedConfig.Temperature < 0 || normalizedConfig.Temperature > 2 {
		return Config{}, fmt.Errorf("invalid model temperature %.2f: expected range 0..2", normalizedConfig.Temperature)
	}

	switch normalizedConfig.ProviderName {
	case "stub":
		if normalizedConfig.ProfileName != "" {
			return Config{}, fmt.Errorf("model profile %q is unsupported for provider %q", normalizedConfig.ProfileName, normalizedConfig.ProviderName)
		}
		if normalizedConfig.ModelName == "" {
			normalizedConfig.ModelName = "stub"
		}
		normalizedConfig.BaseURL = ""
		normalizedConfig.ModelConnectionID = ""
		normalizedConfig.APIKeyEnvVar = ""
		if normalizedConfig.MaxOutputTokens <= 0 {
			normalizedConfig.MaxOutputTokens = 1024
		}
		if normalizedConfig.Timeout <= 0 {
			normalizedConfig.Timeout = 30 * time.Second
		}
	case "openai_compatible":
		if normalizedConfig.ModelName == "" {
			normalizedConfig.ModelName = "gpt-4o-mini"
		}
		if normalizedConfig.BaseURL == "" {
			normalizedConfig.BaseURL = "https://api.openai.com/v1"
		}
		if err := ValidateBaseURL(normalizedConfig.BaseURL); err != nil {
			return Config{}, err
		}
		if normalizedConfig.ModelConnectionID != "" {
			if err := identifiers.ValidateSafeIdentifier("model connection id", normalizedConfig.ModelConnectionID); err != nil {
				return Config{}, err
			}
			normalizedConfig.APIKeyEnvVar = ""
		} else if normalizedConfig.APIKeyEnvVar != "" {
			if !envVarNamePattern.MatchString(normalizedConfig.APIKeyEnvVar) {
				return Config{}, fmt.Errorf("invalid api key env var name %q", normalizedConfig.APIKeyEnvVar)
			}
		} else if !IsLoopbackModelBaseURL(normalizedConfig.BaseURL) {
			return Config{}, fmt.Errorf("openai_compatible provider requires model_connection_id or legacy api_key_env_var for non-localhost base url")
		}
		if normalizedConfig.ProfileName == "fast_local" && !IsLoopbackModelBaseURL(normalizedConfig.BaseURL) {
			return Config{}, fmt.Errorf("model profile %q requires a localhost model base url", normalizedConfig.ProfileName)
		}
		if normalizedConfig.MaxOutputTokens <= 0 {
			switch {
			case normalizedConfig.ProfileName == "fast_local":
				normalizedConfig.MaxOutputTokens = 192
			case IsLoopbackModelBaseURL(normalizedConfig.BaseURL):
				normalizedConfig.MaxOutputTokens = 256
			default:
				normalizedConfig.MaxOutputTokens = 1024
			}
		}
		if normalizedConfig.Timeout <= 0 {
			if normalizedConfig.ProfileName == "fast_local" {
				normalizedConfig.Timeout = 20 * time.Second
			} else {
				normalizedConfig.Timeout = 30 * time.Second
			}
		}
	case "anthropic":
		if normalizedConfig.ProfileName == "fast_local" {
			return Config{}, fmt.Errorf("model profile %q is unsupported for provider %q", normalizedConfig.ProfileName, normalizedConfig.ProviderName)
		}
		if normalizedConfig.ModelName == "" {
			normalizedConfig.ModelName = "claude-sonnet-4-5"
		}
		if normalizedConfig.BaseURL == "" {
			normalizedConfig.BaseURL = "https://api.anthropic.com/v1"
		}
		if err := ValidateBaseURL(normalizedConfig.BaseURL); err != nil {
			return Config{}, err
		}
		if normalizedConfig.ModelConnectionID != "" {
			if err := identifiers.ValidateSafeIdentifier("model connection id", normalizedConfig.ModelConnectionID); err != nil {
				return Config{}, err
			}
			normalizedConfig.APIKeyEnvVar = ""
		} else if normalizedConfig.APIKeyEnvVar != "" {
			if !envVarNamePattern.MatchString(normalizedConfig.APIKeyEnvVar) {
				return Config{}, fmt.Errorf("invalid api key env var name %q", normalizedConfig.APIKeyEnvVar)
			}
		} else {
			return Config{}, fmt.Errorf("anthropic provider requires model_connection_id or legacy api_key_env_var")
		}
		if normalizedConfig.MaxOutputTokens <= 0 {
			normalizedConfig.MaxOutputTokens = 1024
		}
		if normalizedConfig.Timeout <= 0 {
			normalizedConfig.Timeout = 30 * time.Second
		}
	default:
		return Config{}, fmt.Errorf("unsupported model provider %q", normalizedConfig.ProviderName)
	}

	return normalizedConfig, nil
}

func SummarizeConfig(runtimeConfig Config) string {
	normalizedConfig, err := NormalizeConfig(runtimeConfig)
	if err != nil {
		return fmt.Sprintf("invalid model config: %v", err)
	}

	lines := []string{
		fmt.Sprintf("provider: %s", normalizedConfig.ProviderName),
		fmt.Sprintf("profile: %s", summarizeProfileName(normalizedConfig.ProfileName)),
		fmt.Sprintf("model: %s", normalizedConfig.ModelName),
		fmt.Sprintf("temperature: %.2f", normalizedConfig.Temperature),
		fmt.Sprintf("max_output_tokens: %d", normalizedConfig.MaxOutputTokens),
		fmt.Sprintf("timeout_seconds: %d", int(normalizedConfig.Timeout/time.Second)),
	}

	if normalizedConfig.ProviderName == "openai_compatible" || normalizedConfig.ProviderName == "anthropic" {
		modelConnectionID := normalizedConfig.ModelConnectionID
		if modelConnectionID == "" {
			modelConnectionID = "none"
		}
		lines = append(lines,
			fmt.Sprintf("base_url: %s", normalizedConfig.BaseURL),
			fmt.Sprintf("model_connection_id: %s", modelConnectionID),
		)
		if normalizedConfig.ModelConnectionID != "" {
			lines = append(lines, "model_secret_storage: loopgate-owned secret ref")
		} else if normalizedConfig.APIKeyEnvVar != "" {
			lines = append(lines,
				fmt.Sprintf("legacy_api_key_env_var: %s", normalizedConfig.APIKeyEnvVar),
				"model_secret_storage: legacy runtime env reference (compatibility path)",
			)
		} else if normalizedConfig.ProviderName == "openai_compatible" {
			lines = append(lines, "model_secret_storage: none (loopback no-auth model)")
		}
	}

	return strings.Join(lines, "\n")
}

func applyEnvOverrides(baseConfig Config, overrides envOverrides) Config {
	mergedConfig := baseConfig
	if overrides.ProviderName != nil {
		mergedConfig.ProviderName = *overrides.ProviderName
	}
	if overrides.ProfileName != nil {
		mergedConfig.ProfileName = *overrides.ProfileName
	}
	if overrides.ModelName != nil {
		mergedConfig.ModelName = *overrides.ModelName
	}
	if overrides.BaseURL != nil {
		mergedConfig.BaseURL = *overrides.BaseURL
	}
	if overrides.ModelConnectionID != nil {
		mergedConfig.ModelConnectionID = *overrides.ModelConnectionID
	}
	if overrides.APIKeyEnvVar != nil {
		mergedConfig.APIKeyEnvVar = *overrides.APIKeyEnvVar
	}
	if overrides.Temperature != nil {
		mergedConfig.Temperature = *overrides.Temperature
	}
	if overrides.MaxOutputTokens != nil {
		mergedConfig.MaxOutputTokens = *overrides.MaxOutputTokens
	}
	if overrides.Timeout != nil {
		mergedConfig.Timeout = *overrides.Timeout
	}
	return mergedConfig
}

func loadEnvOverrides() (envOverrides, error) {
	overrides := envOverrides{}

	if value, found := lookupNonEmptyEnvAliases(loopgateModelProviderEnv, legacyMorphModelProviderEnv); found {
		overrides.ProviderName = &value
	}
	if value, found := lookupNonEmptyEnvAliases(loopgateModelProfileEnv, legacyMorphModelProfileEnv); found {
		overrides.ProfileName = &value
	}
	if value, found := lookupNonEmptyEnvAliases(loopgateModelNameEnv, legacyMorphModelNameEnv); found {
		overrides.ModelName = &value
	}
	if value, found := lookupNonEmptyEnvAliases(loopgateModelBaseURLEnv, legacyMorphModelBaseURLEnv); found {
		overrides.BaseURL = &value
	}
	if value, found := lookupNonEmptyEnvAliases(loopgateModelConnectionIDEnv, legacyMorphModelConnectionIDEnv); found {
		overrides.ModelConnectionID = &value
	}
	if value, found := lookupNonEmptyEnvAliases(loopgateModelAPIKeyEnvEnv, legacyMorphModelAPIKeyEnvEnv); found {
		overrides.APIKeyEnvVar = &value
	}

	temperatureValue, temperatureSet, err := parseOptionalFloatEnvAliases(loopgateModelTemperatureEnv, loopgateModelTemperatureEnv, legacyMorphModelTemperatureEnv)
	if err != nil {
		return envOverrides{}, err
	}
	if temperatureSet {
		overrides.Temperature = &temperatureValue
	}

	maxOutputTokens, maxOutputTokensSet, err := parseOptionalIntEnvAliases(loopgateModelMaxTokensEnv, loopgateModelMaxTokensEnv, legacyMorphModelMaxTokensEnv)
	if err != nil {
		return envOverrides{}, err
	}
	if maxOutputTokensSet {
		overrides.MaxOutputTokens = &maxOutputTokens
	}

	timeoutSeconds, timeoutSet, err := parseOptionalIntEnvAliases(loopgateModelTimeoutEnv, loopgateModelTimeoutEnv, legacyMorphModelTimeoutEnv)
	if err != nil {
		return envOverrides{}, err
	}
	if timeoutSet {
		timeoutDuration := time.Duration(timeoutSeconds) * time.Second
		overrides.Timeout = &timeoutDuration
	}

	return overrides, nil
}

func lookupNonEmptyEnv(envName string) (string, bool) {
	rawValue, found := os.LookupEnv(envName)
	if !found {
		return "", false
	}
	trimmedValue := strings.TrimSpace(rawValue)
	if trimmedValue == "" {
		return "", false
	}
	return trimmedValue, true
}

func lookupNonEmptyEnvAliases(envNames ...string) (string, bool) {
	for _, envName := range envNames {
		if value, found := lookupNonEmptyEnv(envName); found {
			return value, true
		}
	}
	return "", false
}

func parseOptionalIntEnv(envName string) (int, bool, error) {
	rawValue, found := os.LookupEnv(envName)
	if !found || strings.TrimSpace(rawValue) == "" {
		return 0, false, nil
	}
	parsedValue, err := strconv.Atoi(strings.TrimSpace(rawValue))
	if err != nil {
		return 0, false, fmt.Errorf("parse %s: %w", envName, err)
	}
	return parsedValue, true, nil
}

func parseOptionalIntEnvAliases(displayEnvName string, envNames ...string) (int, bool, error) {
	for _, envName := range envNames {
		if parsedValue, found, err := parseOptionalIntEnvRaw(envName); found || err != nil {
			if err != nil {
				return 0, false, fmt.Errorf("parse %s: %w", displayEnvName, err)
			}
			return parsedValue, true, nil
		}
	}
	return 0, false, nil
}

func parseOptionalFloatEnv(envName string) (float64, bool, error) {
	rawValue, found := os.LookupEnv(envName)
	if !found || strings.TrimSpace(rawValue) == "" {
		return 0, false, nil
	}
	parsedValue, err := strconv.ParseFloat(strings.TrimSpace(rawValue), 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse %s: %w", envName, err)
	}
	return parsedValue, true, nil
}

func parseOptionalFloatEnvAliases(displayEnvName string, envNames ...string) (float64, bool, error) {
	for _, envName := range envNames {
		if parsedValue, found, err := parseOptionalFloatEnvRaw(envName); found || err != nil {
			if err != nil {
				return 0, false, fmt.Errorf("parse %s: %w", displayEnvName, err)
			}
			return parsedValue, true, nil
		}
	}
	return 0, false, nil
}

func parseOptionalIntEnvRaw(envName string) (int, bool, error) {
	rawValue, found := os.LookupEnv(envName)
	if !found || strings.TrimSpace(rawValue) == "" {
		return 0, false, nil
	}
	parsedValue, err := strconv.Atoi(strings.TrimSpace(rawValue))
	if err != nil {
		return 0, false, err
	}
	return parsedValue, true, nil
}

func parseOptionalFloatEnvRaw(envName string) (float64, bool, error) {
	rawValue, found := os.LookupEnv(envName)
	if !found || strings.TrimSpace(rawValue) == "" {
		return 0, false, nil
	}
	parsedValue, err := strconv.ParseFloat(strings.TrimSpace(rawValue), 64)
	if err != nil {
		return 0, false, err
	}
	return parsedValue, true, nil
}

func ValidateBaseURL(baseURL string) error {
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("parse model base url: %w", err)
	}
	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return errors.New("model base url must be absolute")
	}

	switch parsedURL.Scheme {
	case "https":
		return nil
	case "http":
		host := strings.ToLower(parsedURL.Hostname())
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return nil
		}
		return fmt.Errorf("insecure http model base url %q is allowed only for localhost", baseURL)
	default:
		return fmt.Errorf("unsupported model base url scheme %q", parsedURL.Scheme)
	}
}

func IsLoopbackModelBaseURL(baseURL string) bool {
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	if parsedURL.Scheme != "http" {
		return false
	}
	host := strings.ToLower(parsedURL.Hostname())
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func summarizeProfileName(profileName string) string {
	if strings.TrimSpace(profileName) == "" {
		return "default"
	}
	return profileName
}
