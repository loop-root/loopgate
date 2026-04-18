package controlapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"loopgate/internal/identifiers"
)

const maxMCPGatewayInvocationArgumentCount = 64

var mcpGatewayArgumentNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,127}$`)

type MCPGatewayInventoryResponse struct {
	DenyUnknownServers bool                           `json:"deny_unknown_servers"`
	Servers            []MCPGatewayDeclaredServerView `json:"servers"`
}

type MCPGatewayServerStatusResponse struct {
	Servers []MCPGatewayServerRuntimeView `json:"servers"`
}

type MCPGatewayServerRuntimeView struct {
	ServerID          string `json:"server_id"`
	DeclaredEnabled   bool   `json:"declared_enabled"`
	Transport         string `json:"transport"`
	RuntimeState      string `json:"runtime_state"`
	PID               int    `json:"pid,omitempty"`
	Initialized       bool   `json:"initialized,omitempty"`
	StartedAtUTC      string `json:"started_at_utc,omitempty"`
	WorkingDirectory  string `json:"working_directory,omitempty"`
	CommandPath       string `json:"command_path,omitempty"`
	StderrPath        string `json:"stderr_path,omitempty"`
	LastKnownLaunchID string `json:"last_known_launch_attempt_id,omitempty"`
}

type MCPGatewayDeclaredServerView struct {
	ServerID                   string                       `json:"server_id"`
	Enabled                    bool                         `json:"enabled"`
	RequiresApproval           bool                         `json:"requires_approval"`
	EffectiveDecision          string                       `json:"effective_decision"`
	Transport                  string                       `json:"transport"`
	LaunchCommand              string                       `json:"launch_command"`
	LaunchArgs                 []string                     `json:"launch_args"`
	WorkingDirectory           string                       `json:"working_directory,omitempty"`
	AllowedEnvironment         []string                     `json:"allowed_environment"`
	SecretEnvironmentVariables []string                     `json:"secret_environment_variables"`
	Tools                      []MCPGatewayDeclaredToolView `json:"tools"`
}

type MCPGatewayDeclaredToolView struct {
	ToolName          string `json:"tool_name"`
	Enabled           bool   `json:"enabled"`
	RequiresApproval  bool   `json:"requires_approval"`
	EffectiveDecision string `json:"effective_decision"`
}

type MCPGatewayDecisionRequest struct {
	ServerID string `json:"server_id"`
	ToolName string `json:"tool_name"`
}

type MCPGatewayDecisionResponse struct {
	ServerID         string `json:"server_id"`
	ToolName         string `json:"tool_name"`
	Decision         string `json:"decision"`
	RequiresApproval bool   `json:"requires_approval"`
	DenialCode       string `json:"denial_code,omitempty"`
	DenialReason     string `json:"denial_reason,omitempty"`
}

type MCPGatewayEnsureLaunchRequest struct {
	ServerID string `json:"server_id"`
}

type MCPGatewayEnsureLaunchResponse struct {
	ServerID                   string   `json:"server_id"`
	Transport                  string   `json:"transport"`
	LaunchState                string   `json:"launch_state"`
	PID                        int      `json:"pid"`
	Reused                     bool     `json:"reused,omitempty"`
	WorkingDirectory           string   `json:"working_directory,omitempty"`
	CommandPath                string   `json:"command_path,omitempty"`
	CommandArgs                []string `json:"command_args,omitempty"`
	AllowedEnvironment         []string `json:"allowed_environment,omitempty"`
	SecretEnvironmentVariables []string `json:"secret_environment_variables,omitempty"`
	StderrPath                 string   `json:"stderr_path,omitempty"`
}

type MCPGatewayStopRequest struct {
	ServerID string `json:"server_id"`
}

type MCPGatewayStopResponse struct {
	ServerID            string `json:"server_id"`
	Transport           string `json:"transport,omitempty"`
	Stopped             bool   `json:"stopped"`
	PreviousLaunchState string `json:"previous_launch_state,omitempty"`
	PID                 int    `json:"pid,omitempty"`
}

type MCPGatewayInvocationRequest struct {
	ServerID  string                     `json:"server_id"`
	ToolName  string                     `json:"tool_name"`
	Arguments map[string]json.RawMessage `json:"arguments"`
}

type MCPGatewayInvocationValidationResponse struct {
	ServerID               string   `json:"server_id"`
	ToolName               string   `json:"tool_name"`
	Decision               string   `json:"decision"`
	RequiresApproval       bool     `json:"requires_approval"`
	ValidatedArgumentCount int      `json:"validated_argument_count"`
	ValidatedArgumentKeys  []string `json:"validated_argument_keys"`
	DenialCode             string   `json:"denial_code,omitempty"`
	DenialReason           string   `json:"denial_reason,omitempty"`
}

type MCPGatewayInvocationApprovalResponse struct {
	ServerID               string   `json:"server_id"`
	ToolName               string   `json:"tool_name"`
	Decision               string   `json:"decision"`
	RequiresApproval       bool     `json:"requires_approval"`
	ValidatedArgumentCount int      `json:"validated_argument_count"`
	ValidatedArgumentKeys  []string `json:"validated_argument_keys"`
	DenialCode             string   `json:"denial_code,omitempty"`
	DenialReason           string   `json:"denial_reason,omitempty"`
	ApprovalPrepared       bool     `json:"approval_prepared,omitempty"`
	ApprovalRequestID      string   `json:"approval_request_id,omitempty"`
	ApprovalDecisionNonce  string   `json:"approval_decision_nonce,omitempty"`
	ApprovalManifestSHA256 string   `json:"approval_manifest_sha256,omitempty"`
	ApprovalExpiresAtUTC   string   `json:"approval_expires_at_utc,omitempty"`
}

type MCPGatewayApprovalDecisionRequest struct {
	ApprovalRequestID      string `json:"approval_request_id"`
	Approved               bool   `json:"approved"`
	DecisionNonce          string `json:"decision_nonce"`
	ApprovalManifestSHA256 string `json:"approval_manifest_sha256,omitempty"`
}

type MCPGatewayApprovalDecisionResponse struct {
	ApprovalRequestID      string   `json:"approval_request_id"`
	ServerID               string   `json:"server_id"`
	ToolName               string   `json:"tool_name"`
	ValidatedArgumentCount int      `json:"validated_argument_count"`
	ValidatedArgumentKeys  []string `json:"validated_argument_keys"`
	Approved               bool     `json:"approved"`
	ApprovalState          string   `json:"approval_state"`
}

type MCPGatewayExecutionRequest struct {
	ApprovalRequestID      string                     `json:"approval_request_id"`
	ApprovalManifestSHA256 string                     `json:"approval_manifest_sha256"`
	ServerID               string                     `json:"server_id"`
	ToolName               string                     `json:"tool_name"`
	Arguments              map[string]json.RawMessage `json:"arguments"`
}

type MCPGatewayExecutionValidationResponse struct {
	ApprovalRequestID      string   `json:"approval_request_id"`
	ApprovalState          string   `json:"approval_state"`
	ServerID               string   `json:"server_id"`
	ToolName               string   `json:"tool_name"`
	ValidatedArgumentCount int      `json:"validated_argument_count"`
	ValidatedArgumentKeys  []string `json:"validated_argument_keys"`
	ExecutionAuthorized    bool     `json:"execution_authorized"`
	ExecutionMethod        string   `json:"execution_method"`
	ExecutionPath          string   `json:"execution_path"`
}

type MCPGatewayExecutionResponse struct {
	ApprovalRequestID      string          `json:"approval_request_id"`
	ApprovalState          string          `json:"approval_state"`
	ServerID               string          `json:"server_id"`
	ToolName               string          `json:"tool_name"`
	ValidatedArgumentCount int             `json:"validated_argument_count"`
	ValidatedArgumentKeys  []string        `json:"validated_argument_keys"`
	ProcessPID             int             `json:"process_pid"`
	ToolResult             json.RawMessage `json:"tool_result,omitempty"`
	ToolResultSHA256       string          `json:"tool_result_sha256,omitempty"`
	RemoteErrorCode        int             `json:"remote_error_code,omitempty"`
	RemoteErrorMessage     string          `json:"remote_error_message,omitempty"`
}

type ValidatedMCPGatewayInvocationRequest struct {
	ServerID         string
	ToolName         string
	Arguments        map[string]json.RawMessage
	ValidatedArgKeys []string
}

func (mcpGatewayInvocationRequest MCPGatewayInvocationRequest) Validate() error {
	_, err := ValidateMCPGatewayInvocationRequest(mcpGatewayInvocationRequest)
	return err
}

func (ensureLaunchRequest MCPGatewayEnsureLaunchRequest) Validate() error {
	return identifiers.ValidateSafeIdentifier("mcp gateway server id", strings.TrimSpace(ensureLaunchRequest.ServerID))
}

func (stopRequest MCPGatewayStopRequest) Validate() error {
	return identifiers.ValidateSafeIdentifier("mcp gateway server id", strings.TrimSpace(stopRequest.ServerID))
}

func (decisionRequest MCPGatewayApprovalDecisionRequest) Validate() error {
	if err := identifiers.ValidateSafeIdentifier("approval request id", strings.TrimSpace(decisionRequest.ApprovalRequestID)); err != nil {
		return err
	}
	return (ApprovalDecisionRequest{
		Approved:               decisionRequest.Approved,
		DecisionNonce:          decisionRequest.DecisionNonce,
		ApprovalManifestSHA256: decisionRequest.ApprovalManifestSHA256,
	}).Validate()
}

func (executionRequest MCPGatewayExecutionRequest) Validate() error {
	if err := identifiers.ValidateSafeIdentifier("approval request id", strings.TrimSpace(executionRequest.ApprovalRequestID)); err != nil {
		return err
	}
	if strings.TrimSpace(executionRequest.ApprovalManifestSHA256) == "" {
		return fmt.Errorf("approval_manifest_sha256 is required")
	}
	if _, err := ValidateMCPGatewayInvocationRequest(MCPGatewayInvocationRequest{
		ServerID:  executionRequest.ServerID,
		ToolName:  executionRequest.ToolName,
		Arguments: executionRequest.Arguments,
	}); err != nil {
		return err
	}
	return nil
}

func ValidateMCPGatewayInvocationRequest(mcpGatewayInvocationRequest MCPGatewayInvocationRequest) (ValidatedMCPGatewayInvocationRequest, error) {
	validatedRequest := ValidatedMCPGatewayInvocationRequest{
		ServerID: strings.TrimSpace(mcpGatewayInvocationRequest.ServerID),
		ToolName: strings.TrimSpace(mcpGatewayInvocationRequest.ToolName),
	}
	if err := identifiers.ValidateSafeIdentifier("mcp gateway server id", validatedRequest.ServerID); err != nil {
		return ValidatedMCPGatewayInvocationRequest{}, err
	}
	if err := identifiers.ValidateSafeIdentifier("mcp gateway tool name", validatedRequest.ToolName); err != nil {
		return ValidatedMCPGatewayInvocationRequest{}, err
	}
	if mcpGatewayInvocationRequest.Arguments == nil {
		return ValidatedMCPGatewayInvocationRequest{}, fmt.Errorf("arguments object is required")
	}
	if len(mcpGatewayInvocationRequest.Arguments) > maxMCPGatewayInvocationArgumentCount {
		return ValidatedMCPGatewayInvocationRequest{}, fmt.Errorf("arguments exceeds maximum count")
	}

	validatedArguments := make(map[string]json.RawMessage, len(mcpGatewayInvocationRequest.Arguments))
	validatedArgumentKeys := make([]string, 0, len(mcpGatewayInvocationRequest.Arguments))
	for rawArgumentName, rawArgumentValue := range mcpGatewayInvocationRequest.Arguments {
		argumentName := strings.TrimSpace(rawArgumentName)
		if !mcpGatewayArgumentNamePattern.MatchString(argumentName) {
			return ValidatedMCPGatewayInvocationRequest{}, fmt.Errorf("argument name %q is invalid", rawArgumentName)
		}
		trimmedArgumentValue := bytes.TrimSpace(rawArgumentValue)
		if len(trimmedArgumentValue) == 0 || !json.Valid(trimmedArgumentValue) {
			return ValidatedMCPGatewayInvocationRequest{}, fmt.Errorf("argument %q must contain valid JSON", argumentName)
		}
		validatedArguments[argumentName] = append(json.RawMessage(nil), trimmedArgumentValue...)
		validatedArgumentKeys = append(validatedArgumentKeys, argumentName)
	}
	slices.Sort(validatedArgumentKeys)
	validatedRequest.Arguments = validatedArguments
	validatedRequest.ValidatedArgKeys = validatedArgumentKeys
	return validatedRequest, nil
}
