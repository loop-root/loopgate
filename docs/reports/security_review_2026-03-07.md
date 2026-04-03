**Last updated:** 2026-03-24

# Security Review - 2026-03-07

Scope: Loopgate control-plane implementation in this repository (historical snapshot).

## 1) Executive status

Reality check:

- single-instance lock is implemented (`runtime/.morph.lock`)
- policy checker is operation-based (no hardcoded tool-name write list)
- orchestrator ledger logging uses shared secrets redaction helpers
- ledger append failures in orchestrator logger are surfaced (callback/stderr)
- secrets subsystem exists with fail-closed secure backend stub and env read-only backend

## 2) Findings

### High

1. Slash-command write path is less hardened than orchestrator `fs_write`

- Location: `internal/shell/commands.go` (`/write`)
- Behavior: SafePath validation followed by `os.WriteFile`
- Gap: does not use no-follow open path now present in `internal/tools/fs_write.go`
- Impact: residual TOCTOU/symlink race exposure in slash command path

Recommendation:
- route slash `/write` through the same hardened helper used by `internal/tools/fs_write`

### Medium

2. Ledger is append-only but not tamper-evident

- Location: `internal/ledger/ledger.go`
- Behavior: append JSONL with schema version, no hash chaining/signature
- Impact: local file edits cannot be cryptographically detected

Recommendation:
- add per-event hash chain and verification utility

3. Parser remains freeform tag-based

- Location: `internal/orchestrator/parser.go`
- Behavior: string scanning for `<tool_call>...</tool_call>`
- Impact: malformed/ambiguous payload surface remains larger than strict structured responses

Recommendation:
- migrate to explicit structured model response schema

4. No ledger/distillate rotation limits

- Location: `internal/ledger`, `internal/memory`
- Impact: unbounded growth and operational risk over long runtimes

Recommendation:
- size caps + rotation + retention policy

### Low

5. Distillate ID second-resolution collision risk

- Location: `internal/memory/distillate.go`
- Behavior: `dist-YYYYMMDDHHMMSS`

Recommendation:
- add nanosecond or random suffix

## 3) Controls verified in code

- operation-based policy enforcement (`internal/policy/checker.go`)
- single-instance lock (`cmd/morph/main.go`)
- crash-safe state writes (`internal/state/state.go`)
- shared redaction in orchestrator logger (`internal/orchestrator/logger.go`, `internal/secrets/redact.go`)
- fail-closed secret backend behavior (`internal/secrets/stub_secure_store.go`)
- strict backend matching constants (`internal/secrets/types.go`)

## 4) Test-backed checks present

- orchestrator logger redaction and append-failure surfacing tests
- secret leakage prevention and backend matching tests
- `.gitignore` runtime artifact pattern test

## 5) Alignment notes

Previously stale documentation claimed multi-instance locking was not implemented and treated TOCTOU as only conceptual. Current docs were updated to match actual implementation and residual gaps.
