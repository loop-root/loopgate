# Haven / Morph: memory candidates, Loopgate authority, and implementation plan

**Last updated:** 2026-03-29  
**Status:** Phases **A–C** shipped in tree. **Phase D** is ongoing: run the commands below on every prompt/governance/continuity-client change; schedule **fair** memorybench comparisons when you need headline baseline deltas ([memorybench_benchmark_guide.md](../memorybench_benchmark_guide.md)).  
**Scope:** Swift Haven product (`~/Dev/Haven`), Loopgate continuity/TCL, prompts and client proposals — desk notes deferred; Wails prototype in `cmd/haven/` is out of scope for new work.

---

## 1. Correct authority model (what we are *not* doing)

**Haven/Morph does not decide what becomes durable distillates or wake-state truth.**

Per project RFCs and AMP:

- **Active conversation and tool calls are content, not authority** — presence in a thread does not grant durable memory ([RFC 0009](../rfcs/0009-memory-continuity-and-recall.md), [AMP RFC 0006](../AMP/AMP-RFCs/0006-continuity-and-memory-authority.md)).
- **Memory candidacy is explicit and deny-by-default** — only policy-eligible, provenance-bearing inputs may become candidates; unreviewed model prose is denied by default ([RFC 0010](../rfcs/0010-memory-candidate-eligibility-and-wake-state-policy.md)).
- **Loopgate owns** sealed-thread inspection, distillate derivation, resonate keys, and wake-state projection from Loopgate-owned artifacts ([RFC-MORPH-0005](../product-rfcs/RFC-MORPH-0005:%20Continuity%20and%20Memory%20Model.md), [continuity stream architecture](../design_overview/continuity_stream_architecture.md)).

**Morph’s role** is to:

- emit **structured proposals** (e.g. `memory.remember` tool calls with normalized keys/values; sealed-thread **inspection** requests with attributable events),
- surface **projections** Loopgate returns (wake state, recall, inspection status),

—not to treat client-side “distillation” as a bypass around inspector governance.

Any client path that submits a thread to `InspectContinuityThread` should be understood as **proposing continuity for inspection**, with **Loopgate’s inspector + TCL governance** determining outcomes (including “no durable artifact”).

---

## 2. How proposals become (or do not become) memory today (Loopgate)

### 2.1 `memory.remember` (explicit candidate channel)

Execution flows through `rememberMemoryFact` → TCL analysis (`analyzeExplicitMemoryCandidate`) → governance decision (`memoryRememberGovernanceDecision`) → durable records **only** when policy allows (`internal/loopgate/continuity_memory.go`, `memory_capability.go`, `memory_tcl.go`).

So the **model suggests** a candidate via the capability envelope; **Loopgate + TCL** accept, quarantine, or deny. Increasing *suggestions* does not weaken authority if governance and audit paths stay fail-closed.

### 2.2 Continuity inspection (sealed-thread proposal channel)

The architecture doc describes Morph submitting sealed threads for **inspection**; Loopgate decides durable derivation ([continuity_stream_architecture.md](../design_overview/continuity_stream_architecture.md)). That matches “suggest, don’t authorize.”

### 2.3 TCL’s role

TCL is the **canonical semantic vocabulary** for validated memory nodes and conservative anchoring ([RFC 0014](../rfcs/0014-tcl-conformance-and-anchor-freeze.md), [RFC 0013](../rfcs/0013-continuity-tcl-storage-and-query-backend.md), [TCL RFC set](../TCL-RFCs/Thought%20Compression%20Language.md)). The benchmark harness exercises **candidate governance** on the continuity path, not “trust the model because it sounds plausible.”

---

## 3. What memorybench supports (why “more candidates” can be safe)

See [memorybench_plain_english.md](../memorybench_plain_english.md), [memorybench_running_results.md](../memorybench_running_results.md), [memorybench_glossary.md](../memorybench_glossary.md).

**Headline takeaway:** On the promoted fixture slice, `continuity_tcl` scores strongly on **poisoning/governance**, **truth maintenance**, and **task resumption** versus the benchmarked RAG comparators — with the documented caveat that poisoning buckets are partly a governance differential under the harness, not a universal real-world safety proof.

**Policy-matched reruns** show that once candidate governance is aligned, remaining gaps concentrate in specific contradiction families (see running results for the enumerated probe names).

**Implication for product:** Raising the **rate of structured memory proposals** (e.g. more `memory.remember` calls with valid keys and bounded values) is consistent with the architecture **if**:

- TCL + inspector governance remain the gate,
- prompts do not encourage secrets, long prose blobs, or “remember everything,”
- we monitor denials and operator-visible review flows,
- regression tests and memorybench runs stay part of change discipline.

---

## 4. Runtime facts (Phase A lever)

**Phase A** widened model guidance so Haven chat **proposes** structured `memory.remember` candidates when users state durable preferences, goals, or stable context — while still forbidding secrets, long dumps, and “saved forever” claims until tool success. The live contract is **`buildHavenRuntimeFacts` / `buildResidentCapabilityFacts`** in `internal/loopgate/server_haven_chat.go` (Swift via `/v1/haven/chat`). The frozen Wails prototype may still carry older strings; do not treat it as source of truth.

---

## 5. Implementation plan (Haven / Morph + Loopgate)

### Phase A — Align prompts with “propose candidates; Loopgate decides”

**Status (2026-03-28):** **Product path:** runtime facts and `memory.remember` catalog hints in `internal/loopgate/server_haven_chat.go` (Swift uses these via `/v1/haven/chat`). The Wails prototype (`cmd/haven/…`) received parallel string edits earlier; **Wails is frozen** — do not extend it ([`AGENTS.md`](../../AGENTS.md)). Further tuning can follow operator feedback and memorybench reruns.

**Goal:** Increase **structured** suggestions without treating model output as authority.

1. **Revise Haven runtime facts** (Loopgate `server_haven_chat.go`):
   - Replace ultra-narrow “only if user said remember” with guidance like:
     - Prefer **short, structured** fact keys and values Loopgate can normalize.
     - Propose when the user states **durable preferences, standing goals, stable profile/work context** they clearly want carried across sessions — not transient chat noise.
     - **Never** store secrets, credentials, or long unstructured dumps.
     - If uncertain, **one clarifying question** or skip — but “uncertain” should mean epistemic ambiguity, not “never propose.”
   - Keep explicit denial language: candidates **may be rejected** by policy; the model must not claim something is “saved forever” until tool success says so.

2. **Client alignment:** Product UX is **Swift Haven** only (`~/Dev/Haven`). **`cmd/haven` (Wails) is frozen** — do not extend it for parity. Runtime facts for chat live in **Loopgate** `server_haven_chat.go`, which Swift consumes via `/v1/haven/chat`.

3. **Tests:**
   - Existing governance tests must still pass (denied candidates, audit, TCL invalid input).
   - Add/adjust tests if any prompt-adjacent behavior is asserted in golden text.

### Phase B — Post-turn continuity inspection proposals (shipped)

**Status (2026-03-29):** **Shipped.** **Wails is frozen** for product (`AGENTS.md`). **Swift Haven** proposes continuity after chat via **`POST /v1/haven/continuity/inspect-thread`** (Loopgate loads the thread from its threadstore; the client sends only `thread_id`). Implementation: `internal/loopgate/server_haven_continuity.go`, regression `TestHavenContinuityInspectThread_SubmittedAndSkipped` in `server_haven_continuity_test.go`. API: [LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md](../setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md) §7.3.

**Goal:** Use the **inspection API** only as a **proposal pipe**, consistent with [continuity_stream_architecture.md](../design_overview/continuity_stream_architecture.md).

1. **Frozen Wails / Morph ledger path:** Historical prototype only; no new inspection wiring there.

2. **Swift / haven/chat path:** After a completed Messenger turn, **`MorphHTTPClient.havenContinuityInspectThread`** runs **best-effort** when the turn did **not** hit **`approval_required`** (`MessengerViewModel`). **Haven actor + signed** body; **no** raw transcript in the HTTP payload.

3. **Observability:** Inspection and audit paths must continue to use **counts and ids**, not raw model text (existing redaction rules). When extending the handler, add tests for denial paths and malformed input.

### Phase C — Operator UX (projection only)

**Status (2026-03-28):** Denied/error capability results carry **`denial_code`** on `orchestrator.ToolResult` and append `(denial_code: …)` to **model-facing** tool result text inside **Loopgate** `/v1/haven/chat` (`internal/loopgate/server_haven_chat.go`). The same `ToolResult` shape is used by the frozen Wails prototype (`loopgateresult` + `cmd/haven/chat.go`) but **Swift does not receive structured per-tool rows** from chat — only final `assistant_text` (the model may still mention a denial in prose). Loopgate UI `emitUIToolDenied` already had codes for approval UI. Test: `TestHavenToolResultContent_IncludesDenialCode` in `server_haven_chat_test.go`.

1. **Recall / Memory windows:** Continue to show Loopgate **projections** (inventory, inspection review, purge/tombstone) — authoritative UI is Loopgate-mediated state.

2. **Lightweight feedback when `memory.remember` denies:** Where safe, surface a **typed** denial reason (no secrets) so operators understand governance is working — optional product polish.

### Phase D — Verification discipline (current focus)

**Goal:** No prompt, governance, or Haven-memory-surface change ships without automated regression; larger changes also get benchmark evidence when baselines matter.

**Required on every relevant PR / local change (fast):**

```bash
go test ./internal/loopgate/... -count=1
go test ./internal/memorybench/... -count=1
```

- **Loopgate** covers haven chat, continuity inspect-thread, memory governance, and signing boundaries (tens of seconds locally depending on module cache).
- **Memorybench** is the harness package; passing tests assert the benchmark fixtures and runner invariants still hold.

**When you change prompts, TCL governance, or continuity inspection behavior in a way that could move benchmark scores:** run **fair comparisons** and update narrative artifacts as described in [memorybench_benchmark_guide.md](../memorybench_benchmark_guide.md) (and optionally refresh [memorybench_running_results.md](../memorybench_running_results.md) if you are publishing new headline numbers).

**Optional product telemetry:** track **candidate volume vs. acceptance/denial** in dev/staging if metrics exist or are added (non-secret aggregates only).

---

## 6. Explicitly out of scope (this plan)

- **Desk notes as a desktop widget** — deferred; no change to current desk-note HTTP surface required for this plan.
- **Semantic search / unbounded RAG** — not part of RFC 0009/0010 first layers.
- **Weakening TCL or inspector gates** to “make memory stick more” — rejected; violates RFC 0010 and benchmarked governance story.

---

## 7. Reference index

| Topic | Document |
|--------|-----------|
| Continuity model | [RFC 0009](../rfcs/0009-memory-continuity-and-recall.md) |
| Candidate eligibility & wake state | [RFC 0010](../rfcs/0010-memory-candidate-eligibility-and-wake-state-policy.md) |
| Backend & harness | [RFC 0011](../rfcs/0011-swappable-memory-backends-and-benchmark-harness.md) |
| TCL storage/query | [RFC 0013](../rfcs/0013-continuity-tcl-storage-and-query-backend.md) |
| TCL anchor policy | [RFC 0014](../rfcs/0014-tcl-conformance-and-anchor-freeze.md) |
| AMP authority | [AMP RFC 0006](../AMP/AMP-RFCs/0006-continuity-and-memory-authority.md) |
| Client vs Loopgate split | [RFC-MORPH-0005](../product-rfcs/RFC-MORPH-0005:%20Continuity%20and%20Memory%20Model.md) |
| Stream + inspection | [continuity_stream_architecture.md](../design_overview/continuity_stream_architecture.md) |
| Benchmark narrative | [memorybench_plain_english.md](../memorybench_plain_english.md) |
| Headline numbers | [memorybench_running_results.md](../memorybench_running_results.md) |
| TCL specs | [TCL RFC set](../TCL-RFCs/Thought%20Compression%20Language.md) |

---

## 8. Summary

- **Authority:** Loopgate policy + TCL governance + inspector derivation decide durable memory and wake-state projection; Swift Haven **proposes** via `memory.remember` (model tool path) and **`POST /v1/haven/continuity/inspect-thread`** (server-loaded thread; no client transcript payload).
- **Product lever:** **Phase A** runtime facts encourage structured candidates; **Phase B** post-turn continuity proposals; **Phase C** denial signals in Loopgate chat tool results; **deny-by-default** governance and **Phase D** tests stay the proof burden.
- **Shipped:** A–C in tree as described in §5. **Phase D:** default to `go test ./internal/loopgate/...` + `go test ./internal/memorybench/...`; escalate to full memorybench fair runs per [memorybench_benchmark_guide.md](../memorybench_benchmark_guide.md) when scores matter. Desk notes remain a later macOS widget.
