# ADR 0003: Diagnostic slog as operator product surface

**Date:** 2026-04-01  
**Status:** accepted

## Decision

We treat **`logging.diagnostic`** in `config/runtime.yaml` (per-channel `slog` files, levels **error → trace**) as a **first-class product surface** for admins and support: panic recovery, handler errors, and integration boundaries must emit **human-legible, grep-friendly** lines on the right channel, never replacing authoritative audit JSONL but always available when verbosity is raised.

Session-scoped lines **must** carry **`tenant_id`** and **`user_id`** as structured attributes whenever the active control session supplies them (same semantics as audit, including personal-mode sentinel), so multi-tenant deployments can filter logs without mixing tenants; pre-session failures use a documented constant or omission.

## Tradeoff

Higher default verbosity risks disk growth and accidental inclusion of sensitive context if engineers log carelessly; mitigations are channel-specific levels, redaction discipline from `AGENTS.md`, and keeping diagnostic logs explicitly non-authoritative. Tenant labels in logs are metadata for routing support work, not proof of isolation — enforcement remains in policy and storage.

## Consequences

If operators outgrow file-based logs, we can add export to a log stack **without** removing the YAML-driven `slog` path — the escape hatch is a secondary sink fed from the same `internal/loopdiag` entrypoints, not ad-hoc `fmt.Printf` in handlers.
