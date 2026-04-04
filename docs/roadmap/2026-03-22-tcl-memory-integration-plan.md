**Last updated:** 2026-03-24

# TCL Memory Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate Thought Compression Language (TCL) into the Loopgate memory pipeline so explicit memory writes and future memory candidates are normalized, classified, signature-checked, and routed through safe persistence decisions without making TCL an authority source.

**Architecture:** Add a shared Loopgate-side TCL service that accepts `MemoryCandidate` inputs, produces validated TCL nodes plus semantic signatures and dispositions, and then lets Loopgate choose the authoritative downstream action. Phase 1 wires this only to explicit memory writes (`memory.remember` and `RememberMemoryFact`), while later phases expand to task/goal/todo transitions, selected structured assistant/tool outputs, TCL-informed distillates, and compact resonate-key compression.

**Tech Stack:** Go 1.24, Loopgate control-plane handlers, in-repo Wails reference (`cmd/haven/`), append-only audit and continuity memory state, markdown RFCs under `docs/TCL-RFCs/`, Go tests with `go test`.

---

## Scope and guardrails

- TCL is **derived, assistive, and non-authoritative**.
- Loopgate remains the only authority for persistence, denial, review, and quarantine.
- Invalid TCL output must fail closed and must not drive durable memory, policy, or safety decisions.
- Raw prose must not become durable memory simply because TCL can normalize it.
- Selected assistant/tool outputs must enter through a **small allowlist of structured, provenance-bearing candidate sources**.
- Resonance/recall compression must reconstruct only the **semantic contour** of memory, not arbitrary prose.
- Signature matching may hard-block only curated high-confidence known-bad families.

## File structure

### New files

- `internal/tcl/types.go`
  - canonical token enums, node structs, decision structs, candidate structs, signature structs
- `internal/tcl/parser.go`
  - compact-form parser and serializer for canonical TCL expressions
- `internal/tcl/validator.go`
  - strict node and compact-form validation
- `internal/tcl/normalize.go`
  - deterministic normalization from `MemoryCandidate` to `TCLNode`
- `internal/tcl/signatures.go`
  - exact/family/risk signature derivation and hashing
- `internal/tcl/policy.go`
  - curated deny/review/quarantine rules for known-bad semantic families
- `internal/tcl/service.go`
  - high-level `AnalyzeMemoryCandidate(...)` entrypoint returning node, signatures, and disposition
- `internal/tcl/*_test.go`
  - unit coverage for parse, validate, normalize, signatures, and policy outcomes
- `internal/loopgate/memory_tcl.go`
  - Loopgate integration helpers that convert explicit memory requests into `MemoryCandidate` and map TCL outputs to governance outcomes
- `internal/loopgate/memory_tcl_test.go`
  - integration tests for explicit memory path gating, audit behavior, and deny/review/quarantine routing

### Modified files

- `internal/loopgate/memory_capability.go`
  - route `memory.remember` through TCL-aware explicit memory flow before persistence
- `internal/loopgate/server_memory_handlers.go`
  - decode optional TCL metadata fields for direct `RememberMemoryFact` requests
- `internal/loopgate/continuity_memory.go`
  - integrate TCL analysis into `rememberMemoryFact`, which is the actual explicit-memory persistence choke point
- `internal/loopgate/types.go`
  - add optional request metadata fields needed to preserve raw source context for TCL analysis
- `cmd/haven/memory_intent.go`
  - pass original user utterance and candidate-source context when the Wails reference client (`cmd/haven/`) performs deterministic memory writes
- `internal/shell/commands.go`
  - keep shell `/memory remember` aligned with TCL-aware explicit memory handling
- `docs/TCL-RFCs/Thought Compression Language.md`
  - reconcile phase-1 implementation wording and signature tier definitions
- `docs/TCL-RFCs/TCL Syntax.md`
  - reconcile `KEP` wording and canonical compact form details used by code
- `docs/TCL-RFCs/TCL Memory Node Schema.md`
  - align schema versioning and implementation notes
- `docs/rfcs/0009-memory-continuity-and-recall.md`
  - add a short bridge section describing TCL as a non-authoritative candidate classification layer
- `docs/roadmap/roadmap.md`
  - add TCL integration milestone or reference to this plan after phase-1 lands

### Later-phase files

- `internal/loopgate/continuity_memory.go`
  - accept broader TCL-informed candidates and feed TCL-informed distillate derivation
- `cmd/haven/memory.go`
  - replace generic thread-event distillation with structured candidate emission once phase 3 begins
- `internal/loopgate/continuity_memory_test.go`
  - extend coverage to TCL-informed continuity candidate handling

## Phase overview

1. **Phase 1: TCL core + explicit memory write integration**
2. **Phase 2: Signature registry and AV-like risk matching**
3. **Phase 3: Broader memory candidate ingestion**
4. **Phase 4: TCL-informed distillates and compressed resonate keys**
5. **Phase 5: Wake-state and recall refinement**

## Phase 1 deliverable

By the end of phase 1:

- explicit `memory.remember` requests are converted into `MemoryCandidate`
- Loopgate normalizes them into TCL
- TCL returns:
  - canonical node
  - compact expression
  - signature set
  - disposition
  - audit-safe reason codes
- Loopgate decides whether to:
  - persist the fact
  - drop it
  - flag/review/quarantine it
  - deny it
- high-confidence known-bad signatures can hard-block persistence
- normal explicit writes still work
- audit remains redacted and append-only

## Task 1: Lock the phase-1 contract

**Files:**
- Modify: `docs/TCL-RFCs/Thought Compression Language.md`
- Modify: `docs/TCL-RFCs/TCL Syntax.md`
- Modify: `docs/TCL-RFCs/TCL Memory Node Schema.md`
- Modify: `docs/rfcs/0009-memory-continuity-and-recall.md`
- Test: n/a

- [x] **Step 1: Reconcile TCL vocabulary and boundary language**

Update the docs so they all say the same thing about:

- `KEP` meaning "keep as candidate signal"
- TCL as a non-authoritative semantic IR
- exact/family/risk signature tiers
- hard-block limited to curated high-confidence known-bad signatures
- resonate keys as semantic compression handles, not prose replay

- [x] **Step 2: Add a short implementation bridge note to memory RFC 0009**

Document that phase 1 wires TCL only to explicit memory writes, and that broader continuity candidate ingestion comes later.

- [x] **Step 3: Review doc consistency manually**

Check:

- no file implies TCL is canonical durable memory
- no file implies raw prose can be persisted because TCL exists
- version wording is no longer contradictory

## Task 2: Create the shared TCL core package

**Files:**
- Create: `internal/tcl/types.go`
- Create: `internal/tcl/parser.go`
- Create: `internal/tcl/validator.go`
- Create: `internal/tcl/normalize.go`
- Create: `internal/tcl/service.go`
- Test: `internal/tcl/types_test.go`, `internal/tcl/parser_test.go`, `internal/tcl/validator_test.go`, `internal/tcl/normalize_test.go`

- [x] **Step 1: Define core types**

Add exact types for:

- `MemoryCandidate`
- `CandidateSource`
- `CandidateTrust`
- `TCLNode`
- `TCLRelation`
- `TCLMeta`
- `TCLDecision`
- `SignatureSet`
- `AnalysisResult`

Expected minimum fields:

- candidate raw source text
- normalized fields such as `fact_key`, `fact_value`, `reason`
- source lane such as `explicit_fact`, `continuity_candidate`, `tool_output_candidate`
- trust/source metadata
- analysis reasons that are safe to audit

- [x] **Step 2: Implement canonical compact-form serialization and parsing**

Support the syntax already described in `docs/TCL-RFCs/TCL Syntax.md` and reject malformed input.

Run: `go test ./internal/tcl/...`
Expected: parser/serializer tests pass

- [x] **Step 3: Implement strict validation**

Validation must reject:

- unknown tokens
- invalid object/action/state combos
- duplicate qualifiers
- invalid confidence values
- malformed relation targets

Also assert:

- invalid nodes do not produce signatures
- invalid nodes cannot be marked `KEP`

- [x] **Step 4: Implement deterministic normalization from `MemoryCandidate`**

Phase 1 only needs explicit memory-write normalization, for example:

- explicit user memory preference -> `STR(MEM:PRI)[ACT]%(N)` or similar validated node
- suspicious secret-persistence request -> `STR(MEM:PRI:EXT)->WRT[REV]%(N)`

The exact emitted shape must be documented and tested.

## Task 3: Add signature derivation and policy tiers

**Files:**
- Create: `internal/tcl/signatures.go`
- Create: `internal/tcl/policy.go`
- Test: `internal/tcl/signatures_test.go`, `internal/tcl/policy_test.go`

- [x] **Step 1: Define signature tiers**

Implement:

- exact semantic signature
- family signature
- risk motif signature

Each signature must be derived from normalized TCL fields, not raw English.

- [x] **Step 2: Create the initial curated deny/review/quarantine registry**

Phase-1 families:

- secret/token persistence into memory
- policy-override persistence requests
- prompt-injection-style "remember this and ignore safety later"
- explicit hostile self-authorizing memory patterns

Rules:

- deny only for curated high-confidence known-bad families
- quarantine or review for suspicious but not certain families
- allow benign explicit user preference/profile/routine cases

- [x] **Step 3: Add false-positive boundary tests**

Run: `go test ./internal/tcl/...`
Expected: malicious patterns deny or quarantine; benign preferences and names still pass

## Task 4: Wire TCL into explicit memory writes

**Files:**
- Create: `internal/loopgate/memory_tcl.go`
- Modify: `internal/loopgate/memory_capability.go`
- Modify: `internal/loopgate/server_memory_handlers.go`
- Modify: `internal/loopgate/continuity_memory.go`
- Modify: `internal/loopgate/types.go`
- Modify: `cmd/haven/memory_intent.go`
- Modify: `internal/shell/commands.go`
- Test: `internal/loopgate/memory_tcl_test.go`, `cmd/haven/chat_test.go`, `internal/shell/commands_test.go`

- [x] **Step 1: Extend the explicit memory request shape**

Add optional metadata fields needed for TCL analysis, such as:

- original utterance text
- candidate source label
- actor/source channel

These fields must be optional so existing callers stay compatible.

- [x] **Step 2: Build a `MemoryCandidate` from explicit memory requests**

In Loopgate, create a helper that converts:

- raw request metadata
- normalized `fact_key`
- normalized `fact_value`
- `reason`

into a `MemoryCandidate`.

- [x] **Step 3: Route `RememberMemoryFact` through TCL analysis before persistence**

Behavior:

- analyze candidate
- on `KEP`, continue to existing explicit fact persistence
- on `DRP`, return an explicit non-persisted governance outcome with a stable machine-readable code; do not use a success-shaped response that implies the fact was stored
- on `RVW` / `FLG` / `QTN`, return explicit governance outcome
- on high-confidence deny signatures, hard-deny with stable denial code

Current Phase-1 slice note:

- implemented now: `KEP` continues to persistence, curated high-confidence dangerous signatures hard-deny before persistence
- deferred to Task 5: broader explicit review/quarantine outcome codes and audit-safe TCL summaries

Note:

- do not silently downgrade or auto-correct unsafe content
- preserve append-only audit and redaction rules
- wire the TCL decision at the real mutation choke point inside `rememberMemoryFact`, before new artifacts are persisted

- [x] **Step 4: Pass original user utterance from the Wails reference client (`cmd/haven/`) deterministic memory writes**

`cmd/haven/memory_intent.go` currently derives a `MemoryRememberRequest` from user text. Extend that path so Loopgate receives the original utterance for TCL analysis, not just the normalized fact fields.

- [x] **Step 5: Keep capability and direct-client behavior aligned**

`memory.remember` capability execution, direct `RememberMemoryFact` calls, and shell `/memory remember` must hit the same TCL-aware path and produce the same governance outcome.

Run: `go test ./internal/loopgate/... ./cmd/haven/...`
Expected: explicit remember flow still passes for benign data; known-bad patterns are denied or quarantined

## Task 5: Preserve observability and audit safety

**Files:**
- Modify: `internal/loopgate/memory_capability.go`
- Modify: `internal/loopgate/memory_tcl.go`
- Test: `internal/loopgate/memory_tcl_test.go`, `internal/loopgate/continuity_memory_test.go`

- [x] **Step 1: Add audit-safe TCL analysis summaries**

Audit/log payloads may store:

- disposition
- signature family ID
- risk tier
- source lane
- redacted reason code

Audit/log payloads must not store:

- raw secret-like content
- full original user utterance when it contains denied material
- raw hostile instructions

The same redaction rule applies to:

- capability denial responses
- HTTP/API error responses
- the Wails reference client (`cmd/haven/`) runtime facts built from error text
- shell command error output

- [x] **Step 2: Add denial-code coverage**

Add stable denial/review codes for:

- known dangerous semantic signature
- quarantine-required candidate
- review-required candidate
- invalid TCL normalization/validation

Current Phase-1 slice note:

- implemented now: stable codes for dangerous, invalid, dropped, review-required, and quarantine-required explicit-memory governance outcomes
- current curated policy only emits `dangerous` and `keep` paths today; review/quarantine codes are wired for future TCL policy tiers

- [x] **Step 3: Prove no raw dangerous content leaks into audit**

Run targeted tests that submit hostile memory-write candidates and assert that audit files do not contain the raw denied payload.

Also verify:

- the Wails reference client (`cmd/haven/`) deterministic-memory runtime facts do not echo raw denied payload text
- shell `/memory remember` output does not echo raw denied payload text

## Task 6: Phase 1 verification and documentation handoff

**Files:**
- Modify: `docs/roadmap/roadmap.md`
- Test: repo-level targeted suites

- [ ] **Step 1: Run targeted TCL and memory suites**

Run:

```bash
go test ./internal/tcl/... ./internal/loopgate/... ./cmd/haven/...
```

Expected:

- TCL unit tests pass
- explicit memory flow tests pass
- no regressions in the Wails reference client (`cmd/haven/`) memory-intent tests

- [ ] **Step 2: Update roadmap**

Add a short note in `docs/roadmap/roadmap.md` that TCL phase 1 exists or is in progress, and that the next lift is broader candidate ingestion plus TCL-informed continuity.

- [ ] **Step 3: Record implementation boundary**

Document clearly that phase 1 does **not** yet:

- feed generic the Wails reference client (`cmd/haven/`) thread events into TCL
- replace continuity distillation
- widen durable memory candidacy to raw assistant prose
- make resonate keys reconstruct arbitrary text

## Phase 2: Signature registry and AV-like memory defense

**Files:**
- Modify: `internal/tcl/signatures.go`
- Modify: `internal/tcl/policy.go`
- Create: `internal/tcl/registry_test.go`

- [ ] Expand the curated signature registry beyond the initial deny set
- [ ] Add family-level matching across wording variants
- [ ] Add operator-readable risk-tier summaries
- [ ] Add tests for exact vs family vs motif behavior

## Phase 3: Broader memory candidate ingestion

**Files:**
- Modify: `internal/loopgate/continuity_memory.go`
- Modify: `cmd/haven/memory.go`
- Modify: `internal/loopgate/todo_capability.go`
- Test: `internal/loopgate/continuity_memory_test.go`, `cmd/haven/chat_test.go`

- [ ] Introduce additional `MemoryCandidate` producers for:
  - task/goal/todo transitions
  - selected structured assistant/tool outputs
- [ ] Replace generic the Wails reference client (`cmd/haven/`) thread-event mapping with structured candidate emission
- [ ] Keep raw prose out of durable memory candidacy by default
- [ ] Route kept candidates into continuity/distillate lane, not explicit fact lane

## Phase 4: TCL-informed distillates and compressed resonate keys

**Files:**
- Modify: `internal/loopgate/continuity_memory.go`
- Modify: `internal/loopgate/continuity_runtime.go`
- Test: `internal/loopgate/continuity_memory_test.go`

- [ ] Extend distillate derivation to store TCL-informed semantic contour alongside richer authoritative structure
- [ ] Derive resonate keys from compact TCL-informed semantic handles
- [ ] Ensure keys reconstruct only category, intent family, risk posture, and bounded anchors
- [ ] Prove keys do not reconstruct arbitrary prose or bypass distillate lineage

## Phase 5: Wake-state and recall refinement

**Files:**
- Modify: `internal/loopgate/continuity_runtime.go`
- Modify: `cmd/haven/memory.go`
- Modify: `internal/loopgate/client.go`
- Create: `cmd/haven/memory_test.go`
- Test: `internal/loopgate/continuity_memory_test.go`, `cmd/haven/memory_test.go`

- [ ] Use TCL-informed semantics to improve wake ranking and trimming
- [ ] Keep remembered / derived / fresh boundaries explicit in startup and recall
- [ ] Allow bounded semantic-family recall without widening into unrestricted semantic search
- [ ] Preserve deterministic trimming and provenance-rich source refs

## Deferred decisions

These are intentionally deferred until phase-1 code exists:

- whether TCL compact forms are persisted verbatim or only as derived fields
- how much of TCL `REL` should influence resonate key shape
- whether review queues need a dedicated persisted UI object or can reuse existing governance paths
- whether tool-output candidates need per-capability allowlists in policy or code

## Non-goals

- replacing continuity stream, distillates, or wake-state with TCL
- allowing model output to define its own permissions or memory authority
- widening memory candidacy to generic prose or repeated access frequency
- using TCL as a hidden semantic search layer over all history
- reconstructing raw text from resonate keys

## First implementation slice recommendation

Start with:

1. Task 1: lock docs and contract
2. Task 2: create `internal/tcl` core
3. Task 3: add signature tiers and curated deny set
4. Task 4: wire only `RememberMemoryFact` / `memory.remember`
5. Task 5: add audit-safety coverage

Do **not** begin phase 3 candidate expansion until phase 1 is tested and stable.
