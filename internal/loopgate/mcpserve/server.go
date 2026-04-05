// Package mcpserve implements the stdio MCP server that forwards tool calls to a running
// Loopgate over the local Unix socket using delegated session credentials (AMP-aligned
// control plane; see docs/AMP and docs/rfcs/0001-loopgate-token-policy.md).
package mcpserve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"morph/internal/config"
	"morph/internal/loopdiag"
	"morph/internal/loopgate"
)

// Run starts the MCP stdio server with delegated credentials from the environment.
func Run(ctx context.Context) error {
	return RunWithOptions(ctx, RunOptions{})
}

// RunWithOptions starts the MCP stdio server. The default path requires a Loopgate daemon
// plus LOOPGATE_MCP_* delegated credentials; the optional local_open_session mode is for
// local/dev IDE integration only and still uses the normal Unix-socket session-open flow.
func RunWithOptions(ctx context.Context, options RunOptions) error {
	repoRoot, err := LoadRepoRoot()
	if err != nil {
		return err
	}
	runtimeConfig, err := config.LoadRuntimeConfig(repoRoot)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	diagnostic, _ := loopdiag.Open(repoRoot, runtimeConfig.Logging.Diagnostic)
	if diagnostic != nil {
		defer diagnostic.Close()
	}

	diagServer := slog.Default()
	if diagnostic != nil && diagnostic.Server != nil {
		diagServer = diagnostic.Server
	}
	socketPath := SocketPath(repoRoot)
	var lgClient *loopgate.Client
	var actor string
	var clientSession string
	if options.LocalOpenSession != nil {
		lgClient, actor, clientSession, err = newLoopgateClientForServeMode(socketPath, nil, options.LocalOpenSession)
		if err != nil {
			diagServer.Error("mcp_local_open_session_failed", "error", err.Error())
			return err
		}
		// This path is intentionally local/dev only: it opens an ordinary local Loopgate
		// session instead of reusing delegated credentials, and must not be repurposed as
		// a remote or production auth surface.
		diagServer = diagServer.With("actor", actor)
		diagServer.Info("mcp_server_local_open_session", "client_session", clientSession)
	} else {
		delegatedCfg, delegatedActor, delegatedClientSession, delegatedErr := DelegatedConfigFromEnv()
		if delegatedErr != nil {
			return delegatedErr
		}
		diagServer = diagServer.With(
			"tenant_id", delegatedCfg.TenantID,
			"user_id", delegatedCfg.UserID,
			"actor", delegatedActor,
		)
		diagServer.Debug("mcp_server_start", "control_session_id", delegatedCfg.ControlSessionID)
		lgClient, _, _, err = newLoopgateClientForServeMode(socketPath, &delegatedCfg, nil)
		if err != nil {
			diagServer.Error("mcp_delegated_session_failed", "error", err.Error())
			return err
		}
		actor = delegatedActor
		clientSession = delegatedClientSession
		lgClient.ConfigureSession(actor, clientSession, nil)
	}

	mcpServer := server.NewMCPServer(
		"loopgate",
		"0.1.0",
		server.WithToolCapabilities(true),
	)

	if ctx == nil {
		ctx = context.Background()
	}
	registeredTools, err := registeredLoopgateTools(ctx, lgClient, diagServer)
	if err != nil {
		return err
	}
	mcpServer.AddTools(registeredTools...)

	stdioSrv := server.NewStdioServer(mcpServer)
	return stdioSrv.Listen(ctx, os.Stdin, os.Stdout)
}

func registeredLoopgateTools(ctx context.Context, lgClient *loopgate.Client, logger *slog.Logger) ([]server.ServerTool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if logger == nil {
		logger = slog.Default()
	}

	registeredTools := []server.ServerTool{
		{
			Tool: mcp.NewTool("loopgate.status",
				mcp.WithDescription("Returns Loopgate capability inventory and policy summary for the delegated session (same as HTTP GET /v1/status)."),
			),
			Handler: handleStatusTool(lgClient, logger),
		},
		{
			Tool: mcp.NewTool("loopgate.execute_capability",
				mcp.WithDescription("Executes a Loopgate capability by name. Consider using the typed tools instead."),
				mcp.WithString("capability",
					mcp.Description("Capability name, e.g. memory.remember"),
					mcp.Required(),
				),
				mcp.WithString("arguments_json",
					mcp.Description(`JSON object of string key/value arguments.`),
				),
			),
			Handler: handleExecuteCapabilityTool(lgClient, logger),
		},
		{
			Tool: mcp.NewTool("loopgate.memory_wake_state",
				mcp.WithDescription("Loads the current Loopgate memory wake state for the delegated session tenant. Use this near session start when the user asks what you should know, what context carries over, or says something like 'Before you continue, what should you remember about me?'"),
			),
			Handler: handleMemoryWakeStateTool(lgClient, logger),
		},
		{
			Tool: mcp.NewTool("loopgate.memory_discover",
				mcp.WithDescription("Discovers memory items for a slot- or task-seeking query without JSON argument juggling. Use this in a fresh session when you need current carried-over facts or current task state without asking the user to restate them."),
				mcp.WithString("query",
					mcp.Description("Natural-language memory query, e.g. 'Retrieve the current user profile timezone from the profile slot.'"),
					mcp.Required(),
				),
				mcp.WithString("scope",
					mcp.Description("Optional scope override. Defaults to Loopgate's global memory scope."),
				),
				mcp.WithNumber("max_items",
					mcp.Description("Optional maximum number of items to return."),
				),
			),
			Handler: handleMemoryDiscoverTool(lgClient, logger),
		},
		{
			Tool: mcp.NewTool("loopgate.memory_remember",
				mcp.WithDescription("Remembers an explicit fact through Loopgate's validated memory write path. When the user asks to remember a stable fact or clearly states one about themselves, map it into a fact write here. If the mapping is ambiguous, ask one brief clarifying question instead of guessing or making the user fill out schema fields."),
				mcp.WithString("fact_key",
					mcp.Description("Canonical or alias fact key, e.g. profile.timezone or preference.communication_style."),
					mcp.Required(),
				),
				mcp.WithString("fact_value",
					mcp.Description("Fact value to remember, e.g. America/Denver."),
					mcp.Required(),
				),
				mcp.WithString("scope",
					mcp.Description("Optional scope override. Defaults to Loopgate's global memory scope."),
				),
				mcp.WithString("reason",
					mcp.Description("Optional short operator-visible reason for the memory write. Keep this concise; do not turn it into a user-facing form."),
				),
				mcp.WithString("source_text",
					mcp.Description("Optional source text from which the explicit fact was derived, for example the user's exact sentence."),
				),
				mcp.WithString("candidate_source",
					mcp.Description("Optional explicit candidate source, e.g. explicit_fact."),
				),
				mcp.WithString("source_channel",
					mcp.Description("Optional source channel, e.g. conversation."),
				),
			),
			Handler: handleMemoryRememberTool(lgClient, logger),
		},
	}

	statusResp, err := lgClient.Status(ctx)
	if err != nil {
		logger.Error("mcp_status_fetch_failed", "error", err.Error())
		return nil, fmt.Errorf("fetch capability inventory: %w", err)
	}

	// Register capability names directly so callers can still invoke the raw Loopgate
	// surface when a typed wrapper does not exist yet.
	for _, capSum := range statusResp.Capabilities {
		if capSum.Name == "loopgate.status" || capSum.Name == "loopgate.execute_capability" {
			continue
		}
		desc := capSum.Description
		if desc == "" {
			desc = fmt.Sprintf("Execute loopgate capability %s", capSum.Name)
		}
		registeredTools = append(registeredTools, server.ServerTool{
			Tool: mcp.NewTool(capSum.Name,
				mcp.WithDescription(desc),
				mcp.WithString("arguments_json",
					mcp.Description(`JSON object of string key/value arguments.`),
				),
			),
			Handler: handleTypedCapabilityTool(lgClient, capSum.Name, logger),
		})
	}

	return registeredTools, nil
}

func handleStatusTool(lgClient *loopgate.Client, logger *slog.Logger) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		status, err := lgClient.Status(ctx)
		if err != nil {
			logger.Error("mcp_status_call_failed", "error", err.Error())
			return toolResultError(err)
		}
		raw, err := json.MarshalIndent(status, "", "  ")
		if err != nil {
			return toolResultError(fmt.Errorf("encode status: %w", err))
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(string(raw))},
		}, nil
	}
}

func handleExecuteCapabilityTool(lgClient *loopgate.Client, logger *slog.Logger) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		capabilityName := strings.TrimSpace(request.GetString("capability", ""))
		if capabilityName == "" {
			return toolResultError(fmt.Errorf("capability is required"))
		}
		return executeCapabilityWithArgs(ctx, lgClient, logger, capabilityName, request.GetString("arguments_json", ""))
	}
}

func handleTypedCapabilityTool(lgClient *loopgate.Client, capabilityName string, logger *slog.Logger) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return executeCapabilityWithArgs(ctx, lgClient, logger, capabilityName, request.GetString("arguments_json", ""))
	}
}

func handleMemoryWakeStateTool(lgClient *loopgate.Client, logger *slog.Logger) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wakeStateResponse, err := lgClient.LoadMemoryWakeState(ctx)
		if err != nil {
			return toolResultFromLoopgateError(logger, "loopgate.memory_wake_state", err)
		}
		return toolResultJSON(wakeStateResponse, false)
	}
}

func handleMemoryDiscoverTool(lgClient *loopgate.Client, logger *slog.Logger) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		rawQuery := strings.TrimSpace(request.GetString("query", ""))
		if rawQuery == "" {
			return toolResultError(fmt.Errorf("query is required"))
		}
		discoverResponse, err := lgClient.DiscoverMemory(ctx, loopgate.MemoryDiscoverRequest{
			Scope:    strings.TrimSpace(request.GetString("scope", "")),
			Query:    rawQuery,
			MaxItems: request.GetInt("max_items", 0),
		})
		if err != nil {
			return toolResultFromLoopgateError(logger, "loopgate.memory_discover", err)
		}
		return toolResultJSON(discoverResponse, false)
	}
}

func handleMemoryRememberTool(lgClient *loopgate.Client, logger *slog.Logger) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		rememberResponse, err := lgClient.RememberMemoryFact(ctx, loopgate.MemoryRememberRequest{
			Scope:           strings.TrimSpace(request.GetString("scope", "")),
			FactKey:         strings.TrimSpace(request.GetString("fact_key", "")),
			FactValue:       request.GetString("fact_value", ""),
			Reason:          strings.TrimSpace(request.GetString("reason", "")),
			SourceText:      request.GetString("source_text", ""),
			CandidateSource: strings.TrimSpace(request.GetString("candidate_source", "")),
			SourceChannel:   strings.TrimSpace(request.GetString("source_channel", "")),
		})
		if err != nil {
			return toolResultFromLoopgateError(logger, "loopgate.memory_remember", err)
		}
		return toolResultJSON(rememberResponse, false)
	}
}

func executeCapabilityWithArgs(ctx context.Context, lgClient *loopgate.Client, logger *slog.Logger, capabilityName string, argsJSON string) (*mcp.CallToolResult, error) {
	argsJSON = strings.TrimSpace(argsJSON)
	if argsJSON == "" {
		argsJSON = "{}"
	}
	var arguments map[string]string
	if err := json.Unmarshal([]byte(argsJSON), &arguments); err != nil {
		logger.Error("mcp_tool_execute_bad_args", "capability", capabilityName, "error", err.Error())
		return toolResultError(fmt.Errorf("arguments_json must be a JSON object with string values: %w", err))
	}
	if arguments == nil {
		arguments = map[string]string{}
	}

	resp, err := lgClient.ExecuteCapability(ctx, loopgate.CapabilityRequest{
		Capability: capabilityName,
		Arguments:  arguments,
	})
	if err != nil {
		logger.Error("mcp_tool_execute_failed", "capability", capabilityName, "error", err.Error())
		return toolResultError(err)
	}
	raw, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return toolResultError(fmt.Errorf("encode capability response: %w", err))
	}
	st := strings.TrimSpace(resp.Status)
	
	// Why we consider PendingApproval as a success path initially: 
	// Approval workflows are asynchronous. The tool invocation correctly landed and
	// triggered the workflow, so we do not want to flag an operational error.
	isErr := st != loopgate.ResponseStatusSuccess && st != loopgate.ResponseStatusPendingApproval
	if isErr {
		logger.Warn("mcp_tool_execute_denied", "capability", capabilityName, "denial_code", resp.DenialCode)
	} else {
		logger.Debug("mcp_tool_execute_success", "capability", capabilityName)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{mcp.NewTextContent(string(raw))},
		IsError: isErr,
	}, nil
}

func toolResultError(err error) (*mcp.CallToolResult, error) {
	if err == nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent("internal error: nil err")},
			IsError: true,
		}, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{mcp.NewTextContent(err.Error())},
		IsError: true,
	}, nil
}

func toolResultJSON(payload interface{}, isError bool) (*mcp.CallToolResult, error) {
	rawPayload, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return toolResultError(fmt.Errorf("encode tool response: %w", err))
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{mcp.NewTextContent(string(rawPayload))},
		IsError: isError,
	}, nil
}

func toolResultFromLoopgateError(logger *slog.Logger, toolName string, toolErr error) (*mcp.CallToolResult, error) {
	var deniedError loopgate.RequestDeniedError
	if errors.As(toolErr, &deniedError) {
		if logger != nil {
			logger.Warn("mcp_tool_denied", "tool", toolName, "denial_code", deniedError.DenialCode)
		}
		return toolResultJSON(loopgate.CapabilityResponse{
			Status:       loopgate.ResponseStatusError,
			DenialCode:   deniedError.DenialCode,
			DenialReason: deniedError.DenialReason,
		}, true)
	}
	if logger != nil {
		logger.Error("mcp_tool_failed", "tool", toolName, "error", toolErr.Error())
	}
	return toolResultError(toolErr)
}
