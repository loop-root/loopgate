# Checked-In Config Map

This file maps the repo-root **`config/`** directory — **tracked YAML** that is not the same as `internal/config/` (Go loaders).

Use it when changing:

- internal continuity tuning or runtime defaults that ship with the repo

## Files

- `runtime.yaml`
  - default model/runtime hints (checked-in baseline; live runtime often under `runtime/state/`)

## Relationship Notes

- Go-side loading: `internal/config/runtime.go` where applicable
- Persona: `persona/default.yaml` (separate directory)

## Important Watchouts

- No secrets in tracked YAML — use secret refs and secure stores.
