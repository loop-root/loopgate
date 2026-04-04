**Last updated:** 2026-03-24

# Loopgate tool usage

This guide documents how tools are invoked, where policy is enforced, and what is logged.

## 1) Command paths

The operator client (terminal CLI, IDE-hosted agent, or local HTTP UI) has two execution paths:

1. Slash commands (`/ls`, `/cat`, `/write`, etc.) handled by `internal/shell`.
2. Model-driven tool calls parsed from `<tool_call>...</tool_call>` blocks and executed by `internal/orchestrator`.

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

## 3) Model tool-call format

When non-slash input reaches the model, the client parses this structure:

```text
<tool_call>
{"name":"fs_read","args":{"path":"docs/setup/SETUP.md"}}
</tool_call>
```

Execution flow:

`model output -> parser -> schema validation -> policy check -> approval (if required) -> tool execute -> ledger`

## 4) Registered orchestrator tools

Default registry in `internal/tools/defaults.go`:

- `fs_read` (`read`)
- `fs_write` (`write`)
- `fs_list` (`read`)

Policy checker uses each tool's declared operation (`read`/`write`/`execute`), not hardcoded tool names.

## 5) Logging and redaction

Tool events are written to the append-only ledger.

- Tool args, output, and reasons are redacted via `internal/secrets` helpers.
- Truncation is applied after redaction.
- Ledger append failures in orchestrator logging are surfaced (error callback or stderr warning), not silently dropped.

## 6) Security-relevant behavior to keep in mind

- SafePath enforces allowed roots + deny paths on resolved targets.
- Slash-command `/write` and orchestrator `fs_write` both use the hardened write helper in `internal/tools/fs_write.go`.
- `internal/tools/fs_write` uses a no-follow open path (`openat` + `O_NOFOLLOW`) after validation.

If you add new tools, route them through the orchestrator registry and declare operation type explicitly.
