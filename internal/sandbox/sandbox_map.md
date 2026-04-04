# Sandbox Map

This file maps `internal/sandbox/`, the filesystem boundary between the repo’s sandbox home and virtual paths **local clients** expose.

Use it when changing:

- import/export path mapping
- virtual path constants (`/morph/home/...`)
- copy/mirror semantics into or out of the sandbox
- symlink or traversal rules

## Core Role

`internal/sandbox/` implements **canonical sandbox paths**, **root enforcement**, and **safe copy** primitives used by Loopgate and **local clients** when moving data between host and the sandbox.

It is not the control plane: policy and approvals live in Loopgate; this package enforces **where** sandbox-relative operations may land once authorized.

## Key Files

- `sandbox.go`
  - virtual roots (`VirtualHome`, `VirtualWorkspace`, etc.) and `PathsForRepo`
  - path validation, normalization, and rejection of symlinks where disallowed
  - copy/mirror helpers used for staged imports and exports

- `sandbox_test.go`
  - regression tests for path rules and edge cases

## Relationship Notes

- Host-visible granted folders and compare-before-sync logic: `internal/loopgate/folder_access.go`
- Reference workspace UI: `cmd/haven/workspace.go`
- Default policy roots: `core/policy/policy.yaml`

## Important Watchouts

- Do not relax path checks for convenience; failures must stay explicit.
- Virtual paths shown in UI are not the same as OS paths under `runtime/sandbox/`.
