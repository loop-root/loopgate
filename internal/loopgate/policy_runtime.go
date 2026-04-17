package loopgate

import (
	"fmt"
	"net/http"
	"time"

	"loopgate/internal/config"
	policypkg "loopgate/internal/policy"
	toolspkg "loopgate/internal/tools"
)

type serverPolicyRuntime struct {
	policy              config.Policy
	policyContentSHA256 string
	checker             *policypkg.Checker
	registry            *toolspkg.Registry
	mcpGatewayManifests map[string]mcpGatewayServerManifest
	httpClient          *http.Client
}

func (server *Server) currentPolicyRuntime() serverPolicyRuntime {
	server.policyRuntimeMu.RLock()
	runtime := server.policyRuntime
	legacyPolicy := server.policy
	legacyPolicyContentSHA256 := server.policyContentSHA256
	legacyChecker := server.checker
	legacyRegistry := server.registry
	legacyMCPGatewayManifests := server.mcpGatewayManifests
	legacyHTTPClient := server.httpClient
	server.policyRuntimeMu.RUnlock()
	if runtime.checker != nil || runtime.registry != nil {
		// Preserve compatibility for older code paths and tests that still mutate
		// legacy Server fields directly while we finish the policy-runtime migration.
		if legacyChecker != nil && runtime.checker != legacyChecker {
			runtime.checker = legacyChecker
		}
		if legacyRegistry != nil && runtime.registry != legacyRegistry {
			runtime.registry = legacyRegistry
		}
		if legacyHTTPClient != nil && runtime.httpClient != legacyHTTPClient {
			runtime.httpClient = legacyHTTPClient
		}
		if len(runtime.mcpGatewayManifests) == 0 && len(legacyMCPGatewayManifests) > 0 {
			runtime.mcpGatewayManifests = legacyMCPGatewayManifests
		}
		if runtime.policy.Version == "" && legacyPolicy.Version != "" {
			runtime.policy = legacyPolicy
		}
		if runtime.policyContentSHA256 == "" && legacyPolicyContentSHA256 != "" {
			runtime.policyContentSHA256 = legacyPolicyContentSHA256
		}
		return runtime
	}
	return serverPolicyRuntime{
		policy:              legacyPolicy,
		policyContentSHA256: legacyPolicyContentSHA256,
		checker:             legacyChecker,
		registry:            legacyRegistry,
		mcpGatewayManifests: legacyMCPGatewayManifests,
		httpClient:          legacyHTTPClient,
	}
}

func (server *Server) storePolicyRuntime(runtime serverPolicyRuntime) {
	server.policyRuntimeMu.Lock()
	server.policyRuntime = runtime
	server.policy = runtime.policy
	server.policyContentSHA256 = runtime.policyContentSHA256
	server.checker = runtime.checker
	server.registry = runtime.registry
	server.mcpGatewayManifests = runtime.mcpGatewayManifests
	server.httpClient = runtime.httpClient
	server.policyRuntimeMu.Unlock()
}

func cloneConfiguredCapabilities(source map[string]configuredCapability) map[string]configuredCapability {
	if len(source) == 0 {
		return map[string]configuredCapability{}
	}
	copied := make(map[string]configuredCapability, len(source))
	for capabilityName, configuredCapabilityDefinition := range source {
		copied[capabilityName] = configuredCapability{
			Name:           configuredCapabilityDefinition.Name,
			Description:    configuredCapabilityDefinition.Description,
			Method:         configuredCapabilityDefinition.Method,
			Path:           configuredCapabilityDefinition.Path,
			ContentClass:   configuredCapabilityDefinition.ContentClass,
			Extractor:      configuredCapabilityDefinition.Extractor,
			ResponseFields: append([]configuredCapabilityField(nil), configuredCapabilityDefinition.ResponseFields...),
			ConnectionKey:  configuredCapabilityDefinition.ConnectionKey,
		}
	}
	return copied
}

func cloneHTTPClientWithTimeout(existingHTTPClient *http.Client, timeout time.Duration) *http.Client {
	if existingHTTPClient == nil {
		return &http.Client{Timeout: timeout}
	}
	clonedHTTPClient := *existingHTTPClient
	clonedHTTPClient.Timeout = timeout
	return &clonedHTTPClient
}

func registerOperatorMountToolsOnRegistry(server *Server, registry *toolspkg.Registry) error {
	tools := []toolspkg.Tool{
		operatorMountFSRead{server: server},
		operatorMountFSList{server: server},
		operatorMountFSWrite{server: server},
		operatorMountFSMkdir{server: server},
	}
	for _, tool := range tools {
		if err := registry.TryRegister(tool); err != nil {
			return fmt.Errorf("register %s: %w", tool.Name(), err)
		}
	}
	return nil
}

func registerConfiguredCapabilitiesOnRegistry(server *Server, registry *toolspkg.Registry, configuredCapabilities map[string]configuredCapability) error {
	for _, capabilityName := range sortedConfiguredCapabilityNames(configuredCapabilities) {
		configuredCapabilityDefinition := configuredCapabilities[capabilityName]
		if err := registry.TryRegister(&configuredCapabilityTool{
			definition: configuredCapabilityDefinition,
			executeFn:  server.executeConfiguredCapability,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (server *Server) buildPolicyRuntime(policyLoadResult config.PolicyLoadResult, configuredCapabilities map[string]configuredCapability) (serverPolicyRuntime, error) {
	mcpGatewayManifests, err := buildMCPGatewayServerManifests(policyLoadResult.Policy)
	if err != nil {
		return serverPolicyRuntime{}, fmt.Errorf("build mcp gateway manifests: %w", err)
	}

	registry, err := toolspkg.NewSandboxRegistry(server.sandboxPaths.Home, policyLoadResult.Policy)
	if err != nil {
		return serverPolicyRuntime{}, fmt.Errorf("create tool registry: %w", err)
	}
	if err := registerOperatorMountToolsOnRegistry(server, registry); err != nil {
		return serverPolicyRuntime{}, fmt.Errorf("register operator mount tools: %w", err)
	}
	if err := registerConfiguredCapabilitiesOnRegistry(server, registry, configuredCapabilities); err != nil {
		return serverPolicyRuntime{}, fmt.Errorf("register configured capabilities: %w", err)
	}

	existingRuntime := server.currentPolicyRuntime()
	httpClient := cloneHTTPClientWithTimeout(existingRuntime.httpClient, time.Duration(policyLoadResult.Policy.Tools.HTTP.TimeoutSeconds)*time.Second)

	return serverPolicyRuntime{
		policy:              policyLoadResult.Policy,
		policyContentSHA256: policyLoadResult.ContentSHA256,
		checker:             policypkg.NewChecker(policyLoadResult.Policy),
		registry:            registry,
		mcpGatewayManifests: mcpGatewayManifests,
		httpClient:          httpClient,
	}, nil
}

func (server *Server) currentConfiguredCapabilitiesSnapshot() map[string]configuredCapability {
	server.providerRuntime.mu.Lock()
	defer server.providerRuntime.mu.Unlock()
	return cloneConfiguredCapabilities(server.providerRuntime.configuredCapabilities)
}

func (server *Server) reloadPolicyRuntimeFromDisk() (serverPolicyRuntime, error) {
	policyLoadResult, err := config.LoadPolicyWithHash(server.repoRoot)
	if err != nil {
		return serverPolicyRuntime{}, fmt.Errorf("load signed policy: %w", err)
	}
	return server.buildPolicyRuntime(policyLoadResult, server.currentConfiguredCapabilitiesSnapshot())
}
