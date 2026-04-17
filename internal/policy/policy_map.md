# Policy Map

This file maps `internal/policy/`, the tool-level policy checker driven by checked-in YAML.

Use it when changing:

- allow/deny semantics for tool categories (filesystem, host, shell, http)
- how `ToolInfo` maps to policy decisions
- integration with `internal/config` policy structs

## Core Role

`internal/policy/` evaluates **whether a registered tool operation is allowed** under the loaded `config.Policy` (typically from `core/policy/policy.yaml` via the config loader).

It does not execute tools or mint capabilities; Loopgate calls this after resolving tool metadata.

## Key Files

- `checker.go`
  - `Checker` and `Check(tool ToolInfo) CheckResult`
  - category routing: filesystem, host, http, shell, default deny

- `decision.go`
  - `Decision` type and related policy decision values

- `checker_test.go`
  - regression tests for allow/deny behavior

## Relationship Notes

- Policy file on disk: `core/policy/policy.yaml`
- Config binding: `internal/config/policy.go`
- Tools expose category/operation: `internal/tools/`

## Important Watchouts

- Unknown categories should deny, not pass through.
- Keep policy evaluation deterministic; avoid environment-dependent “helpful” defaults.
- Host-category tools currently reuse `tools.filesystem.*` enablement flags.
  There is not yet a separate `tools.host` policy block, so docs and operator
  guidance must say that explicitly rather than implying independent host
  policy toggles.
