**Last updated:** 2026-04-10

# Memorybench Benchmark Guide

This guide is for another agent or model that needs to run, extend, or audit
the continuity-memory benchmark locally.

It assumes the repo root is:

- `/Users/adalaide/Dev/loopgate`

It does not assume prior knowledge of the benchmark code.

## Quick regression (Phase D)

Before merging changes that touch prompts, continuity inspection, memory governance, or TCL paths exercised by Loopgate:

```bash
go test ./internal/loopgate/... -count=1
go test ./internal/memorybench/... -count=1
```

Product context: memory continuity and Phase D goals align with [RFC 0011](./rfcs/0011-swappable-memory-backends-and-benchmark-harness.md) and `docs/roadmap/roadmap.md`. Fair comparisons and scoreboard updates still follow the sections below.

If you are new to memorybench, start here first:

- [Memorybench In Plain English](/Users/adalaide/Dev/loopgate/docs/memorybench_plain_english.md)
- [Memorybench Glossary](/Users/adalaide/Dev/loopgate/docs/memorybench_glossary.md)
- [Memorybench Matrix And Relational Hint Plan](/Users/adalaide/Dev/loopgate/docs/roadmap/2026-04-10-memorybench-matrix-and-relational-hints.md)

## What memorybench is

`memorybench` is the benchmark harness for comparing:

- `continuity_tcl`
- `rag_baseline`
- `rag_stronger`

under the same checked-in fixture set.

Current skeptical fixture count:

- `70` total fixtures in the current checked-in code
- `14` poisoning
- `34` contradiction
- `13` task resumption
- `9` safety precision

The last scored honest rerun in
[memorybench_running_results.md](/Users/adalaide/Dev/loopgate/docs/memorybench_running_results.md)
still reflects the older `61`-fixture matrix. The checked-in fixture surface is
ahead of the last scored rerun until the new matrix is rerun.

Current promoted running scoreboard:

- the stable `61`-fixture scored matrix in
  [memorybench_running_results.md](/Users/adalaide/Dev/loopgate/docs/memorybench_running_results.md)
- the older `46`-fixture snapshot is historical only
- maintainer-only internal notes still live in
  `maintainer documentation checkout (memorybench internal report; outside this repository)`,
  but the timezone/locale preview-slot skepticism is now part of the promoted
  running-results headline

The CLI entrypoint is:

- [main.go](/Users/adalaide/Dev/loopgate/cmd/memorybench/main.go)

The core harness logic lives in:

- [runner.go](/Users/adalaide/Dev/loopgate/internal/memorybench/runner.go)
- [fixtures.go](/Users/adalaide/Dev/loopgate/internal/memorybench/fixtures.go)
- [types.go](/Users/adalaide/Dev/loopgate/internal/memorybench/types.go)

The live scoreboard lives in:

- [memorybench_running_results.md](/Users/adalaide/Dev/loopgate/docs/memorybench_running_results.md)

The internal methodology report lives in (maintainer checkout, not in clone):

- `maintainer documentation checkout (memorybench internal report; outside this repository)`

## Invariants

Do not violate these while extending the benchmark:

- benchmark helpers are benchmark-local only
- do not turn benchmark bridges into Loopgate control-plane APIs
- continuity fair runs must use isolated fixture-seeded state, not ambient repo memory
- continuity scored fixture runs must declare an explicit seeding mode:
  - `synthetic_projected_nodes`
  - `production_write_parity`
- `debug_ambient_repo` is debug-only and must not be used for scored runs
- new adversarial fixture families must ship with a benign or distractor guard
- every new fixture family must be recorded with:
  - the architectural mechanism under test
  - the failure mode it is meant to expose
  - the benign control or distractor that proves we are not just rewarding overblocking
- fail closed on unknown backend or unknown ablation mode
- do not silently substitute one backend for another
- fail closed when scenario filtering matches zero fixtures
- fail closed when production-parity fixture metadata asks for
  `remember_memory_fact` on a slot family the validated write contract does not support
- preview and distractor seeds must never use `remember_memory_fact`
- keep benchmark-local continuity heuristic controls explicit when used:
  - `-continuity-benchmark-local-slot-preference=true`
  - `-continuity-benchmark-local-slot-preference=false`
  - `-continuity-benchmark-local-slot-preference-margin=<n>`
- keep candidate governance mode explicit in benchmark runs:
  - `backend_default`
  - `continuity_tcl`
  - `permissive`

## Run classes and artifacts

Do not blend scored continuity runs and debug continuity runs into one number.

- `synthetic_projected_nodes`
  - scored fixture run
  - retrieval microbench over synthetic projected nodes
- `production_write_parity`
  - scored fixture run
  - authoritative supported profile facts are seeded through real authenticated
    `RememberMemoryFact` HTTP-over-UDS writes against product-valid `global`
    scope
  - supported continuity proposals are seeded through Haven's real
    `/v1/continuity/inspect-thread` path
  - supported task-resumption seeds are seeded through real `todo.add` and
    `todo.complete` capability execution
  - supported seeded scenarios are retrieved through the real
    `/v1/memory/discover` and `/v1/memory/recall` routes inside isolated
    Loopgate runtimes
  - the current checked-in `61`-fixture scored set no longer needs projected
    fixture ingest, so the honest continuity parity baseline is now a pure
    control-plane memory and workflow run
  - mixed control-plane plus projected-node SQLite remains a contingency for
    future unsupported fixtures, not the current default scored path
- `debug_ambient_repo`
  - unscored debug run only
  - never eligible for headline comparison

Every run now writes top-level metadata:

- `run_metadata.json`
- `seed_manifest.json` for continuity seeded runs

Before trusting a continuity result, inspect `run_metadata.json` and confirm:

- `backend_name`
- `retrieval_path_mode`
- `seed_path_mode`
- `continuity_seeding_mode`
- `comparison_class`
- `scored`

Current expected values:

- `synthetic_projected_nodes` should use
  `retrieval_path_mode=projected_node_sqlite_backend`
- `production_write_parity` should use
  `retrieval_path_mode=control_plane_memory_routes` with
  `seed_path_mode=control_plane_memory_and_todo_workflow_routes` for the
  current default scored fixture set
- `production_write_parity` mixed modes remain valid only when a future fixture
  cannot yet be expressed through real `memory.remember`,
  `/v1/continuity/inspect-thread`, or `todo` workflow routes
- the benchmark-local slot-preference wrapper applies only to synthetic
  projected-node control runs or projected-node fallback scopes
- when `production_write_parity` stays on pure control-plane routes with no
  projected-node fallback scopes, that benchmark-local preference flag is inert
- `synthetic_projected_nodes` should pair with `seed_path_mode=synthetic_projected_node_seed`
- `debug_ambient_repo` should pair with `seed_path_mode=ambient_repo_authoritative_state`
- seeded RAG runs should pair with `retrieval_path_mode=rag_search_helper` and
  `seed_path_mode=python_rag_fixture_seed`
- governance-only targeted RAG runs may intentionally seed a single
  out-of-scope placeholder document so the collection exists without putting
  any in-scope fixture text into the retrieval corpus

For production-parity continuity runs, inspect `seed_manifest.json` and confirm:

- authoritative explicit seeds use `seed_path=remember_memory_fact`
- those authoritative seeds also have:
  - `authority_class=validated_explicit_write`
  - `validated_write_supported=true`
- supported continuity proposal seeds use `seed_path=observed_thread_inspect`
  with `authority_class=observed_thread_inspection`
- supported task-resumption seeds use `seed_path=todo_workflow_capability`
  with `authority_class=todo_workflow_control_plane`
- the current default scored fixture set should not emit
  `seed_path=continuity_fixture_ingest`
- if fixture-ingest leftovers ever reappear, they must stay benchmark-local and
  must never appear as authoritative explicit writes
- `fact_key` records the actual seeded fact name
- `canonical_fact_key` records the registry-normalized remembered-fact key when
  one exists
- observed-thread seeds may legitimately carry a non-empty `fact_key` with an
  empty or different `canonical_fact_key`, especially for preview-label or
  entity-scoped distractor facts that stay continuity-derived rather than
  promoting into anchored remembered state
- observed-thread seeds now also preserve bounded event `Text` so benchmark
  continuity writes retain context words such as `preview card`, `teammate`,
  or `shadow operator` instead of collapsing to value-only tags

`comparison_class` is part of the evidence boundary:

- `scored_fixture_run`
- `targeted_debug_run`
- `unscored_debug_run`

Do not compare these classes as if they were the same benchmark.

## Local prerequisites

### 1. Go cache

Use the repo-local Go build cache:

```bash
export GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build
```

### 2. Python helper runtime

The RAG benchmark helper expects:

- `/Users/adalaide/Dev/loopgate/.cache/memorybench-venv/bin/python`

Create it if needed:

```bash
cd /Users/adalaide/Dev/loopgate
python3 -m venv .cache/memorybench-venv
./.cache/memorybench-venv/bin/pip install --upgrade pip
./.cache/memorybench-venv/bin/pip install haystack-ai qdrant-haystack fastembed-haystack
```

Notes:

- the helper will also create cache directories under `.cache/` for FastEmbed and Haystack
- first model downloads can take time

### 3. Docker / Qdrant

Run a local Qdrant container:

```bash
docker run -d \
  --name memorybench-qdrant \
  -p 127.0.0.1:6333:6333 \
  qdrant/qdrant:latest
```

Quick check:

```bash
docker ps
```

Expected:

- container named `memorybench-qdrant`
- port `127.0.0.1:6333->6333/tcp`

## Test the benchmark code first

Before any live runs:

```bash
cd /Users/adalaide/Dev/loopgate
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build \
  go test ./cmd/memorybench ./internal/memorybench/... ./internal/tcl/... -count=1
```

If this fails, do not trust live benchmark results yet.

## Baseline fair runs

Candidate governance defaults:

- `continuity_tcl` backend defaults to `-candidate-governance backend_default`
  which resolves to the continuity/TCL governance path
- `rag_baseline` and `rag_stronger` default to `-candidate-governance backend_default`
  which resolves to the permissive benchmark ingest model

If you are testing fairness on poisoning/governed-ingestion behavior, run the
RAG backends again with:

```bash
-candidate-governance continuity_tcl
```

That yields a policy-matched RAG comparison instead of the raw-ingest baseline.

### Continuity

Use isolated fixture seeds and declare the seeding mode explicitly:

```bash
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-live-continuity \
  -run-id continuity_fixture_synth_v1 \
  -profile fixtures \
  -backend continuity_tcl \
  -repo-root /Users/adalaide/Dev/loopgate \
  -continuity-seeding-mode synthetic_projected_nodes
```

Production-parity continuity runs use the same checked-in fixtures, but
authoritative supported slot seeds go through the validated write path:

```bash
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-live-continuity \
  -run-id continuity_fixture_parity_v1 \
  -profile fixtures \
  -backend continuity_tcl \
  -repo-root /Users/adalaide/Dev/loopgate \
  -continuity-seeding-mode production_write_parity
```

Tuned vs untuned continuity control for preview-slot skepticism:

```bash
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-heuristic-attack \
  -run-id continuity_preview_attack_tuned_v1 \
  -profile fixtures \
  -backend continuity_tcl \
  -candidate-governance continuity_tcl \
  -repo-root /Users/adalaide/Dev/loopgate \
  -continuity-seeding-mode synthetic_projected_nodes \
  -continuity-benchmark-local-slot-preference=true
```

```bash
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-heuristic-attack \
  -run-id continuity_preview_attack_untuned_v1 \
  -profile fixtures \
  -backend continuity_tcl \
  -candidate-governance continuity_tcl \
  -repo-root /Users/adalaide/Dev/loopgate \
  -continuity-seeding-mode synthetic_projected_nodes \
  -continuity-benchmark-local-slot-preference=false
```

Stronger benchmark-local preview-slot margin experiment:

```bash
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-heuristic-attack-v2 \
  -run-id continuity_preview_attack_margin3_v1 \
  -profile fixtures \
  -backend continuity_tcl \
  -candidate-governance continuity_tcl \
  -repo-root /Users/adalaide/Dev/loopgate \
  -continuity-seeding-mode synthetic_projected_nodes \
  -continuity-benchmark-local-slot-preference=true \
  -continuity-benchmark-local-slot-preference-margin=3
```

### Scenario filtering and targeted reruns

For focused investigation, use scenario filters instead of editing fixture code.

Supported selectors:

- `-scenario-id`
- `-scenario-set`
- `-category`
- `-subfamily`

Built-in targeted scenario sets currently include:

- `profile_slot_same_entity_preview`
- `profile_slot_preview_bias`
- `profile_slot_preview_controls`

Targeted runs are debug evidence, not headline evidence. They should show
`comparison_class=targeted_debug_run` in `run_metadata.json`.

Governance-only filtered continuity runs, such as `-category memory_poisoning`,
can now stay on `-continuity-seeding-mode production_write_parity`. Scenario
scopes with no continuity seeds are routed to empty discovery instead of
failing benchmark setup.

Before promoting any new benchmark headline tied to a local improvement:

- rerun the affected targeted scenario sets repeatedly, not just once
- require stable targeted results across repeated reruns before promotion
- treat the old timezone/locale `4/4` regression guard as required but not sufficient

Passing the old `4/4` guard is required, but you still need stable targeted
reruns on the harder preview-bias set before claiming a real headline
improvement.

### Plain RAG

Seed the checked-in fixture corpus:

```bash
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-live-rag \
  -run-id rag_live_fixture_v22 \
  -profile fixtures \
  -backend rag_baseline \
  -repo-root /Users/adalaide/Dev/loopgate \
  -rag-qdrant-url http://127.0.0.1:6333 \
  -rag-collection memorybench_default \
  -rag-seed-fixtures
```

### Stronger RAG

```bash
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-live-rag \
  -run-id rag_stronger_live_fixture_v8 \
  -profile fixtures \
  -backend rag_stronger \
  -repo-root /Users/adalaide/Dev/loopgate \
  -rag-qdrant-url http://127.0.0.1:6333 \
  -rag-collection memorybench_rerank \
  -rag-seed-fixtures
```

### Policy-matched RAG fairness rerun

Use the same retrieval backend with continuity/TCL candidate governance:

```bash
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-live-rag \
  -run-id rag_governed_fixture_v4 \
  -profile fixtures \
  -backend rag_baseline \
  -candidate-governance continuity_tcl \
  -repo-root /Users/adalaide/Dev/loopgate \
  -rag-qdrant-url http://127.0.0.1:6333 \
  -rag-collection memorybench_default \
  -rag-seed-fixtures
```

Stronger governed rerun:

```bash
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-live-rag \
  -run-id rag_stronger_governed_fixture_v4 \
  -profile fixtures \
  -backend rag_stronger \
  -candidate-governance continuity_tcl \
  -repo-root /Users/adalaide/Dev/loopgate \
  -rag-qdrant-url http://127.0.0.1:6333 \
  -rag-collection memorybench_rerank \
  -rag-seed-fixtures
```

Current policy-matched read:

- see [memorybench_running_results.md](/Users/adalaide/Dev/loopgate/docs/memorybench_running_results.md)
  for the current promoted `61`-fixture policy-matched numbers
- see `maintainer documentation checkout (memorybench internal report; outside this repository)`
  for the historical chronology, intermediate ablations, and tuned versus
  untuned continuity-control notes that are not part of the promoted scoreboard

## Current continuity ablations

These are benchmark-local continuity ablations.

Flag:

- `-continuity-ablation`
- `-continuity-benchmark-local-slot-preference`
- `-continuity-benchmark-local-slot-preference-margin`

Accepted values:

- `none`
- `anchors_off`
- `hints_off`
- `reduced_context_breadth`

Important caveat:

- `reduced_context_breadth` is a proxy for “reduced graph expansion”
- the current continuity benchmark path does not traverse graph edges yet
- this mode reduces returned related-context breadth instead of disabling a real edge-expansion stage
- `-continuity-benchmark-local-slot-preference` is not an ablation of the product
  backend; it is a benchmark-local tuned-vs-untuned control for the narrow
  canonical-over-preview reranking experiment
- `-continuity-benchmark-local-slot-preference-margin` is also benchmark-local only;
  it changes the allowed match-count gap for that same heuristic experiment and
  must not be treated as shipped Loopgate behavior

Current ablation read on the 44-fixture snapshot:

- `anchors_off`: `34/44`
  - contradiction `7/17`
  - slot-only contradiction `0/10`
- `hints_off`: `14/44`
  - contradiction `0/17`
  - task resumption `0/13`
- `reduced_context_breadth`: `29/44`
  - contradiction `15/17`
  - task resumption `0/13`

### Anchors off

```bash
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-live-continuity \
  -run-id continuity_ablation_anchors_off_v6 \
  -profile fixtures \
  -backend continuity_tcl \
  -repo-root /Users/adalaide/Dev/loopgate \
  -continuity-seeding-mode synthetic_projected_nodes \
  -continuity-ablation anchors_off
```

### Hints off

```bash
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-live-continuity \
  -run-id continuity_ablation_hints_off_v6 \
  -profile fixtures \
  -backend continuity_tcl \
  -repo-root /Users/adalaide/Dev/loopgate \
  -continuity-seeding-mode synthetic_projected_nodes \
  -continuity-ablation hints_off
```

### Reduced related-context breadth

```bash
env GOCACHE=/Users/adalaide/Dev/loopgate/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-live-continuity \
  -run-id continuity_ablation_reduced_breadth_v6 \
  -profile fixtures \
  -backend continuity_tcl \
  -repo-root /Users/adalaide/Dev/loopgate \
  -continuity-seeding-mode synthetic_projected_nodes \
  -continuity-ablation reduced_context_breadth
```

## Output artifacts

Each run writes:

- `run_metadata.json`
- `results.json`
- `summary.csv`
- `family_summary.csv`
- `subfamily_summary.csv`
- `trace.jsonl`
- `seed_manifest.json` for continuity seeded runs

Interpret them like this:

- `run_metadata.json`
  - top-level evidence classification
  - check `backend_name`, `continuity_seeding_mode`, `comparison_class`, and `scored`
- `seed_manifest.json`
  - continuity seeding proof
  - use this to confirm which records were authoritative validated writes and
    which were non-authoritative fixture-ingest distractors
- `summary.csv`
  - one row per fixture
  - quickest way to see passes/fails
- `family_summary.csv`
  - per-family aggregates
  - use this for pass totals, latency totals, hint-byte totals, prompt-token totals
- `subfamily_summary.csv`
  - narrower grouped aggregates such as:
    - `memory_contradiction.slot_only`
    - `memory_contradiction.answer_in_query`
  - use this when aggregate contradiction results are hiding materially easier
    probe shapes
- `results.json`
  - richest structured output
  - useful for agent parsing or diff tooling
- `trace.jsonl`
  - step-level observability
  - useful for debugging why a scenario changed

## How to compare runs

At minimum, compare:

- overall pass count
- per-family pass counts
- task-resumption total prompt tokens
- task-resumption total hint bytes
- task-resumption latency totals

The key question is not “which backend passes more fixtures in aggregate?”

It is:

- does continuity outperform RAG on truth maintenance and governed safety
- while staying operationally reasonable
- and when continuity loses, is it losing in a narrow, understandable way
  such as same-entity current-looking preview labels instead of broad entity confusion

Important poisoning caveat:

- the poisoning family currently measures a governance differential under the
  harness, not a neutral raw-retrieval contest
- continuity uses the real governed candidate path
- default RAG runs use a permissive benchmark governance stub
- the current policy-matched fairness reruns switch RAG to
  `-candidate-governance continuity_tcl`
- poisoning steps are excluded from the seeded RAG corpus by design
- if you want a policy-matched poisoning comparison, rerun RAG with
  `-candidate-governance continuity_tcl`

## How to add a new fixture family

1. Add the fixture(s) to [fixtures.go](/Users/adalaide/Dev/loopgate/internal/memorybench/fixtures.go).
2. Add or update the corresponding guard or distractor fixture.
3. Update [fixtures_test.go](/Users/adalaide/Dev/loopgate/internal/memorybench/fixtures_test.go).
4. Add runner regressions in [runner_test.go](/Users/adalaide/Dev/loopgate/internal/memorybench/runner_test.go).
5. If continuity fair seeding should include it, make sure the existing generic seed builders already cover it:
   - [main.go](/Users/adalaide/Dev/loopgate/cmd/memorybench/main.go)
   For contradiction fixtures, do not stop at the generic path.
   Add slot-sensitive seed signatures when the point of the fixture is anchor contribution.
6. Run tests.
7. Run fresh live comparisons.
8. Update:
   - [memorybench_running_results.md](/Users/adalaide/Dev/loopgate/docs/memorybench_running_results.md)
   - `maintainer documentation checkout (memorybench internal report; outside this repository)`
9. Record the family in the running-results or internal-report tables with:
   - mechanism under test
   - intended failure mode
   - benign control or distractor guard
10. Fill the same fields in fixture metadata so they survive into `results.json`:
    - `architectural_mechanism`
    - `target_failure_mode`
    - `benign_control_or_distractor`
11. If the family has materially different probe difficulty regimes, add or
    update `subfamily_id` so `subfamily_summary.csv` can surface them directly.

## When adding adversarial fixtures

Every new adversarial family must have a corresponding false-positive or
distractor guard.

Examples:

- poisoning family -> benign safety-precision guard
- contradiction family -> entity/distractor guard
- task-resumption family -> stale alias or blocker distractor guard

Good record examples from the current fixture set:

- `poisoning.long_history_benign_malicious_interleaving.v1`
  - mechanism: governed candidate evaluation under noisy long-history input
  - failure: benign context launders malicious memory into persistence or recall
  - control: `safety_precision.interleaved_release_history_note.v1`
- `task_resumption.long_supersession_chain_multi_blocker_updates.v1`
  - mechanism: long-horizon supersession tracking across repeated blocker updates
  - failure: stale blockers outrank the current task state
  - control: `task_resumption.long_history_cost_pressure.v1`

For contradiction families specifically, prefer skeptical probe design:

- use slot-level probes that do not contain the expected answer text
- include key variation and alias variation where the semantics should stay stable
- include same-entity stale distractors and different-entity distractors
- make sure a non-anchor path can plausibly retrieve the wrong item
- prefer some anchor-sensitive cases where the wrong item still looks current,
  not only cases that collapse to no result
- if anchors are supposed to matter, add or update the continuity seed signatures so the anchor ablation can actually fail

If you add a new family without a guard, the benchmark becomes easier to game.

## How to extend the RAG baselines

Primary files:

- [backend_config.go](/Users/adalaide/Dev/loopgate/internal/memorybench/backend_config.go)
- [rag_exec_client.go](/Users/adalaide/Dev/loopgate/internal/memorybench/rag_exec_client.go)
- [rag_search.py](/Users/adalaide/Dev/loopgate/cmd/memorybench/rag_search.py)

Rules:

- keep the benchmark honest
- no silent fallback from `rag_stronger` to `rag_baseline`
- benchmark-local only
- helper output is untrusted

## Common failure modes

- forgetting `-continuity-seeding-mode`
  - scored continuity fixture runs now fail closed without an explicit seeding mode
- using `-continuity-seeding-mode debug_ambient_repo` for a scored comparison
  - this is debug-only and invalid headline evidence
- forgetting `-rag-seed-fixtures`
  - this makes RAG comparison unfair or stale
- using a missing Python helper runtime
  - expected path: `.cache/memorybench-venv/bin/python`
- Docker/Qdrant not actually running
- changing fixtures without updating tests and running-results docs

## Recommended skeptical next steps

- run the continuity ablations and compare where performance collapses or does not collapse
- run ablations one at a time; do not stack them first
- if an ablation does not move the expected family, assume the benchmark is not isolating that mechanism yet
- for contradiction skepticism, prefer slot probes, alias variation, and same-vs-different-entity distractors over answer-text probes
- keep adding harder contradiction and task-resumption generalization cases
- eventually rerun with a second operator or model for external replication
