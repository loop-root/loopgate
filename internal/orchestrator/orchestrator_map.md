# Orchestrator Map

This file maps `internal/orchestrator/`, tool-call coordination, structured output parsing, and rate limits.

Use it when changing:

- how tool calls are parsed from model output
- policy + approval gating around tool execution
- structured/JSON orchestration helpers used by chat flows
- logging hooks for tool decisions

## Core Role

`internal/orchestrator/` coordinates **tool execution** with `policy.Checker`, optional `Approver`, and `tools.Registry`. It also contains **structured** parsing and validation helpers (`structured.go`, `parser.go`) used when the model returns JSON-shaped plans or results.

## Key Files

- `orchestrator.go`
  - `Orchestrator` type, `New` / `NewWithConfig`, tool execution loop with policy and limits

- `parser.go` / `parser_test.go`
  - parsing model/tool output into structured actions

- `structured.go` / `structured_test.go`
  - structured orchestration types and validation

- `limits.go` / `limits_test.go`
  - rate and call limits per turn/session

- `logger.go` / `logger_test.go`
  - logging interfaces for tool calls and results

- `types.go`
  - shared types for tool calls and results

## Relationship Notes

- Policy: `internal/policy/`
- Tools: `internal/tools/`
- Reference Wails chat loop: `cmd/haven/chat.go`

## Important Watchouts

- Model output is untrusted: parsers must fail closed on malformed input.
- Do not bypass approval or policy hooks for convenience in the chat path.
