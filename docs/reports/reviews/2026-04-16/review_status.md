**Last updated:** 2026-04-16

# Review Closure Status — 2026-04-16

This document is the concrete closure pass for the current public-release prep.
It consolidates the three review sources now archived together under this folder:

- [Claude Opus handoff review](./claude_opus_code_review.md)
- [Senior readiness walkthrough](./walkthrough.md)
- [Senior engineering code review](./loopgate_code_review.md)

Use this file as the current truth for what is closed, what is still open, what
is intentionally deferred, and whether the repo is safe to return to product
work.

## Closed

These findings are either fully implemented or stale because the repo has
already moved past them.

### In-repo review items

- `P1-1` `loopgate init` shipped.
- `P1-2` approval CLI shipped.
- `P2-1` lock ordering and lock-domain reasoning are documented inline and in [`docs/design_overview/loopgate_locking.md`](../../../design_overview/loopgate_locking.md).
- `P2-2` approval creation / cancellation rollback is now atomic to readers.
- `P2-4` tracked dead directories and tracked `bin/loopgate` repo-shape issue are closed.
- `P2-5` `Makefile` and `.golangci.yml` now exist, and the lint gate is wired into CI.
- `P3-1` `README.md` now has a clear limitations section with links to the active gap list and this closure report.
- `P3-2` non-shipping multi-node language moved out of `AGENTS.md` into [`docs/roadmap/future_enterprise_direction.md`](../../../roadmap/future_enterprise_direction.md).
- `P3-3` [`docs/setup/POLICY_REFERENCE.md`](../../../setup/POLICY_REFERENCE.md) now exists.
- `P3-4` `CHANGELOG.md` exists.
- `P3-5` `context_map.md` is committed and `CONTRIBUTING.md` points at it correctly.
- `P4-2` Claude settings hook JSON now rejects unknown nested fields instead of silently accepting them.
- `P4-3` ledger crash / append-sync semantics are now documented for operators and contributors.

### Product-gap items

- `1` `loopgate init` command
- `2` auth failures in the audit ledger
- `3` approval CLI
- `4` `loopgate-doctor` / operator docs now surface audit-integrity posture and `bootstrap_pending`
- `5` nonce replay retention now matches the 1-hour control-session TTL
- `10` `loopgate-policy-admin explain`
- `11` HMAC checkpoints enabled by default on macOS
- `12` `core/policy/policy.yaml` is now a documented strict starter policy instead of a personal development profile
- `13` repo `AGENTS.md` exists
- `14` `Makefile`
- `15` CI policy signing check

### Desktop-review findings closed or stale

- Duplicated signed-header parsing / auth-path duplication
- `sessionMACRotationMaster` unlocked read in auth path
- package-global ledger append cache ownership
- O(n) promotion duplicate scan
- duplicated secret-export heuristic
- MCP cleanup holding `server.mu` across process-liveness probe
- undocumented lock model
- implicit approval state machine
- version subcommand / build info gap
- startup summary gap
- runtime sandbox residue committed in-tree
- tracked `bin/loopgate` artifact in git
- `MORPH_*` runtime namespace drift
- non-Darwin escape hatch removed; unsupported platforms now fail closed at startup
- Loopgate-mediated model runtime / model connection surface removed from the control plane
- dead `internal/model`, `internal/modelruntime`, `internal/prompt`, `internal/shell`, and `internal/setup` packages removed
- unbounded UI event buffer claim

Notes:
- The `go 1.25.0 does not exist` finding is treated as stale for this pass. The
  local toolchain is newer and the repo currently tests green under it.
- The `loopgate-admin` binary is still present locally on disk, but it is no
  longer tracked by git. That makes it local hygiene, not a repo-history issue.

## Still open

These are the current first-public blockers from the latest validated desktop
review round.

### Security / runtime blockers

- host-folder symlink escape in `internal/loopgate/server_host_access_handlers.go`
  - current granted-folder path checks are lexical before later filesystem operations
  - symlinks inside a granted folder can still escape the granted root
- `shell_exec` PATH trust issue in `internal/tools/shell_exec.go`
  - allowlists are based on the command name rather than the resolved executable path
  - PATH shadowing can bypass the intended command allowlist if `shell_exec` is enabled

### Local-only cleanup worth doing before screenshots or packaging

- remove ignored root binary `loopgate-admin`
- remove ignored local `output/` clutter if it is not serving an active purpose

Those are not git blockers, but they do affect the feeling of local cleanliness.

## Defer

These are real items, but they are not the best use of pre-announcement time.

### Structural refactors

- `P2-3` full `internal/loopgate` package breakup
- continuing the god-package reduction beyond the current state-domain and approval-package seams

### Additional hardening / coverage

- `P4-1` fuzz tests on JSON / YAML parsers
- `P4-4` umask hardening before socket listen
- Product gap `7` build-tagged end-to-end integration test
- Product gap `16` policy-sign coverage gate in CI
- `pruneExpiredLocked` is still O(n) over several maps
- `countPendingApprovalsForSessionLocked` is still a linear scan
- audit-chain crash-recovery integration testing beyond the current unit-level coverage

### Scale / enterprise / future-scope concerns

- hook endpoint rate limiting (product gap `6`)
- metrics / saturation surfacing for enterprise-style load
- audit append throughput improvements
- replacing string-based fs-read rate-limit dispatch with registry metadata
- removing legacy nonce snapshot fallback
- Pi adapter (product gap `17`)
- full remote / enterprise architecture follow-through

## Safe to return to product work

Not yet.

The short first-public polish pass is now landed. The repo is in good enough
shape to return to product work without carrying misleading public docs or
missing contributor basics.

### Decision

- **Not yet safe to return to product work**
- **Not yet safe to announce the repo publicly**

The repo is much closer, and the retired model/runtime surface is now gone, but
the two validated security findings above should be fixed before we treat the
repo as ready for first public release.
