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
- `P3-4` `CHANGELOG.md` exists.
- `P3-5` `context_map.md` is committed and `CONTRIBUTING.md` points at it correctly.

### Product-gap items

- `1` `loopgate init` command
- `2` auth failures in the audit ledger
- `3` approval CLI
- `4` `loopgate-doctor` / operator docs now surface audit-integrity posture and `bootstrap_pending`
- `5` nonce replay retention now matches the 1-hour control-session TTL
- `10` `loopgate-policy-admin explain`
- `11` HMAC checkpoints enabled by default on macOS
- `13` repo `AGENTS.md` exists

### Desktop-review findings closed or stale

- Duplicated signed-header parsing / auth-path duplication
- `sessionMACRotationMaster` unlocked read in auth path
- package-global ledger append cache ownership
- O(n) promotion duplicate scan
- duplicated secret-export heuristic
- MCP cleanup holding `server.mu` across process-liveness probe
- undocumented lock model
- implicit approval state machine
- runtime sandbox residue committed in-tree
- tracked `bin/loopgate` artifact in git
- `MORPH_*` runtime namespace drift
- unbounded UI event buffer claim

Notes:
- The `go 1.25.0 does not exist` finding is treated as stale for this pass. The
  local toolchain is newer and the repo currently tests green under it.
- The `loopgate-admin` binary is still present locally on disk, but it is no
  longer tracked by git. That makes it local hygiene, not a repo-history issue.

## Still open

These are the items that still materially affect first public impression or
ongoing contributor ergonomics.

### Public-repo / launch-hygiene open items

- `P2-5` Add `Makefile`
- `P2-5` Add `.golangci.yml`
- `P3-1` Add a clear `README` limitations section that points readers at the active gap list and review status
- `P3-2` Remove non-shipping multi-node enterprise language from `AGENTS.md` and move it into a future-vision doc
- `P3-3` Add `docs/setup/POLICY_REFERENCE.md`
- Product gap `12` Replace `core/policy/policy.yaml` with a better-commented starter policy
- Product gap `15` Add CI policy-sign verification to `.github/workflows/test.yml`

### Still-open lower-level findings

- `P4-2` Tighten `cmd/loopgate/hooks.go` unknown-field handling
- `P4-3` Document ledger crash/fsync semantics
- Product gap `8` is only partially closed: tag and changelog exist, but there is still no `loopgate` version subcommand/flag
- Product gap `9` is only partially closed: startup prints socket path and audit-integrity mode, but not the fuller structured summary proposed in the review
- `pruneExpiredLocked` is still O(n) over several maps
- `countPendingApprovalsForSessionLocked` is still a linear scan

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

Not quite yet for a first public GitHub impression.

The repo is already in good enough shape to stop large hardening work, but one
short polish pass should happen before shifting attention fully back to product
features.

### Minimum pre-public polish set

1. Add `Makefile`
2. Add `.golangci.yml`
3. Add an honest `README` limitations section with links to the active gap list and this closure report
4. Move non-shipping multi-node language out of `AGENTS.md`
5. Add `docs/setup/POLICY_REFERENCE.md`
6. Add CI policy-sign verification
7. Replace the current personal/dev-flavored starter policy with a cleaner commented default

### Decision

- **Safe to return to product work after the short polish set above lands**
- **Not yet ideal to announce the repo publicly before that pass**

That means the repo is no longer blocked by trust-model or major hardening debt.
It is blocked mainly by documentation honesty, contributor ergonomics, and
first-impression cleanup.
