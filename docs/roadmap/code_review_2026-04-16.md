# Code Review Findings — Handoff for Codex

**Date:** 2026-04-16
**Reviewer:** External senior-engineer pass (Claude Opus 4)
**Audience:** Codex agent picking up implementation work
**Scope:** Whole-repo review of Loopgate as it stands at commit `3f9ba979`

---

## Status snapshot — 2026-04-16 evening pass

Canonical closure report:
- [`docs/reports/reviews/2026-04-16/review_status.md`](../reports/reviews/2026-04-16/review_status.md)

Latest validated follow-up after that original pass:

- [x] removed the non-Darwin escape hatch; unsupported platforms now fail closed
- [x] removed the Loopgate-mediated model runtime / model connection surface and the dead `internal/model*`, `internal/prompt`, `internal/shell`, and `internal/setup` packages
- [ ] fix host-folder symlink escape in `internal/loopgate/server_host_access_handlers.go`
- [ ] fix `shell_exec` PATH trust in `internal/tools/shell_exec.go`

Checklist status:

- [x] `P1-1` Build `loopgate init` command
- [x] `P1-2` Build approval CLI
- [x] `P2-1` Document lock ordering on `Server` struct
- [x] `P2-2` Fix split-lock pattern in approval creation
- [ ] `P2-3` Decompose `internal/loopgate/` package
  Deferred for post-launch follow-through. State-domain extraction and bounded approval-package extraction are underway, but the full package split is intentionally not treated as a first-public-repo blocker.
- [x] `P2-4` Delete dead directories and tracked binary
  Closed for the tracked repo shape. Local ignored artifacts may still exist on disk outside git.
- [x] `P2-5` Add `Makefile` and `.golangci.yml`
- [x] `P3-1` Surface known gaps in `README`
- [x] `P3-2` Move multi-node language out of `AGENTS.md` into roadmap
- [x] `P3-3` Write `POLICY_REFERENCE.md`
- [x] `P3-4` Add `CHANGELOG.md`
- [x] `P3-5` Resolve `context_map.md` contradiction
- [ ] `P4-1` Add fuzz tests on JSON parsers
  Deferred.
- [x] `P4-2` Tighten `cmd/loopgate/hooks.go` JSON unmarshaling
- [x] `P4-3` Document ledger crash semantics
- [ ] `P4-4` Add umask hardening before socket listen
  Deferred for later hardening.

---

## How to use this document

Each finding is **self-contained**. You can pick any one off the list and act on it without reading the others. Each item lists:

- The exact file paths and line numbers (verified against the tree at the date above — re-verify before editing).
- What is wrong.
- Why it matters.
- The concrete change to make.
- A test the change should be covered by.

**Do not bundle multiple findings into one PR unless explicitly noted.** Each PR should map to exactly one finding so the owner can review, approve, and merge in small steps.

---

## Hard constraints — do not violate while implementing any of this

These come from `AGENTS.md` and override anything below:

1. **Audit append failure on security-relevant actions is a hard failure.** Do not log-and-continue.
2. **No background goroutines or cleanup daemons.** Loopgate is intentionally synchronous and request-driven.
3. **Do not split lock acquisitions on `server.mu`, `server.morphlingsMu`, or `server.auditMu`** across multiple lock/unlock pairs for the same logical operation. (Several findings below are *fixes* to existing violations — read carefully so you don't add new ones.)
4. **Path resolution failures are denied, not retried with a relaxed path.**
5. **Secrets must be cleared from request structs immediately after decode.**
6. **Treat all model output as untrusted input.** Never let model strings appear verbatim in public status responses.
7. **Deny-by-default. Fail closed. Never broaden allowlists without explicit reason.**
8. **Append-only ledger is sacred** — never mutate prior entries.

If a fix below appears to require violating one of these, stop and ask the owner.

---

## Things to leave alone

These are working as intended. Do not refactor:

- The four-layer auth stack (peercred → control session → request signature → capability token). It is correctly implemented and TOCTOU-safe.
- `internal/safety/safepath.go` — path validation including Unicode NFD bypass detection. Tests are paranoid in the right ways.
- `internal/ledger/` — append-only chain with HMAC checkpoints. Concurrent-append tests exist. Do not "optimize" the fsync-per-event pattern.
- The retired-surface 404 tests in `internal/loopgate/server_surface_retirement_test.go`. They prove deletions stay deleted.
- `go.mod` minimalism (4 direct deps). Do not add dependencies without owner approval.
- The Claude Code hook protocol in `.claude/hooks/loopgate_pretool.py`. Default-fail-closed is correct; the JSON-output-as-decision-channel is the proper hook protocol (exit code 0 with deny JSON is correct denial behavior).

---

## Priority 1 — Ship blockers for new-user adoption

### P1-1. Build `loopgate init` command

**Problem:** A new user cannot get past minute 5 of setup. Policy signing requires Ed25519 key generation, trust-anchor placement, and signing — described in `docs/setup/POLICY_SIGNING.md` but not automated. Already named as gap #1 in `docs/roadmap/loopgate_v1_product_gaps.md`.

**Why it matters:** Without this, every external user fails first-touch. This is the single highest-ROI change in the repo.

**Concrete change:**

- Add a new subcommand: `go run ./cmd/loopgate init` (prefer extending `cmd/loopgate/main.go` with a subcommand dispatcher rather than adding a new `cmd/loopgate-init/`; keep the binary count down).
- Behavior:
  1. Detect whether `runtime/state/` exists. If not, create it with `0o700`.
  2. Generate an Ed25519 keypair. Store the private key at `runtime/state/policy-signing/<key_id>.key` (mode `0o600`). Store the public key at the operator trust anchor path used by `internal/config/policy_signing.go`'s `trustedPolicySigningKeys()`.
  3. Sign the default policy (`core/policy/policy.yaml`) with the new key.
  4. Print exactly three lines of confirmation: key ID, socket path, and the next command (`go run ./cmd/loopgate`).
  5. Idempotent: if everything already exists and verifies, exit 0 with "already initialized" — do not regenerate keys.
- Add `--key-id <id>` flag (default to `local-operator-<short-hostname>`).
- Add `--force` flag for re-init (must move existing key to `.bak` first; never silently overwrite).

**Tests:**

- Init on empty `runtime/` succeeds and produces a valid signed policy.
- Init twice in a row is idempotent (second run does not regenerate).
- Init with `--force` rotates the key and re-signs.
- After init, `go run ./cmd/loopgate-policy-admin validate` succeeds.
- After init, `go run ./cmd/loopgate-policy-sign -verify-setup` succeeds.

**Docs to update:** `docs/setup/GETTING_STARTED.md` step 3 should become `loopgate init && loopgate-policy-admin validate`. `README.md` Quick start should reflect this.

---

### P1-2. Build approval CLI

**Problem:** When Loopgate's hook returns "ask," there is no way for the operator to approve from the terminal. Operators must script the HTTP API directly. Already named as gap #3 in `docs/roadmap/loopgate_v1_product_gaps.md`.

**Why it matters:** Approvals are the core of the governance story. Without a CLI, the product is unusable for the human-in-the-loop case.

**Concrete change:**

- Extend `cmd/loopgate-policy-admin/main.go` with three new subcommands:
  - `loopgate-policy-admin approvals list` — list pending approvals (id, session, tool, requested-at, requester).
  - `loopgate-policy-admin approvals approve <id> [--reason <text>]` — grant.
  - `loopgate-policy-admin approvals deny <id> [--reason <text>]` — deny.
- All three connect over the existing Unix socket using the same auth flow `internal/loopgate/client.go` already implements. Reuse that client; do not write a parallel HTTP wire path.
- Output should be tabular for `list` and a single confirmation line for `approve`/`deny`.
- `approve` and `deny` should print the resulting audit ledger entry's hash so operators can correlate.

**Tests:**

- Integration test: create a pending approval via the API, list it, approve it, verify state transition in the ledger.
- Deny path equivalent.
- Auth failure path (wrong session) returns a clear error, not a panic.

**Docs to update:** `docs/setup/OPERATOR_GUIDE.md` — add a section "When Loopgate asks for approval" with examples.

---

## Priority 2 — Code health debt that will hurt later

### P2-1. Document lock ordering on `Server` struct

**Problem:** `internal/loopgate/server.go` defines a `Server` struct with **9 mutexes**: `mu`, `auditMu`, `uiMu`, `connectionsMu`, `modelConnectionsMu`, `hostAccessPlansMu`, `providerTokenMu`, `pkceMu`, `policyRuntimeMu`. There is no documented acquisition order. This is a future deadlock waiting to happen.

**Why it matters:** When a deadlock does happen, the diagnosis will take hours instead of minutes. The fix is to define and document the order *now*, while context is fresh.

**Concrete change:**

- Read every `Lock()`/`RLock()` site for these 9 mutexes. List the acquisition order observed in the existing code.
- If the observed order is consistent (i.e., no two call paths acquire two of these locks in opposite orders), document it as a comment block above the `Server` struct definition. Format:
  ```go
  // Lock ordering invariant: when holding multiple of the following, acquire in this order.
  // Violations risk deadlock.
  //   1. mu               — primary state (sessions, tokens, approvals)
  //   2. auditMu          — audit append serialization
  //   3. uiMu             — UI projection
  //   ...
  ```
- If the observed order is **inconsistent** (one path does A→B, another does B→A), do not silently fix — report this to the owner as a bug. It is a real deadlock window.

**Tests:** This is a documentation change with no behavior change. No new tests required, but existing tests must continue to pass.

---

### P2-2. Fix split-lock pattern in approval creation

**Problem:** `internal/loopgate/server.go` lines ~766–852 (verify exact range before editing) acquires `server.mu`, performs capacity checks, releases the lock, calls `logEvent()` to audit the approval, then re-acquires `mu` to delete the approval if the audit append failed. Between unlock and re-lock, the approval is **visible to concurrent readers**. This violates `AGENTS.md` §505.2.

**Why it matters:** Audit-trail ambiguity. A UI handler reading `server.approvals` between the unlock and the rollback can see and act on an approval that never officially existed. Not a security bypass, but a debugging nightmare.

**Concrete change (pick one — discuss with owner before implementing):**

Option A — slow but correct: hold `server.mu` across the `logEvent()` call. This violates the soft rule against holding locks across I/O, but `logEvent()` is fast (local fsync) and the correctness gain is worth it.

Option B — staged visibility: introduce a `tentativeApprovals` map. Approvals are inserted there first under the lock, audit is appended, then on success the approval is moved to `approvals` under a second lock acquisition. Readers of `approvals` only ever see fully-audited approvals. This is more code but preserves the no-I/O-under-lock convention.

The owner's call. Default to Option A unless owner says otherwise — it's the smaller change.

**Same fix applies to:** `internal/loopgate/control_session_recovery.go` lines ~92–136 (approval cancellation rollback). Apply the same pattern chosen above.

**Tests:**

- Concurrent test: two goroutines race to read `server.approvals` while another goroutine creates and rolls back. Verify readers never observe a rolled-back approval.
- Existing approval creation tests must continue to pass.

---

### P2-3. Decompose `internal/loopgate/` package

**Problem:** This single package is ~24k SLOC across 80+ handler files plus 47 test files. It owns the entire request lifecycle, all 9 mutexes, the approval state machine, the session table, the audit dispatch, and the model API bridge. It is the godfile of godpackages.

**Why it matters:** Every change to any handler requires understanding the whole package. New contributors will not survive it. Test compile times suffer. The package boundary is doing zero work — everything inside is in the same lexical scope.

**Concrete change:** This is a multi-PR effort. **Do not attempt in one PR.** Sequence:

1. PR 1: Extract `internal/loopgate/auth/` — move `request_auth.go`, `pkce.go`, `peercred_*.go`, `session_*.go`. The `Server` struct still owns the state, but auth helpers live in their own package.
2. PR 2: Extract `internal/loopgate/ledgerbridge/` — move `server_audit_*.go` (audit dispatch, not the ledger itself).
3. PR 3: Extract `internal/loopgate/handlers/capabilities/` — move capability-execution handlers.
4. PR 4: Extract `internal/loopgate/handlers/sandbox/` — move sandbox handlers.
5. PR 5: Continue per concern.

Each PR must:
- Pass all tests with `-race`.
- Not change behavior.
- Not change the public API surface.
- Update package map docs (`internal/loopgate/<package>/<package>_map.md`).

**Stop after each PR and wait for owner review.** If a PR uncovers a hidden coupling that requires breaking the structure, surface it before continuing.

---

### P2-4. Delete dead directories and tracked binary

**Problem:** Three empty placeholder directories add cognitive noise, and a 23 MB build artifact is checked into git.

**Why it matters:** Every contributor who clones this repo wastes 30 seconds wondering whether `internal/orchestrator/` is something they need to know about.

**Concrete change (single PR):**

- `git rm -r internal/orchestrator/` (verified empty)
- `git rm -r internal/relationhints/` (verified empty)
- `git rm -r internal/threadstore/` (verified empty)
- `git rm bin/loopgate` (verified tracked, 23 MB Mach-O binary)
- Add `bin/` to `.gitignore`.
- Run `go mod tidy` to drop the stale `gopkg.in/check.v1` indirect dependency that no longer has a real consumer.

**Tests:** Existing tests must pass. `go build ./...` must still succeed.

---

### P2-5. Add `Makefile` and `.golangci.yml`

**Problem:** No Makefile, no linter config beyond `go vet`. Builds depend on developer memory.

**Why it matters:** A Makefile signals professionalism and prevents footgun commands. A linter catches a class of bugs that no human review will catch consistently.

**Concrete change:**

Create `Makefile` at repo root with these targets:

```
build:        go build -o bin/loopgate ./cmd/loopgate
test:         go test ./...
test-race:    go test -race -count=1 ./...
lint:         golangci-lint run
vuln:         ./scripts/govulncheck.sh
ship-check:   $(MAKE) test-race && $(MAKE) lint && $(MAKE) vuln
clean:        rm -rf bin/ runtime/state/loopgate.sock
```

Create `.golangci.yml` enabling:
- `errcheck`
- `staticcheck`
- `ineffassign`
- `unused`
- `govet`
- `gosec` (security-focused, appropriate for this project)

Do not enable opinionated style linters (`gofumpt`, `wsl`, `lll`) without owner approval — they will produce noise.

**Tests:** Run `make ship-check` and fix any new findings the linters surface. **If `gosec` reports something that requires a real code change, do not silently fix it — list it for owner review separately.**

---

## Priority 3 — Documentation and release polish

### P3-1. Surface known gaps in README

**Problem:** `README.md` describes Loopgate as if it's polished. `docs/roadmap/loopgate_v1_product_gaps.md` lists 17 known gaps. The README does not link to it.

**Concrete change:** Add a new section to `README.md` after "Active product surface":

```markdown
## Known limitations

Loopgate is under active hardening. The following are known gaps you will hit:

- No `loopgate init` command — first-time setup requires manual key generation
- No CLI for approving pending requests — use the HTTP API directly
- Policy authoring requires reading example YAML; no schema reference yet

See [docs/roadmap/loopgate_v1_product_gaps.md](./docs/roadmap/loopgate_v1_product_gaps.md) for the full list.
```

When P1-1 and P1-2 ship, update this section accordingly.

---

### P3-2. Move multi-node language out of AGENTS.md into roadmap

**Problem:** `AGENTS.md` describes a multi-node enterprise architecture (admin node, local node, mTLS, IDP integration) that does not exist in code. This creates a credibility gap — a careful reader sees the design doc claiming features the code does not provide.

**Why it matters:** Owner has confirmed multi-node is the eventual goal but currently aspirational. AGENTS.md should describe the *current* enforcement model. Aspirational architecture belongs in a separate doc.

**Concrete change:**

- Create `docs/roadmap/multi_node_enterprise_vision.md`. Move the "Multi-tenancy model," "Node roles," "Tenant isolation," "Admin node authority," and "Offline behavior" sections out of `AGENTS.md` into this new file. Add a header noting the doc describes future direction, not current implementation.
- In `AGENTS.md`, replace those sections with a single paragraph: "Loopgate is currently a single-node local governance engine. A future multi-node enterprise architecture is described in [docs/roadmap/multi_node_enterprise_vision.md]. The rules below describe single-node enforcement only."
- Update `AGENTS.md`'s "Last updated" date.

**Tests:** N/A (docs change).

---

### P3-3. Write `POLICY_REFERENCE.md`

**Problem:** Policy authoring requires reading `internal/policy/checker.go` to understand whether `allowed_command_prefixes` is exact-match, prefix-match, or regex.

**Concrete change:** Create `docs/setup/POLICY_REFERENCE.md` with one section per tool, listing every accepted YAML field, its type, and its matching semantics. Use the actual policy struct definitions in `internal/policy/` as the source of truth. Include a complete worked example for each tool.

**Tests:** N/A (docs change). After writing, sanity-check by hand-evaluating one example against the policy checker.

---

### P3-4. Add CHANGELOG.md

**Problem:** No CHANGELOG. Users cannot tell what version they are running or what changed.

**Concrete change:** Create `CHANGELOG.md` at repo root following Keep-a-Changelog format. Seed it with a single `## [Unreleased]` section. The owner will manage entries going forward; you only need to create the file with a clear convention. Add a `## [0.x.0] — 2026-04-XX — Initial review baseline` entry capturing the current state.

---

### P3-5. Resolve `context_map.md` contradiction

**Problem:** `CONTRIBUTING.md` requires reading `context_map.md` before opening a PR. `DOCUMENTATION_SCOPE.md` says `context_map.md` is local-only and gitignored. New contributors will hit this contradiction immediately.

**Concrete change:** Either commit a public `context_map.md` (preferred — it's a useful onboarding doc) or remove the requirement from `CONTRIBUTING.md`. Owner's call. Default: commit a generic public version.

---

## Priority 4 — Hardening that can wait

### P4-1. Add fuzz tests on JSON parsers

**Problem:** Loopgate parses untrusted JSON in many places. No fuzz tests exist.

**Concrete change:** Add `go test -fuzz` targets for:
- The capability request decoder in `internal/loopgate/request_body_runtime.go`
- The hook input decoder in `cmd/loopgate/hooks.go`
- The policy YAML loader in `internal/config/`

Run each fuzz target for at least 5 minutes locally. File any crashes as separate findings.

---

### P4-2. Tighten `cmd/loopgate/hooks.go` JSON unmarshaling

**Problem:** `UnmarshalJSON` on the settings type does not use `DisallowUnknownFields`. Inconsistent with `request_body_runtime.go` which does.

**Why it matters:** Low — settings.json is user-controlled, so an attacker who can write to it has already won. But the inconsistency is a code smell and a cheap fix.

**Concrete change:** In `cmd/loopgate/hooks.go` around lines 397–417, replace the `json.Unmarshal` calls with a `json.NewDecoder(bytes.NewReader(rawBytes))` chain that calls `DisallowUnknownFields()` before `Decode()`.

**Tests:** Add a regression test that settings.json with an unknown field is rejected.

---

### P4-3. Document ledger crash semantics

**Problem:** A crash between `enc.Encode()` and `syncLedgerFileHandle()` in `internal/ledger/ledger.go` (around lines 140–147) can lose the last unfsync'd event. The chain stays valid, but one event is gone.

**Why it matters:** Operators need to know this is the design contract, not a bug.

**Concrete change:** Add a comment block above the `Append` function explaining the fsync semantics and the one-event-loss guarantee on hard crash. Add a paragraph to `docs/setup/LEDGER_AND_AUDIT_INTEGRITY.md` saying the same thing.

**Tests:** N/A (documentation only).

---

### P4-4. Add umask hardening before socket listen

**Problem:** `internal/loopgate/server.go` around lines 509–516 calls `net.Listen` then `os.Chmod(socket, 0o600)`. Sub-millisecond TOCTOU window between the two.

**Why it matters:** Low — for a single-user dev machine this is fine. For shared multi-user systems it is a real (if narrow) window.

**Concrete change:** Before `net.Listen`, call `syscall.Umask(0o077)` and capture the prior umask. After `Chmod`, restore it. Document why in a comment.

**Tests:** Verify the socket file mode is `0o600` immediately after listen, even with a permissive ambient umask.

---

## Out-of-scope context Codex should know

### Strategic direction (from owner conversation 2026-04-16)

- **Hooks model is the moat.** Owner explicitly tried building Loopgate as an MCP server + proxy and rejected it as "theater not governance" because it only gated MCP-routed tool calls, not all tool calls. **Do not propose making Loopgate an MCP server as a primary integration path.** Out-of-tree MCP→HTTP forwarders are fine; reintroducing in-tree MCP is not.
- **Second harness candidate is pi.dev.** Codex may be asked to build a pi.dev integration using the same hook-equivalent pattern. Do not start that work without explicit instruction.
- **Multi-node enterprise is the eventual goal but not current.** See P3-2.

### Owner workflow

The owner does not write code by hand. They direct AI agents (Claude Code, Cursor, Codex). When you propose changes, write the actual diff — do not write "you should change X" and leave the editing to them. They will copy your output and apply it via tooling.

When in doubt about scope, **default to the smaller change** and ask. Do not refactor adjacent code "while you're in there." Do not add abstractions for hypothetical future needs.

### Pre-ship state hygiene

`runtime/` is gitignored but the local filesystem may contain leftover state from past dev runs (audit logs, memory partitions, sandbox fixtures from other projects like `haven_cli/`). When packaging a release, all of `runtime/` must be wiped. Do not commit anything inside `runtime/`.

---

## Verification before marking any finding complete

For every PR:

1. `go test -race -count=1 ./...` passes.
2. Existing AGENTS.md invariants are not weakened.
3. The PR addresses **exactly one** finding from this document (unless owner approves bundling).
4. The PR description references the finding ID (e.g., "Implements P1-1").
5. Documentation that describes the touched area is updated in the same PR.
