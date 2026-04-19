# Cmd Map

This file maps **`cmd/`** Go **main** entrypoints. **`cmd/loopgate/`** is the primary server binary, and the remaining binaries are support tools for the active local Loopgate workflow.

Use it when changing:

- how Loopgate binds its socket or loads signed policy
- which binaries are still part of the active local-first product
- flags and startup diagnostics for the active binaries

## Core Role

`cmd/` contains small binaries. **Loopgate** (`cmd/loopgate/`) is the primary shipped server from this repository. The active product-facing surfaces are Loopgate itself, the **HTTP-on-UDS** control plane, and typed local APIs used by direct local operator clients. **In-tree MCP removed** (ADR 0010).

## `cmd/loopgate/`

- `main.go`
  - constructs socket path `runtime/state/loopgate.sock` under cwd-as-repo-root
  - starts `loopgate.NewServerWithOptions` and runs until signal
  - also provides operator subcommands:
    - `setup`
    - `install-hooks`
    - `install-launch-agent`
    - `remove-hooks`
  - `setup` is the guided first-run path: local signer init/reuse, starter policy profile selection, signed policy write, hook install, and optional macOS LaunchAgent install
  - `install-hooks` copies the tracked hook bundle from `claude/hooks/scripts/` into the target Claude config dir and wires the supported hook events into `settings.json`
  - `install-launch-agent` writes a per-repo macOS LaunchAgent plist pointed at the current Loopgate binary and can load it immediately with `launchctl`
  - `remove-hooks` removes only the Loopgate-managed hook entries and leaves copied scripts in place

## `cmd/loopgate-policy-sign/`

- `main.go`
  - reads `core/policy/policy.yaml`
  - signs it with a PKCS#8 PEM-encoded Ed25519 private key supplied by the operator
  - resolves the signer key from `-private-key-file`, then `LOOPGATE_POLICY_SIGNING_PRIVATE_KEY_FILE`, then the default operator path under `os.UserConfigDir()/Loopgate/policy-signing/`
  - trusts the compiled fallback key plus any operator-local public keys under `os.UserConfigDir()/Loopgate/policy-signing/trusted/` (or `LOOPGATE_POLICY_SIGNING_TRUST_DIR`)
  - `-verify-setup` checks that the trusted public key, current `policy.yaml.sig`, and resolved private key all match before rollout
  - writes `core/policy/policy.yaml.sig`

## `cmd/loopgate-policy-admin/`

- `main.go`
  - validates signed repo policy or an arbitrary policy YAML file against the same strict parser used at runtime
  - explains the current Claude Code tool policy surface, including deny-unknown-tools behavior and per-tool overrides
  - diffs two normalized policy documents so operators can review effective changes before signing
  - renders starter admin policy templates for `strict`, `balanced`, and `developer` (still accepting `strict-mvp` as a compatibility alias)
  - hot-applies the already signed on-disk policy to a running local Loopgate instance via `apply`
  - `apply -verify-setup` also verifies the local signer key against the trusted public key set before hot reload
  - treats detached signature verification as required for the default repo policy path and optional for ad hoc template files

## `cmd/loopgate-doctor/`

- `main.go`
  - builds offline derived operator reports from local repo state
  - writes diagnostic bundles without touching authoritative audit history
  - explains one approval or capability request by walking the verified audit ledger for its `approval_request_id` or `request_id`
  - can query a running local Loopgate instance for the read-only audit export trust preflight via `trust-check`
  - `report` includes `ledger_verify.hmac_checkpoints`, including `bootstrap_pending` before the first successful server start creates the default macOS Keychain-backed checkpoint key

## Relationship Notes

- Control plane implementation: `internal/loopgate/loopgate_map.md`

## `cmd/loopgate-ledger/`

- `main.go`
  - verifies the authoritative local audit chain over the active JSONL plus any sealed segments
  - verifies audit HMAC checkpoints too in the shipped macOS-first default posture
  - provides readable `tail -verbose` output for operator/demo review
  - provides `demo-reset` as an explicit local demo-only destructive reset path

## Important Watchouts

- Loopgate must stay on local Unix socket transport by default (see AGENTS).
- Any remaining legacy runner stdin/stdout JSON is a trust boundary — callers must validate.
