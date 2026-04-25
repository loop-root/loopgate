# Loopdiag Package Map

This file maps `internal/loopdiag/`, Loopgate's optional diagnostic logging
helpers.

Use it when changing:

- diagnostic log channels
- diagnostic log file naming or permissions
- diagnostic HTTP middleware
- diagnostic log levels

## Core Role

`loopdiag/` creates non-authoritative text logs for local operator
troubleshooting. These logs complement the audit ledger; they never replace it.

The package exists to keep diagnostic logging explicit, bounded, and separate
from the security-relevant audit trail.

## Key Files

- `manager.go`
  - `Manager` with per-channel `slog.Logger` values
  - `Open` for creating channel log files under the configured diagnostic dir
  - `Close` for file cleanup
  - diagnostic level parsing
  - `HTTPMiddleware` for body-free, authorization-free request diagnostics

## Relationship Notes

- Runtime config shape lives in `internal/config/runtime.go`.
- Server emission sites live in `internal/loopgate/server_diagnostic_logging.go`
  and neighboring runtime files.
- Troubleshooting bundles read diagnostic tails through `internal/troubleshoot/`.

## Important Watchouts

- Diagnostic logs are not audit truth and are not tamper-evident.
- Do not log request bodies, authorization headers, tokens, API keys, private
  keys, or raw secret-bearing payloads.
- File permissions should stay owner-only.
- Diagnostic directory handling must remain repo-bounded where support bundles
  read from it.
