**Last updated:** 2026-03-27

# RFC 0011: Swappable Memory Backends and Benchmark Harness

Status: draft

## 1. Summary

We need a fair way to compare the current continuity/TCL memory system
against a conventional RAG baseline without changing the rest of the product.

This RFC proposes:

- one stable Loopgate memory API for Haven and workers
- one internal backend boundary for memory retrieval/consolidation engines
- at least two interchangeable backends:
  - `continuity_tcl`
  - `rag_baseline`
  - `rag_stronger`
- one benchmark harness that runs both systems against the same workloads,
  prompt budgets, and success criteria

The goal is not to prove the continuity model by ideology.
The goal is to determine whether it actually produces better behavior for the operator experience.

## 2. Motivation

The current continuity design is modeled on how human memory tends to behave:

- bounded working context instead of full replay
- episodic continuity rather than a mutable fact table
- semantic consolidation over time
- contradiction handling by stable semantic slot
- associative relations rather than pure lexical similarity

Conventional RAG is different.
It is typically:

- chunk store + embedding index
- similarity retrieval
- weak contradiction handling
- weak supersession semantics
- good fuzzy document recall

Those are different philosophies of memory.
They should be compared on product behavior, not taste.

## 3. Goals

- keep Haven and worker-facing memory calls stable while backend choice changes
- compare continuity/TCL against RAG under the same workloads
- preserve Loopgate as the authority boundary regardless of backend
- measure not only retrieval relevance, but end-to-end agent usefulness
- allow future hybrid backends without another architectural split

## 4. Non-goals

This RFC does not:

- replace current Loopgate memory governance with direct backend access
- declare a winner in advance
- require a separate repository for the first comparison
- expose raw vector stores or backend internals to Haven
- allow the model to choose the active backend at runtime

## 5. Design principle

The swap boundary should sit inside Loopgate, below policy/governance and above
physical storage/index implementation.

That keeps:

- approval and memory candidacy policy unchanged
- durable-memory authority in Loopgate
- Haven contract stable
- benchmark results attributable to the memory engine, not to unrelated API drift

## 6. Proposed backend boundary

Haven should continue to talk to the existing Loopgate memory surfaces:

- `memory.remember`
- continuity inspection
- wake-state load
- discover
- recall

Loopgate should route those operations through an internal backend interface
after request validation and governance.

Suggested shape:

```go
type MemoryBackend interface {
	Name() string

	// Store governed durable artifacts produced by Loopgate.
	StoreInspection(ctx context.Context, inspection InspectionArtifact) error
	StoreDistillate(ctx context.Context, distillate DistillateArtifact) error
	StoreExplicitFact(ctx context.Context, fact ExplicitFactArtifact) error

	// Build startup context.
	BuildWakeState(ctx context.Context, request WakeStateRequest) (WakeStateResult, error)

	// Serve bounded retrieval.
	Discover(ctx context.Context, request DiscoverRequest) (DiscoverResult, error)
	Recall(ctx context.Context, request RecallRequest) (RecallResult, error)
}
```

Important boundary:

- Loopgate still decides whether a thing may become durable memory
- backends decide how governed memory is indexed, consolidated, and retrieved
- backends must not mint authority, approvals, or policy outcomes

## 7. Initial backend set

### 7.1 `continuity_tcl`

This is the current direction:

- append-only continuity stream
- distillates
- wake-state projection
- explicit remembered facts
- TCL semantic projection
- anchor tuples for contradiction handling
- relation links as future work

Expected strengths:

- preference/state updates
- contradiction suppression
- compact wake-state assembly
- explicit provenance
- agent continuity

Expected weaknesses:

- fuzzy long-document recall
- broad semantic lookup over unstructured corpora

### 7.2 `rag_baseline`

This is the comparison system, not a straw man.

Minimum baseline:

- chunked text/record ingestion
- embeddings-based retrieval
- metadata filters for scope and source kind
- bounded top-k recall
- simple recency weighting

Initial benchmark shell config should at least carry:

- Qdrant base URL
- collection name
- embedding model label
- reranker label

The first live implementation may remain benchmark-local rather than Loopgate
authoritative:

- `cmd/memorybench` may shell out to a checked-in Python helper
- the helper may use Haystack + Qdrant for hybrid retrieval
- the Go harness must still parse helper output as untrusted data
- production or control-plane paths must not silently adopt the benchmark helper

Unsupported RAG features should fail closed. For example, if reranking is not
yet wired, the harness must reject a configured reranker rather than ignore it.

### 7.3 `rag_stronger`

`rag_stronger` is an explicit second RAG comparator rather than a hidden mode
inside `rag_baseline`.

It may add one standard RAG improvement layer, such as reranking over a wider
candidate pool, but it must still obey the same benchmark fairness rules:

- same checked-in fixture corpus
- same benchmark tasks
- same prompt budgets
- same evaluation rubric

If `rag_stronger` fails to outperform `rag_baseline`, the benchmark should say
that plainly rather than silently folding them together.

The RAG comparators should still respect Loopgate policy:

- only eligible content enters the index
- quarantined or denied content stays out
- raw secret-bearing content remains denied

Expected strengths:

- semantic document retrieval
- fuzzy lookup across older material
- lower implementation complexity in some retrieval paths

Expected weaknesses:

- weak contradiction/supersession semantics
- more prompt bloat risk
- less explicit continuity structure

### 7.4 `hybrid` (future)

Hybrid should remain future work until the benchmark tells us whether it is
actually needed.

## 8. Benchmark harness

The first benchmark harness should live in this repo so it can call the same
Loopgate code and the same evaluation fixtures.

The first runnable harness entrypoint should be:

- `cmd/memorybench`

Do not begin with a separate repo.
That introduces too many uncontrolled differences.

The harness should load checked-in scenario fixtures rather than only synthetic
hard-coded smoke inputs. Synthetic smoke remains useful for reporter/bootstrap
checks, but benchmarkable behavior must come from fixture-driven scenarios.

Where a backend needs explicit corpus ingestion, that ingestion should also come
from checked-in, versioned benchmark fixtures rather than ad hoc hand-loaded
documents. The benchmark must be able to rebuild the comparison corpus
deterministically.

That governed corpus should exclude benchmark inputs that represent denied or
quarantined poisoning candidates. Otherwise the benchmark only proves that raw
poison text is searchable after being indexed by design.

Poisoning workloads should also distinguish between two different questions:

- would the backend govern and block the candidate at ingestion time
- if dangerous content were present in the backend’s indexed corpus, would it later resurface in retrieval or prompt context

Those are related but not identical. A benchmark that collapses them together
can either unfairly punish governed systems for benign retrieval noise or hide a
backend that would have accepted the dangerous candidate in the first place.

Fixture corpora should also isolate scenarios by scope so one contradiction or
poisoning case does not contaminate another. A benchmark that lets unrelated
fixtures retrieve each other’s records is measuring harness noise more than
backend behavior.

For local continuity-backed comparisons, `cmd/memorybench` may optionally point
at a repo root and query the current `continuity_tcl` projected-node discovery
surface rather than using synthetic retrieval observations alone.

For controlled fixture comparisons, the harness should also support isolated
continuity seeding so `continuity_tcl` can run against a temporary
fixture-derived store rather than the operator’s ambient real memory state.
That isolated continuity seeding should reflect governed memory shape rather
than raw transcript shape: current slot values should seed as active, superseded
values should seed as tombstoned, and ambiguity distractors may remain active
only when they live under distinct semantic slots.

Poisoning evaluation should score actual dangerous content resurfacing rather
than treating every retrieval result as a poisoning leak. Otherwise benign
retrieval noise looks like prompt-injection persistence even when the dangerous
candidate itself was correctly excluded from governed memory.

Where possible, the benchmark should call a backend-local governed-candidate
evaluator in addition to its retrieval path. For `continuity_tcl`, that
evaluator should reuse the TCL-backed memory-policy logic without mutating
authoritative state. For `rag_baseline`, the evaluator may explicitly model raw
permissive ingest if no equivalent governance layer exists.

The candidate-governance path should be explicit at run time. The harness
should support at least:

- `backend_default`
  - continuity uses TCL governance
  - RAG uses permissive benchmark ingest
- `continuity_tcl`
  - all backends reuse the TCL-backed governance path
- `permissive`
  - all backends use the permissive benchmark ingest model

This avoids a fairness trap where poisoning results are interpreted as a pure
retrieval contest when they are actually reflecting different ingestion-policy
semantics.

The safety bucket also needs benign near-miss fixtures. These should include
phrases that mention words like `secret` or `safety instructions` in harmless
contexts so the benchmark can measure false-positive blocking rather than only
blocking power. A system that blocks obvious poison but also suppresses benign
user preferences is not trustworthy enough to ship.

Those near-miss fixtures should evolve alongside the adversarial set. If the
benchmark adds authority-spoof or provenance-spoof poisoning cases, it should
also add benign approval/checklist notes so the system is not rewarded for
blocking every mention of `approval`, `audit note`, or similar operator-facing
language.

Benchmark backend selection must be explicit and fail closed. An unimplemented
`rag_baseline` or `hybrid` selection should return a clear error rather than
silently reusing the `continuity_tcl` path.

Once `rag_baseline` is benchmark-wired, that wiring should still remain local to
the benchmark harness unless and until a separate design explicitly introduces a
production retrieval path. The benchmark helper is not a Loopgate control-plane
feature.

### 8.1 Fairness rules

Each backend must use:

- the same input corpus
- the same prompt template family
- the same model provider/model where possible
- the same token budget
- the same evaluation tasks
- the same success rubric

If one backend requires explicit seeding or indexing, the harness should build
that corpus from the same checked-in fixture set used to define scenarios.

For decision-making, the benchmark should also summarize results into three
operator-facing buckets:

- truth maintenance
- safety and trust
- operational cost

### 8.2 Workload families

The benchmark set should include at least:

1. Preference updates
   - user changes theme, favorite tool, or naming preference
   - system must prefer the latest stable slot value

2. Task continuity
   - interrupted work resumes after delay or restart
   - system must recover current next-step and blocker state without replaying everything
   - benchmark scoring should explicitly track missing critical context and stale/wrong context injection during resume
   - include blocker changes over time, multi-hop dependency context, and longer-history bounded-cost resume cases

3. Contradiction handling
   - stale and current claims coexist in history
   - system must not surface both as simultaneous truth

4. Multi-hop association
   - A relates to B, B relates to C
   - system must recover useful linked context without brute-force replay

5. Fuzzy document recall
   - answer depends on semantically related prior text
   - system must locate relevant material from a larger corpus

6. Fresh-vs-remembered separation
   - system must distinguish remembered state from newly checked state

7. Adversarial/noisy memory
   - irrelevant, repetitive, or poisoning-shaped prior content
   - system should avoid contaminating wake state or recall

8. Memory poisoning and prompt-injection persistence
   - hostile content attempts to become durable memory or retrieval-relevant context
   - system should block, quarantine, suppress, or clearly isolate it

### 8.3 Metrics

Do not rely on retrieval relevance alone.

Track at least:

- task success rate
- task resumption success
- contradiction correctness
- missing critical context count
- wrong-context injection count
- stale-memory intrusion rate
- stale-memory suppression error rate
- false contradiction count
- false suppression count
- recall precision
- recall coverage
- provenance correctness
- poisoning attempt count
- poisoning blocked count
- poisoning leak count
- hint-only match count
- hint bytes retrieved and injected
- prompt token count
- latency
- model cost
- operator-correction rate

### 8.4 Human-reviewed scorecards

Some dimensions need human judgment.

Suggested rubric per run:

- `correct`
- `partially_correct`
- `incorrect`
- `contradictory`
- `overlong_context`
- `missed_relevant_memory`
- `used_stale_memory`
- `poisoning_leaked`

## 9. Benchmark observability

Benchmark observability must be built in from the start.
Do not rely on ad hoc logs as the primary source of truth.

The first benchmark runner should emit structured artifacts under:

`runtime/benchmarks/<run_id>/`

Minimum files:

- `results.json`
- `summary.csv`
- `family_summary.csv`
  - includes per-family pass counts plus average, max, and total operational metrics such as retrieval latency and prompt-token burden
- `subfamily_summary.csv`
  - includes narrower grouped aggregates such as `memory_contradiction.slot_only`
    vs `memory_contradiction.answer_in_query`
- `trace.jsonl`

Tracked headline comparisons may also be summarized in:

- `docs/memorybench_running_results.md`
- `~/Dev/projectDocs/morph/memorybench-internal/memorybench_internal_report.md` (maintainer archive; not in clone)
- `docs/memorybench_benchmark_guide.md`

Benchmark-local backend ablations are allowed when they remain:

- local to the benchmark harness
- explicit in CLI/config
- fail-closed
- clearly separate from authoritative Loopgate behavior

### 9.1 Required metadata

Each run artifact set should include:

- schema version
- benchmark version
- run id
- git commit
- backend name
- benchmark profile
- model provider and model name
- prompt-template hash
- token budget
- start and finish timestamps

Each scenario result should include:

- scenario id
- category
- subfamily id where applicable
- expected outcome
- rubric version
- fixture version
- backend metrics
- outcome metrics
- retrieved artifacts

### 9.2 Required backend metrics

At minimum:

- sync latency
- retrieval latency
- candidates considered
- items returned
- rows touched where applicable
- hint-only matches
- hint bytes retrieved
- hint bytes injected
- prompt tokens retrieved
- prompt tokens injected
- approximate final prompt size

### 9.3 Required outcome metrics

At minimum:

- pass/fail
- score
- end-to-end success
- retrieval correctness
- provenance correctness
- contradiction hits and misses
- false contradiction count
- false suppression count
- stale-memory intrusion count
- stale-memory suppression count
- poisoning attempt count
- poisoning blocked count
- poisoning leak count

### 9.4 Event hook interface

The harness should expose a small observer interface so reporting stays separate
from execution:

- `OnRunStarted`
- `OnScenarioStarted`
- `OnRetrievalCompleted`
- `OnEvaluationCompleted`
- `OnRunCompleted`

This allows:

- CLI summaries
- file reporters
- Haven benchmark views
- later charts or white-paper tables

without rewriting the runner.

## 10. Configuration model

Backend choice should be explicit in runtime config and never model-controlled.

Suggested shape:

```yaml
memory:
  backend: continuity_tcl   # continuity_tcl | rag_baseline | rag_stronger | hybrid
  benchmark_profile: default
```

Loopgate should fail closed on unknown backend names.

## 11. Storage expectations

Backends may use different internal storage, but must not weaken existing
security rules.

Requirements:

- no raw secret persistence
- no bypass around quarantine/memory-candidate policy
- no public API exposure
- no direct backend access from Haven
- append-only or explicitly derived artifacts where the current design expects them

The RAG baseline may use embeddings or indexes, but benchmark-local helper
transport, model caches, and vector stores remain implementation details rather
than new control-plane APIs.

## 12. Decision rule

This comparison should not ask, “which memory system is philosophically pure?”

It should ask:

- which backend helps the governed assistant finish work more reliably
- which backend keeps continuity coherent
- which backend stays bounded and explainable
- which backend has better operator trust characteristics

If `rag_baseline` wins on those product metrics, the continuity system has not
earned its complexity.

If `continuity_tcl` wins on stateful agent behavior while RAG wins on fuzzy
document retrieval, the likely answer is a narrow hybrid.

## 13. Rollout order

1. define the internal `MemoryBackend` boundary in Loopgate
2. adapt the current continuity/TCL path to the interface
3. implement a conservative `rag_baseline`
4. add benchmark fixtures and scorecards
5. compare both under the same harness before widening either design
