**Last updated:** 2026-03-24

# Loopgate tool usage

This guide documents how tools are invoked, where policy is enforced, and what is logged.

## 1) Command paths

The operator client (terminal CLI, IDE-hosted agent, or local HTTP UI) has two execution paths:

1. Slash commands (`/ls`, `/cat`, `/write`, etc.) handled by `internal/shell`.
2. Provider-native structured tool use when a request explicitly includes native tool definitions.

Both paths are policy-gated. Model output is untrusted.

## 2) Built-in slash commands

Current commands:

- `/help`
- `/exit`
- `/reset`
- `/pwd`
- `/ls [path]`
- `/cat <file>`
- `/write <file> <text...>`
- `/policy`
- `/debug help`
- `/debug safepath <path>`

Notes:

- `/write` respects policy and can require explicit approval.
- `/debug safepath` is read-only and helps explain allow/deny path decisions.

## 3) Native structured tool use

When a request includes provider-native tool definitions, the model may return
structured tool-use blocks through the provider API instead of plain text.

Execution flow:

`model output -> native tool block decode -> Loopgate request validation -> policy check -> approval (if required) -> capability execute -> audit`

There is no longer an XML `<tool_call>...</tool_call>` fallback in the active
Loopgate path.

## 4) Registered native tools

Default allowlisted tools in `internal/tools/defaults.go` / `internal/model/toolschema.go` include:

- `fs_read` (`read`)
- `fs_write` (`write`)
- `fs_list` (`read`)

Policy checker uses each tool's declared operation (`read`/`write`/`execute`), not hardcoded tool names.

## 5) Logging and redaction

Tool events are written to the append-only ledger.

- Tool args, output, and reasons are redacted via `internal/secrets` helpers.
- Truncation is applied after redaction.
- Ledger append failures on security-relevant Loopgate actions are surfaced, not silently dropped.

## 6) Security-relevant behavior to keep in mind

- SafePath enforces allowed roots + deny paths on resolved targets.
- Slash-command `/write` and governed capability writes both use the hardened write helper in `internal/tools/fs_write.go`.
- `internal/tools/fs_write` uses a no-follow open path (`openat` + `O_NOFOLLOW`) after validation.

If you add new tools, register them explicitly, declare operation type honestly,
and keep the native tool allowlist in `internal/model/toolschema.go` aligned
with the real execution path.
