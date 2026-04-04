**Last updated:** 2026-04-03

# Loopgate vs RAG Demo Runbook

This demo is intentionally benchmark-backed. It should reuse the checked-in memorybench fixtures and the hardened production-parity continuity seeding mode instead of inventing a separate story.

## Primary story: task resumption

Show this first.

- **Left / RAG:** retrieval tends to pull more stale or adjacent context, increasing prompt burden.
- **Right / Loopgate:** continuity returns bounded current state with materially lower prompt burden.

Use the built-in scenario set:

- `demo_task_resumption`

Current highlighted fixtures:

- `task_resumption.blocker_changes_over_time.v1`
- `task_resumption.long_history_cost_pressure.v1`

What to call out:

- current blocker and next step remain primary on the Loopgate side
- prompt burden stays lower on the Loopgate side
- the comparison should use the production-parity continuity run, not synthetic continuity

## Secondary story: slot truth maintenance

Show this second.

- **Left / RAG:** current-looking preview or distractor text can still compete with the canonical current fact
- **Right / Loopgate:** the anchored explicit slot fact survives and wins

Use the built-in scenario set:

- `demo_slot_truth`

Current highlighted fixtures:

- `contradiction.profile_timezone_same_entity_wrong_current_probe.v1`
- `contradiction.profile_locale_preview_bias_far_match_slot_probe.v1`

What to call out:

- the old timezone/locale preview-label weakness is no longer reproduced in the hardened production-parity benchmark
- the Loopgate side is using the validated write path plus bounded slot-seeking retrieval preference, not a hand-edited demo

## How to run

Use the script:

```bash
scripts/demo/run_memory_story.sh
```

Environment overrides:

- `REPO_ROOT`
- `OUTPUT_ROOT`
- `QDRANT_URL`
- `GOCACHE_DIR`
- `RUN_STAMP`

The script runs:

- continuity parity for `demo_task_resumption`
- continuity parity for `demo_slot_truth`
- `rag_stronger` for `demo_task_resumption`
- `rag_stronger` for `demo_slot_truth`

## Interpretation order

For each run:

1. `run_metadata.json`
2. `seed_manifest.json` for continuity parity runs
3. `results.json`
4. `trace.jsonl`
5. summary CSVs

Do not narrate from summary CSVs first.

## Narrative guardrails

- Do not claim that RAG always retrieves the right answer plus stale baggage.
- Do claim that RAG tends to overfetch or mis-rank stale/current-looking context, increasing prompt burden and blurring current state.
- Do present continuity parity as the product-faithful benchmark and synthetic continuity as retrieval-only evidence.

## Source of truth

For the current promoted benchmark narrative, see:

- [memorybench_running_results.md](/Users/adalaide/Dev/loopgate/docs/memorybench_running_results.md)
- [memorybench_benchmark_guide.md](/Users/adalaide/Dev/loopgate/docs/memorybench_benchmark_guide.md)
