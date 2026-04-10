**Last updated:** 2026-04-10

# Memory State And Artifact Lookup Contract

## 1. Direct answer

The memory contract is now explicitly split in two:

- **wake state** carries small current-state context that should be injected
  directly into the model prompt
- **artifact lookup** returns bounded references to stored continuity artifacts
  that the model can materialize later through a second control-plane call

This keeps continuity useful without turning the wake state into a graph dump.

## 2. What is in wake state

Wake state remains the place for current authoritative context:

- active goals
- unresolved tasks and next steps
- current project-facing blockers
- deadlines and scheduled task state
- stable user profile facts such as preferred name, timezone, and locale
- a small number of recent derived continuity facts when they are relevant

Wake state is still intentionally compact. It is not a search result and not a
general evidence surface.

## 3. What moved to lookup

Stored continuity artifacts now have an explicit lookup/get contract:

- `POST /v1/memory/artifacts/lookup`
  - query-driven
  - returns bounded `MemoryArtifactRef` handles with titles and summaries
- `POST /v1/memory/artifacts/get`
  - takes artifact refs
  - returns bounded structured materialization

The current artifact API is intentionally limited to stored continuity state
artifacts. Hybrid evidence remains advisory lookup data on `discover`, not a
persisted artifact class that pretends to be durable memory.

## 4. Why this split matters

The split prevents three common failure modes:

1. **wake-state flooding**
   - too much context up front hides the current truth instead of clarifying it

2. **fake authority from evidence**
   - broad evidence snippets should not masquerade as stored durable state

3. **unbounded graph expansion**
   - one memory handle should not automatically drag half the graph into the
     prompt

The artifact API forces a second deliberate read. That keeps prompt costs,
debuggability, and trust boundaries under control.

## 4.1 Prompt policy

The prompt policy is now explicit:

- **default prompt assembly**: inject wake state only
- **stored continuity follow-up**: use artifact lookup/get when the model needs
  more stored state context than wake state should carry
- **hybrid evidence**: attach bounded discover-time evidence only when a
  current continuity state anchor already exists and the query is clearly asking
  for supporting rationale or related background

Hybrid evidence is advisory only:

- it is not wake state
- it is not a stored artifact class
- it must not recursively expand into more graph neighbors automatically

## 5. Current API shape

### `MemoryArtifactRef`

Each ref carries:

- `artifact_ref`
- `kind`
- `state_class`
- `scope`
- `key_id`
- `thread_id`
- `distillate_id`
- `created_at_utc`
- `title`
- `summary`
- `tags`

The ref is a handle, not permission.

### `MemoryArtifactGetItem`

Materialized items return:

- the original `ref`
- bounded `content_text`
- `facts`
- `active_goals`
- `unresolved_items`
- `epistemic_flavor`

This is enough for the model to inspect stored state without requiring the
whole wake state to be pre-expanded.

## 6. Current limits

- artifact refs currently resolve only stored continuity state artifacts
- hybrid evidence is still returned separately on `discover`
- there is no automatic recursive expansion from one artifact ref to related
  artifacts
- invalid or unsupported refs fail closed

These limits are deliberate. They keep the API honest and stop the system from
quietly becoming a context firehose.

## 7. Product-safe interpretation

The honest v1 story is:

> Loopgate injects compact current state directly and lets the assistant look up
> additional stored continuity artifacts only when needed.

That is the right foundation for a UI and prompt policy. It preserves the
continuity/state memory advantage without pretending that every related note or
design thread belongs in wake state.
