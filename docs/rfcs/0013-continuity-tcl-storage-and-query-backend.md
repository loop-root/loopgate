**Last updated:** 2026-03-26

# RFC 0013: Continuity/TCL Storage and Query Backend

Status: draft

## 1. Summary

This RFC defines the implementation-oriented storage and lookup design for the
`continuity_tcl` memory backend introduced in
[RFC 0011](./0011-swappable-memory-backends-and-benchmark-harness.md).

It proposes:

- `SQLite` as the authoritative local-first storage engine
- bounded TCL-derived semantic projections plus bounded hint payloads
- typed node/edge storage for relation-aware recall
- append-only lineage and durable state transitions
- optional vector sidecar retrieval as a future acceleration layer, not as the
  source of truth

This RFC is intended to be detailed enough that another engineer or model can
implement the storage layer without re-deriving the architecture.

## 1.1 TCL reference set

This RFC depends on the tracked TCL RFCs for semantic vocabulary and compact
expression shape.

Implementations should treat the following as the semantic reference set:

- [Thought Compression Language](../TCL-RFCs/Thought%20Compression%20Language.md)
  for purpose, vocabulary, disposition model, and signature tiers
- [TCL Syntax](../TCL-RFCs/TCL%20Syntax.md) for compact syntax and relation
  operators
- [TCL Memory Node Schema](../TCL-RFCs/TCL%20Memory%20Node%20Schema.md) for
  validated node field names and metadata/decision structure
- [English to TCL](../TCL-RFCs/English%20to%20TCL.md) for translation examples
  and normalization guidance

This backend RFC does not redefine TCL semantics.
It defines how validated TCL-derived semantics are stored, indexed, and queried
inside the `continuity_tcl` backend.

## 2. Motivation

The current JSON-based memory layout is useful for prototyping, debugging, and
replay validation, but it is not the ideal long-term storage substrate for:

- indexed contradiction lookup
- relation-aware expansion
- bounded recall ranking
- compact wake-state derivation
- scalable local persistence

The continuity/TCL design needs a backend that preserves:

- provenance
- append-only reasoning where required
- contradiction slots through anchor tuples
- relation-aware memory neighborhoods
- compact bounded startup state

It also needs to remain meaningfully comparable to a real RAG baseline.

## 3. Design goals

- keep Loopgate as the authority boundary
- move off flat JSON as the primary runtime store
- preserve append-only lineage semantics
- support exact anchor lookup, family lookup, and relation expansion
- support bounded hint payloads for specificity
- remain local-first and operationally simple
- support a future vector accelerator without making it authoritative

## 4. Non-goals

This RFC does not:

- define the RAG baseline storage model
- require remote or multi-user deployment
- replace policy/governance with retrieval logic
- allow unrestricted graph traversal or unbounded semantic search
- store raw secret-bearing or quarantined content as prompt-eligible memory

## 5. Storage engine choice

### 5.1 Authoritative store

The authoritative store for the `continuity_tcl` backend should be `SQLite`.

Reasons:

- current product architecture is local, single-user, and single-instance in
  typical desktop usage
- `SQLite` keeps operational complexity low
- `SQLite` supports JSON fields where useful without forcing a JSON-only design
- `SQLite` supports local full-text search if needed
- `SQLite` is a better fit than a directory service or vector database for
  append-oriented lineage, typed edges, and bounded deterministic reads

### 5.2 Not LDAP

LDAP is not the right primary store for this memory system.

The memory model is not primarily a user/account directory.
It is a provenance-bearing continuity graph with bounded retrieval rules,
append-only lineage, contradiction slots, and durable derived artifacts.

### 5.3 Future upgrade path

If the product later becomes remote, multi-user, or service-hosted, the likely
upgrade path is `PostgreSQL`, not a directory or vector store.

## 6. Core storage principle

The system should store:

- memory nodes as durable semantic units
- bounded hints as specificity carriers
- semantic projections as query/risk metadata
- typed edges as relation structure
- append-only events as provenance and lineage evidence
- wake snapshots as derived convenience views

The system should not store:

- giant mutable memory blobs
- unbounded prose dumps as the primary durable form
- model output as authority

## 7. Memory unit model

Each durable memory unit should be understood as:

- a semantic node
- with one provenance-bearing origin
- with zero or more bounded hints
- with one semantic projection
- with zero or more typed relations to other nodes
- with a lifecycle state

This means TCL is not the stored memory by itself.
It is part of the semantic projection that helps classify, compare, and
retrieve the memory.

For token meanings, relation vocabulary, compact syntax, and decision semantics,
follow the TCL RFCs listed in [1.1](#11-tcl-reference-set).

## 8. Proposed durable records

### 8.1 Events

Append-only event records capture provenance and lifecycle transitions.

Suggested table:

```sql
CREATE TABLE memory_events (
  event_id TEXT PRIMARY KEY,
  created_at_utc TEXT NOT NULL,
  scope TEXT NOT NULL,
  event_type TEXT NOT NULL,
  source_kind TEXT NOT NULL,
  source_ref TEXT NOT NULL,
  payload_json TEXT NOT NULL
);
```

Use cases:

- explicit remember accepted
- continuity inspection derived
- review accepted/rejected
- quarantine applied
- node superseded
- node tombstoned

### 8.2 Nodes

Nodes are the durable semantic memory records.

```sql
CREATE TABLE memory_nodes (
  node_id TEXT PRIMARY KEY,
  created_at_utc TEXT NOT NULL,
  updated_at_utc TEXT NOT NULL,
  scope TEXT NOT NULL,
  node_kind TEXT NOT NULL,
  epistemic_flavor TEXT NOT NULL,
  certainty_score INTEGER NOT NULL,
  state TEXT NOT NULL, -- active|tombstoned|quarantined|denied|review_pending
  anchor_version TEXT,
  anchor_key TEXT,
  pattern_family_id TEXT,
  provenance_event_id TEXT NOT NULL,
  replaced_by_node_id TEXT,
  FOREIGN KEY(provenance_event_id) REFERENCES memory_events(event_id)
);
```

Notes:

- `anchor_version` + `anchor_key` is the contradiction slot identity
- `pattern_family_id` is a reusable semantic family/prototype id
- `replaced_by_node_id` is explicit lineage, not implicit overwrite

### 8.3 Hints

Hints preserve bounded specificity that TCL alone does not capture well.

```sql
CREATE TABLE memory_hints (
  hint_id TEXT PRIMARY KEY,
  node_id TEXT NOT NULL,
  created_at_utc TEXT NOT NULL,
  hint_kind TEXT NOT NULL, -- exact_value|snippet|span_ref|structured_value
  hint_text TEXT NOT NULL,
  byte_count INTEGER NOT NULL,
  source_ref TEXT NOT NULL,
  FOREIGN KEY(node_id) REFERENCES memory_nodes(node_id)
);
```

Rules:

- hints are bounded
- hints are untrusted specificity carriers
- hints are not authority by themselves
- hints should generally stay small enough to avoid reintroducing full-history
  prompt bloat

### 8.4 Semantic projections

Semantic projections store the classification output that powers retrieval,
signatures, and contradiction logic.

```sql
CREATE TABLE semantic_projections (
  node_id TEXT PRIMARY KEY,
  tcl_core_json TEXT NOT NULL,
  exact_signature TEXT,
  family_signature TEXT,
  risk_motifs_json TEXT,
  confidence REAL,
  FOREIGN KEY(node_id) REFERENCES memory_nodes(node_id)
);
```

Notes:

- `tcl_core_json` is the stable structured semantic packet derived from the
  validated TCL node shape
- anchor fields remain on `memory_nodes` for fast indexed contradiction lookup
- signatures and motifs support dangerous-family matching and retrieval ranking

`tcl_core_json` should follow the TCL vocabulary and field naming from the TCL
RFCs rather than inventing backend-local token names.

At minimum, the stored semantic packet should preserve the validated semantic
core:

- `ACT`
- `OBJ`
- `QUAL`
- `OUT`
- `STA`
- `REL`
- `META`
- optional `DECISION`

The backend may persist contradiction anchor data separately on `memory_nodes`
for indexed lookup even when the validated TCL node carries anchor information
through the Go implementation.

### 8.5 Pattern families

Pattern families support reuse of common semantic shapes without making node
meaning retroactively mutable.

```sql
CREATE TABLE pattern_families (
  pattern_family_id TEXT PRIMARY KEY,
  version TEXT NOT NULL,
  description TEXT NOT NULL,
  tcl_shape_json TEXT NOT NULL
);
```

Important rule:

- pattern families are compression/indexing aids
- every node still stores its own resolved projection and provenance
- changing a family must not silently rewrite historical meaning

Pattern families should be derived from TCL-normalized semantic shape and
signature tiers, not from raw English text.

### 8.6 Edges

Edges model the relation graph.

```sql
CREATE TABLE memory_edges (
  edge_id TEXT PRIMARY KEY,
  from_node_id TEXT NOT NULL,
  to_node_id TEXT NOT NULL,
  edge_type TEXT NOT NULL, -- same_entity|depends_on|derived_from|same_goal_family|related_to
  weight REAL NOT NULL,
  created_at_utc TEXT NOT NULL,
  provenance_event_id TEXT NOT NULL,
  FOREIGN KEY(from_node_id) REFERENCES memory_nodes(node_id),
  FOREIGN KEY(to_node_id) REFERENCES memory_nodes(node_id),
  FOREIGN KEY(provenance_event_id) REFERENCES memory_events(event_id)
);
```

Important rule:

- typed edges are safer than generic affinity edges
- `related_to` should be treated conservatively
- `derived_from`, `depends_on`, and `same_entity` are safer for automatic expansion

Where relation edges originate from TCL, their meanings should remain aligned
with the TCL relation vocabulary:

- `SUP`
- `CNT`
- `REL`
- `DRV`
- `DEP`
- `IMP`

The backend may map those into storage/query-friendly internal edge classes,
but it should not silently invent new semantics for existing TCL relation
tokens.

### 8.7 Wake snapshots

Wake state remains a derived bounded convenience view.

```sql
CREATE TABLE wake_snapshots (
  snapshot_id TEXT PRIMARY KEY,
  scope TEXT NOT NULL,
  created_at_utc TEXT NOT NULL,
  payload_json TEXT NOT NULL
);
```

This does not replace node or event truth.

## 9. Required indexes

```sql
CREATE INDEX idx_nodes_anchor
  ON memory_nodes(anchor_version, anchor_key, state);

CREATE INDEX idx_nodes_scope_state_time
  ON memory_nodes(scope, state, created_at_utc);

CREATE INDEX idx_nodes_pattern_family
  ON memory_nodes(pattern_family_id, state);

CREATE INDEX idx_edges_from_type
  ON memory_edges(from_node_id, edge_type);

CREATE INDEX idx_edges_to_type
  ON memory_edges(to_node_id, edge_type);

CREATE INDEX idx_hints_node
  ON memory_hints(node_id, created_at_utc);
```

If local full-text search is added later, it should index hints or bounded
derived text only, not raw ungoverned content.

## 10. Query model

Lookup should be staged and bounded.

The retrieval order should be:

1. exact anchor match
2. semantic family / signature match
3. typed graph expansion
4. optional vector fallback
5. ranking and trimming

This order matters.

The system should prefer:

- exact semantic slot identity
- explicit typed relation structure
- bounded specificity hints

It should not lead with broad similarity when a stronger typed signal exists.

## 11. Seed lookup

Given an incoming query token and hint:

- validate the candidate
- extract anchor tuple if present
- extract family/signature if present
- use hint text only as bounded disambiguation input

Suggested lookup stages:

### 11.1 Exact anchor

```text
WHERE anchor_version = ?
  AND anchor_key = ?
  AND state = 'active'
```

This is the strongest lookup mode.

### 11.2 Family/signature lookup

If exact anchor is absent or insufficient:

- match `pattern_family_id`
- match `family_signature`
- optionally match `exact_signature`

### 11.3 Hint refinement

Use bounded hint text only to distinguish among close candidates.
Do not let hint similarity override contradiction slot rules.

If a candidate carries compact TCL text, it should first be parsed and
validated against the TCL syntax RFC before contributing to lookup or
persistence.

## 12. Graph expansion

After seed selection, expand through typed edges in a bounded way.

Recommended defaults:

- maximum hop count: 2
- maximum candidate nodes considered: 40
- default auto-expand edge types:
  - `same_entity`
  - `depends_on`
  - `derived_from`
  - `same_goal_family`

Do not automatically expand broadly through unconstrained `related_to` edges
unless ranking pressure is very strong.

## 13. Ranking

After collecting candidates, rank them deterministically.

Suggested ranking inputs:

- exact anchor match
- family/signature match
- edge weight
- recency
- certainty score
- epistemic flavor
- hint match quality
- scope match
- node state

Exact/family/risk signature meaning should remain aligned with the signature
tier guidance in the main TCL RFC rather than being redefined per backend.

Suggested exclusions:

- quarantined
- denied
- review-pending unless explicitly requested
- tombstoned unless lineage/provenance view is explicitly requested

## 14. Example query flow

Given:

- query token with anchor `v1 + usr_profile:identity:fact:name`
- hint `"Grace"`

The backend should:

1. find active nodes with that anchor
2. ignore tombstoned prior values unless explicitly requested
3. expand one hop to relevant identity/profile support nodes
4. rank candidates
5. return only a compact bounded subset for wake or recall

This is a graph neighborhood lookup, not raw semantic search over everything.

## 15. Hint rules

Hints should exist because TCL captures shape better than specificity.

Rules:

- hints should be bounded by bytes
- hints should be stored separately from the semantic core
- hints should be source-linked
- hints should not become a hidden raw transcript cache

Suggested initial size policy:

- default per-hint cap: 256 bytes
- larger hints only when the candidate kind explicitly requires it

Hints are complementary to TCL, not replacements for it.
TCL should continue to carry the semantic skeleton; hints carry bounded
specificity that the semantic sketch intentionally abstracts away.

## 16. Vector sidecar

### 16.1 Role

An optional vector index may be added later as a retrieval accelerator.

Its job is:

- fuzzy semantic recall over hints or bounded derived text
- fallback retrieval when no strong anchor/family path exists

It is not:

- the source of truth
- the authority over contradiction or supersession
- the wake-state assembler of first resort

### 16.2 Recommended shape

If added, the vector sidecar should index:

- bounded hint text
- limited derived semantic text
- node id as the join key back to SQLite

Recommended join pattern:

1. vector lookup returns candidate node ids
2. SQLite remains authoritative for state, lineage, anchor tuple, and edge expansion
3. Loopgate filters/ranks results using authoritative metadata

## 17. State transitions

Node states must remain explicit.

Suggested states:

- `active`
- `tombstoned`
- `quarantined`
- `denied`
- `review_pending`

Important rule:

- supersession should create a new node and mark the old one `tombstoned`
- old nodes should not be rewritten in place as if history changed

## 18. Migration from JSON-backed storage

Migration should be staged.

### Phase 1

- keep current JSON artifacts as the authoritative path
- add a SQLite mirror writer
- validate parity through tests

### Phase 2

- read wake/recall from SQLite
- keep JSON export/debug artifacts as derived outputs

### Phase 3

- retire JSON as the primary runtime store
- retain export/replay/debug formats only where still useful

## 19. Implementation priorities

Build in this order:

1. SQLite schema and migration layer
2. node + semantic projection persistence
3. hint persistence
4. anchor and family lookup
5. typed edge persistence and bounded graph expansion
6. wake-state derivation from SQLite
7. optional vector sidecar

## 20. Decision

The `continuity_tcl` backend should treat:

- `SQLite` as authoritative storage
- TCL as semantic projection
- hints as bounded specificity carriers
- typed edges as the relation graph
- vector search as optional acceleration only

That gives the memory system a real storage/query architecture while preserving
the product’s control-plane and memory-governance invariants.

## 21. TCL spec drift to reconcile

The TCL RFCs are strong enough to serve as the semantic reference set for this
backend, but there is one implementation/spec drift that should be reconciled
before deeper rollout:

- the Go implementation currently carries `ANCHOR` on `TCLNode` and supports
  relation targets as either `@MID` references or nested target expressions
- `TCL Memory Node Schema.md` still presents the strict relation shape as
  MID-targeted and does not list `ANCHOR` in the node structure

This does not block the storage/query design in this RFC, but the TCL docs and
implementation should be brought back into alignment so future models and
engineers do not have to infer the true node shape from code alone.
