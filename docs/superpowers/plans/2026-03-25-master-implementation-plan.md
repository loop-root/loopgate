**Last updated:** 2026-03-24

# Master implementation plan (historical) — Loopgate

> **Archive / context:** This document captured a **desktop-first ship** slice (Wails bundle + `.dmg`) from an earlier product framing. **This repository’s focus is Loopgate** as the control plane; MCP and proxy are the primary integration surfaces. Keep the **engineering tasks and file references**; treat **distribution and demo** steps as optional or superseded unless you are explicitly maintaining the in-repo **`cmd/haven/`** reference shell.

> **For agentic workers:** If executing tasks from this plan, use superpowers:subagent-driven-development or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax for tracking.

**Date:** 2026-03-25 (revised)  
**Status:** Historical — use `docs/roadmap/roadmap.md` and current `sprints/*.md` for active direction.

**Goal (original):** tighten the **reference desktop loop** (organizer + memory + approvals) and packaging, in support of a governed local assistant experience. Tasks were ordered by **dogfood impact**, not architectural completeness.

**Guiding principle:** If a task doesn't make the **reference organizer loop** better, safer, or easier to demo, it moves to Post-Launch.

**Tech stack:** Go 1.24, Loopgate (control plane), in-repo **Wails/React reference** under `cmd/haven/`, `internal/tcl`, `internal/loopgate`, `internal/sandbox`, `internal/secrets`, `internal/safety`, append-only JSONL + state snapshots, Go tests via `go test`.

### v1 transport (explicit product decision)

**Local clients ↔ Loopgate v1 use HTTP** on the **local control-plane binding** (Unix domain socket; see RFC 0001 local transport profile). **Apple XPC is not on the v1 critical path**—it was deferred to reduce engineering cost and ship time. Optional XPC or other Mach-bound hardening remains a **post-launch** exploration, documented in `docs/rfcs/0001-loopgate-token-policy.md` and `docs/loopgate-threat-model.md`, not a prerequisite for packaged installs.

---

## Supersedes

This plan consolidates and replaces:

- `docs/superpowers/plans/2026-03-23-tcl-conflict-anchor-implementation.md`
- `docs/superpowers/plans/claude_feedback-2026-03-23-26.md`
- `docs/superpowers/plans/2026-03-24-memory-and-hardening-consolidated-plan.md`
- `docs/superpowers/plans/gemini_feedback-2026-03-24.md`
- `docs/superpowers/plans/gemini_enterprise_and_tcl_strategy.md`

A legacy desktop capability MVP plan (removed from this repo; archived upstream) is **completed**. The TCL conflict anchor design spec (`specs/2026-03-23-tcl-conflict-anchor-design.md`) remains the design reference for TCL work. **v1 stays on HTTP** over the local socket per above; optional XPC hardening is backlog only (`docs/loopgate-threat-model.md`).

## Scope and constraints

- Loopgate remains the authority boundary.
- Natural language never creates authority.
- No fallback should silently weaken path, audit, or memory-safety rules.
- Raw model-originated strings must not become trusted projection state.
- Do not commit unless the user explicitly requests it.
- Prefer the smallest invariant-preserving change at each step.
- **New constraint:** If a task doesn't affect the first 100 users' experience, it moves to Post-Launch.

---

## Phase overview and sequencing

| Phase | Focus | Timeline | Tasks |
|-------|-------|----------|-------|
| **Phase 1: Ship Blockers** | Bugs that would embarrass us during dogfooding or demo | **Week 1–2** | Tasks 1–5 |
| **Phase 2: Memory That Works** | Make "remember my name" reliable for the first user | **Week 2–3** | Tasks 6–7 |
| **Phase 3: Ship Prep** | .dmg, first-run experience, persona tuning, demo | **Week 3–5** | Tasks 8–11 |
| **Phase 4: Dogfood & Fix** | Use it yourself every day, fix what breaks | **Week 5–7** | Task 12 |
| **Post-Launch** | Everything else — architecture perfection, optional XPC-class hardening, enterprise | **After ship** | Tracked below |

**Key change from the previous plan:** TCL conflict anchors (previously Phase 1, Tasks 1–4), **optional** XPC-style transport work (previously Phase 5), and most defense-in-depth hardening (previously Phase 4) are moved to Post-Launch. **v1 intentionally standardizes on HTTP** on the local control plane (not XPC) to save engineering cost; remaining items are correct work but they don't affect the first user on the v1 path.

---

## Review provenance

| Source | Shorthand |
|--------|-----------|
| Claude (Downloads review) | **CR** |
| Gemini (code review) | **GR** |
| ChatGPT/Claude (TCL review) | **TR** |
| Claude (consolidated review, 2026-03-25) | **MR** |

---

## Phase 1: Ship Blockers

These are bugs that would bite during dogfooding or be visible during a demo. Fix them first.

### Task 1: Make continuity mutation ordering audit-safe

**Source:** CR S1, GR Finding 2, MR F10-F12 — confirmed by all reviewers as the top correctness risk.
**Why ship-blocking:** If audit and memory state diverge during normal use, the system's core integrity guarantee is broken. A reviewer or journalist testing the audit trail would find orphaned entries.

**Files:**
- Create: `internal/loopgate/continuity_mutation_ordering_test.go`
- Modify: `internal/loopgate/continuity_memory.go`
- Modify: `internal/loopgate/continuity_runtime.go`
- Modify: `internal/loopgate/continuity_memory_test.go`

- [x] **Step 1: Write the failing ordering tests**

```go
func TestMutateContinuityMemory_DoesNotLeaveReplayableMutationWhenAuditFails(t *testing.T)
func TestMutateContinuityMemory_SaveFailureDoesNotCreateAmbiguousDurableState(t *testing.T)
func TestContinuityReplay_RejectsOrRepairsOrphanedMutationSequence(t *testing.T)
```

- [x] **Step 2: Run to verify they fail**

Run: `go test ./internal/loopgate/... -run 'Test(MutateContinuityMemory|ContinuityReplay_)' -count=1`

Expected: FAIL.

- [x] **Step 3: Implement the smallest safe ordering fix**

Choose one conservative approach and document it:

- either make durable continuity append contingent on successful audit
- or add explicit orphan-marking / startup reconciliation

Also ensure multi-file mutation events don't leave partially applied state.

- [x] **Step 4: Run to verify they pass**

Expected: PASS.

---

### Task 2: Fix secret redaction quoted-value leak

**Source:** MR S8, GR Finding 6.
**Why ship-blocking:** If a user pastes `password="my secret key"` into chat and the redaction leaks `secret key"` into the audit log, the security story is undermined.

**Files:**
- Modify: `internal/secrets/redact.go`
- Modify: `internal/secrets/secrets_test.go` (plan originally said `redact_test.go`; tests live alongside package tests)

- [x] **Step 1: Write the failing redaction test**

```go
func TestRedactText_QuotedValueWithSpaces(t *testing.T)
func TestRedactText_BasicAuthToken(t *testing.T)
```

Assert that `RedactText("password=\"my secret key\"")` does not contain `secret key` in the output. Assert that `Basic dXNlcjpwYXNz` is redacted alongside `Bearer` tokens.

- [x] **Step 2: Run to verify they fail**

Run: `go test ./internal/secrets/... -run 'TestRedactText_(QuotedValue|BasicAuth)' -count=1`

Expected: FAIL.

- [x] **Step 3: Add quoted-value and Basic auth redaction patterns**

Add patterns for double-quoted and single-quoted values. Place before the existing unquoted pattern. Add `Basic` auth alongside `Bearer`.

- [x] **Step 4: Run to verify they pass**

Expected: PASS.

---

### Task 3: Add `MarshalJSON` defense-in-depth to `ModelConnectionStoreRequest`

**Source:** MR F18.
**Why ship-blocking:** This struct carries a raw API key with no marshal guard. One accidental `log.Printf("%+v", req)` anywhere in the call chain leaks the user's key.

**Files:**
- Modify: `internal/loopgate/types.go`
- Modify: `internal/loopgate/types_test.go` (marshal test colocated with types)

- [x] **Step 1: Write the failing marshal test**

```go
func TestModelConnectionStoreRequest_MarshalJSON_RedactsSecretValue(t *testing.T)
```

- [x] **Step 2: Run to verify it fails**

Run: `go test ./internal/loopgate/... -run 'TestModelConnectionStoreRequest_MarshalJSON' -count=1`

Expected: FAIL.

- [x] **Step 3: Add custom `MarshalJSON`**

Follow the existing `CapabilityRequest` pattern.

- [x] **Step 4: Run to verify it passes**

Expected: PASS.

---

### Task 4: Stop leaking resolved filesystem paths in sandbox error responses

**Source:** MR S12.
**Why ship-blocking:** A sandbox denial currently returns the real absolute path (e.g. `{checkout}/runtime/sandbox/...`) in the API response. A demo where someone triggers a denied path would expose the developer's filesystem layout.

**Files:**
- Modify: `internal/loopgate/server_sandbox_handlers.go`
- Create: `internal/loopgate/server_sandbox_handlers_test.go`

- [x] **Step 1: Write the failing test**

```go
func TestRedactSandboxError_DoesNotExposeAbsolutePaths(t *testing.T)
```

- [x] **Step 2: Run to verify it fails**

Run: `go test ./internal/loopgate/... -run 'TestRedactSandboxError_DoesNotExposeAbsolutePaths' -count=1`

Expected: FAIL.

- [x] **Step 3: Return only sentinel error messages to clients**

Return `"sandbox path is outside sandbox home"` not the full wrapped chain. Log detailed path server-side.

- [x] **Step 4: Run to verify it passes**

Expected: PASS.

---

### Task 5: Tighten continuity inspect validation and input bounds

**Source:** CR S5.
**Why ship-blocking:** Without bounds, a bug or malicious input could submit an enormous inspection request that hangs the server during a demo.

**Files:**
- Modify: `internal/loopgate/types.go`
- Modify: `internal/loopgate/continuity_memory.go`
- Modify: `internal/loopgate/server_memory_handlers.go`
- Modify: `internal/loopgate/continuity_memory_test.go`
- Tests for inspect bounds are in `internal/loopgate/continuity_mutation_ordering_test.go` alongside ordering tests.

- [x] **Step 1: Write the failing request-validation tests**

```go
func TestContinuityInspectRequest_RejectsTooManyEvents(t *testing.T)
func TestContinuityInspectRequest_RejectsOversizedApproxPayload(t *testing.T)
```

- [x] **Step 2: Run to verify they fail**

Run: `go test ./internal/loopgate/... -run 'TestContinuityInspectRequest_' -count=1`

Expected: FAIL.

- [x] **Step 3: Add conservative request-level bounds**

- `maxContinuityEventsPerInspection`
- optional payload size upper bound

- [x] **Step 4: Run to verify they pass**

Expected: PASS.

**Implementation report:** `docs/superpowers/reports/2026-03-25-phase-1-ship-blockers-and-hardening-report.md`

---

## Phase 2: Memory That Works

End users expect **durable explicit memory** (“My name is Ada”) to round-trip across sessions. If this doesn't work reliably, continuity feels broken. These two tasks fix the upstream key normalization gap that reviewers flagged as the reason memory “feels lackluster.”

### Task 6: Add explicit key normalization before anchor derivation

**Source:** TR (primary — identified normalize.go:75-113 gap), CR §2.4, GR §4.
**Why pre-ship:** Without this, "remember my name is Ada" works but "remember my name" and "my name is Ada" generate different keys and bypass deduplication. The user ends up with three entries for their name.

**Files:**
- Create: `internal/tcl/key_normalization_test.go`
- Modify: `internal/tcl/normalize.go`
- Modify: `internal/tcl/normalize_test.go`

- [x] **Step 1: Write the failing key-normalization tests**

```go
func TestNormalizeExplicitFactKey_NameAliasesCollapseToName(t *testing.T)
func TestNormalizeExplicitFactKey_PreferredNameAliasesCollapse(t *testing.T)
func TestNormalizeExplicitFactKey_PreferenceAliasesCollapseToStableFacet(t *testing.T)
```

Assertions:

- `user_name`, `my_name`, and `full_name` normalize to the same canonical key
- `user_preferred_name` normalizes to `preferred_name`

- [x] **Step 2: Run to verify they fail**

Run: `go test ./internal/tcl/... -run 'TestNormalizeExplicitFactKey_' -count=1`

Expected: FAIL.

- [x] **Step 3: Implement minimal explicit key normalization**

In `internal/tcl/normalize.go`:

- add a narrow canonicalization table for identity/profile keys
- apply canonicalization before `deriveExplicitFactConflictAnchor`
- keep the mapping small and test-covered

- [x] **Step 4: Run to verify they pass**

Expected: PASS.

---

### Task 7: Verify anchor-based supersession works end-to-end

**Source:** TR, CR §2.1.
**Why pre-ship:** This proves that "My name is Ada" followed by "My name is Grace" actually supersedes rather than accumulating. Without this, memory grows without bound and contradicts itself.

**Files:**
- Modify: `internal/tcl/normalize.go`
- Modify: `internal/tcl/normalize_test.go`
- Modify: `internal/loopgate/continuity_memory_test.go`

- [x] **Step 1: Write end-to-end supersession tests**

```go
func TestNormalizeMemoryCandidate_ExplicitNameGetsConflictAnchor(t *testing.T)
func TestNormalizeMemoryCandidate_UnstablePreferenceHasNoConflictAnchor(t *testing.T)
```

Plus a Loopgate-level integration test:

```go
func TestRememberMemoryFact_NameSupersessionWorksEndToEnd(t *testing.T)
```

Assert: store "name=Ada", then "name=Grace" — wake state contains only Grace.

- [x] **Step 2: Run to verify they fail or pass**

Run: `go test ./internal/tcl/... ./internal/loopgate/... -run 'Test(NormalizeMemoryCandidate_|RememberMemoryFact_NameSupersession)' -count=1`

If these already pass (because the anchor system was partially landed per the roadmap), verify and move on. If they fail, implement the minimal fix.

- [x] **Step 3: If failing, implement minimal anchor derivation fix**

Ensure stable anchors emit for canonical identity/profile slots and that supersession uses the persisted tuple.

- [x] **Step 4: Run to verify they pass**

Expected: PASS.

---

## Phase 3: Ship Prep

### Task 8: Tune the persona for warmth

**Why pre-ship:** The persona.yaml produces an assistant that feels like a compliance officer. The northstar says "quirky and warm, like Weebo." These conflict. The first user's emotional impression is set in the first 30 seconds.

**Files:**
- Modify: `persona/default.yaml`
- Modify: `internal/prompt/` (if system prompt references persona values)

- [x] **Step 1: Update persona values**

Change:
- `warmth: medium` → `warmth: high`
- `humor: low` → `humor: medium`
- `skepticism: high` → `skepticism: medium`
- `tone: calm, direct, respectful, pragmatic` → `tone: warm, direct, clear, gently playful`

Keep `honesty: strict`, `safety_mindset: high`, `security_mindset: high` unchanged.

- [x] **Step 2: Review system prompt compilation**

Read `internal/prompt/` to verify persona values flow into the system prompt. Adjust any prompt text that produces overly hedging or bureaucratic language. The model should say "Done! Moved 23 files into 4 folders." not "I have completed the requested file organization operation." (A `VOICE (USER-FACING)` block exists in `internal/prompt/compiler.go`.)

- [ ] **Step 3: Test with 5 sample conversations**

Run the **reference Wails shell** (`cmd/haven/`) or another local client, and have five different conversations (greeting, remember name, organize downloads, ask about security, ask for something policy denies). Verify the tone feels warm and capable, not robotic.

---

### Task 9: First-run experience polish

**Why pre-ship:** The magic moment must happen in under 3 minutes from install. Setup currently involves model provider selection, folder grants, and preference mode. This needs to feel effortless.

**Files:**
- Modify: `cmd/haven/setup.go`
- Modify: `cmd/haven/frontend/` (setup flow components)

- [x] **Step 1: Audit the current first-run flow**

Walk through setup as a new user. Time it. Note every point of confusion or friction.

- [x] **Step 2: Simplify to the minimum viable setup**

The setup should ask exactly three things:
1. How do you connect to an AI model? (Ollama detected / API key / skip)
2. Can the assistant access your Downloads folder? (Grant / Skip)
3. That's it — the shell greets the user and immediately offers to look at Downloads.

Wallpaper, presence mode, and other preferences should be discoverable in Settings, not part of first-run. **Implemented:** `cmd/haven/frontend/src/components/SetupWizard.tsx` (welcome → model → folders → finish); defaults in `CompleteSetup` via `setup.go`.

- [ ] **Step 3: Verify the 3-minute promise**

Cold install → setup → first useful action in under 3 minutes.

---

### Task 10: Build the signed .dmg

**Why pre-ship:** This is the distribution artifact. Without it, there is no ship.

**Files:**
- Create or modify: build scripts, `Makefile`, or CI config
- `cmd/haven/main.go` (ensure embedded frontend is current)

- [x] **Step 1: Build the production Wails bundle (reference shell)**

```bash
./scripts/haven/build-macos-app.sh
```

(Wails embeds the built frontend; see `scripts/haven/README.md`.)

- [ ] **Step 2: Code sign and notarize**

Sign with Developer ID. Notarize with Apple. Test that Gatekeeper accepts it on a clean machine.

- [ ] **Step 3: Package as .dmg**

Create a drag-to-Applications .dmg with a background image. Test the full install flow on a machine that has never seen the project. (Runbook notes in `scripts/haven/README.md`.)

- [ ] **Step 4: Test Homebrew Cask formula**

Write a Homebrew Cask formula pointing at the `.dmg` download URL (example formula name was `haven` in the original plan—pick a name consistent with your distribution brand).

---

### Task 11: Record the demo

**Why pre-ship:** The demo is the launch artifact. It needs to show the security story through the UX, not through architecture diagrams.

- [x] **Step 1: Write the demo script (90 seconds)**

Demo script materials lived under `docs/superpowers/demos/` in the upstream monorepo; **this clone may not include them**. Outline for a ~90s recording:

1. Install the desktop bundle (5s — show `.dmg` drag)
2. First-run setup — grant Downloads (15s)
3. Assistant greets user, offers to organize Downloads (10s)
4. Assistant scans and proposes a plan (15s)
5. User reviews the plan in the approval card (10s)
6. User approves, files move (10s)
7. Show the Security panel — audit trail, permissions, what was allowed (15s)
8. Quit and reopen — continuity: name + task still present (10s)

- [ ] **Step 2: Record with clean desktop, good resolution**

1920x1080 minimum. Clean macOS desktop. No notifications. No personal files visible.

- [ ] **Step 3: Edit to 90 seconds with subtle callouts**

No voiceover needed for v1 — text annotations are fine. Show, don't explain.

---

## Phase 4: Dogfood & Fix

### Task 12: Dogfood the reference loop for one week

**Why:** The best test plan is daily use by the person who built it. No plan document will find the bugs that real usage will.

- [ ] **Step 1: Use the reference shell (or your primary local client) as the daily driver for 7 days**

Every day:
- Open the client
- Run a **Downloads** (or similar) organize flow end-to-end
- Exercise **explicit memory** and verify persistence
- Check that memory persists across restarts
- Try to break the approval flow
- Note every friction point, crash, or "that felt wrong" moment

- [ ] **Step 2: Fix the top 5 issues found during dogfooding**

Address in priority order. These are unknown unknowns — the plan can't predict them, but making time for them is critical.

- [ ] **Step 3: Run full verification suite**

```bash
go test ./...
```

Expected: PASS.

---

## Post-Launch Backlog

These tasks are all architecturally correct and well-designed. They move to post-launch because they don't affect the first 100 users' experience. They should be executed after shipping, in this priority order.

### Post-Launch Tier 1: Security hardening (first month after ship)

| Task | Source | Description |
|------|--------|-------------|
| **Fix `ensureWithinRoot` silent `EvalSymlinks` failure** | MR S1 | **Done (2026-03-25):** hard error in `internal/sandbox/sandbox.go` |
| **Fix deny-list fail-open asymmetry** | MR S5 | **Done (2026-03-25):** fail closed in `internal/safety/safepath.go` |
| **Morphling worker session open atomicity** | CR §1.3, GR F4 | **Done:** `openMorphlingWorkerSession` holds `server.mu` from launch lookup through delete + session insert (`morphling_workers.go`) |
| **Stop projecting raw morphling memory strings** | MR F6/F17 | **Done (2026-03-25):** `MorphlingSummary` uses counts + `morphlingProjectionStatusText` |
| **Harden nonce replay persistence** | CR S2 | Atomic write (temp+rename) for `nonce_replay.json` |
| **Add fsync to base ledger Append** | MR | **Done (2026-03-25):** `f.Sync()` in `internal/ledger/ledger.go` |
| **Fix test clock bypass in `writeContinuityArtifacts`** | MR F13 | **Done (2026-03-25):** `nowUTC` parameter through save/write continuity artifacts |

### Post-Launch Tier 2: Architecture completion (months 2–3)

| Task | Source | Description |
|------|--------|-------------|
| **TCL conflict-anchor foundation** | TR, CR §2.1 | Full `ConflictAnchor` type, validation, and canonical serialization in `internal/tcl/` |
| **Persist anchor tuples and switch contradiction handling** | TR, CR §2.1 | Loopgate anchor persistence, tuple-based supersession, legacy record handling |
| **`invoke_capability` path consistency** | CR §3.2 | Expand or reject `invoke_capability` on XML path to match structured path |
| **`isSecretExportCapability` typed registry** | CR S4 | Replace substring heuristic with explicit allowlist |
| **Decouple WakeState from continuityMemoryState** | CR §2.2, GR Task 2 | Store separately, always rebuild from authority |
| **Exact-match trie redaction** | GR | Defense-in-depth against LLM formatting variability |

### Post-Launch Tier 3: Platform evolution (months 3–6)

| Task | Source | Description |
|------|--------|-------------|
| **Optional macOS XPC (or similar) transport hardening** | Threat model / RFCs | **Not v1.** If pursued after ship, see `docs/loopgate-threat-model.md` and `docs/rfcs/0001-loopgate-token-policy.md`; v1 remains HTTP on the local socket. |
| **Morphling spawn as model-callable tool** | CR §3.2 | Separate security review required |
| **Entropy-based unknown-secret scanner** | GR | Requires Policy Alert UX design |
| **Fleet management / central policy** | GR enterprise | Enterprise-tier — needs product validation first |
| **Browser/research capability surface** | Roadmap | Must preserve Loopgate governance |

---

## What Changed From the Previous Plan and Why

| Previous plan | This plan | Rationale |
|---------------|-----------|-----------|
| Phase 1: TCL anchors (4 tasks, highest priority) | Post-Launch Tier 2 | The anchor system was partially landed per the roadmap. The key normalization fix (now Task 6) is the only piece that affects first users. Full anchor foundation can wait. |
| Phase 2: Hardening (3 tasks, highest priority) | Phase 1: Ship Blockers (5 tasks) | Reframed around "would this embarrass us in a demo" instead of "is this architecturally ideal." Audit ordering stays. Input bounds stay. Secret marshal guard and path leakage added. |
| Phase 3: Projection hygiene (4 tasks) | Split: morphling projection → Post-Launch; nonce → Post-Launch; tool path → Post-Launch | Bounded **morphling** workers aren't part of the reference organizer MVP loop. `invoke_capability` asymmetry won't be hit in the Downloads organizer flow. |
| Phase 4: Defense-in-depth (6 tasks) | Post-Launch Tier 1 | Important but won't affect first users. The Openat defense layer mitigates the EvalSymlinks issue. The deny-list asymmetry requires a specific attack. |
| Phase 5: XPC migration | Post-Launch Tier 3 (optional) | **v1 = HTTP** on local UDS to save cost; XPC is optional future hardening, not a migration requirement for ship. |
| No ship prep phase | Phase 3: Ship Prep (4 tasks) | Added persona tuning, first-run polish, .dmg build, demo recording. These are the actual ship blockers. |
| No dogfood phase | Phase 4: Dogfood & Fix | Added a dedicated week of self-use. The best bug finder is daily usage. |
| 19 tasks before ship | 12 tasks before ship | 7 tasks cut from pre-ship scope. Post-launch backlog is explicit and prioritized. |

---

## Execution Notes

- **TDD discipline:** Every code task follows write-failing-test → run → implement → run-passing. Do not skip the failing-test step.
- **Smallest change:** Prefer the smallest invariant-preserving change at each step.
- **Ship orientation:** If you're spending more than 2 days on any single task, stop and ask whether it's truly ship-blocking or whether it should move to Post-Launch.
- **Dogfood trumps plan:** If daily usage in Phase 4 reveals that something in Post-Launch is actually critical, pull it forward. If something in Phase 1 turns out to not matter in practice, deprioritize it.
- **Commit discipline:** Do not commit unless the user explicitly requests it.
- **Demo is the deliverable:** The plan is successful when the 90-second demo feels magical. Everything else is supporting work.
