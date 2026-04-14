**Last updated:** 2026-03-24

# Continuity Stream Architecture

Status: implemented with authoritative lineage governance
Scope: operator client + Loopgate continuity and durable memory boundary  
Date: 2026-03-12

## Intent

The operator client now uses a bounded three-thread continuity model:

- `current`: live continuity intake for the active session
- `next`: empty staged continuation buffer
- `previous`: most recently sealed thread

The continuity stream remains local and append-only in the The operator client ledger.
Durable memory artifacts do not stay client-local anymore. Once a sealed
`previous` thread crosses policy thresholds, The operator client submits it to Loopgate for
inspection. Loopgate decides whether any durable memory artifacts are created.

## Authority split

The operator client owns:

- continuity event emission into the local append-only ledger
- explicit `current / next / previous` thread-role state
- local thread rollover at explicit session boundaries
- ephemeral prompt projection of live thread context

Loopgate owns:

- sealed-thread inspection
- durable distillate derivation
- resonate key minting
- wake-state projection from Loopgate-owned artifacts
- governed discovery and recall

Continuity and memory remain content, not authority.

## Data model

The operator client persists thread-role state in:

- `runtime/state/continuity_threads.json`

Core record:

- `thread_id`
- `scope`
- `state`
  - `open`
  - `sealed`
  - `inspected`
  - `tombstoned`
- `created_at_utc`
- `sealed_at_utc`
- `last_continuity_event_at_utc`
- `seal_reason`
- `event_count`
- `approx_payload_bytes`
- `approx_prompt_tokens`
- `inspection_id`
- `inspection_completed_at_utc`
- `derived_distillate_ids`
- `derived_resonate_key_ids`

Role pointers:

- `current_thread_id`
- `next_thread_id`
- `previous_thread_id`

Continuity-tagged ledger events now carry:

- `continuity_event.thread_id`

Loopgate persists durable memory under:

- `runtime/state/memory/continuity_events.jsonl`
- `runtime/state/memory/goal_events.jsonl`
- `runtime/state/memory/profile_events.jsonl`
- `runtime/state/memory/state.json`
- `runtime/state/memory/goals_current.json`
- `runtime/state/memory/tasks_current.json`
- `runtime/state/memory/reviews_current.json`
- `runtime/state/memory/profile_resolved.json`
- `runtime/state/memory/ranking_cache.json`
- `runtime/state/memory/distillates/*.json`
- `runtime/state/memory/wake/runtime/*.json`
- `runtime/state/memory/wake/diagnostic/*.json`
- `runtime/state/memory/profiles/corrections/*.json`
- `runtime/state/memory/profiles/revalidation/*.json`

Core Loopgate artifacts:

- inspection records
- append-only continuity event history
- inspection-root review and lineage-governance state
- distillates
- resonate keys
- runtime wake-state projection
- diagnostic wake report
- resolved profile snapshot and correction artifacts

## Rollover semantics

This baseline keeps rollover boring and explicit.

Rollover happens only on:

- session shutdown
- `/reset`
- startup recovery when the prior `current` thread still has continuity events

Rules:

1. summarize `current` by scanning continuity-tagged ledger events for its
   `thread_id`
2. if `current` is empty, do nothing
3. mark `current` sealed with deterministic metrics
4. promote `next -> current`
5. allocate a fresh empty `next`
6. set sealed former `current` as `previous`
7. if thresholds are crossed, submit sealed `previous` to Loopgate

`next` may remain empty indefinitely.

## Thresholds

Current threshold policy is an OR rule on the sealed `previous` thread:

- `memory.submit_previous_min_events`
- `memory.submit_previous_min_payload_bytes`
- `memory.submit_previous_min_prompt_tokens`

If any threshold is met, The operator client submits the sealed thread for Loopgate
inspection.

The current defaults are:

- `submit_previous_min_events: 3`
- `submit_previous_min_payload_bytes: 512`
- `submit_previous_min_prompt_tokens: 120`

## Inspection handoff

The operator client submits:

- deterministic `inspection_id`
- sealed thread metadata
- validated continuity events for that thread
- lineage-bearing source refs and ledger hash references

Loopgate processing is synchronous in this baseline.

That means:

- no background inspector daemon
- no partial in-progress durable-memory state
- restart retry is simply a safe replay of the same `inspection_id`

Double-processing protection:

- `inspection_id` is deterministic per thread
- Loopgate stores completed inspections and returns the stored result on replay

## Durable artifact model

Loopgate currently derives one distillate per sealed thread that produces
structured material, but derivation is not eligibility.

The distillate stores:

- source event refs
- tags
- structured facts
- goal open/close ops
- unresolved-item open/close ops

Loopgate also mints one resonate key per derived distillate.

Eligibility is now rooted at the inspection record:

- `derivation_outcome`
- `review.status`
- `lineage.status`

Wake state, discovery, recall, and replay all defer to that single authority
path. Stale keys and stale cached wake state do not bypass it.

Wake state is rebuilt from Loopgate-owned distillates and resonate keys, not
from raw chat history.

## Prompt projection model

The operator client prompt continuity is now the combination of:

1. Loopgate durable wake-state summary
2. client-local three-thread projection

The local projection is ephemeral and derived on demand from the append-only
ledger. It is not stored as a durable memory artifact.

## Restart and crash behavior

Crash assumptions are explicit:

- if The operator client crashes before rollover, `current` remains open and startup recovery
  will seal and roll it if it contains continuity events
- if The operator client crashes after sealing but before inspection, the thread remains
  `sealed` and can be retried
- if The operator client crashes after Loopgate inspection but before local state update,
  replaying the same `inspection_id` returns the existing Loopgate result
- if Loopgate is unavailable during inspection, The operator client keeps the thread sealed
  and auditable; it does not silently drop or rewrite continuity

## Current limitations

Implemented now:

- three explicit thread roles
- deterministic rollover
- Loopgate-owned distillates, resonate keys, wake-state projection, recall, and
  discovery
- inspection-root review state and lineage status
- authoritative tombstone and purge exclusion for wake, discovery, recall, and replay
- startup wake-state rebuild from authoritative Loopgate memory state
- deterministic internal goal-family normalization from `config/goal_aliases.yaml` with fallback family IDs
- centralized scoring weights and explicit memory correction config from `config/runtime.yaml`
- no client-local durable wake-state, key, or distillate writes on the active
  path

Not implemented yet:

- thread-scoped wake-state selection beyond the current global durable wake
  projection plus local role projection
- asynchronous inspection workers

## Next slices

1. add broader operator projection/UX for pending continuity review without
   moving authority into the operator client
2. tighten restart tests around sealed-but-uninspected and inspected-but-not-yet
   acknowledged threads
3. add thread-scoped wake-state selection on top of the existing authoritative
   Loopgate memory state
4. reconcile RFC 0009 / 0010 text with the implemented three-thread substrate
