# Shell Map

This file maps `internal/shell/`, **slash-commands**, autosuggest, and terminal UX for the **Loopgate operator CLI** surface.

Use it when changing:

- parsing `/command` input, help text, or manpage-style docs
- integration between commands, Loopgate, sandbox, and ledger logging

## Core Role

`internal/shell/` implements `HandleCommand` and a large catalog of **operator commands** (runtime config, tools, morphlings, etc.). This package supports terminal-based workflows and integration tests that drive the same Loopgate backends as MCP and HTTP clients.

## Key Files

- `commands.go`
  - `HandleCommand`, command dispatch, Loopgate client usage

- `catalog.go`, `manpages.go`, `manpages_test.go`
  - discoverable command list and help strings

- `autosuggest.go`, `completer.go`, `summaries.go`
  - readline integration and suggestions

- `commands_test.go`, `autosuggest_test.go`
  - parser and UX regressions

## Relationship Notes

- Results formatting: `internal/loopgateresult/`
- Loopgate client: `internal/loopgate/client.go`
- UI output: `internal/ui/`

## Important Watchouts

- Commands must not bypass Loopgate for privileged operations.
- Keep audit/ledger logging consistent with AGENTS rules for tool events.
