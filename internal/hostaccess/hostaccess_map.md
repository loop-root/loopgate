# Host Access Map

`internal/hostaccess` owns low-level host filesystem path traversal primitives
used by Loopgate host-folder tools.

## Boundary

This package owns:

- relative path normalization under a granted root
- path policy errors for invalid or escaping relative paths
- read-only `openat` traversal with `O_NOFOLLOW`
- parent-directory opening for controlled rename/mkdir operations
- `lstat` and mkdir helpers that stay under the granted root

This package must not import `internal/loopgate`.

## Callers

`internal/loopgate/server_host_access_handlers.go` maps these low-level path
results into control-plane responses and denial codes. Keep HTTP response
shaping and audit behavior in `internal/loopgate`.

## Invariants

- Relative paths must not escape the granted folder root.
- Symlinks must fail closed during traversal.
- Policy errors stay distinguishable from execution errors so callers can
  return precise denial codes.
- Host access helpers do not own operator approval, audit append, or server
  runtime state.
