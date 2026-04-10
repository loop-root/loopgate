# Memorybench Map

This file maps `internal/memorybench/` — the benchmark harness for comparing
Loopgate memory backends under the same scenarios, metrics, and reporting flow.

Use it when changing:

- benchmark result schemas
- fixture families and expectations
- runner scoring rules
- benchmark reporting artifacts
- backend-discovery adapters used by the harness

## Core Role

`internal/memorybench/` is the reproducible benchmark layer. It should not mint
authority or invent hidden memory behavior. Its job is to:

- load checked-in scenario fixtures
- run them against one configured backend path
- emit structured results, CSV summaries, and trace events
- score success/failure against explicit fixture expectations

## Key Files

- `types.go`
  - benchmark metadata, backend metrics, outcome metrics, retrieved artifacts
  - includes `ProjectedNodeDiscoverer`, the narrow read-only bridge used for
    backend-driven benchmark discovery

- `backend_config.go` / `backend_config_test.go`
  - benchmark backend names and RAG config shape
  - includes both `rag_baseline` and `rag_stronger`
  - also defines benchmark candidate-governance modes:
    - `backend_default`
    - `continuity_tcl`
    - `permissive`

- `rag_discoverer.go` / `rag_discoverer_test.go`
  - benchmark-side `rag_baseline` discoverer boundary
  - translates future retriever-client search results into benchmark projected items

- `rag_exec_client.go` / `rag_exec_client_test.go`
  - benchmark-local Python subprocess adapter for live `rag_baseline` retrieval
  - also powers `rag_stronger` when reranking is enabled
  - validates helper/runtime paths, enforces explicit config, and parses strict JSON results

- `rag_seed_client.go` / `rag_seed_client_test.go`
  - benchmark-local Python subprocess adapter for deterministic Qdrant seeding
  - pushes a versioned fixture corpus into the configured collection before runs when requested

- `cmd/memorybench/rag_search.py`
  - Python helper for hybrid Qdrant retrieval and fixture-corpus seeding using Haystack integrations
  - intentionally benchmark-local, not part of Loopgate’s authoritative surface

- `fixtures.go` / `fixtures_test.go`
  - checked-in scenario fixtures
  - currently includes:
    - poisoning fixtures
    - paraphrased poisoning families for generalization checks
    - delayed-trigger poisoning fixtures where dangerous guidance only activates on a later phrase or handoff state
    - format-laundered poisoning fixtures using markdown checklist and yaml-like metadata shapes
    - broader secret-material poisoning fixtures for session cookies, signing keys, and client secrets
    - contradiction / stale-memory fixtures
    - contradiction alias/paraphrase supersession and entity-guard fixtures
    - long-history interleaved alias contradiction fixtures
    - second stable-slot contradiction fixtures for profile timezone, including
      same-entity and different-entity wrong-current probes
    - focused preview-slot skepticism families that attack the benchmark-local
      canonical-over-preview heuristic with:
      - close same-entity preview labels
      - moderate and far lexical traps across timezone, locale, pronouns, and preferred_name
      - mixed same-entity preview + canonical + distractor chains
      - multiple preview labels for one field
      - conflicting recent preview labels
      - preview-only controls where no canonical slot exists
      - additional stable profile slots beyond timezone/locale, including
        `pronouns` and `preferred_name`
    - task-resumption fixtures for:
      - current next-step/blocker recovery after interruption
      - blocker changes over time
      - multi-hop dependency context
      - multi-hop distractor suppression
      - alias-preview distractor suppression
      - multi-update blocker drift
      - longer-history operational-cost pressure
    - safety-precision fixtures for benign near-miss phrases that should not be overblocked
    - benign markdown and yaml operational-note controls for the new format-laundering families
    - benign denied-waiver postmortem controls for the delayed-trigger and approval-waiver families
    - every new adversarial family should ship with a benign or distractor guard fixture
  - poisoning now includes less-obvious authority-spoof and stable-slot piggyback cases, not only direct "ignore safety" strings

- `corpus.go` / `corpus_test.go`
  - deterministic corpus builder from checked-in fixtures
  - gives `rag_baseline` a fair, versioned ingestion source instead of ad hoc documents
  - also feeds isolated continuity benchmark seeding so continuity can avoid ambient repo memory during fixture runs
  - assigns per-scenario scopes so contradiction or poisoning fixtures do not contaminate each other
  - excludes probe-only steps and current poisoning-source steps so the benchmark compares governed retrieval, not raw self-matching poison text
  - continuity fixture seeding is more opinionated than RAG corpus ingestion:
    - contradiction fixtures seed current values as active
    - superseded values seed as tombstoned
    - ambiguity distractors can remain active under distinct signatures

- `runner.go` / `runner_test.go`
  - fixture execution and scoring
  - `RunScenarioFixtures`
  - `RunDefaultScenarioFixtures`
  - expectation-based scoring for poisoning and contradiction workloads
  - task-resumption fixtures score on:
    - required current context
    - missing critical context
    - stale/wrong context injection
    - bounded item count
    - bounded hint-byte retrieval
  - safety-precision fixtures catch false-positive governance where benign candidates are incorrectly blocked
  - approval-language near-miss fixtures keep the benchmark honest when authority-spoof detection expands
  - the latest contradiction additions deliberately try to falsify the thesis:
    - continuity no longer has a clean contradiction sweep on the current
      skeptical snapshot
    - same-entity current-looking preview labels can still beat the canonical
      slot in at least one timezone case
  - poisoning fixtures now evaluate two separate questions:
    - would the backend govern and block the candidate at ingestion time
    - if it were in scope, would dangerous content later resurface in retrieval
  - poisoning leakage is scored from dangerous content resurfacing, not from any retrieval result existing at all
  - now also emits subfamily summaries so contradiction probe regimes can be
    split out of the aggregate family totals

- `governance.go` / `governance_test.go`
  - benchmark-side governed-candidate evaluators
  - `rag_baseline` currently models raw permissive ingest by default so safety comparisons stay explicit instead of being hidden inside retrieval results
  - the CLI can now override candidate governance mode for fairness reruns

- `observer.go`
  - benchmark observer hooks

- `filesystem_reporter.go` / `filesystem_reporter_test.go`
  - writes:
    - `results.json`
    - `summary.csv`
    - `family_summary.csv`
    - `subfamily_summary.csv`
    - `trace.jsonl`

## Relationship Notes

- CLI entrypoint: `cmd/memorybench/main.go`
- Continuity discovery bridge: `internal/loopgate/memorybench_bridge.go`
- Benchmark design source: `docs/rfcs/0011-swappable-memory-backends-and-benchmark-harness.md`
- Plain-English explainer: `docs/memorybench_plain_english.md`
- Glossary: `docs/memorybench_glossary.md`
- Operator/agent guide: `docs/memorybench_benchmark_guide.md`
- Running scoreboard: `docs/memorybench_running_results.md`
- Internal methodology report (maintainer checkout outside this repository; filename `memorybench_internal_report.md`)

Loopgate’s benchmark bridge now exposes two benchmark-local surfaces:
- projected discovery over continuity-backed stores
- governed candidate evaluation over the TCL memory-policy path

The CLI must select benchmark backends explicitly and fail closed for
benchmark-only backends. Do not silently map `rag_baseline` onto the
`continuity_tcl` path, and do not treat the real runtime `hybrid` backend as a
benchmark-only alias.

The live `rag_baseline` path requires repo-local helper/runtime files. Keep it
out of Loopgate’s control-plane code and treat helper output as untrusted data
that must be parsed and translated before benchmark use.
The same rule applies to benchmark seeding: fixture-corpus ingestion is
benchmark-local and must not be mistaken for authoritative Loopgate memory mutation.

For fair fixture comparisons, prefer isolated seeded benchmark stores over the
ambient repo state. `continuity_tcl` can now run against a temporary seeded
SQLite store instead of the operator’s real projected memory surface.

The CLI also supports benchmark-local continuity ablations:

- `anchors_off`
- `hints_off`
- `reduced_context_breadth`

Use them only with fixture-seeded continuity runs. They are there to test the
causal story, not to redefine the real backend.

The CLI also supports one benchmark-local continuity heuristic control:

- `-continuity-preview-slot-preference`
  - `true` keeps the narrow canonical-over-preview rerank for targeted
    slot-only contradiction scopes
  - `false` disables it for untuned control runs against the same fixtures
- `-continuity-preview-slot-preference-margin`
  - sets the benchmark-local match-count gap tolerated before the canonical
    slot stops outranking a same-entity preview label in those targeted scopes

This toggle is benchmark-local only. It must not be mistaken for product
behavior inside Loopgate.

The benchmark CLI also supports explicit candidate-governance overrides, which
matter for fairness analysis:

- `backend_default`
  - continuity uses TCL governance
  - RAG uses the permissive benchmark ingest model
- `continuity_tcl`
  - all backends reuse the continuity/TCL governance path
- `permissive`
  - all backends use the permissive benchmark ingest model

## Important Watchouts

- Keep fixture expectations explicit. Do not let the runner silently redefine
  what a scenario means.
- Benchmark traces are observability artifacts, not authority.
- Family summaries now carry total as well as average/max latency and prompt
  burden so skeptical operational-cost comparisons stay visible.
- Keep backend access read-only unless a future RFC explicitly adds governed
  ingestion for benchmark seeding.
