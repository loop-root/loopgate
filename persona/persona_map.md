# Persona Map

This file maps **`persona/`** — default **operator persona** copy and **values** loaded into prompts and product tone.

Use it when changing:

- self-description, voice, or checked-in persona YAML
- alignment between `default.yaml` and prompt compiler expectations

## Files

- `default.yaml`
  - structured persona fields consumed by `internal/config` persona loading and `internal/prompt`

- `values.md`
  - human-readable values / principles reference for authors (not automatically enforced as code)

## Relationship Notes

- Prompt assembly: `internal/prompt/prompt_map.md`
- Config binding: `internal/config/persona.go`

## Important Watchouts

- Persona text becomes model input context — keep it reviewable and non-authoritative for permissions.
