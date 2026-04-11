package loopgate

import (
	"context"
	"fmt"
	"strings"

	modelpkg "morph/internal/model"
	anthropicprovider "morph/internal/model/anthropic"
	openai "morph/internal/model/openai"
	modelruntime "morph/internal/modelruntime"
	"morph/internal/secrets"
)

func (server *Server) validateModelConfig(ctx context.Context, runtimeConfig modelruntime.Config) (modelruntime.Config, error) {
	validatedConfig, err := modelruntime.ValidateConfig(ctx, runtimeConfig)
	if err != nil {
		return modelruntime.Config{}, err
	}

	switch validatedConfig.ProviderName {
	case "anthropic":
		if strings.TrimSpace(validatedConfig.ModelConnectionID) == "" {
			if strings.TrimSpace(validatedConfig.APIKeyEnvVar) != "" {
				return modelruntime.Config{}, fmt.Errorf("Loopgate-managed anthropic config requires model_connection_id; legacy api_key_env_var is unsupported")
			}
			return modelruntime.Config{}, fmt.Errorf("Loopgate-managed anthropic config requires model_connection_id")
		}
	case "openai_compatible":
		if strings.TrimSpace(validatedConfig.ModelConnectionID) == "" {
			if modelruntime.IsLoopbackModelBaseURL(validatedConfig.BaseURL) {
				return validatedConfig, nil
			}
			if strings.TrimSpace(validatedConfig.APIKeyEnvVar) != "" {
				return modelruntime.Config{}, fmt.Errorf("Loopgate-managed openai_compatible config requires model_connection_id; legacy api_key_env_var is unsupported for non-localhost base url")
			}
			return modelruntime.Config{}, fmt.Errorf("Loopgate-managed openai_compatible config requires model_connection_id for non-localhost base url")
		}
	default:
		return validatedConfig, nil
	}

	if _, _, err := server.resolveLoopgateManagedModelConnection(ctx, validatedConfig); err != nil {
		return modelruntime.Config{}, err
	}
	return validatedConfig, nil
}

func (server *Server) newModelClientFromRuntimeConfig(runtimeConfig modelruntime.Config) (*modelpkg.Client, modelruntime.Config, error) {
	validatedConfig, err := server.validateModelConfig(context.Background(), runtimeConfig)
	if err != nil {
		return nil, modelruntime.Config{}, err
	}

	switch validatedConfig.ProviderName {
	case "anthropic":
		if strings.TrimSpace(validatedConfig.ModelConnectionID) == "" {
			return modelruntime.NewClientFromConfig(validatedConfig)
		}
		modelConnectionRecord, secretStore, err := server.resolveLoopgateManagedModelConnection(context.Background(), validatedConfig)
		if err != nil {
			return nil, modelruntime.Config{}, err
		}
		provider, err := anthropicprovider.NewProvider(anthropicprovider.Config{
			BaseURL:         validatedConfig.BaseURL,
			ModelName:       validatedConfig.ModelName,
			Temperature:     validatedConfig.Temperature,
			MaxOutputTokens: validatedConfig.MaxOutputTokens,
			Timeout:         validatedConfig.Timeout,
			APIKeyRef:       modelConnectionRecord.Credential,
			SecretStore:     secretStore,
		})
		if err != nil {
			return nil, modelruntime.Config{}, err
		}
		return modelpkg.NewClient(provider), validatedConfig, nil
	case "openai_compatible":
		if strings.TrimSpace(validatedConfig.ModelConnectionID) == "" {
			return modelruntime.NewClientFromConfig(validatedConfig)
		}
		modelConnectionRecord, secretStore, err := server.resolveLoopgateManagedModelConnection(context.Background(), validatedConfig)
		if err != nil {
			return nil, modelruntime.Config{}, err
		}
		provider, err := openai.NewProvider(openai.Config{
			BaseURL:         validatedConfig.BaseURL,
			ModelName:       validatedConfig.ModelName,
			Temperature:     validatedConfig.Temperature,
			MaxOutputTokens: validatedConfig.MaxOutputTokens,
			Timeout:         validatedConfig.Timeout,
			APIKeyRef:       modelConnectionRecord.Credential,
			SecretStore:     secretStore,
		})
		if err != nil {
			return nil, modelruntime.Config{}, err
		}
		return modelpkg.NewClient(provider), validatedConfig, nil
	default:
		return modelruntime.NewClientFromConfig(validatedConfig)
	}
}

func (server *Server) resolveLoopgateManagedModelConnection(ctx context.Context, validatedConfig modelruntime.Config) (modelConnectionRecord, secrets.SecretStore, error) {
	if strings.TrimSpace(validatedConfig.ModelConnectionID) == "" {
		return modelConnectionRecord{}, nil, fmt.Errorf("missing model_connection_id")
	}

	resolvedModelConnection, err := server.resolveModelConnection(validatedConfig.ModelConnectionID)
	if err != nil {
		return modelConnectionRecord{}, nil, err
	}
	if strings.TrimSpace(resolvedModelConnection.ProviderName) != strings.TrimSpace(validatedConfig.ProviderName) {
		return modelConnectionRecord{}, nil, fmt.Errorf(
			"model connection %q provider mismatch: connection uses %q but runtime config requires %q",
			validatedConfig.ModelConnectionID,
			resolvedModelConnection.ProviderName,
			validatedConfig.ProviderName,
		)
	}
	if normalizeModelBaseURLForComparison(resolvedModelConnection.BaseURL) != normalizeModelBaseURLForComparison(validatedConfig.BaseURL) {
		return modelConnectionRecord{}, nil, fmt.Errorf(
			"model connection %q base url mismatch: connection uses %q but runtime config requires %q",
			validatedConfig.ModelConnectionID,
			resolvedModelConnection.BaseURL,
			validatedConfig.BaseURL,
		)
	}

	secretStore, err := server.secretStoreForRef(resolvedModelConnection.Credential)
	if err != nil {
		return modelConnectionRecord{}, nil, err
	}
	if _, err := secretStore.Metadata(ctx, resolvedModelConnection.Credential); err != nil {
		return modelConnectionRecord{}, nil, fmt.Errorf("validate model connection secret ref: %w", err)
	}
	return resolvedModelConnection, secretStore, nil
}

func normalizeModelBaseURLForComparison(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
}
