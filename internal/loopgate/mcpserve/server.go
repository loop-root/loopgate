// Package mcpserve implements the stdio MCP server that forwards tool calls to a running
// Loopgate over the local Unix socket using delegated session credentials (AMP-aligned
// control plane; see docs/AMP and docs/rfcs/0001-loopgate-token-policy.md).
package mcpserve

import (
	"context"
	"encoding/json"
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

// Run starts the MCP stdio server. It requires a Loopgate daemon listening on the socket
// and valid LOOPGATE_MCP_* delegated credentials (see docs/setup/LOOPGATE_MCP.md).
func Run(ctx context.Context) error {
	delegatedCfg, actor, clientSession, err := DelegatedConfigFromEnv()
	if err != nil {
		return err
	}

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
	
	// Why we inject context here: Any action executed by the MCP server inherently executes
	// within the constraints of the bound session. Tagging all diagnostic traces with the 
	// TenantID ensures logs can be securely segregated without complex correlation later.
	diagServer = diagServer.With(
		"tenant_id", delegatedCfg.TenantID,
		"user_id", delegatedCfg.UserID,
		"actor", actor,
	)

	diagServer.Debug("mcp_server_start", "control_session_id", delegatedCfg.ControlSessionID)

	socketPath := SocketPath(repoRoot)

	lgClient, err := loopgate.NewClientFromDelegatedSession(socketPath, delegatedCfg)
	if err != nil {
		diagServer.Error("mcp_delegated_session_failed", "error", err.Error())
		return fmt.Errorf("mcp delegated session: %w", err)
	}
	lgClient.ConfigureSession(actor, clientSession, nil)

	mcpServer := server.NewMCPServer(
		"loopgate",
		"0.1.0",
		server.WithToolCapabilities(true),
	)

	mcpServer.AddTool(mcp.NewTool("loopgate.status",
		mcp.WithDescription("Returns Loopgate capability inventory and policy summary for the delegated session (same as HTTP GET /v1/status)."),
	), handleStatusTool(lgClient, diagServer))

	mcpServer.AddTool(mcp.NewTool("loopgate.execute_capability",
		mcp.WithDescription("Executes a Loopgate capability by name. Consider using the typed tools instead."),
		mcp.WithString("capability",
			mcp.Description("Capability name, e.g. memory.remember"),
			mcp.Required(),
		),
		mcp.WithString("arguments_json",
			mcp.Description(`JSON object of string key/value arguments.`),
		),
	), handleExecuteCapabilityTool(lgClient, diagServer))

	if ctx == nil {
		ctx = context.Background()
	}

	statusResp, err := lgClient.Status(ctx)
	if err != nil {
		diagServer.Error("mcp_status_fetch_failed", "error", err.Error())
		return fmt.Errorf("fetch capability inventory: %w", err)
	}

	// Why we map MCP Tools one-to-one with Loopgate Capabilities: 
	// 1. By registering typed tools dynamically, the caller doesn't need to learn a bespoke format;
	//    they just invoke "memory.remember" like a native tool. 
	// 2. This does not bypass the policy engine since all requests natively route
	//    to executeCapabilityWithArgs regardless.
	for _, capSum := range statusResp.Capabilities {
		// Skip registering a tool that clashes with the built-in system tools above.
		if capSum.Name == "loopgate.status" || capSum.Name == "loopgate.execute_capability" {
			continue
		}
		desc := capSum.Description
		if desc == "" {
			desc = fmt.Sprintf("Execute loopgate capability %s", capSum.Name)
		}
		mcpServer.AddTool(mcp.NewTool(capSum.Name,
			mcp.WithDescription(desc),
			mcp.WithString("arguments_json",
				mcp.Description(`JSON object of string key/value arguments.`),
			),
		), handleTypedCapabilityTool(lgClient, capSum.Name, diagServer))
	}

	stdioSrv := server.NewStdioServer(mcpServer)
	return stdioSrv.Listen(ctx, os.Stdin, os.Stdout)
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
