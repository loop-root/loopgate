# Modelruntime Map

This file maps `internal/modelruntime/`, loading model client configuration and constructing `model.Client` from repo state and environment.

Use it when changing:

- `model_runtime.json` path and schema
- how provider name, base URL, and API key resolution interact with secrets
- env overrides for CI or local dev

## Core Role

`internal/modelruntime/` bridges **on-disk and env configuration** to a concrete **`model.Client`** (Anthropic/OpenAI-compatible adapters under `internal/model/`).

It keeps API key material off disk where possible by referencing env vars and `SecretRef`-style flows appropriate to the deployment.

## Key Files

- `runtime.go`
  - `Config`, `ConfigPath`, `LoadConfig`, `NewClientFromRepo`, `NewClientFromConfig`, env overrides
  - default path under repo: `runtime/state/model_runtime.json` (see `runtimeConfigRelativePath` in source)

- `runtime_test.go`
  - config loading and validation tests

## Relationship Notes

- Providers: `internal/model/anthropic/`, `internal/model/openai/`
- Secrets: `internal/secrets/`
- Local setup and model-settings routes may influence runtime state through Loopgate-managed configuration.

## Important Watchouts

- Do not persist raw API keys in repo-tracked files.
- Validation failures should be explicit, not silently defaulted to a permissive client.
