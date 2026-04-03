# Safety Map

This file maps `internal/safety/`, **strict path resolution** and related filesystem safety primitives.

Use it when changing:

- symlink handling, `..` rejection, or “fail closed if resolution cannot be proven”
- Unicode normalization for path comparisons

## Core Role

`internal/safety/` implements **`resolvePathStrict`** and helpers: canonical resolution without silent fallback to a less-trusted path. Used wherever host or repo paths must be pinned before reads/writes.

## Key Files

- `safepath.go`
  - strict resolution, parent validation for new paths, normalization

- `safepath_test.go`
  - traversal, symlink, and edge-case coverage

## Relationship Notes

- Sandbox virtual layout: `internal/sandbox/`
- Tools and Loopgate host access paths should use consistent strict resolution policy.

## Important Watchouts

- AGENTS invariant: never relax strict resolution into a weaker resolver on error.
- Fail closed: if resolution fails, deny — do not guess.
