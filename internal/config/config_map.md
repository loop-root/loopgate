# Config Package Map

This file maps `internal/config/`, the Go types and loaders for **checked-in YAML** and **runtime JSON** under the repo.

Use it when changing:

- policy struct shape vs `core/policy/policy.yaml`
- legacy class-policy compatibility loading vs `core/policy/morphling_classes.yaml`
- persona loading vs `persona/default.yaml`
- atomic JSON read/write helpers for `runtime/state/*.json`

## Core Role

`internal/config/` binds **filesystem config** to typed `Policy`, persona, and related structs. It is the compile-time / load-time counterpart to `internal/policy.Checker` (which consumes `Policy` at runtime).

## Key Files

- `policy.go`
  - `Policy` struct, YAML loading for `core/policy/policy.yaml` and the legacy class-policy compatibility path
  - hashing / change detection where used for policy acceptance flows

- `persona.go`
  - persona document loading

- `runtime.go`
  - `RuntimeConfig` including `logging.diagnostic` and **`tenancy.deployment_tenant_id` / `deployment_user_id`** (validated opaque strings; applied at Loopgate session open)

- `store.go`
  - `LoadJSONConfig` / `SaveJSONConfig` — atomic JSON persistence under a state directory

- `config_test.go`, `repo_files_test.go`, `gitignore_test.go`
  - validation that expected repo files parse and stay consistent

## Relationship Notes

- Checked-in YAML: `core/policy/`, `config/*.yaml`, `persona/`
- Policy enforcement: `internal/policy/`
- Loopgate loads policy from repo root: `internal/loopgate/`

## Important Watchouts

- Policy shape changes require matching updates in `core/policy/*.yaml` and tests.
- Do not widen allowed roots or tools in struct defaults; source of truth remains the YAML.
