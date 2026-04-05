# UI Map

This file maps `internal/ui/`, **terminal UI primitives** (colors, banners, panels, spinners, logo) for CLI and tooling.

Use it when changing:

- startup banner layout, ANSI color usage, or terminal-safe rendering
- pixel-art logo rendering or borders

## Core Role

`internal/ui/` is for terminal UI primitives, not a desktop application shell. It provides string builders for terminal output: `Banner`, styled lines, panels, readline-friendly components, and safe truncation helpers.

## Key Files

- `ui.go`
  - banner, layout helpers, metadata lines (pre-validated inputs only)

- `color.go`, `term.go`, `panel.go`, `border.go`, `spinner.go`, `select.go`
  - terminal presentation

- `logo.go`, `pixelart.go`
  - ASCII / pixel logo variants

- `safe.go`
  - safe string handling for UI

- `ui_test.go`, `pixelart_test.go`
  - rendering tests

## Relationship Notes

- Consumer: `internal/shell/` and any CLI entrypoints still using terminal UX

## Important Watchouts

- `BannerConfig` fields must be pre-validated; never pass raw secrets or model output directly into banners.
