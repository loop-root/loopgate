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

## IDE dry-run notes

The 2026-04-03 acceptance dry-run used the main **Cursor IDE**, not the newer Cursor app surface.

Observed friction and operator guidance:

- The Cursor IDE successfully connected through the **historical in-tree MCP** path (`mcp-serve` / `local-open-session`). **That surface is deprecated and removed** (ADR 0010 — reduced attack surface); new integrations use **HTTP on the Unix socket** or an **out-of-tree** MCP→HTTP forwarder. This bullet records **dry-run evidence only**.
- The newer Cursor app surface did not expose the same tool inventory reliably, so the demo should use the main Cursor IDE for now.
- Cursor IDE surfaced the built-in Loopgate tools with **underscore names** instead of dotted names:
  - `loopgate_status`
  - `loopgate_memory_wake_state`
  - `loopgate_memory_discover`
  - `loopgate_memory_remember`
- The generic capability dispatcher may appear under a host-prefixed name such as `user-loopgate-invoke_capability`.
- If `invoke_capability` is not in the session capability scope, that tool is expected to deny with `capability_token_scope_denied`. For the demo path, prefer the typed memory tools and direct typed capability tools instead of the generic dispatcher.

Acceptance evidence from the dry-run:

- `loopgate_status` returned the live daemon inventory and policy summary.
- `loopgate_memory_wake_state` returned a bounded global wake pack with a small token footprint.
- `loopgate_memory_remember` successfully stored `profile.timezone=America/Denver`.
- `loopgate_memory_discover` returned that timezone fact as the top result for the slot-seeking query.
- `todo_add` succeeded directly even when the generic dispatcher was not in scope.

## Preferred live demo shape

Do not center the live demo on schema-shaped memory writes such as `fact_key=...`.

Prefer a cross-session continuity story:

1. **Session A**
   - say something naturally, for example:
     - `Remember that I'm in Denver now.`
     - `Remember that I'm focused on trimming the Loopgate memory demo this week.`
     - `Remember that I prefer direct, concise communication.`
2. The agent should either:
   - call the typed memory tool directly when the mapping is clear, or
   - ask one brief clarifying question if the durable fact mapping is ambiguous
3. **Session B**
   - start a fresh agent/session
   - ask for the carried-over fact naturally, for example:
     - `What timezone should you assume for me?`
     - `What am I focused on this week?`
     - `Before we continue, what should you remember about me?`

This is the real product story: continuity across agent/session boundaries, not manual schema entry.

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

## Follow-up issues to watch

- The wake-state dry-run surfaced `preference.timezone=MDT` while the explicit remember demo wrote `profile.timezone=America/Denver`. That is not a demo blocker, but it is a real product-level consistency question for wake-pack composition and should be treated as follow-up investigation rather than hand-waved away in the demo.

## Source of truth

For the current promoted benchmark narrative, see:

- [memorybench_running_results.md](/Users/adalaide/Dev/loopgate/docs/memorybench_running_results.md)
- [memorybench_benchmark_guide.md](/Users/adalaide/Dev/loopgate/docs/memorybench_benchmark_guide.md)
