**Last updated:** 2026-04-03

# Loopgate setup (minimal)

This file intentionally stays **short**. Older step-by-step operator flows referred to legacy desktop clients and are **removed** until replaced with accurate, Loopgate-only instructions.

## Prerequisites

- Go (version in `go.mod`)
- macOS or Linux (POSIX filesystem semantics expected)

```bash
go version
```

## Validate the tree

From your Loopgate checkout:

```bash
go mod tidy
go test ./...
```

## Run the control plane

```bash
go run ./cmd/loopgate
```

Default local socket: `runtime/state/loopgate.sock` (under your checkout; paths are typically gitignored).

## Integrate from your IDE

- **MCP:** [LOOPGATE_MCP.md](./LOOPGATE_MCP.md) — Claude Code, Cursor, VS Code, Anti‑Gravity, OpenAI Codex, and other MCP hosts.
- **HTTP on Unix socket:** [LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md](./LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md) — session open, signing, route inventory.

## Configuration and policy

- Runtime: `config/runtime.yaml` — optional **`control_plane.expected_session_client_executable`**: when set to a non-empty **absolute** path, only that client binary may open a control session (`POST /v1/session/open`); empty keeps the default (no executable pinning).
- Policy: `core/policy/policy.yaml` (required at startup; Loopgate fails closed if missing) — under **`safety`**, **`haven_trusted_sandbox_auto_allow`** (default-on when omitted) and optional **`haven_trusted_sandbox_auto_allow_capabilities`** restrict Haven’s automatic upgrade of `NeedsApproval` → `Allow` for `TrustedSandboxLocal` tools.
- Morphling classes: `core/policy/morphling_classes.yaml`
- Persona (optional declarative defaults for unprivileged clients): `persona/default.yaml`

## Further reading

- [Secrets](./SECRETS.md)
- [Tool usage](./TOOL_USAGE.md)
- [Tenancy](./TENANCY.md)
- [Docs index](../README.md)
- [Threat model](../loopgate-threat-model.md)
- [RFC 0001 — token and request integrity](../rfcs/0001-loopgate-token-policy.md)

## Environment

- `LOOPGATE_SOCKET` — override Unix socket path
- `MORPH_REPO_ROOT` — legacy name; sets repo root for default socket resolution when unset uses working directory
