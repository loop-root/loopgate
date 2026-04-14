package loopgate

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"morph/internal/config"
	"morph/internal/identifiers"
	"morph/internal/secrets"
)

var errMCPGatewayServerNotFound = errors.New("mcp gateway server not found")
var errMCPGatewayServerDisabled = errors.New("mcp gateway server disabled")
var errMCPGatewayToolNotFound = errors.New("mcp gateway tool not found")
var errMCPGatewayToolDisabled = errors.New("mcp gateway tool disabled")

type mcpGatewayServerManifest struct {
	ServerID           string
	Enabled            bool
	RequiresApproval   bool
	Transport          string
	LaunchCommand      string
	LaunchArgs         []string
	WorkingDirectory   string
	AllowedEnvironment []string
	SecretEnvironment  map[string]secrets.SecretRef
	ToolManifests      map[string]mcpGatewayToolManifest
}

type mcpGatewayToolManifest struct {
	ToolName           string
	Enabled            bool
	RequiresApproval   bool
	RequiredArguments  []string
	AllowedArguments   []string
	DeniedArguments    []string
	ArgumentValueKinds map[string]string
	DeclaredByPolicy   bool
}

type mcpGatewayInvocationPolicyDecision struct {
	ServerManifest   mcpGatewayServerManifest
	ToolManifest     mcpGatewayToolManifest
	RequiresApproval bool
}

func buildMCPGatewayServerManifests(policy config.Policy) (map[string]mcpGatewayServerManifest, error) {
	manifests := make(map[string]mcpGatewayServerManifest, len(policy.Tools.MCPGateway.Servers))
	for serverID, serverPolicy := range policy.Tools.MCPGateway.Servers {
		if err := identifiers.ValidateSafeIdentifier("mcp gateway server id", serverID); err != nil {
			return nil, err
		}
		manifest := mcpGatewayServerManifest{
			ServerID:           serverID,
			Enabled:            boolWithDefault(serverPolicy.Enabled, true),
			RequiresApproval:   boolWithDefault(serverPolicy.RequiresApproval, false),
			Transport:          strings.TrimSpace(serverPolicy.Transport),
			LaunchCommand:      strings.TrimSpace(serverPolicy.Launch.Command),
			LaunchArgs:         append([]string(nil), serverPolicy.Launch.Args...),
			WorkingDirectory:   strings.TrimSpace(serverPolicy.WorkingDirectory),
			AllowedEnvironment: append([]string(nil), serverPolicy.AllowedEnvironment...),
			SecretEnvironment:  copySecretEnvironment(serverPolicy.SecretEnvironment),
			ToolManifests:      buildMCPGatewayToolManifests(serverPolicy.ToolPolicies),
		}
		manifests[serverID] = manifest
	}
	return manifests, nil
}

func copySecretEnvironment(source map[string]secrets.SecretRef) map[string]secrets.SecretRef {
	if len(source) == 0 {
		return map[string]secrets.SecretRef{}
	}
	copied := make(map[string]secrets.SecretRef, len(source))
	for environmentVariableName, secretRef := range source {
		copied[environmentVariableName] = secretRef
	}
	return copied
}

func buildMCPGatewayToolManifests(source map[string]config.MCPGatewayToolPolicy) map[string]mcpGatewayToolManifest {
	if len(source) == 0 {
		return map[string]mcpGatewayToolManifest{}
	}
	copied := make(map[string]mcpGatewayToolManifest, len(source))
	for toolName, toolPolicy := range source {
		copied[toolName] = mcpGatewayToolManifest{
			ToolName:           toolName,
			Enabled:            boolWithDefault(toolPolicy.Enabled, true),
			RequiresApproval:   boolWithDefault(toolPolicy.RequiresApproval, false),
			RequiredArguments:  append([]string(nil), toolPolicy.RequiredArguments...),
			AllowedArguments:   append([]string(nil), toolPolicy.AllowedArguments...),
			DeniedArguments:    append([]string(nil), toolPolicy.DeniedArguments...),
			ArgumentValueKinds: copyStringMap(toolPolicy.ArgumentValueKinds),
			DeclaredByPolicy:   true,
		}
	}
	return copied
}

func copyStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return map[string]string{}
	}
	copied := make(map[string]string, len(source))
	for key, value := range source {
		copied[key] = value
	}
	return copied
}

func boolWithDefault(configuredValue *bool, defaultValue bool) bool {
	if configuredValue == nil {
		return defaultValue
	}
	return *configuredValue
}

func (server *Server) resolveMCPGatewayServerManifest(serverID string) (mcpGatewayServerManifest, error) {
	policyRuntime := server.currentPolicyRuntime()
	trimmedServerID := strings.TrimSpace(serverID)
	if err := identifiers.ValidateSafeIdentifier("mcp gateway server id", trimmedServerID); err != nil {
		return mcpGatewayServerManifest{}, fmt.Errorf("%w: %v", errMCPGatewayServerNotFound, err)
	}

	serverManifest, found := policyRuntime.mcpGatewayManifests[trimmedServerID]
	if !found {
		return mcpGatewayServerManifest{}, fmt.Errorf("%w: %s", errMCPGatewayServerNotFound, trimmedServerID)
	}
	if !serverManifest.Enabled {
		return mcpGatewayServerManifest{}, fmt.Errorf("%w: %s", errMCPGatewayServerDisabled, trimmedServerID)
	}
	return serverManifest, nil
}

func (server *Server) resolveMCPGatewayToolManifest(serverID string, toolName string) (mcpGatewayServerManifest, mcpGatewayToolManifest, error) {
	serverManifest, err := server.resolveMCPGatewayServerManifest(serverID)
	if err != nil {
		return mcpGatewayServerManifest{}, mcpGatewayToolManifest{}, err
	}

	trimmedToolName := strings.TrimSpace(toolName)
	if err := identifiers.ValidateSafeIdentifier("mcp gateway tool name", trimmedToolName); err != nil {
		return mcpGatewayServerManifest{}, mcpGatewayToolManifest{}, fmt.Errorf("%w: %v", errMCPGatewayToolNotFound, err)
	}

	toolManifest, found := serverManifest.ToolManifests[trimmedToolName]
	if !found || !toolManifest.DeclaredByPolicy {
		return mcpGatewayServerManifest{}, mcpGatewayToolManifest{}, fmt.Errorf("%w: %s/%s", errMCPGatewayToolNotFound, serverManifest.ServerID, trimmedToolName)
	}
	if !toolManifest.Enabled {
		return mcpGatewayServerManifest{}, mcpGatewayToolManifest{}, fmt.Errorf("%w: %s/%s", errMCPGatewayToolDisabled, serverManifest.ServerID, trimmedToolName)
	}
	return serverManifest, toolManifest, nil
}

func (server *Server) evaluateMCPGatewayInvocationPolicy(serverID string, toolName string) (mcpGatewayInvocationPolicyDecision, error) {
	serverManifest, toolManifest, err := server.resolveMCPGatewayToolManifest(serverID, toolName)
	if err != nil {
		return mcpGatewayInvocationPolicyDecision{}, err
	}

	return mcpGatewayInvocationPolicyDecision{
		ServerManifest:   serverManifest,
		ToolManifest:     toolManifest,
		RequiresApproval: serverManifest.RequiresApproval || toolManifest.RequiresApproval,
	}, nil
}

func (server *Server) buildMCPGatewayInventoryResponse() MCPGatewayInventoryResponse {
	policyRuntime := server.currentPolicyRuntime()
	serverIDs := make([]string, 0, len(policyRuntime.mcpGatewayManifests))
	for serverID := range policyRuntime.mcpGatewayManifests {
		serverIDs = append(serverIDs, serverID)
	}
	slices.Sort(serverIDs)

	response := MCPGatewayInventoryResponse{
		DenyUnknownServers: policyRuntime.policy.MCPGatewayDenyUnknownServers(),
		Servers:            make([]MCPGatewayDeclaredServerView, 0, len(serverIDs)),
	}
	for _, serverID := range serverIDs {
		serverManifest := policyRuntime.mcpGatewayManifests[serverID]
		response.Servers = append(response.Servers, buildMCPGatewayDeclaredServerView(serverManifest))
	}
	return response
}

func buildMCPGatewayDeclaredServerView(serverManifest mcpGatewayServerManifest) MCPGatewayDeclaredServerView {
	toolNames := make([]string, 0, len(serverManifest.ToolManifests))
	for toolName := range serverManifest.ToolManifests {
		toolNames = append(toolNames, toolName)
	}
	slices.Sort(toolNames)

	secretEnvironmentVariables := make([]string, 0, len(serverManifest.SecretEnvironment))
	for environmentVariableName := range serverManifest.SecretEnvironment {
		secretEnvironmentVariables = append(secretEnvironmentVariables, environmentVariableName)
	}
	slices.Sort(secretEnvironmentVariables)

	serverView := MCPGatewayDeclaredServerView{
		ServerID:                   serverManifest.ServerID,
		Enabled:                    serverManifest.Enabled,
		RequiresApproval:           serverManifest.RequiresApproval,
		EffectiveDecision:          mcpGatewayEffectiveDecision(serverManifest.Enabled, serverManifest.RequiresApproval),
		Transport:                  serverManifest.Transport,
		LaunchCommand:              serverManifest.LaunchCommand,
		LaunchArgs:                 append([]string(nil), serverManifest.LaunchArgs...),
		WorkingDirectory:           serverManifest.WorkingDirectory,
		AllowedEnvironment:         append([]string(nil), serverManifest.AllowedEnvironment...),
		SecretEnvironmentVariables: secretEnvironmentVariables,
		Tools:                      make([]MCPGatewayDeclaredToolView, 0, len(toolNames)),
	}
	for _, toolName := range toolNames {
		toolManifest := serverManifest.ToolManifests[toolName]
		serverView.Tools = append(serverView.Tools, MCPGatewayDeclaredToolView{
			ToolName:          toolManifest.ToolName,
			Enabled:           toolManifest.Enabled,
			RequiresApproval:  toolManifest.RequiresApproval,
			EffectiveDecision: mcpGatewayEffectiveDecision(toolManifest.Enabled, serverManifest.RequiresApproval || toolManifest.RequiresApproval),
		})
	}
	return serverView
}

func (server *Server) buildMCPGatewayDecisionResponse(request MCPGatewayDecisionRequest) MCPGatewayDecisionResponse {
	response := MCPGatewayDecisionResponse{
		ServerID: strings.TrimSpace(request.ServerID),
		ToolName: strings.TrimSpace(request.ToolName),
	}

	decision, err := server.evaluateMCPGatewayInvocationPolicy(request.ServerID, request.ToolName)
	if err == nil {
		response.Decision = mcpGatewayEffectiveDecision(true, decision.RequiresApproval)
		response.RequiresApproval = decision.RequiresApproval
		response.ServerID = decision.ServerManifest.ServerID
		response.ToolName = decision.ToolManifest.ToolName
		return response
	}

	response.Decision = "deny"
	response.DenialCode = mcpGatewayDecisionDenialCode(err)
	response.DenialReason = err.Error()
	return response
}

func (server *Server) buildMCPGatewayInvocationValidationResponse(request MCPGatewayInvocationRequest) (MCPGatewayInvocationValidationResponse, error) {
	validatedRequest, err := validateMCPGatewayInvocationRequest(request)
	if err != nil {
		return MCPGatewayInvocationValidationResponse{}, err
	}

	response := MCPGatewayInvocationValidationResponse{
		ServerID:               validatedRequest.ServerID,
		ToolName:               validatedRequest.ToolName,
		ValidatedArgumentCount: len(validatedRequest.ValidatedArgKeys),
		ValidatedArgumentKeys:  append([]string(nil), validatedRequest.ValidatedArgKeys...),
	}

	decision, err := server.evaluateMCPGatewayInvocationPolicy(validatedRequest.ServerID, validatedRequest.ToolName)
	if err != nil {
		response.Decision = "deny"
		response.DenialCode = mcpGatewayDecisionDenialCode(err)
		response.DenialReason = err.Error()
		return response, nil
	}
	if argumentValidationErr := validateMCPGatewayToolArguments(decision.ToolManifest, validatedRequest); argumentValidationErr != nil {
		response.Decision = "deny"
		response.DenialCode = DenialCodeMCPGatewayArgumentsInvalid
		response.DenialReason = argumentValidationErr.Error()
		return response, nil
	}

	response.Decision = mcpGatewayEffectiveDecision(true, decision.RequiresApproval)
	response.RequiresApproval = decision.RequiresApproval
	response.ServerID = decision.ServerManifest.ServerID
	response.ToolName = decision.ToolManifest.ToolName
	return response, nil
}

func mcpGatewayEffectiveDecision(enabled bool, requiresApproval bool) string {
	if !enabled {
		return "deny"
	}
	if requiresApproval {
		return "needs_approval"
	}
	return "allow"
}

func mcpGatewayDecisionDenialCode(err error) string {
	switch {
	case errors.Is(err, errMCPGatewayServerNotFound):
		return DenialCodeMCPGatewayServerNotFound
	case errors.Is(err, errMCPGatewayServerDisabled):
		return DenialCodeMCPGatewayServerDisabled
	case errors.Is(err, errMCPGatewayToolNotFound):
		return DenialCodeMCPGatewayToolNotFound
	case errors.Is(err, errMCPGatewayToolDisabled):
		return DenialCodeMCPGatewayToolDisabled
	default:
		return DenialCodePolicyDenied
	}
}

func buildMCPGatewayInvocationAuditData(tokenClaims capabilityToken, response MCPGatewayInvocationValidationResponse) map[string]interface{} {
	auditData := map[string]interface{}{
		"server_id":                strings.TrimSpace(response.ServerID),
		"tool_name":                strings.TrimSpace(response.ToolName),
		"validated_argument_keys":  append([]string(nil), response.ValidatedArgumentKeys...),
		"validated_argument_count": response.ValidatedArgumentCount,
		"decision":                 strings.TrimSpace(response.Decision),
		"requires_approval":        response.RequiresApproval,
		"actor_label":              tokenClaims.ActorLabel,
		"client_session_label":     tokenClaims.ClientSessionLabel,
		"control_session_id":       tokenClaims.ControlSessionID,
	}
	if strings.TrimSpace(response.DenialCode) != "" {
		auditData["denial_code"] = strings.TrimSpace(response.DenialCode)
	}
	return auditData
}

func validateMCPGatewayToolArguments(toolManifest mcpGatewayToolManifest, validatedRequest validatedMCPGatewayInvocationRequest) error {
	validatedArgumentSet := make(map[string]struct{}, len(validatedRequest.ValidatedArgKeys))
	for _, argumentName := range validatedRequest.ValidatedArgKeys {
		validatedArgumentSet[argumentName] = struct{}{}
	}

	for _, requiredArgument := range toolManifest.RequiredArguments {
		if _, found := validatedArgumentSet[requiredArgument]; !found {
			return fmt.Errorf("required argument %q is missing", requiredArgument)
		}
	}

	if len(toolManifest.AllowedArguments) > 0 {
		allowedArgumentSet := make(map[string]struct{}, len(toolManifest.AllowedArguments))
		for _, allowedArgument := range toolManifest.AllowedArguments {
			allowedArgumentSet[allowedArgument] = struct{}{}
		}
		for _, argumentName := range validatedRequest.ValidatedArgKeys {
			if _, allowed := allowedArgumentSet[argumentName]; !allowed {
				return fmt.Errorf("argument %q is not allowed by MCP gateway tool policy", argumentName)
			}
		}
	}

	if len(toolManifest.DeniedArguments) > 0 {
		deniedArgumentSet := make(map[string]struct{}, len(toolManifest.DeniedArguments))
		for _, deniedArgument := range toolManifest.DeniedArguments {
			deniedArgumentSet[deniedArgument] = struct{}{}
		}
		for _, argumentName := range validatedRequest.ValidatedArgKeys {
			if _, denied := deniedArgumentSet[argumentName]; denied {
				return fmt.Errorf("argument %q is denied by MCP gateway tool policy", argumentName)
			}
		}
	}

	for argumentName, expectedValueKind := range toolManifest.ArgumentValueKinds {
		rawValue, found := validatedRequest.Arguments[argumentName]
		if !found {
			continue
		}
		actualValueKind := mcpGatewayRawJSONValueKind(rawValue)
		if actualValueKind != expectedValueKind {
			return fmt.Errorf("argument %q must be %s, got %s", argumentName, expectedValueKind, actualValueKind)
		}
	}

	return nil
}

func mcpGatewayRawJSONValueKind(rawValue []byte) string {
	trimmedValue := strings.TrimSpace(string(rawValue))
	switch {
	case trimmedValue == "null":
		return "null"
	case trimmedValue == "true" || trimmedValue == "false":
		return "boolean"
	case strings.HasPrefix(trimmedValue, "\""):
		return "string"
	case strings.HasPrefix(trimmedValue, "{"):
		return "object"
	case strings.HasPrefix(trimmedValue, "["):
		return "array"
	default:
		return "number"
	}
}
