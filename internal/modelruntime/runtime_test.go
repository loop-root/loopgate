package modelruntime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigFromEnv_DefaultsToStub(t *testing.T) {
	runtimeConfig, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}
	if runtimeConfig.ProviderName != "stub" {
		t.Fatalf("expected stub provider by default, got %q", runtimeConfig.ProviderName)
	}
	if runtimeConfig.ModelName != "stub" {
		t.Fatalf("expected stub model name by default, got %q", runtimeConfig.ModelName)
	}
	if runtimeConfig.BaseURL != "" {
		t.Fatalf("expected stub config to omit base url, got %q", runtimeConfig.BaseURL)
	}
}

func TestLoadConfigFromEnv_OpenAICompatible(t *testing.T) {
	t.Setenv("LOOPGATE_MODEL_PROVIDER", "openai_compatible")
	t.Setenv("LOOPGATE_MODEL_NAME", "gpt-4o-mini")
	t.Setenv("LOOPGATE_MODEL_BASE_URL", "https://example.test/v1")
	t.Setenv("LOOPGATE_MODEL_API_KEY_ENV", "EXAMPLE_API_KEY")
	t.Setenv("LOOPGATE_MODEL_TEMPERATURE", "0.2")
	t.Setenv("LOOPGATE_MODEL_MAX_OUTPUT_TOKENS", "2048")
	t.Setenv("LOOPGATE_MODEL_TIMEOUT_SECONDS", "45")

	runtimeConfig, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}
	if runtimeConfig.ProviderName != "openai_compatible" {
		t.Fatalf("unexpected provider: %q", runtimeConfig.ProviderName)
	}
	if runtimeConfig.APIKeyEnvVar != "EXAMPLE_API_KEY" {
		t.Fatalf("unexpected api key env var: %q", runtimeConfig.APIKeyEnvVar)
	}
	if runtimeConfig.MaxOutputTokens != 2048 {
		t.Fatalf("unexpected max output tokens: %d", runtimeConfig.MaxOutputTokens)
	}
	if runtimeConfig.Timeout != 45*time.Second {
		t.Fatalf("unexpected timeout: %v", runtimeConfig.Timeout)
	}
}

func TestLoadConfigFromEnv_Anthropic(t *testing.T) {
	t.Setenv("LOOPGATE_MODEL_PROVIDER", "anthropic")
	t.Setenv("LOOPGATE_MODEL_NAME", "claude-sonnet-4-5")
	t.Setenv("LOOPGATE_MODEL_BASE_URL", "https://api.anthropic.com/v1")
	t.Setenv("LOOPGATE_MODEL_API_KEY_ENV", "ANTHROPIC_API_KEY")
	t.Setenv("LOOPGATE_MODEL_TEMPERATURE", "0.1")
	t.Setenv("LOOPGATE_MODEL_MAX_OUTPUT_TOKENS", "1024")
	t.Setenv("LOOPGATE_MODEL_TIMEOUT_SECONDS", "60")

	runtimeConfig, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}
	if runtimeConfig.ProviderName != "anthropic" {
		t.Fatalf("unexpected provider: %q", runtimeConfig.ProviderName)
	}
	if runtimeConfig.APIKeyEnvVar != "ANTHROPIC_API_KEY" {
		t.Fatalf("unexpected api key env var: %q", runtimeConfig.APIKeyEnvVar)
	}
	if runtimeConfig.BaseURL != "https://api.anthropic.com/v1" {
		t.Fatalf("unexpected base url: %q", runtimeConfig.BaseURL)
	}
}

func TestLoadConfigFromEnv_LoopbackOpenAICompatibleUsesSmallerDefaultOutputBudget(t *testing.T) {
	t.Setenv("LOOPGATE_MODEL_PROVIDER", "openai_compatible")
	t.Setenv("LOOPGATE_MODEL_NAME", "phi4")
	t.Setenv("LOOPGATE_MODEL_BASE_URL", "http://127.0.0.1:11434/v1")
	t.Setenv("LOOPGATE_MODEL_API_KEY_ENV", "OLLAMA_API_KEY")

	runtimeConfig, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}
	if runtimeConfig.MaxOutputTokens != 256 {
		t.Fatalf("expected loopback default max output tokens 256, got %d", runtimeConfig.MaxOutputTokens)
	}
}

func TestLoadConfigFromEnv_FastLocalProfileUsesTighterDefaults(t *testing.T) {
	t.Setenv("LOOPGATE_MODEL_PROVIDER", "openai_compatible")
	t.Setenv("LOOPGATE_MODEL_PROFILE", "fast_local")
	t.Setenv("LOOPGATE_MODEL_NAME", "phi4")
	t.Setenv("LOOPGATE_MODEL_BASE_URL", "http://127.0.0.1:11434/v1")
	t.Setenv("LOOPGATE_MODEL_API_KEY_ENV", "OLLAMA_API_KEY")

	runtimeConfig, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}
	if runtimeConfig.ProfileName != "fast_local" {
		t.Fatalf("expected fast_local profile, got %q", runtimeConfig.ProfileName)
	}
	if runtimeConfig.MaxOutputTokens != 192 {
		t.Fatalf("expected fast_local max output tokens 192, got %d", runtimeConfig.MaxOutputTokens)
	}
	if runtimeConfig.Timeout != 20*time.Second {
		t.Fatalf("expected fast_local timeout 20s, got %v", runtimeConfig.Timeout)
	}
}

func TestLoadConfig_MergesPersistedConfigWithEnvOverrides(t *testing.T) {
	repoRoot := t.TempDir()

	if err := SavePersistedConfig(ConfigPath(repoRoot), Config{
		ProviderName:    "stub",
		ModelName:       "stub",
		MaxOutputTokens: 512,
		Timeout:         12 * time.Second,
	}); err != nil {
		t.Fatalf("save persisted config: %v", err)
	}

	t.Setenv("LOOPGATE_MODEL_PROVIDER", "openai_compatible")
	t.Setenv("LOOPGATE_MODEL_API_KEY_ENV", "OVERRIDE_API_KEY")
	t.Setenv("LOOPGATE_MODEL_BASE_URL", "https://override.test/v1")
	t.Setenv("LOOPGATE_MODEL_MAX_OUTPUT_TOKENS", "2048")

	runtimeConfig, err := LoadConfig(repoRoot)
	if err != nil {
		t.Fatalf("load merged config: %v", err)
	}

	if runtimeConfig.ProviderName != "openai_compatible" {
		t.Fatalf("unexpected provider: %q", runtimeConfig.ProviderName)
	}
	if runtimeConfig.APIKeyEnvVar != "OVERRIDE_API_KEY" {
		t.Fatalf("unexpected api key env var: %q", runtimeConfig.APIKeyEnvVar)
	}
	if runtimeConfig.BaseURL != "https://override.test/v1" {
		t.Fatalf("unexpected base url: %q", runtimeConfig.BaseURL)
	}
	if runtimeConfig.MaxOutputTokens != 2048 {
		t.Fatalf("unexpected max output tokens: %d", runtimeConfig.MaxOutputTokens)
	}
}

func TestLoadConfigFromEnv_IgnoresLegacyMorphEnvNames(t *testing.T) {
	t.Setenv("MORPH_MODEL_PROVIDER", "openai_compatible")
	t.Setenv("MORPH_MODEL_NAME", "gpt-4o-mini")
	t.Setenv("MORPH_MODEL_BASE_URL", "https://example.test/v1")
	t.Setenv("MORPH_MODEL_API_KEY_ENV", "EXAMPLE_API_KEY")

	runtimeConfig, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("load runtime config with legacy morph env: %v", err)
	}
	if runtimeConfig.ProviderName != "stub" {
		t.Fatalf("expected legacy morph env to be ignored, got provider %q", runtimeConfig.ProviderName)
	}
	if runtimeConfig.APIKeyEnvVar != "" {
		t.Fatalf("expected legacy morph api key env to be ignored, got %q", runtimeConfig.APIKeyEnvVar)
	}
}

func TestSavePersistedConfig_DoesNotWriteSecretValues(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("OPENAI_API_KEY", "sk-live-should-not-be-written")

	configPath := ConfigPath(repoRoot)
	if err := SavePersistedConfig(configPath, Config{
		ProviderName:    "openai_compatible",
		ModelName:       "gpt-4o-mini",
		BaseURL:         "https://api.openai.com/v1",
		APIKeyEnvVar:    "OPENAI_API_KEY",
		Temperature:     0.2,
		MaxOutputTokens: 1024,
		Timeout:         30 * time.Second,
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	savedBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if strings.Contains(string(savedBytes), "sk-live-should-not-be-written") {
		t.Fatalf("runtime config leaked raw secret: %s", string(savedBytes))
	}

	fileInfo, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat saved config: %v", err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("expected private permissions on runtime config, got %o", fileInfo.Mode().Perm())
	}

	configDirInfo, err := os.Stat(filepath.Dir(configPath))
	if err != nil {
		t.Fatalf("stat runtime config dir: %v", err)
	}
	if configDirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("expected private permissions on runtime config dir, got %o", configDirInfo.Mode().Perm())
	}
}

func TestValidateConfig_DeniesMissingAPIKeyEnv(t *testing.T) {
	_, err := ValidateConfig(context.Background(), Config{
		ProviderName: "openai_compatible",
		ModelName:    "gpt-4o-mini",
		BaseURL:      "https://api.openai.com/v1",
		APIKeyEnvVar: "MISSING_API_KEY",
		Timeout:      30 * time.Second,
	})
	if err == nil {
		t.Fatal("expected missing api key env var to be denied")
	}
	if !strings.Contains(err.Error(), "MISSING_API_KEY") {
		t.Fatalf("expected missing env var in error, got %v", err)
	}
}

func TestValidateConfig_AllowsModelConnectionWithoutLegacyEnvValidation(t *testing.T) {
	validatedConfig, err := ValidateConfig(context.Background(), Config{
		ProviderName:      "openai_compatible",
		ModelName:         "gpt-4o-mini",
		BaseURL:           "https://api.openai.com/v1",
		ModelConnectionID: "primary",
		APIKeyEnvVar:      "MISSING_API_KEY",
		Timeout:           30 * time.Second,
	})
	if err != nil {
		t.Fatalf("expected model connection config to validate, got %v", err)
	}
	if validatedConfig.ModelConnectionID != "primary" {
		t.Fatalf("unexpected model connection id: %q", validatedConfig.ModelConnectionID)
	}
	if validatedConfig.APIKeyEnvVar != "" {
		t.Fatalf("expected legacy env var to be cleared when model connection is set, got %q", validatedConfig.APIKeyEnvVar)
	}
}

func TestValidateConfig_AllowsAnthropicModelConnectionWithoutLegacyEnvValidation(t *testing.T) {
	validatedConfig, err := ValidateConfig(context.Background(), Config{
		ProviderName:      "anthropic",
		ModelName:         "claude-sonnet-4-5",
		BaseURL:           "https://api.anthropic.com/v1",
		ModelConnectionID: "primary",
		APIKeyEnvVar:      "MISSING_API_KEY",
		Timeout:           30 * time.Second,
	})
	if err != nil {
		t.Fatalf("expected anthropic model connection config to validate, got %v", err)
	}
	if validatedConfig.ModelConnectionID != "primary" {
		t.Fatalf("unexpected model connection id: %q", validatedConfig.ModelConnectionID)
	}
	if validatedConfig.APIKeyEnvVar != "" {
		t.Fatalf("expected legacy env var to be cleared when model connection is set, got %q", validatedConfig.APIKeyEnvVar)
	}
}

func TestNormalizeConfig_DeniesAnthropicWithoutCredentialSource(t *testing.T) {
	_, err := NormalizeConfig(Config{
		ProviderName: "anthropic",
		ModelName:    "claude-sonnet-4-5",
		BaseURL:      "https://api.anthropic.com/v1",
	})
	if err == nil {
		t.Fatal("expected anthropic config without credentials to be denied")
	}
}

func TestNormalizeConfig_DeniesFastLocalProfileForAnthropic(t *testing.T) {
	_, err := NormalizeConfig(Config{
		ProviderName: "anthropic",
		ProfileName:  "fast_local",
		ModelName:    "claude-sonnet-4-5",
		BaseURL:      "https://api.anthropic.com/v1",
		APIKeyEnvVar: "ANTHROPIC_API_KEY",
	})
	if err == nil {
		t.Fatal("expected fast_local profile on anthropic to be denied")
	}
}

func TestValidateConfig_AllowsLoopbackWithoutLegacyEnvValidation(t *testing.T) {
	validatedConfig, err := ValidateConfig(context.Background(), Config{
		ProviderName: "openai_compatible",
		ProfileName:  "fast_local",
		ModelName:    "qwen2.5:3b",
		BaseURL:      "http://127.0.0.1:11434/v1",
		Timeout:      20 * time.Second,
	})
	if err != nil {
		t.Fatalf("expected loopback config to validate, got %v", err)
	}
	if validatedConfig.ModelConnectionID != "" {
		t.Fatalf("expected no model connection id for loopback config, got %q", validatedConfig.ModelConnectionID)
	}
	if validatedConfig.APIKeyEnvVar != "" {
		t.Fatalf("expected no legacy env var for loopback config, got %q", validatedConfig.APIKeyEnvVar)
	}
}

func TestNormalizeConfig_DeniesNonLocalHTTPBaseURL(t *testing.T) {
	_, err := NormalizeConfig(Config{
		ProviderName: "openai_compatible",
		ModelName:    "gpt-4o-mini",
		BaseURL:      "http://example.com/v1",
		APIKeyEnvVar: "OPENAI_API_KEY",
	})
	if err == nil {
		t.Fatal("expected insecure remote http base url to be denied")
	}
}

func TestNormalizeConfig_DeniesFastLocalProfileForRemoteBaseURL(t *testing.T) {
	_, err := NormalizeConfig(Config{
		ProviderName: "openai_compatible",
		ProfileName:  "fast_local",
		ModelName:    "gpt-4o-mini",
		BaseURL:      "https://api.openai.com/v1",
		APIKeyEnvVar: "OPENAI_API_KEY",
	})
	if err == nil {
		t.Fatal("expected fast_local profile on remote base url to be denied")
	}
}

func TestNormalizeConfig_DeniesTraversalLikeProviderName(t *testing.T) {
	_, err := NormalizeConfig(Config{
		ProviderName: "../../stub",
	})
	if err == nil {
		t.Fatal("expected traversal-like provider name to be denied")
	}
}

func TestLoadPersistedConfig_ReadsSavedFile(t *testing.T) {
	repoRoot := t.TempDir()
	configPath := filepath.Join(repoRoot, "runtime", "state", "model_runtime.json")

	if err := SavePersistedConfig(configPath, Config{
		ProviderName:    "openai_compatible",
		ProfileName:     "fast_local",
		ModelName:       "gpt-4o-mini",
		BaseURL:         "http://127.0.0.1:11434/v1",
		APIKeyEnvVar:    "OPENAI_API_KEY",
		Temperature:     0.1,
		MaxOutputTokens: 4096,
		Timeout:         45 * time.Second,
	}); err != nil {
		t.Fatalf("save persisted config: %v", err)
	}

	loadedConfig, err := LoadPersistedConfig(configPath)
	if err != nil {
		t.Fatalf("load persisted config: %v", err)
	}
	if loadedConfig.ModelName != "gpt-4o-mini" {
		t.Fatalf("unexpected model name: %q", loadedConfig.ModelName)
	}
	if loadedConfig.ProfileName != "fast_local" {
		t.Fatalf("unexpected profile name: %q", loadedConfig.ProfileName)
	}
	if loadedConfig.Timeout != 45*time.Second {
		t.Fatalf("unexpected timeout: %v", loadedConfig.Timeout)
	}
}
