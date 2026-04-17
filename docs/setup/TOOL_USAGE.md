**Last updated:** 2026-03-24

# Loopgate tool usage

This guide documents how tools are invoked, where policy is enforced, and what is logged.

## 1) Governed capability execution

Current operator surfaces submit typed capability requests to Loopgate over the
local HTTP control plane. There is one real execution path:

`operator surface -> Loopgate request validation -> policy check -> approval (if required) -> capability execute -> audit`

Model output, client-side summaries, and UI text remain untrusted content. They
do not create authority on their own.

## 2) Registered tools

Default registered tools in `internal/tools/defaults.go` include:

- `fs_read` (`read`)
- `fs_write` (`write`)
- `fs_list` (`read`)
- `fs_mkdir` (`write`)
- `host.folder.list` (`read`)
- `host.folder.read` (`read`)
- `host.organize.plan` (`read`)
- `host.plan.apply` (`write`)

Policy checker uses each tool's declared operation (`read`/`write`/`execute`), not hardcoded tool names.

## 3) Logging and redaction

Tool events are written to the append-only ledger.

- Tool args, output, and reasons are redacted via `internal/secrets` helpers.
- Truncation is applied after redaction.
- Ledger append failures on security-relevant Loopgate actions are surfaced, not silently dropped.

## 4) Security-relevant behavior to keep in mind

- SafePath enforces allowed roots + deny paths on resolved targets.
- Governed capability writes use the hardened write helper in `internal/tools/fs_write.go`.
- `internal/tools/fs_write` uses a no-follow open path (`openat` + `O_NOFOLLOW`) after validation.

If you add new tools, register them explicitly, declare operation type honestly,
and keep the registry aligned with the real execution path.
