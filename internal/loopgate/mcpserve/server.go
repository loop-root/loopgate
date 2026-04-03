// Package mcpserve implements the stdio MCP server that forwards tool calls to a running
// Loopgate over the local Unix socket using delegated session credentials (AMP-aligned
// control plane; see docs/AMP and docs/rfcs/0001-loopgate-token-policy.md).
package mcpserve

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

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
	socketPath := SocketPath(repoRoot)

	lgClient, err := loopgate.NewClientFromDelegatedSession(socketPath, delegatedCfg)
	if err != nil {
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
	), handleStatusTool(lgClient))

	mcpServer.AddTool(mcp.NewTool("loopgate.execute_capability",
		mcp.WithDescription("Executes a Loopgate capability by name with string arguments. Uses the same enforcement path as HTTP POST /v1/capabilities/execute."),
		mcp.WithString("capability",
			mcp.Description("Capability name, e.g. memory.remember"),
			mcp.Required(),
		),
		mcp.WithString("arguments_json",
			mcp.Description(`JSON object of string key/value arguments, e.g. {} or {"key":"value"}. Omitted keys default to empty object.`),
		),
	), handleExecuteCapabilityTool(lgClient))

	if ctx == nil {
		ctx = context.Background()
	}
	stdioSrv := server.NewStdioServer(mcpServer)
	return stdioSrv.Listen(ctx, os.Stdin, os.Stdout)
}

func handleStatusTool(lgClient *loopgate.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		status, err := lgClient.Status(ctx)
		if err != nil {
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

func handleExecuteCapabilityTool(lgClient *loopgate.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		capabilityName := strings.TrimSpace(request.GetString("capability", ""))
		if capabilityName == "" {
			return toolResultError(fmt.Errorf("capability is required"))
		}

		argsJSON := strings.TrimSpace(request.GetString("arguments_json", ""))
		if argsJSON == "" {
			argsJSON = "{}"
		}
		var arguments map[string]string
		if err := json.Unmarshal([]byte(argsJSON), &arguments); err != nil {
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
			return toolResultError(err)
		}
		raw, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return toolResultError(fmt.Errorf("encode capability response: %w", err))
		}
		st := strings.TrimSpace(resp.Status)
		isErr := st != loopgate.ResponseStatusSuccess && st != loopgate.ResponseStatusPendingApproval
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(string(raw))},
			IsError: isErr,
		}, nil
	}
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
