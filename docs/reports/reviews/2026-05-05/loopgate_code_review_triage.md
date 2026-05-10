---
status: active
owner_area: loopgate-review-remediation
tags:
  - code-review
  - security
  - refactor
  - production-readiness
related_reviews:
  - /Users/adalaide/dev/code-reviews/2026-05-05-loopgate-code-review.md
  - /Users/adalaide/dev/code-reviews/2026-05-05-loopgate-strategic-assessment.md
related_docs:
  - ../../../roadmap/refactor_and_agent_first_docs_plan.md
  - ../../../contributor/coding_standards.md
  - ../../../../AGENTS.md
---

# Loopgate code review triage - 2026-05-05

This note validates the external 2026-05-05 Loopgate code review against the
current `main` branch after the CodeQL remediation PRs were merged.

## Summary

The review is directionally useful. The highest-confidence findings are:

- F-15: versioned install roots orphan audit history across upgrades.
- F-07: ledger canonicalization needs golden tests and unsafe integer guards.
- F-18: `internal/loopgate` remains too large and should continue shrinking
  into sibling `internal/...` packages.
- F-11: Linux secret-store posture is not production-grade.
- F-01: PID-bound token rotation needs a clearer denial code and docs.

Some findings need regrading because the current code has caps or behavior that
the review either missed or described imprecisely.

## Validated findings

| ID | Triage | Notes |
| --- | --- | --- |
| F-01 | Valid | PID/EPID-bound capability tokens intentionally fail after client process rotation, but the denial is still generic. Add a specific denial code and operator docs. |
| F-02 | Needs focused review | The socket directory mode already limits exposure. The process-global umask concern is plausible, but the immediate risk appears lower than the original P1. |
| F-03 | Valid, low priority | Authorization header parser has unnecessary denial-code shape differences. This is cleanup, not a production blocker. |
| F-04 | Partially valid | Session and approval caps exist. Remaining concern is prune work under `server.mu`, lack of visible counters, and hot-path latency under saturation. Regrade below P0 unless benchmarks prove otherwise. |
| F-05 | Valid | Policy signing trust-dir env override is security-relevant and should be loud, owner/mode checked, and tested. |
| F-06 | Valid, low priority | `testing.Testing()` in production code is awkward and removable, but not an urgent runtime defect. |
| F-07 | Remediated | Canonicalization now has golden bytes/hash coverage and rejects integer payload values outside JSON's safe integer range before hashing. Sequence parsing also rejects fractional and unsafe sequence numbers. |
| F-08 | Valid | Ledger append success with post-write `Stat` failure is probably correct, but the warning should be observable. |
| F-09 | Valid | HMAC checkpoint key rotation is missing; incident response story is incomplete without it. |
| F-10 | Needs design decision | `internal/audit` is thin, but it encodes must-persist vs warn-only policy. Either document the boundary or fold it into `internal/ledger`. |
| F-11 | Valid with correction | `secure` currently resolves to the local-dev store path, while platform-specific secure backends can stub/fail closed. The broader Linux posture problem remains valid. |
| F-12 | Valid | Text redaction is best effort. Structured-field redaction is the real boundary and should be easier to enforce. |
| F-13 | Plausible | Repeated symlink resolution may be a throughput cost. Validate with benchmarks before optimizing. |
| F-14 | Plausible | `openat` traversal shape is correct. The suggested flag/test cleanup is reasonable but not urgent. |
| F-15 | Remediated | Install wrapper now points `LOOPGATE_REPO_ROOT` at a stable managed state root while binaries remain versioned. The installer migrates legacy per-version `runtime/`, `core/`, and `config/` into the stable root on first upgrade. |
| F-16 | Valid | `--yes` makes quickstart install and load the LaunchAgent. That should require explicit consent or much louder output. |
| F-17 | Valid | Checksum-from-same-release verifies corruption, not release provenance. Archive signatures are the stronger story. |
| F-18 | In progress | `internal/auditruntime`, `internal/protocol`, `internal/approvalruntime`, `internal/hostaccess`, and `internal/controlruntime` now sit outside `internal/loopgate` with one-way imports. `internal/controlruntime` currently owns session MAC derivation/storage primitives, nonce replay persistence stores, pure replay table helpers, and sliding-window rate-limit math. The top-level `internal/loopgate` file count is still too high, so continue extracting cohesive runtime domains. |
| F-19 | Valid | Lint coverage is intentionally small today. Expand in a controlled PR because new linters will surface many findings. |
| F-20 | Valid, low priority | Legacy `MORPH_REPO_ROOT` rejection test is useful but should be clearly marked as a temporary regression guard. |
| F-21 | Valid, low priority | Legacy nonce snapshot fallback should have an explicit removal target. |
| F-22 | Mostly addressed | Existing goroutines appear bounded or CLI-local. Add comments/tests where useful rather than treating this as a violation. |
| F-23 | Needs release review | cgo/darwin packaging should be checked against the release workflow before changing build tags. |

## Remediation order

1. Continue F-18 with low-risk package moves that preserve dependency direction.
2. Fix F-01 and F-16 for clearer operator behavior.
3. Reassess F-04 with the existing benchmark harness and add control-plane
   state counters if the lock/prune path shows up under load.

## God-package extraction rule

Prefer sibling runtime packages over deep subpackages under
`internal/loopgate`. The dependency direction is:

```text
internal/loopgate -> internal/<runtime-package>
```

The extracted package must not import `internal/loopgate`.

The first safe remediation slice is moving the already-extracted audit runtime
from `internal/loopgate/auditruntime` to `internal/auditruntime`. That change is
mechanical and keeps `Server.logEvent` as the compatibility facade.
