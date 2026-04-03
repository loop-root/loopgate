package setup

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"morph/internal/loopgate"
	modelruntime "morph/internal/modelruntime"
)

type scriptedPrompter struct {
	answers []string
	index   int
}

func (prompter *scriptedPrompter) Ask(promptLabel string, defaultValue string) (string, error) {
	if prompter.index >= len(prompter.answers) {
		return defaultValue, nil
	}
	answer := prompter.answers[prompter.index]
	prompter.index++
	if answer == "" {
		return defaultValue, nil
	}
	return answer, nil
}

func (prompter *scriptedPrompter) AskSecret(promptLabel string) (string, error) {
	return prompter.Ask(promptLabel, "")
}

func TestRunModelWizard_SavesValidatedOpenAIConfig(t *testing.T) {
	repoRoot := t.TempDir()

	result, err := RunModelWizard(context.Background(), repoRoot, modelruntime.Config{}, func(_ context.Context, runtimeConfig modelruntime.Config) (modelruntime.Config, error) {
		return modelruntime.NormalizeConfig(runtimeConfig)
	}, func(_ context.Context, request loopgate.ModelConnectionStoreRequest) (loopgate.ModelConnectionStatus, error) {
		if strings.TrimSpace(request.SecretValue) == "" {
			t.Fatal("expected secret value to be forwarded to Loopgate")
		}
		return loopgate.ModelConnectionStatus{ConnectionID: request.ConnectionID, SecureStoreRefID: "model-" + request.ConnectionID}, nil
	}, func(context.Context, string) ([]string, error) {
		t.Fatal("did not expect loopback probe for remote config")
		return nil, nil
	}, &scriptedPrompter{
		answers: []string{
			"openai_compatible",
			"remote",
			"https://api.openai.com/v1",
			"gpt-4o-mini",
			"default",
			"0.10",
			"2048",
			"45",
			"primary",
			"sk-runtime-only",
			"yes",
		},
	})
	if err != nil {
		t.Fatalf("run setup wizard: %v", err)
	}

	if result.RuntimeConfig.ProviderName != "openai_compatible" {
		t.Fatalf("unexpected provider: %q", result.RuntimeConfig.ProviderName)
	}
	if result.RuntimeConfig.ModelConnectionID != "primary" {
		t.Fatalf("unexpected model connection id: %q", result.RuntimeConfig.ModelConnectionID)
	}
	if result.RuntimeConfig.Timeout != 45*time.Second {
		t.Fatalf("unexpected timeout: %v", result.RuntimeConfig.Timeout)
	}

	savedBytes, err := os.ReadFile(result.SavedPath)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if strings.Contains(string(savedBytes), "sk-runtime-only") {
		t.Fatalf("saved config leaked raw secret: %s", string(savedBytes))
	}
	if !strings.Contains(result.Summary, "Validation authority: Loopgate") {
		t.Fatalf("expected summary to mention loopgate validation, got %q", result.Summary)
	}
}

func TestRunModelWizard_SavesValidatedAnthropicConfig(t *testing.T) {
	repoRoot := t.TempDir()

	result, err := RunModelWizard(context.Background(), repoRoot, modelruntime.Config{}, func(_ context.Context, runtimeConfig modelruntime.Config) (modelruntime.Config, error) {
		return modelruntime.NormalizeConfig(runtimeConfig)
	}, func(_ context.Context, request loopgate.ModelConnectionStoreRequest) (loopgate.ModelConnectionStatus, error) {
		if request.ProviderName != "anthropic" {
			t.Fatalf("unexpected connection provider: %#v", request)
		}
		if strings.TrimSpace(request.SecretValue) == "" {
			t.Fatal("expected secret value to be forwarded to Loopgate")
		}
		return loopgate.ModelConnectionStatus{ConnectionID: request.ConnectionID, SecureStoreRefID: "model-" + request.ConnectionID}, nil
	}, func(context.Context, string) ([]string, error) {
		t.Fatal("did not expect loopback probe for anthropic config")
		return nil, nil
	}, &scriptedPrompter{
		answers: []string{
			"anthropic",
			"https://api.anthropic.com/v1",
			"claude-sonnet-4-5",
			"default",
			"0.00",
			"1024",
			"60",
			"claude-primary",
			"sk-ant-runtime-only",
			"yes",
		},
	})
	if err != nil {
		t.Fatalf("run setup wizard: %v", err)
	}

	if result.RuntimeConfig.ProviderName != "anthropic" {
		t.Fatalf("unexpected provider: %q", result.RuntimeConfig.ProviderName)
	}
	if result.RuntimeConfig.ModelConnectionID != "claude-primary" {
		t.Fatalf("unexpected model connection id: %q", result.RuntimeConfig.ModelConnectionID)
	}
	if result.RuntimeConfig.Timeout != 60*time.Second {
		t.Fatalf("unexpected timeout: %v", result.RuntimeConfig.Timeout)
	}
	if !strings.Contains(result.Summary, "model_secret_storage: loopgate-owned secret ref") {
		t.Fatalf("expected loopgate-owned secret summary, got %q", result.Summary)
	}
}

func TestRunModelWizard_DeniesMissingAPIKeyEnv(t *testing.T) {
	repoRoot := t.TempDir()

	_, err := RunModelWizard(context.Background(), repoRoot, modelruntime.Config{}, func(_ context.Context, runtimeConfig modelruntime.Config) (modelruntime.Config, error) {
		if strings.TrimSpace(runtimeConfig.ModelConnectionID) == "missing" {
			return modelruntime.Config{}, fmt.Errorf("validate model connection: connection %q not found", runtimeConfig.ModelConnectionID)
		}
		return modelruntime.NormalizeConfig(runtimeConfig)
	}, func(_ context.Context, request loopgate.ModelConnectionStoreRequest) (loopgate.ModelConnectionStatus, error) {
		return loopgate.ModelConnectionStatus{ConnectionID: request.ConnectionID, SecureStoreRefID: "model-" + request.ConnectionID}, nil
	}, func(context.Context, string) ([]string, error) {
		t.Fatal("did not expect loopback probe for remote config")
		return nil, nil
	}, &scriptedPrompter{
		answers: []string{
			"openai_compatible",
			"remote",
			"https://api.openai.com/v1",
			"gpt-4o-mini",
			"default",
			"0.00",
			"1024",
			"30",
			"missing",
			"sk-runtime-only",
			"yes",
		},
	})
	if err == nil {
		t.Fatal("expected setup wizard to deny missing model connection")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected missing model connection error, got %v", err)
	}
}

func TestRunModelWizard_AnthropicBlankSecretExplainsMissingConnectionReuse(t *testing.T) {
	repoRoot := t.TempDir()

	_, err := RunModelWizard(context.Background(), repoRoot, modelruntime.Config{}, func(_ context.Context, runtimeConfig modelruntime.Config) (modelruntime.Config, error) {
		if strings.TrimSpace(runtimeConfig.ModelConnectionID) == "primary" {
			return modelruntime.Config{}, fmt.Errorf("model connection %q not found", runtimeConfig.ModelConnectionID)
		}
		return modelruntime.NormalizeConfig(runtimeConfig)
	}, func(_ context.Context, request loopgate.ModelConnectionStoreRequest) (loopgate.ModelConnectionStatus, error) {
		t.Fatalf("did not expect model connection storage when secret is blank: %#v", request)
		return loopgate.ModelConnectionStatus{}, nil
	}, func(context.Context, string) ([]string, error) {
		t.Fatal("did not expect loopback probe for anthropic config")
		return nil, nil
	}, &scriptedPrompter{
		answers: []string{
			"anthropic",
			"https://api.anthropic.com/v1",
			"claude-sonnet-4-5",
			"default",
			"0.00",
			"1024",
			"30",
			"primary",
			"",
			"yes",
		},
	})
	if err == nil {
		t.Fatal("expected missing anthropic model connection reuse to be denied")
	}
	if !strings.Contains(err.Error(), "secret input is hidden and will not echo") {
		t.Fatalf("expected friendly hidden-secret guidance, got %v", err)
	}
}

func TestRunModelWizard_FastLocalProfileUsesProfileDefaults(t *testing.T) {
	repoRoot := t.TempDir()

	result, err := RunModelWizard(context.Background(), repoRoot, modelruntime.Config{}, func(_ context.Context, runtimeConfig modelruntime.Config) (modelruntime.Config, error) {
		return modelruntime.NormalizeConfig(runtimeConfig)
	}, func(_ context.Context, request loopgate.ModelConnectionStoreRequest) (loopgate.ModelConnectionStatus, error) {
		t.Fatalf("did not expect model connection storage for loopback config: %#v", request)
		return loopgate.ModelConnectionStatus{}, nil
	}, func(_ context.Context, baseURL string) ([]string, error) {
		if baseURL != "http://127.0.0.1:11434/v1" {
			t.Fatalf("unexpected loopback probe base url: %q", baseURL)
		}
		return []string{"phi4", "qwen2.5:3b"}, nil
	}, &scriptedPrompter{
		answers: []string{
			"openai_compatible",
			"loopback",
			"http://127.0.0.1:11434/v1",
			"phi4",
			"fast_local",
			"",
			"",
			"",
			"yes",
		},
	})
	if err != nil {
		t.Fatalf("run setup wizard: %v", err)
	}
	if result.RuntimeConfig.ProfileName != "fast_local" {
		t.Fatalf("unexpected profile: %q", result.RuntimeConfig.ProfileName)
	}
	if result.RuntimeConfig.MaxOutputTokens != 192 {
		t.Fatalf("unexpected fast_local max output tokens: %d", result.RuntimeConfig.MaxOutputTokens)
	}
	if result.RuntimeConfig.Timeout != 20*time.Second {
		t.Fatalf("unexpected fast_local timeout: %v", result.RuntimeConfig.Timeout)
	}
}

func TestRunModelWizard_CancelDoesNotWriteFile(t *testing.T) {
	repoRoot := t.TempDir()

	_, err := RunModelWizard(context.Background(), repoRoot, modelruntime.Config{}, func(_ context.Context, runtimeConfig modelruntime.Config) (modelruntime.Config, error) {
		return modelruntime.NormalizeConfig(runtimeConfig)
	}, func(context.Context, loopgate.ModelConnectionStoreRequest) (loopgate.ModelConnectionStatus, error) {
		return loopgate.ModelConnectionStatus{}, nil
	}, func(context.Context, string) ([]string, error) {
		t.Fatal("did not expect loopback probe for stub config")
		return nil, nil
	}, &scriptedPrompter{
		answers: []string{
			"stub",
			"no",
		},
	})
	if err == nil {
		t.Fatal("expected cancellation error")
	}

	if _, statErr := os.Stat(modelruntime.ConfigPath(repoRoot)); !os.IsNotExist(statErr) {
		t.Fatalf("expected no config file after cancellation, got stat err=%v", statErr)
	}
}

func TestRunModelWizard_LoopbackProbeRejectsUnknownModelName(t *testing.T) {
	repoRoot := t.TempDir()

	_, err := RunModelWizard(context.Background(), repoRoot, modelruntime.Config{}, func(_ context.Context, runtimeConfig modelruntime.Config) (modelruntime.Config, error) {
		return modelruntime.NormalizeConfig(runtimeConfig)
	}, func(context.Context, loopgate.ModelConnectionStoreRequest) (loopgate.ModelConnectionStatus, error) {
		t.Fatal("did not expect model connection storage for loopback config")
		return loopgate.ModelConnectionStatus{}, nil
	}, func(_ context.Context, baseURL string) ([]string, error) {
		if baseURL != "http://127.0.0.1:11434/v1" {
			t.Fatalf("unexpected loopback probe base url: %q", baseURL)
		}
		return []string{"qwen2.5:3b"}, nil
	}, &scriptedPrompter{
		answers: []string{
			"openai_compatible",
			"loopback",
			"http://127.0.0.1:11434/v1",
			"phi4",
		},
	})
	if err == nil {
		t.Fatal("expected loopback probe to reject unknown model")
	}
	if !strings.Contains(err.Error(), "available models") {
		t.Fatalf("expected available model list in error, got %v", err)
	}
}
