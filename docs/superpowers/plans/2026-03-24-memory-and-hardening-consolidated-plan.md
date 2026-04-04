**Last updated:** 2026-03-24

# Memory and Hardening Consolidated Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Consolidate the active TCL/memory work and the newly confirmed security and architecture hardening work into one ordered plan that improves continuity correctness, memory quality, and Loopgate boundary integrity without widening authority.

**Architecture:** Keep the work split into two explicit tracks under one plan. Track A finishes the TCL-owned memory-anchor direction and fixes the upstream key-normalization gap so explicit memory writes become deterministic and deduplicated. Track B hardens the control plane around continuity durability, worker lifecycle concurrency, projection hygiene, and tool-path consistency, while preserving Loopgate as the sole authority boundary.

**Tech Stack:** Go 1.24, Loopgate, `internal/tcl`, `internal/loopgate`, append-only JSONL + state snapshots, in-repo Wails/React reference shell under `cmd/haven/`, Go tests via `go test`.

---

## Why this plan exists

This document supersedes the current split between:

- `docs/superpowers/plans/2026-03-23-tcl-conflict-anchor-implementation.md`
- `docs/superpowers/plans/claude_feedback-2026-03-23-26.md`

It also incorporates the confirmed near-term findings from the 2026-03-24 review pass:

- continuity mutation audit ordering remains the top correctness and auditability risk
- morphling worker session open still has a concurrency-sensitive split
- morphling public status projection still returns raw model-originated strings
- the reference Wails client’s tool-calling path still has an XML fallback asymmetry around `invoke_capability`
- nonce replay persistence and secret-export classification still need hardening

The result should be one plan with two tracks, one priority order, and one handoff for future implementation work.

## Scope and constraints

- Loopgate remains the authority boundary.
- Natural language never creates authority.
- TCL owns semantic classification and conflict-anchor derivation.
- Loopgate owns persistence, replay, contradiction handling, audit, projection, and denial behavior.
- No fallback should silently weaken path, audit, or memory-safety rules.
- Raw model-originated strings must not become trusted projection state just because they are convenient for UI.
- Do not commit unless the user explicitly requests it.

## Recommended sequencing

1. Execute **Track A, Tasks 1-4** first.
2. Execute **Track B, Tasks 5-7** immediately after, because they address correctness and audit integrity.
3. Execute **Track B, Tasks 8-10** next, because they tighten projection and execution consistency.
4. Finish with **Task 11** for docs and full verification.

Rationale:

- Track A fixes the memory-quality direction already underway.
- The first half of Track B addresses harder integrity bugs that can leave persisted state in a confusing or unaudited condition.
- Later Track B tasks are important but less urgent than continuity ordering and worker-session atomicity.

## File structure

### Track A: TCL and memory quality

**Create:**

- `internal/tcl/conflict_anchor_test.go`
- `internal/loopgate/memory_conflict_anchor_test.go`
- `internal/tcl/key_normalization_test.go`

**Modify:**

- `internal/tcl/types.go`
- `internal/tcl/validator.go`
- `internal/tcl/normalize.go`
- `internal/tcl/normalize_test.go`
- `internal/loopgate/memory_tcl.go`
- `internal/loopgate/continuity_memory.go`
- `internal/loopgate/continuity_memory_test.go`
- `internal/loopgate/types.go`
- `docs/rfcs/0009-memory-continuity-and-recall.md`
- `docs/roadmap/roadmap.md`

### Track B: Control-plane hardening

**Create:**

- `internal/loopgate/continuity_mutation_ordering_test.go`
- `internal/loopgate/morphling_worker_race_test.go`
- `internal/loopgate/tool_call_path_test.go`

**Modify:**

- `internal/loopgate/continuity_memory.go`
- `internal/loopgate/continuity_runtime.go`
- `internal/loopgate/server.go`
- `internal/loopgate/morphlings.go`
- `internal/loopgate/morphling_workers.go`
- `internal/loopgate/server_morphling_worker_handlers.go`
- `internal/loopgate/morphlings_contract_test.go`
- `internal/loopgate/server_memory_handlers.go`
- `internal/loopgate/types.go`
- `cmd/haven/chat.go`
- `cmd/haven/desktop.go`
- `cmd/haven/frontend/src/components/windows/LoopgateWindow.tsx`
- `internal/orchestrator/parser.go`
- `internal/orchestrator/structured.go`
- `internal/model/toolschema.go`
- `docs/loopgate-threat-model.md`
- `docs/roadmap/roadmap.md`

---

## Track A: TCL and memory quality

### Task 1: Finish the TCL conflict-anchor foundation

**Files:**
- Create: `internal/tcl/conflict_anchor_test.go`
- Modify: `internal/tcl/types.go`
- Modify: `internal/tcl/validator.go`

- [ ] **Step 1: Write the failing TCL anchor tests**

Create tests for:

```go
func TestConflictAnchorCanonicalKey_V1(t *testing.T)
func TestConflictAnchorCanonicalKey_IncludesFacet(t *testing.T)
func TestValidateNode_RejectsInvalidConflictAnchor(t *testing.T)
```

Assertions:

- canonical serialization is deterministic
- `Version == "v1"` is enforced
- delimiters and invalid characters are rejected
- optional facet participates in the canonical key when present

- [ ] **Step 2: Run the TCL tests to verify they fail**

Run: `go test ./internal/tcl/... -run 'TestConflictAnchor|TestValidateNode_RejectsInvalidConflictAnchor' -count=1`

Expected: FAIL because anchor fields, serialization helpers, or validation rules are incomplete.

- [ ] **Step 3: Implement the minimal anchor type and validation**

Implement:

- `ConflictAnchor` in `internal/tcl/types.go`
- `ANCHOR *ConflictAnchor` on the normalized TCL node shape
- `CanonicalKey()` deterministic serialization helper
- strict validation for:
  - supported version
  - required fields
  - lowercase ASCII, digits, underscore
  - optional facet with the same charset contract

- [ ] **Step 4: Run the TCL tests to verify they pass**

Run: `go test ./internal/tcl/... -run 'TestConflictAnchor|TestValidateNode_RejectsInvalidConflictAnchor' -count=1`

Expected: PASS.

### Task 2: Add explicit key normalization before anchor derivation

**Files:**
- Create: `internal/tcl/key_normalization_test.go`
- Modify: `internal/tcl/normalize.go`
- Modify: `internal/tcl/normalize_test.go`

- [ ] **Step 1: Write the failing key-normalization tests**

Add tests for common model-generated variants:

```go
func TestNormalizeExplicitFactKey_NameAliasesCollapseToName(t *testing.T)
func TestNormalizeExplicitFactKey_PreferredNameAliasesCollapse(t *testing.T)
func TestNormalizeExplicitFactKey_PreferenceAliasesCollapseToStableFacet(t *testing.T)
```

Assertions:

- `user_name`, `my_name`, and `full_name` normalize to the same canonical key when they truly represent the same slot
- `user_preferred_name` normalizes to `preferred_name`
- stable preference variants normalize before anchor derivation, not after

- [ ] **Step 2: Run the normalization tests to verify they fail**

Run: `go test ./internal/tcl/... -run 'TestNormalizeExplicitFactKey_' -count=1`

Expected: FAIL because normalization currently relies on exact known keys before anchor derivation.

- [ ] **Step 3: Implement minimal explicit key normalization**

In `internal/tcl/normalize.go`:

- add a narrow canonicalization table for explicit identity/profile keys
- apply canonicalization before `deriveExplicitFactConflictAnchor`
- keep the mapping small, explainable, and test-covered
- do not broaden to speculative fuzzy matching

- [ ] **Step 4: Run the normalization tests to verify they pass**

Run: `go test ./internal/tcl/... -run 'TestNormalizeExplicitFactKey_' -count=1`

Expected: PASS.

### Task 3: Emit anchors only for stable explicit facts

**Files:**
- Modify: `internal/tcl/normalize.go`
- Modify: `internal/tcl/normalize_test.go`

- [ ] **Step 1: Write the failing explicit-anchor derivation tests**

Add tests for:

```go
func TestNormalizeMemoryCandidate_ExplicitNameGetsConflictAnchor(t *testing.T)
func TestNormalizeMemoryCandidate_ExplicitPreferenceThemeGetsConflictAnchor(t *testing.T)
func TestNormalizeMemoryCandidate_UnstablePreferenceHasNoConflictAnchor(t *testing.T)
```

- [ ] **Step 2: Run the anchor-derivation tests to verify they fail**

Run: `go test ./internal/tcl/... -run 'TestNormalizeMemoryCandidate_(ExplicitNameGetsConflictAnchor|ExplicitPreferenceThemeGetsConflictAnchor|UnstablePreferenceHasNoConflictAnchor)' -count=1`

Expected: FAIL until normalized explicit keys and stable-slot anchor derivation are wired together.

- [ ] **Step 3: Implement minimal anchor derivation**

Implement:

- stable anchors for canonical identity/profile slots
- stable anchors for recognized preference facets only
- `ANCHOR=nil` for generic or unstable statements
- no Loopgate-side synthesis fallback

- [ ] **Step 4: Run the anchor-derivation tests to verify they pass**

Run: `go test ./internal/tcl/... -run 'TestNormalizeMemoryCandidate_(ExplicitNameGetsConflictAnchor|ExplicitPreferenceThemeGetsConflictAnchor|UnstablePreferenceHasNoConflictAnchor)' -count=1`

Expected: PASS.

### Task 4: Persist anchor tuples and switch contradiction handling to TCL-owned data

**Files:**
- Create: `internal/loopgate/memory_conflict_anchor_test.go`
- Modify: `internal/loopgate/memory_tcl.go`
- Modify: `internal/loopgate/continuity_memory.go`
- Modify: `internal/loopgate/continuity_memory_test.go`
- Modify: `internal/loopgate/types.go`

- [ ] **Step 1: Write the failing Loopgate anchor-persistence tests**

Create tests for:

```go
func TestRememberMemoryFact_SupersedesOnlyWhenAnchorTupleMatches(t *testing.T)
func TestRememberMemoryFact_CoexistsWhenTCLReturnsNoAnchor(t *testing.T)
func TestRememberMemoryFact_FailsClosedWhenTCLValidationFails(t *testing.T)
func TestWakeState_UsesPersistedAnchorTupleForConflictResolution(t *testing.T)
func TestWakeState_LegacyAnchorlessFactsDoNotTriggerFallbackSynthesis(t *testing.T)
func TestWakeState_EqualStrengthAnchoredContradictionBecomesAmbiguous(t *testing.T)
func TestReplay_AnchorlessLegacyRecordsRemainAnchorless(t *testing.T)
```

- [ ] **Step 2: Run the Loopgate memory tests to verify they fail**

Run: `go test ./internal/loopgate/... -run 'Test(RememberMemoryFact|WakeState|Replay_)' -count=1`

Expected: FAIL because Loopgate still contains local semantic fallback behavior or incomplete persisted anchor handling.

- [ ] **Step 3: Implement minimal Loopgate anchor persistence and replay changes**

Implement:

- anchor version and canonical key persistence on explicit facts
- tuple-based supersession and contradiction grouping
- no read-time synthesis for legacy anchorless records
- fail-closed behavior when TCL output is invalid for the explicit-memory path

- [ ] **Step 4: Run the Loopgate memory tests to verify they pass**

Run: `go test ./internal/loopgate/... -run 'Test(RememberMemoryFact|WakeState|Replay_)' -count=1`

Expected: PASS.

---

## Track B: Control-plane hardening

### Task 5: Make continuity mutation ordering audit-safe

**Files:**
- Create: `internal/loopgate/continuity_mutation_ordering_test.go`
- Modify: `internal/loopgate/continuity_memory.go`
- Modify: `internal/loopgate/continuity_runtime.go`
- Modify: `internal/loopgate/continuity_memory_test.go`

- [ ] **Step 1: Write the failing ordering tests**

Create tests for:

```go
func TestMutateContinuityMemory_DoesNotLeaveReplayableMutationWhenAuditFails(t *testing.T)
func TestMutateContinuityMemory_SaveFailureDoesNotCreateAmbiguousDurableState(t *testing.T)
func TestContinuityReplay_RejectsOrRepairsOrphanedMutationSequence(t *testing.T)
```

- [ ] **Step 2: Run the continuity-ordering tests to verify they fail**

Run: `go test ./internal/loopgate/... -run 'Test(MutateContinuityMemory|ContinuityReplay_)' -count=1`

Expected: FAIL because continuity JSONL append currently occurs before audit and before snapshot save.

- [ ] **Step 3: Implement the smallest safe ordering fix**

Choose one conservative approach and document it in code comments:

- either make durable continuity append contingent on successful audit
- or add explicit orphan-marking / startup reconciliation that prevents silent replay of unaudited mutations

Also ensure multi-file mutation events do not leave partially applied durable state without a defined recovery path.

- [ ] **Step 4: Run the continuity-ordering tests to verify they pass**

Run: `go test ./internal/loopgate/... -run 'Test(MutateContinuityMemory|ContinuityReplay_)' -count=1`

Expected: PASS.

### Task 6: Tighten continuity inspect validation and input bounds

**Files:**
- Modify: `internal/loopgate/types.go`
- Modify: `internal/loopgate/continuity_memory.go`
- Modify: `internal/loopgate/server_memory_handlers.go`
- Modify: `internal/loopgate/continuity_memory_test.go`

- [ ] **Step 1: Write the failing request-validation tests**

Add tests for:

```go
func TestContinuityInspectRequest_RejectsTooManyEvents(t *testing.T)
func TestContinuityInspectRequest_RejectsOversizedApproxPayload(t *testing.T)
```

- [ ] **Step 2: Run the request-validation tests to verify they fail**

Run: `go test ./internal/loopgate/... -run 'TestContinuityInspectRequest_' -count=1`

Expected: FAIL because request validation currently checks non-negative counts but not stricter semantic caps.

- [ ] **Step 3: Add conservative request-level bounds**

Implement:

- `maxContinuityEventsPerInspection`
- optional upper bound for declared payload metadata
- comments explaining that these sit on top of the existing signed-body transport cap

- [ ] **Step 4: Run the request-validation tests to verify they pass**

Run: `go test ./internal/loopgate/... -run 'TestContinuityInspectRequest_' -count=1`

Expected: PASS.

### Task 7: Make morphling worker session open atomic for one logical operation

**Files:**
- Create: `internal/loopgate/morphling_worker_race_test.go`
- Modify: `internal/loopgate/morphling_workers.go`
- Modify: `internal/loopgate/server_morphling_worker_handlers.go`

- [ ] **Step 1: Write the failing morphling race tests**

Create tests for:

```go
func TestOpenMorphlingWorkerSession_ConcurrentOpenConsumesLaunchExactlyOnce(t *testing.T)
func TestOpenMorphlingWorkerSession_DoesNotInvalidateFreshSessionViaSecondOpen(t *testing.T)
```

- [ ] **Step 2: Run the morphling race tests to verify they fail**

Run: `go test ./internal/loopgate/... -run 'TestOpenMorphlingWorkerSession_' -count=1`

Expected: FAIL or flake because launch lookup, morphling validation, and launch/session mutation are split across lock scopes.

- [ ] **Step 3: Implement a single logical critical section**

Refactor so:

- launch consumption and worker-session issuance happen as one logical operation
- concurrent callers get deterministic outcomes
- no newly issued session can be revoked by a second opener racing the same launch token

- [ ] **Step 4: Run the morphling race tests to verify they pass**

Run: `go test ./internal/loopgate/... -run 'TestOpenMorphlingWorkerSession_' -count=1`

Expected: PASS.

### Task 8: Stop projecting raw morphling status and memory strings in public summaries

**Files:**
- Modify: `internal/loopgate/morphlings.go`
- Modify: `internal/loopgate/types.go`
- Modify: `internal/loopgate/morphlings_contract_test.go`
- Modify: `cmd/haven/desktop.go`
- Modify: `cmd/haven/frontend/src/components/windows/LoopgateWindow.tsx`

- [ ] **Step 1: Write the failing projection tests**

Add tests for:

```go
func TestMorphlingStatusProjection_DoesNotReturnRawWorkerStatusText(t *testing.T)
func TestMorphlingStatusProjection_DoesNotReturnRawWorkerMemoryStrings(t *testing.T)
```

- [ ] **Step 2: Run the projection tests to verify they fail**

Run: `go test ./internal/loopgate/... -run 'TestMorphlingStatusProjection_' -count=1`

Expected: FAIL because summaries currently return verbatim `StatusText` and `MemoryStrings`.

- [ ] **Step 3: Implement projection-safe summary fields**

Change summaries to expose one of:

- counts only
- taint-marked placeholders
- short, non-model-authored lifecycle text derived from authoritative state

Keep raw strings only in internal records, not operator-facing status summaries.

- [ ] **Step 4: Run the projection tests to verify they pass**

Run: `go test ./internal/loopgate/... -run 'TestMorphlingStatusProjection_' -count=1`

Expected: PASS.

### Task 9: Make `invoke_capability` behavior consistent across structured and fallback tool paths

**Files:**
- Create: `internal/loopgate/tool_call_path_test.go`
- Modify: `cmd/haven/chat.go`
- Modify: `internal/orchestrator/parser.go`
- Modify: `internal/orchestrator/structured.go`

- [ ] **Step 1: Write the failing tool-path consistency tests**

Create tests for:

```go
func TestToolCallParsing_XMLInvokeCapabilityExpandsToInnerTool(t *testing.T)
func TestToolCallParsing_StructuredAndXMLPathsYieldSameCapability(t *testing.T)
```

- [ ] **Step 2: Run the tool-path tests to verify they fail**

Run: `go test ./internal/orchestrator/... -run 'TestToolCallParsing_' -count=1`

Expected: FAIL because `invoke_capability` expansion exists on the structured path but not the XML fallback path.

- [ ] **Step 3: Implement the smallest consistency fix**

Choose one:

- expand `invoke_capability` on the XML parser path too
- or explicitly reject XML `invoke_capability` with a clear surfaced validation error so behavior is not silently divergent

Do not create a second execution authority; keep Loopgate execution unchanged.

- [ ] **Step 4: Run the tool-path tests to verify they pass**

Run: `go test ./internal/orchestrator/... -run 'TestToolCallParsing_' -count=1`

Expected: PASS.

### Task 10: Harden nonce replay persistence and capability classification

**Files:**
- Modify: `internal/loopgate/server.go`
- Modify: `internal/loopgate/server_test.go`
- Modify: `docs/rfcs/0001-loopgate-token-policy.md`

- [ ] **Step 1: Write the failing hardening tests**

Add tests for:

```go
func TestSaveNonceReplayState_WriteFailureIsObservable(t *testing.T)
func TestIsSecretExportCapability_BorderlineNamesRemainDocumentedAndCovered(t *testing.T)
```

- [ ] **Step 2: Run the hardening tests to verify they fail**

Run: `go test ./internal/loopgate/... -run 'Test(SaveNonceReplayState|IsSecretExportCapability_)' -count=1`

Expected: FAIL because nonce persistence currently swallows write failure and capability classification is only heuristic.

- [ ] **Step 3: Implement the smallest durable hardening**

Implement:

- atomic nonce replay persistence where practical, or at minimum explicit observable failure semantics
- stronger comments and tests around capability-name heuristics
- a follow-up TODO or typed registry hook for replacing the heuristic with explicit capability metadata

- [ ] **Step 4: Run the hardening tests to verify they pass**

Run: `go test ./internal/loopgate/... -run 'Test(SaveNonceReplayState|IsSecretExportCapability_)' -count=1`

Expected: PASS.

### Task 11: Update docs and run the final verification suite

**Files:**
- Modify: `docs/rfcs/0009-memory-continuity-and-recall.md`
- Modify: `docs/roadmap/roadmap.md`
- Modify: `docs/rfcs/0001-loopgate-token-policy.md`
- Modify: `docs/superpowers/plans/2026-03-24-memory-and-hardening-consolidated-plan.md` if implementation drift requires plan notes

- [ ] **Step 1: Update the docs to reflect landed behavior**

Document:

- TCL-owned anchor derivation and key normalization
- no Loopgate-local anchor synthesis fallback
- continuity mutation ordering and reconciliation behavior
- morphling projection policy
- tool-path consistency decision
- nonce replay durability behavior

- [ ] **Step 2: Run focused package tests**

Run:

```bash
go test ./internal/tcl/... ./internal/orchestrator/... ./internal/loopgate/... -count=1
```

Expected: PASS.

- [ ] **Step 3: Run broader regression checks**

Run:

```bash
go test ./cmd/haven/... ./internal/model/... ./internal/tools/... -count=1
```

Expected: PASS.

- [ ] **Step 4: Run one final repo-wide verification pass if runtime allows**

Run:

```bash
go test ./... 
```

Expected: PASS, or a clearly documented list of known unrelated failures if the repo is not green baseline.

---

## Deferred items intentionally not bundled into this pass

- full memory-system simplification beyond the explicit anchor and contradiction path
- vector or semantic memory retrieval improvements
- replacing the compact `invoke_capability` pattern with fully explicit native tool definitions everywhere
- making morphling spawn model-callable as a separate product/security review
- optional macOS XPC (or similar) transport hardening for the local control plane (`docs/loopgate-threat-model.md`)

These are important, but they should stay out of this implementation pass unless a task above proves impossible without touching them.

## Execution notes

- Prefer the smallest invariant-preserving change at each step.
- For Track B, do not “paper over” split-brain risks with comments alone; either make the ordering safe or make the failure explicit and replay-safe.
- Do not let any fallback logic silently recreate the very ambiguity this plan is trying to remove.
- If a task reveals that the real issue is larger than expected, stop after the failing test and update this plan rather than improvising a wider refactor.
