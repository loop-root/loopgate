# Setup Wizard Map

This file maps `internal/setup/`, the **interactive model setup wizard** (provider selection, validation, Loopgate connection storage).

Use it when changing:

- CLI or TUI flows that configure `modelruntime.Config`
- probing models, saving connections through Loopgate, or wizard UX

## Core Role

`internal/setup/` provides `RunModelWizard` and related helpers: prompt interfaces (`Prompter`, `Selector`), validation hooks, and persistence via injected `ConnectionStorer`. Haven’s first-run experience may reuse similar patterns; product-specific onboarding lives primarily in `cmd/haven/setup.go`.

## Key Files

- `wizard.go`
  - main wizard implementation, HTTP/model listing, config write

- `wizard_test.go`
  - wizard behavior tests

## Relationship Notes

- Runtime client: `internal/modelruntime/`
- Loopgate connections API: `internal/loopgate/model_connections.go` (and client)
- Terminal UI primitives: `internal/ui/`

## Important Watchouts

- Secrets belong in secure storage via Loopgate / secrets paths, not echoed into logs.
- Wizard output should match validated `modelruntime.Config` invariants.
