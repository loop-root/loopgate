package mcpserve

import (
	"fmt"
	"strings"

	"morph/internal/identifiers"
	"morph/internal/loopgate"
)

// LocalOpenSessionConfig is a convenience wrapper for local/dev IDE integration only.
// It reuses the standard Loopgate session-open flow over the local Unix socket; it does
// not introduce a new auth model or a remote bootstrap path.
type LocalOpenSessionConfig struct {
	Actor                 string
	ClientSession         string
	RequestedCapabilities []string
	WorkspaceID           string
}

type RunOptions struct {
	LocalOpenSession *LocalOpenSessionConfig
}

func newLoopgateClientForServeMode(socketPath string, delegatedConfig *loopgate.DelegatedSessionConfig, localConfig *LocalOpenSessionConfig) (*loopgate.Client, string, string, error) {
	if localConfig != nil {
		actor, clientSession, requestedCapabilities, err := validatedLocalOpenSession(localConfig)
		if err != nil {
			return nil, "", "", err
		}
		localClient := loopgate.NewClient(socketPath)
		localClient.ConfigureSession(actor, clientSession, requestedCapabilities)
		if workspaceID := strings.TrimSpace(localConfig.WorkspaceID); workspaceID != "" {
			localClient.SetWorkspaceID(workspaceID)
		}
		return localClient, actor, clientSession, nil
	}
	if delegatedConfig == nil {
		return nil, "", "", fmt.Errorf("delegated session config is required when local_open_session is disabled")
	}
	delegatedClient, err := loopgate.NewClientFromDelegatedSession(socketPath, *delegatedConfig)
	if err != nil {
		return nil, "", "", fmt.Errorf("mcp delegated session: %w", err)
	}
	return delegatedClient, "", "", nil
}

func validatedLocalOpenSession(localConfig *LocalOpenSessionConfig) (string, string, []string, error) {
	if localConfig == nil {
		return "", "", nil, fmt.Errorf("local open session config is required")
	}

	actor := strings.TrimSpace(localConfig.Actor)
	if actor == "" {
		actor = DefaultActor
	}
	if err := identifiers.ValidateSafeIdentifier("mcp local actor", actor); err != nil {
		return "", "", nil, err
	}

	clientSession := strings.TrimSpace(localConfig.ClientSession)
	if clientSession == "" {
		clientSession = DefaultClientSession
	}
	if err := identifiers.ValidateSafeIdentifier("mcp local client session", clientSession); err != nil {
		return "", "", nil, err
	}

	requestedCapabilities := make([]string, 0, len(localConfig.RequestedCapabilities))
	for _, requestedCapability := range localConfig.RequestedCapabilities {
		trimmedCapability := strings.TrimSpace(requestedCapability)
		if trimmedCapability == "" {
			continue
		}
		requestedCapabilities = append(requestedCapabilities, trimmedCapability)
	}
	if len(requestedCapabilities) == 0 {
		return "", "", nil, fmt.Errorf("at least one requested capability is required for local_open_session mode")
	}
	return actor, clientSession, requestedCapabilities, nil
}
