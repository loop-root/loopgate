# Cmd Map

This file maps **`cmd/`** Go **main** entrypoints. **`cmd/loopgate/`** is the primary server binary. Any remaining legacy shells under `cmd/` are deletion candidates, not active product surfaces.

Use it when changing:

- how Loopgate binds its socket or loads signed policy
- which binaries are still part of the active local-first product
- flags and startup diagnostics for the active binaries

## Core Role

`cmd/` contains small binaries. **Loopgate** (`cmd/loopgate/`) is the primary shipped server from this repository. The active product-facing surfaces are Loopgate itself, the **HTTP-on-UDS** control plane, and typed local APIs used by direct local operator clients. **In-tree MCP removed** (ADR 0010).

## `cmd/loopgate/`

- `main.go`
  - constructs socket path `runtime/state/loopgate.sock` under cwd-as-repo-root
  - runs `memory.InspectUnsupportedRawMemoryArtifacts` with warnings to stderr
  - starts `loopgate.NewServerWithOptions` and runs until signal

## `cmd/loopgate-policy-sign/`

- `main.go`
  - reads `core/policy/policy.yaml`
  - signs it with a PKCS#8 PEM-encoded Ed25519 private key supplied by the operator
  - resolves the signer key from `-private-key-file`, then `LOOPGATE_POLICY_SIGNING_PRIVATE_KEY_FILE`, then the default operator path under `os.UserConfigDir()/Loopgate/policy-signing/`
  - `-verify-setup` checks that the embedded trust anchor, current `policy.yaml.sig`, and resolved private key all match before rollout
  - writes `core/policy/policy.yaml.sig`

## `cmd/loopgate-policy-admin/`

- `main.go`
  - validates signed repo policy or an arbitrary policy YAML file against the same strict parser used at runtime
  - explains the current Claude Code tool policy surface, including deny-unknown-tools behavior and per-tool overrides
  - diffs two normalized policy documents so operators can review effective changes before signing
  - renders starter admin policy templates for `strict-mvp` and `developer`
  - hot-applies the already signed on-disk policy to a running local Loopgate instance via `apply`
  - `apply -verify-setup` also verifies the local signer key against the embedded trust anchor before hot reload
  - treats detached signature verification as required for the default repo policy path and optional for ad hoc template files

## `cmd/loopgate-doctor/`

- `main.go`
  - builds offline derived operator reports from local repo state
  - writes diagnostic bundles without touching authoritative audit history
  - can query a running local Loopgate instance for the read-only audit export trust preflight via `trust-check`

## Relationship Notes

- Control plane implementation: `internal/loopgate/loopgate_map.md`

## Important Watchouts

- Loopgate must stay on local Unix socket transport by default (see AGENTS).
- Any remaining legacy runner stdin/stdout JSON is a trust boundary — callers must validate.
