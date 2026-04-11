package loopgate

import (
	"context"
	"strings"
	"testing"
	"time"

	modelruntime "morph/internal/modelruntime"
	"morph/internal/secrets"
)

type fakeModelPolicySecretStore struct{}

func (fakeModelPolicySecretStore) Put(context.Context, secrets.SecretRef, []byte) (secrets.SecretMetadata, error) {
	return secrets.SecretMetadata{}, nil
}

func (fakeModelPolicySecretStore) Get(context.Context, secrets.SecretRef) ([]byte, secrets.SecretMetadata, error) {
	return nil, secrets.SecretMetadata{}, nil
}

func (fakeModelPolicySecretStore) Delete(context.Context, secrets.SecretRef) error {
	return nil
}

func (fakeModelPolicySecretStore) Metadata(context.Context, secrets.SecretRef) (secrets.SecretMetadata, error) {
	return secrets.SecretMetadata{Status: "stored"}, nil
}

func TestValidateModelConfig_DeniesLoopgateRemoteLegacyAPIKeyEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-test")

	testCases := []struct {
		name          string
		runtimeConfig modelruntime.Config
		wantSubstring string
	}{
		{
			name: "openai compatible remote env denied",
			runtimeConfig: modelruntime.Config{
				ProviderName: "openai_compatible",
				ModelName:    "gpt-4o-mini",
				BaseURL:      "https://api.openai.com/v1",
				APIKeyEnvVar: "OPENAI_API_KEY",
				Timeout:      30 * time.Second,
			},
			wantSubstring: "requires model_connection_id",
		},
		{
			name: "anthropic remote env denied",
			runtimeConfig: modelruntime.Config{
				ProviderName: "anthropic",
				ModelName:    "claude-sonnet-4-5",
				BaseURL:      "https://api.anthropic.com/v1",
				APIKeyEnvVar: "ANTHROPIC_API_KEY",
				Timeout:      30 * time.Second,
			},
			wantSubstring: "requires model_connection_id",
		},
	}

	server := &Server{}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := server.validateModelConfig(context.Background(), testCase.runtimeConfig)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), testCase.wantSubstring) {
				t.Fatalf("expected error containing %q, got %v", testCase.wantSubstring, err)
			}
		})
	}
}

func TestValidateModelConfig_AllowsLoopbackOpenAICompatibleWithoutModelConnection(t *testing.T) {
	server := &Server{}
	validatedConfig, err := server.validateModelConfig(context.Background(), modelruntime.Config{
		ProviderName: "openai_compatible",
		ModelName:    "phi4",
		BaseURL:      "http://127.0.0.1:11434/v1",
		Timeout:      30 * time.Second,
	})
	if err != nil {
		t.Fatalf("expected loopback config to validate, got %v", err)
	}
	if validatedConfig.ModelConnectionID != "" {
		t.Fatalf("expected no model connection id, got %q", validatedConfig.ModelConnectionID)
	}
}

func TestValidateModelConfig_DeniesModelConnectionMetadataMismatch(t *testing.T) {
	testCases := []struct {
		name          string
		runtimeConfig modelruntime.Config
		record        modelConnectionRecord
		wantSubstring string
	}{
		{
			name: "provider mismatch denied",
			runtimeConfig: modelruntime.Config{
				ProviderName:      "openai_compatible",
				ModelName:         "gpt-4o-mini",
				BaseURL:           "https://api.openai.com/v1",
				ModelConnectionID: "primary",
				Timeout:           30 * time.Second,
			},
			record: modelConnectionRecord{
				ConnectionID: "primary",
				ProviderName: "anthropic",
				BaseURL:      "https://api.openai.com/v1",
				Credential: secrets.SecretRef{
					ID:          "model-primary",
					Backend:     secrets.BackendSecure,
					AccountName: "model.primary",
					Scope:       "model_inference.primary",
				},
				Status: "stored",
			},
			wantSubstring: "provider mismatch",
		},
		{
			name: "base url mismatch denied",
			runtimeConfig: modelruntime.Config{
				ProviderName:      "openai_compatible",
				ModelName:         "gpt-4o-mini",
				BaseURL:           "https://api.openai.com/v1",
				ModelConnectionID: "primary",
				Timeout:           30 * time.Second,
			},
			record: modelConnectionRecord{
				ConnectionID: "primary",
				ProviderName: "openai_compatible",
				BaseURL:      "https://api.example.test/v1",
				Credential: secrets.SecretRef{
					ID:          "model-primary",
					Backend:     secrets.BackendSecure,
					AccountName: "model.primary",
					Scope:       "model_inference.primary",
				},
				Status: "stored",
			},
			wantSubstring: "base url mismatch",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := &Server{
				modelConnectionPath: t.TempDir() + "/model_connections.json",
				modelConnections: map[string]modelConnectionRecord{
					"primary": testCase.record,
				},
				resolveSecretStore: func(validatedRef secrets.SecretRef) (secrets.SecretStore, error) {
					return fakeModelPolicySecretStore{}, nil
				},
				now: func() time.Time { return time.Unix(0, 0).UTC() },
			}

			_, err := server.validateModelConfig(context.Background(), testCase.runtimeConfig)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), testCase.wantSubstring) {
				t.Fatalf("expected error containing %q, got %v", testCase.wantSubstring, err)
			}
		})
	}
}

func TestNewModelClientFromRuntimeConfig_DeniesLoopgateRemoteLegacyAPIKeyEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")

	server := &Server{}
	_, _, err := server.newModelClientFromRuntimeConfig(modelruntime.Config{
		ProviderName: "openai_compatible",
		ModelName:    "gpt-4o-mini",
		BaseURL:      "https://api.openai.com/v1",
		APIKeyEnvVar: "OPENAI_API_KEY",
		Timeout:      30 * time.Second,
	})
	if err == nil {
		t.Fatal("expected model client initialization to fail")
	}
	if !strings.Contains(err.Error(), "requires model_connection_id") {
		t.Fatalf("expected model_connection_id denial, got %v", err)
	}
}
