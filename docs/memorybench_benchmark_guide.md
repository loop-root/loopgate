**Last updated:** 2026-03-29

# Memorybench Benchmark Guide

This guide is for another agent or model that needs to run, extend, or audit
the continuity-memory benchmark locally.

It assumes the repo root is:

- `/Users/adalaide/Dev/morph`

It does not assume prior knowledge of the benchmark code.

## Quick regression (Phase D)

Before merging changes that touch prompts, continuity inspection, memory governance, or TCL paths exercised by Loopgate:

```bash
go test ./internal/loopgate/... -count=1
go test ./internal/memorybench/... -count=1
```

Product context: [Haven_Memory_Candidates_and_Loopgate_Plan.md](./HavenOS/Haven_Memory_Candidates_and_Loopgate_Plan.md) §5 Phase D. Fair comparisons and scoreboard updates still follow the sections below.

If you are new to memorybench, start here first:

- [Memorybench In Plain English](/Users/adalaide/Dev/morph/docs/memorybench_plain_english.md)
- [Memorybench Glossary](/Users/adalaide/Dev/morph/docs/memorybench_glossary.md)

## What memorybench is

`memorybench` is the benchmark harness for comparing:

- `continuity_tcl`
- `rag_baseline`
- `rag_stronger`

under the same checked-in fixture set.

Current skeptical fixture count:

- `61` total fixtures in the current checked-in code
- `8` poisoning
- `34` contradiction
- `13` task resumption
- `6` safety precision

Current promoted running scoreboard:

- still the stable `46`-fixture snapshot in
  [memorybench_running_results.md](/Users/adalaide/Dev/morph/docs/memorybench_running_results.md)
- newer preview-slot skepticism runs are tracked in the maintainer-only
  `~/Dev/projectDocs/morph/memorybench-internal/memorybench_internal_report.md`
  until they are promoted
- the newer `61`-fixture lexical-trap extension remains benchmark-local
  heuristic evaluation only; it is not yet part of the promoted running-results headline

The CLI entrypoint is:

- [main.go](/Users/adalaide/Dev/morph/cmd/memorybench/main.go)

The core harness logic lives in:

- [runner.go](/Users/adalaide/Dev/morph/internal/memorybench/runner.go)
- [fixtures.go](/Users/adalaide/Dev/morph/internal/memorybench/fixtures.go)
- [types.go](/Users/adalaide/Dev/morph/internal/memorybench/types.go)

The live scoreboard lives in:

- [memorybench_running_results.md](/Users/adalaide/Dev/morph/docs/memorybench_running_results.md)

The internal methodology report lives in (maintainer checkout, not in clone):

- `~/Dev/projectDocs/morph/memorybench-internal/memorybench_internal_report.md`

## Invariants

Do not violate these while extending the benchmark:

- benchmark helpers are benchmark-local only
- do not turn benchmark bridges into Loopgate control-plane APIs
- continuity fair runs must use isolated fixture-seeded state, not ambient repo memory
- new adversarial fixture families must ship with a benign or distractor guard
- every new fixture family must be recorded with:
  - the architectural mechanism under test
  - the failure mode it is meant to expose
  - the benign control or distractor that proves we are not just rewarding overblocking
- fail closed on unknown backend or unknown ablation mode
- do not silently substitute one backend for another
- keep benchmark-local continuity heuristic controls explicit when used:
  - `-continuity-preview-slot-preference=true`
  - `-continuity-preview-slot-preference=false`
  - `-continuity-preview-slot-preference-margin=<n>`
- keep candidate governance mode explicit in benchmark runs:
  - `backend_default`
  - `continuity_tcl`
  - `permissive`

## Local prerequisites

### 1. Go cache

Use the repo-local Go build cache:

```bash
export GOCACHE=/Users/adalaide/Dev/morph/.cache/go-build
```

### 2. Python helper runtime

The RAG benchmark helper expects:

- `/Users/adalaide/Dev/morph/.cache/memorybench-venv/bin/python`

Create it if needed:

```bash
cd /Users/adalaide/Dev/morph
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
cd /Users/adalaide/Dev/morph
env GOCACHE=/Users/adalaide/Dev/morph/.cache/go-build \
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

Use isolated fixture seeds:

```bash
env GOCACHE=/Users/adalaide/Dev/morph/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-live-continuity \
  -run-id continuity_live_fixture_v22 \
  -profile fixtures \
  -backend continuity_tcl \
  -repo-root /Users/adalaide/Dev/morph \
  -continuity-seed-fixtures
```

Tuned vs untuned continuity control for preview-slot skepticism:

```bash
env GOCACHE=/Users/adalaide/Dev/morph/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-heuristic-attack \
  -run-id continuity_preview_attack_tuned_v1 \
  -profile fixtures \
  -backend continuity_tcl \
  -candidate-governance continuity_tcl \
  -repo-root /Users/adalaide/Dev/morph \
  -continuity-seed-fixtures \
  -continuity-preview-slot-preference=true
```

```bash
env GOCACHE=/Users/adalaide/Dev/morph/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-heuristic-attack \
  -run-id continuity_preview_attack_untuned_v1 \
  -profile fixtures \
  -backend continuity_tcl \
  -candidate-governance continuity_tcl \
  -repo-root /Users/adalaide/Dev/morph \
  -continuity-seed-fixtures \
  -continuity-preview-slot-preference=false
```

Stronger benchmark-local preview-slot margin experiment:

```bash
env GOCACHE=/Users/adalaide/Dev/morph/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-heuristic-attack-v2 \
  -run-id continuity_preview_attack_margin3_v1 \
  -profile fixtures \
  -backend continuity_tcl \
  -candidate-governance continuity_tcl \
  -repo-root /Users/adalaide/Dev/morph \
  -continuity-seed-fixtures \
  -continuity-preview-slot-preference=true \
  -continuity-preview-slot-preference-margin=3
```

### Plain RAG

Seed the checked-in fixture corpus:

```bash
env GOCACHE=/Users/adalaide/Dev/morph/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-live-rag \
  -run-id rag_live_fixture_v22 \
  -profile fixtures \
  -backend rag_baseline \
  -repo-root /Users/adalaide/Dev/morph \
  -rag-qdrant-url http://127.0.0.1:6333 \
  -rag-collection memorybench_default \
  -rag-seed-fixtures
```

### Stronger RAG

```bash
env GOCACHE=/Users/adalaide/Dev/morph/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-live-rag \
  -run-id rag_stronger_live_fixture_v8 \
  -profile fixtures \
  -backend rag_stronger \
  -repo-root /Users/adalaide/Dev/morph \
  -rag-qdrant-url http://127.0.0.1:6333 \
  -rag-collection memorybench_rerank \
  -rag-seed-fixtures
```

### Policy-matched RAG fairness rerun

Use the same retrieval backend with continuity/TCL candidate governance:

```bash
env GOCACHE=/Users/adalaide/Dev/morph/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-live-rag \
  -run-id rag_governed_fixture_v4 \
  -profile fixtures \
  -backend rag_baseline \
  -candidate-governance continuity_tcl \
  -repo-root /Users/adalaide/Dev/morph \
  -rag-qdrant-url http://127.0.0.1:6333 \
  -rag-collection memorybench_default \
  -rag-seed-fixtures
```

Stronger governed rerun:

```bash
env GOCACHE=/Users/adalaide/Dev/morph/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-live-rag \
  -run-id rag_stronger_governed_fixture_v4 \
  -profile fixtures \
  -backend rag_stronger \
  -candidate-governance continuity_tcl \
  -repo-root /Users/adalaide/Dev/morph \
  -rag-qdrant-url http://127.0.0.1:6333 \
  -rag-collection memorybench_rerank \
  -rag-seed-fixtures
```

Current policy-matched read:

- see [memorybench_running_results.md](/Users/adalaide/Dev/morph/docs/memorybench_running_results.md)
  for the current promoted `46`-fixture policy-matched numbers
- see `~/Dev/projectDocs/morph/memorybench-internal/memorybench_internal_report.md`
  for the newer `61`-fixture preview-slot skepticism comparison, including
  tuned, untuned, and stronger-margin continuity controls

## Current continuity ablations

These are benchmark-local continuity ablations.

Flag:

- `-continuity-ablation`
- `-continuity-preview-slot-preference`
- `-continuity-preview-slot-preference-margin`

Accepted values:

- `none`
- `anchors_off`
- `hints_off`
- `reduced_context_breadth`

Important caveat:

- `reduced_context_breadth` is a proxy for “reduced graph expansion”
- the current continuity benchmark path does not traverse graph edges yet
- this mode reduces returned related-context breadth instead of disabling a real edge-expansion stage
- `-continuity-preview-slot-preference` is not an ablation of the product
  backend; it is a benchmark-local tuned-vs-untuned control for the narrow
  canonical-over-preview reranking experiment
- `-continuity-preview-slot-preference-margin` is also benchmark-local only;
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
env GOCACHE=/Users/adalaide/Dev/morph/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-live-continuity \
  -run-id continuity_ablation_anchors_off_v6 \
  -profile fixtures \
  -backend continuity_tcl \
  -repo-root /Users/adalaide/Dev/morph \
  -continuity-seed-fixtures \
  -continuity-ablation anchors_off
```

### Hints off

```bash
env GOCACHE=/Users/adalaide/Dev/morph/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-live-continuity \
  -run-id continuity_ablation_hints_off_v6 \
  -profile fixtures \
  -backend continuity_tcl \
  -repo-root /Users/adalaide/Dev/morph \
  -continuity-seed-fixtures \
  -continuity-ablation hints_off
```

### Reduced related-context breadth

```bash
env GOCACHE=/Users/adalaide/Dev/morph/.cache/go-build \
  go run ./cmd/memorybench \
  -output-root /tmp/memorybench-live-continuity \
  -run-id continuity_ablation_reduced_breadth_v6 \
  -profile fixtures \
  -backend continuity_tcl \
  -repo-root /Users/adalaide/Dev/morph \
  -continuity-seed-fixtures \
  -continuity-ablation reduced_context_breadth
```

## Output artifacts

Each run writes:

- `results.json`
- `summary.csv`
- `family_summary.csv`
- `subfamily_summary.csv`
- `trace.jsonl`

Interpret them like this:

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
- the current RAG comparators use a permissive benchmark governance stub
- poisoning steps are excluded from the seeded RAG corpus by design
- if you want a policy-matched poisoning comparison, rerun RAG with
  `-candidate-governance continuity_tcl`

## How to add a new fixture family

1. Add the fixture(s) to [fixtures.go](/Users/adalaide/Dev/morph/internal/memorybench/fixtures.go).
2. Add or update the corresponding guard or distractor fixture.
3. Update [fixtures_test.go](/Users/adalaide/Dev/morph/internal/memorybench/fixtures_test.go).
4. Add runner regressions in [runner_test.go](/Users/adalaide/Dev/morph/internal/memorybench/runner_test.go).
5. If continuity fair seeding should include it, make sure the existing generic seed builders already cover it:
   - [main.go](/Users/adalaide/Dev/morph/cmd/memorybench/main.go)
   For contradiction fixtures, do not stop at the generic path.
   Add slot-sensitive seed signatures when the point of the fixture is anchor contribution.
6. Run tests.
7. Run fresh live comparisons.
8. Update:
   - [memorybench_running_results.md](/Users/adalaide/Dev/morph/docs/memorybench_running_results.md)
   - `~/Dev/projectDocs/morph/memorybench-internal/memorybench_internal_report.md`
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

- [backend_config.go](/Users/adalaide/Dev/morph/internal/memorybench/backend_config.go)
- [rag_exec_client.go](/Users/adalaide/Dev/morph/internal/memorybench/rag_exec_client.go)
- [rag_search.py](/Users/adalaide/Dev/morph/cmd/memorybench/rag_search.py)

Rules:

- keep the benchmark honest
- no silent fallback from `rag_stronger` to `rag_baseline`
- benchmark-local only
- helper output is untrusted

## Common failure modes

- forgetting `-continuity-seed-fixtures`
  - this contaminates continuity with ambient repo state
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
