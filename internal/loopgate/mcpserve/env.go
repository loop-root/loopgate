package mcpserve

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"morph/internal/identifiers"
	"morph/internal/loopgate"
)

const (
	envControlSessionID = "LOOPGATE_MCP_CONTROL_SESSION_ID"
	envCapabilityToken  = "LOOPGATE_MCP_CAPABILITY_TOKEN"
	envApprovalToken    = "LOOPGATE_MCP_APPROVAL_TOKEN"
	envSessionMACKey    = "LOOPGATE_MCP_SESSION_MAC_KEY"
	envExpiresAt        = "LOOPGATE_MCP_EXPIRES_AT"
	envActor            = "LOOPGATE_MCP_ACTOR"
	envClientSession    = "LOOPGATE_MCP_CLIENT_SESSION"
)

// DefaultActor is the Loopgate actor label used for capability execute when the operator
// does not set LOOPGATE_MCP_ACTOR. Policy must allow this actor for MCP sessions.
const DefaultActor = "mcp"

// DefaultClientSession is the client session label passed on capability requests.
const DefaultClientSession = "mcp-stdio"

// LoadRepoRoot matches cmd/loopgate resolution for MORPH_REPO_ROOT / cwd.
func LoadRepoRoot() (string, error) {
	repoRoot := os.Getenv("MORPH_REPO_ROOT")
	if strings.TrimSpace(repoRoot) == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("determine repo root: %w", err)
		}
		repoRoot = wd
	}
	return filepath.Clean(repoRoot), nil
}

// SocketPath returns the Loopgate Unix socket path (LOOPGATE_SOCKET or default under repo).
func SocketPath(repoRoot string) string {
	if envSocket := strings.TrimSpace(os.Getenv("LOOPGATE_SOCKET")); envSocket != "" {
		return filepath.Clean(envSocket)
	}
	return filepath.Join(repoRoot, "runtime", "state", "loopgate.sock")
}

// DelegatedConfigFromEnv builds session credentials for an MCP subprocess. The parent
// process (or operator) must export these after a normal session open; values are sensitive.
func DelegatedConfigFromEnv() (loopgate.DelegatedSessionConfig, string, string, error) {
	controlSessionID := strings.TrimSpace(os.Getenv(envControlSessionID))
	if err := identifiers.ValidateSafeIdentifier("LOOPGATE_MCP control session id", controlSessionID); err != nil {
		return loopgate.DelegatedSessionConfig{}, "", "", err
	}

	cfg := loopgate.DelegatedSessionConfig{
		ControlSessionID: controlSessionID,
		CapabilityToken:  strings.TrimSpace(os.Getenv(envCapabilityToken)),
		ApprovalToken:    strings.TrimSpace(os.Getenv(envApprovalToken)),
		SessionMACKey:    strings.TrimSpace(os.Getenv(envSessionMACKey)),
	}

	rawExpiry := strings.TrimSpace(os.Getenv(envExpiresAt))
	if rawExpiry == "" {
		return loopgate.DelegatedSessionConfig{}, "", "", fmt.Errorf("%s is required (RFC3339 / RFC3339Nano)", envExpiresAt)
	}
	expiresAt, err := parseRFC3339Flexible(rawExpiry)
	if err != nil {
		return loopgate.DelegatedSessionConfig{}, "", "", fmt.Errorf("parse %s: %w", envExpiresAt, err)
	}
	cfg.ExpiresAt = expiresAt

	actor := strings.TrimSpace(os.Getenv(envActor))
	if actor == "" {
		actor = DefaultActor
	}
	if err := identifiers.ValidateSafeIdentifier("LOOPGATE_MCP actor", actor); err != nil {
		return loopgate.DelegatedSessionConfig{}, "", "", err
	}

	clientSession := strings.TrimSpace(os.Getenv(envClientSession))
	if clientSession == "" {
		clientSession = DefaultClientSession
	}
	if err := identifiers.ValidateSafeIdentifier("LOOPGATE_MCP client session", clientSession); err != nil {
		return loopgate.DelegatedSessionConfig{}, "", "", err
	}

	return cfg, actor, clientSession, nil
}

func parseRFC3339Flexible(raw string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, raw)
}
