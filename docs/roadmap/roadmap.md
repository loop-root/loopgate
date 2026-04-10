**Last updated:** 2026-04-08

# Loopgate roadmap

## Current baseline

**Snapshot from:** 2026-03-12 (feature list). **Doc reviewed:** 2026-03-24.

The repo ships the **Loopgate** control plane with a real security boundary.
**Primary integration targets** are **HTTP-on-UDS local clients** and the separate **Haven TUI/CLI** operator shell. **In-tree MCP is deprecated and removed** (ADR 0010 — reduced attack surface); **reserved** for a possible future thin forwarder via new ADR; **out-of-tree** MCP→HTTP bridges remain an operator choice. The in-repo **`cmd/haven/`** Wails tree is **reference-only** (contracts/tests).

Implemented:

- **Reference Wails shell** (`cmd/haven/`) handles model interaction, continuity stream, and session runtime on the unprivileged client side for parity testing — **not** a ship target
- Loopgate runs as a local Unix-socket control plane and remains the authority boundary
- capability and approval tokens are bound to the authenticated local session
- privileged Loopgate requests use a server-issued session MAC key, signed requests, and replay protection
- approval decisions require single-use decision nonces and explicit approval state transitions
- Loopgate and unprivileged clients that use the client-ledger path each maintain append-only hash-linked audit/ledger chains where that path is active
- sandbox crossing is mediated through the mini-filesystem rooted at `/morph/home`
- quarantine metadata, blob view, and prune lifecycle are implemented with append-only audit
- model/provider connections and secret refs are handled through Loopgate with secure-backend resolution
- Loopgate exposes typed display-safe UI status, event, and approval endpoints
- connection-backed `public_read`, client-credentials, and PKCE flows exist in Loopgate
- morphling class policy is authoritative from `core/policy/morphling_classes.yaml`
- morphlings now use a Loopgate-owned lifecycle record plus explicit state machine:
  - `requested`
  - `authorizing`
  - `pending_spawn_approval`
  - `spawned`
  - `running`
  - `completing`
  - `pending_review`
  - `terminating`
  - `terminated`
- morphling request-level denials remain distinct from instantiated morphling lifecycle termination
- morphling audit no longer stores raw goal text; it records a session-bound `goal_hmac`
- restart recovery resolves persisted nonterminal morphling records before new work is accepted
- socket-bound morphling worker launch/session flow is implemented on the local Unix socket
- worker updates are bounded to projected `status_text`, bounded `memory_strings`, and staged artifact refs
- morphling completions now move through real worker-driven `running`, `completing`, and `pending_review` transitions
- staged morphling completions are finalized under Loopgate control and resolved by explicit operator review
- The operator client maintains an explicit three-thread continuity model:
  - `current`
  - `next`
  - `previous`
- sealed `previous` threads are submitted to Loopgate for idempotent inspection once thresholds are crossed
- Loopgate now owns durable continuity artifacts:
  - distillates
  - resonate keys
  - wake-state projection
  - governed discovery and recall
- inspection-root review and lineage governance is now authoritative for continuity-derived memory eligibility
- startup prompt continuity is now the combination of Loopgate durable wake state plus client-local thread projection
- memory scoring weights now live in `config/runtime.yaml` and goal-family aliases live in `config/goal_aliases.yaml`
- Loopgate memory now writes split derived artifacts under `runtime/state/memory/` instead of a single flat snapshot file
- end-to-end integration tests now cover:
  - local socket replay rejection
  - denied writes that never land on disk
  - audit-chain/redaction round trips
  - quarantine lifecycle
  - sandbox symlink escape denial
  - morphling lifecycle invariants
  - morphling worker lifecycle and review races
  - task plan golden path: plan → validation → lease → mediated execution → staged result → completion
  - task plan boundary denials: unknown capability, hash mismatch, plan-not-found, already-leased, wrong morphling ID, double execute, double complete
  - morphling runner golden path: separate-process runner consuming a lease through Loopgate mediation
  - morphling runner subprocess build: binary builds, parses JSON config, produces structured JSON output
  - morphling runner lease expiry: expired leases rejected at execute time
  - morphling runner duplicate completion: second complete rejected
  - morphling runner crash recovery: crash after /execute leaves plan in executing state, recovery /complete succeeds
  - morphling runner concurrent execution: two runners on same lease, exactly one succeeds

Validation status:

- `go test ./...` was passing as of 2026-03-14 — re-run locally before release; this file is not a CI badge.

## TCL memory integration status (2026-03-23)

Phase 1 of TCL integration is now implemented for explicit memory writes.

Implemented:

- explicit `RememberMemoryFact` requests are normalized into a shared `tcl.MemoryCandidate`
- Loopgate evaluates explicit memory writes through TCL before any inspection, distillate, or resonate-key artifacts are persisted
- explicit-memory governance now has stable denial codes for dangerous, invalid, dropped, review-required, and quarantine-required outcomes
- Loopgate audit events now store audit-safe TCL summaries for explicit-memory keep and deny paths
- deterministic-memory runtime facts and shell `/memory remember` output now use safe denial text instead of reflecting raw denied payloads (reference client path)
- contradiction handling now uses persisted TCL conflict anchor tuples (`version + canonical_key`) so memories only compete when they target the same semantic slot
- continuity-derived provider facts now reuse the same TCL analysis service for conservative anchor derivation and semantic signature persistence
- new distillate facts now persist an internal semantic projection with anchor tuple, exact signature, family signature, and risk motifs when analysis succeeds
- new memory facts now treat that semantic projection as the canonical internal anchor/signature carrier; legacy `conflict_key*` fields are accepted only at the decode/storage compatibility boundary and normalized away in the runtime model
- explicit todo/task metadata facts now route through that same analysis service and persist task-shaped semantic projections instead of bypassing TCL entirely
- goal and unresolved-item workflow transitions now also persist internal semantic projections, so durable continuity records no longer split between classified facts and unclassified workflow ops
- generic `preference.stated_preference` memories only supersede when a stable attribute anchor can be derived; otherwise they coexist
- wake-state contradiction resolution now uses persisted anchor tuples rather than Loopgate-local semantic key synthesis
- legacy anchorless records remain anchorless during replay and coexist rather than triggering read-time anchor reconstruction
- superseded lineages now keep replacement pointers and remain tombstoned for a built-in 30-day retention window before purge is allowed

Phase 1 boundary:

- generic client thread events do not yet flow into TCL
- continuity distillation is only partially informed by TCL; conservative provider-fact candidates may carry persisted semantic projections, but TCL does not replace distillation
- durable memory candidacy is not widened to raw assistant prose
- resonate keys do not reconstruct arbitrary text; they remain bounded recall handles

Next lift:

- broader candidate ingestion from non-workflow memory-like artifacts
- richer signature-registry coverage across wording variants and operator-readable risk tiers
- backend-owned memory authority consolidation plan:
  `docs/roadmap/2026-04-08-backend-owned-memory-redesign-plan.md`
  Current state: live remember/inspect/discover/recall/review/tombstone/purge
  now cross the backend seam; explicit remember candidate prep is backend-owned
  in the live path, the dead per-artifact store hooks were removed, and
  continuity inspect now rejects cross-session/thread/scope packet smuggling
  plus duplicate or non-monotonic event metadata. Continuity-derived fact
  persistence is also now bounded to scalar values that survive TCL analysis
  instead of blindly storing arbitrary payload shapes. Stable profile-slot
  discover is now exact-anchor-first for current `name`, `preferred_name`,
  `timezone`, and `locale` values so anchored state no longer depends on
  lexical tag overlap alone. Continuity-derived fact persistence now also
  routes through a backend-owned typed candidate helper instead of hand-building
  persisted facts directly from inline TCL analysis results. New continuity
  writes persist a Loopgate-owned canonical `observed_packet` in continuity
  JSONL instead of the raw inspect request body, and distillation derives from
  that typed packet rather than the caller payload bundle. The raw
  `/v1/continuity/inspect` route has now been removed, and the supported
  server-loaded `/v1/continuity/inspect-thread` path hands the backend a
  Loopgate-owned observed packet directly instead of fabricating a raw
  continuity request.
  Haven-backed observed packets now also stamp the authoritative control
  session id and carry the stable threadstore event hash on each source ref,
  so supported continuity provenance is stronger than a bare `thread:index`
  pointer. The explicit remember TCL candidate-builder seam has also moved
  behind the continuity backend, so the live memory-analysis path no longer
  depends on a `Server` test hook. Wake-state and recall facts now carry an
  explicit `state_class` (`authoritative_state` vs `derived_context`) so hard
  and soft memory no longer need to be inferred from precedence heuristics.
  The removed raw continuity-inspect route used to drop caller-supplied
  `source_refs` outright; continuity proposal provenance now only enters
  through the supported server-loaded path. The continuity memory benchmark’s
  `production_write_parity`
  seeding path now uses real authenticated `memory.remember` HTTP-over-UDS
  writes inside isolated Loopgate runtimes instead of direct in-process calls
  into `rememberMemoryFact(...)`. That makes the write-path benchmark claim much
  more honest, although projected-node discovery is still a harness-level
  retrieval benchmark rather than a full end-to-end `/v1/memory/discover` run.
  before packets enter backend-owned observed continuity state, so first-class
  provenance refs only survive on supported server-loaded paths. Backend-owned
  observed packets now also allowlist source-ref kinds, so new provenance
  sources have to be added intentionally rather than arriving as arbitrary
  strings in observed continuity state.
  Remaining gaps are
  test-only compatibility seams, stronger authoritative provenance for
  continuity sources, and broader TCL-informed continuity derivation.
- TCL-informed continuity and bounded semantic compression for distillates and resonate keys
- replace the current conservative attribute-anchor heuristics with richer TCL-derived conflict anchors for more preference and intent families
- simplify the memory/TCL implementation before widening it further; a dated
  engineering sketch is archived for maintainers at
  maintainer documentation checkout: `roadmap-drafts/2026-03-25-tcl-memory-simplification-plan.md` (outside this repository)
- define a swappable memory backend boundary and benchmark harness so
  `continuity_tcl` can be compared fairly against a `rag_baseline`; see
  `docs/rfcs/0011-swappable-memory-backends-and-benchmark-harness.md`
- keep benchmark comparator labels benchmark-only until they exist as real
  runtime backends behind the same Loopgate-owned seam
- define the authoritative storage/query shape for the `continuity_tcl`
  backend so it can evolve past JSON snapshots while keeping Loopgate in
  control; see `docs/rfcs/0013-continuity-tcl-storage-and-query-backend.md`
- freeze canonical TCL conformance and conservative anchor expansion rules
  before widening memory semantics further; see
  `docs/rfcs/0014-tcl-conformance-and-anchor-freeze.md`
- define an operator-client-owned scheduler/background execution model so the operator client can carry
  multi-step work forward without moving authority out of Loopgate; see
  `docs/rfcs/0012-scheduler-and-background-agent-execution.md`

## Current architecture

### Reference operator shell (`cmd/haven/`)

- **In-repo reference:** Wails/React under `cmd/haven/` (not a ship target)
- persona and prompt compilation (via Loopgate-backed model paths where used)
- local session state and approval rendering
- local ledger and continuity thread projection on the client side

### Loopgate

- control-plane authority for capabilities and lifecycle transitions
- policy evaluation
- capability orchestration
- approval creation and enforcement
- capability/approval token minting and request integrity
- secret resolution and connection handling
- quarantine and sandbox mediation
- morphling lifecycle ownership
- append-only control-plane audit

## What is still missing

The control plane is real, but **multi-tenancy** and broader enterprise deployment are still landing.

Not yet implemented (non-exhaustive):

- **`tenant_id` namespace** across remaining resources (memory partitions and core audit paths are in progress; grants and other state may still be global per instance)
- **Enterprise identity layer:** customer **IdP** via **OIDC/OAuth** (and SAML where required) for admin and/or node identity — RFC-first; config-sourced tenancy until then (`docs/setup/TENANCY.md`)
- **Enterprise secrets layer:** pluggable backends for **Vault**, **cloud KMS**, **HSM**-backed stores, **TPM**/platform identity for bootstrap — RFC-first; see `docs/setup/SECRETS.md` § *Enterprise integration layer*
- Loopgate-side memory inspector / distillate review pipeline
- explicit capability draft -> validate -> provision flow for developer DX
- additional secure secret-storage backends beyond macOS Keychain (covered by enterprise secrets layer above)
- broader typed integrations and skills beyond the current narrow adapter set
- full **remote / multi-node** deployment profile (mTLS admin path, policy sync)
- packaging and first-run onboarding polish

Deliberately not implemented yet:

- a public Loopgate network API
- a public morphling API
- generic unrestricted browsing or extraction
- remote deployment before the local governed workflow story is stronger

## Next phases

## Phase 1: Memory inspector and continuity hardening

- move toward Loopgate-authored distillates with explicit provenance
- keep v1 distillation field-first and deterministic
- add a narrow review queue for memory candidates requiring operator review
- extend the current global tag index toward explicit thread/task-scoped discovery
- keep remembered state distinct from freshly checked state

Lift: medium-large

## Phase 2: Capability provisioning DX

- first-run setup path that drafts config instead of exposing raw schema
- `/capability draft ...` style workflow in operator clients
- Loopgate validation/provision/reject cycle with explicit audit
- draft files remain reviewable and diffable
- no natural-language authority; model output remains draft content only

Lift: medium-large

## Phase 3: Typed capability and workflow expansion

- add only the narrow adapters needed for real operator workflows
- improve capability selection and denial explanation quality
- improve multi-step aggregation over safe structured results
- keep raw remote bodies quarantined and out of prompt/memory by default
- avoid widening extraction contracts without a concrete workflow need

Lift: medium-large

## Phase 4: Operational hardening

- CLI packaging / `loopgate` or legacy `morph` entrypoints (naming as shipped)
- packaging/install path
- Loopgate supervised launch
- richer secret rotation workflows
- CI/static analysis expansion
- more restart/crash invariant coverage

Lift: medium-large

## Phase 5: Enterprise / remote profile (RFC-first, in progress)

- `tenant_id` isolation and admin-node governance (mTLS)
- define remote transport profiles that preserve the Loopgate authority model
- replace local peer binding with deployment identity where required
- **Integration layers (sequenced after core enterprise slice):** customer **IdP (OIDC/OAuth)** and **enterprise secret backends (Vault / KMS / HSM / TPM-related bootstrap)** as explicit plugin surfaces — see `sprints/2026-04-01-loopgate-enterprise-phased-plan.md` § *Future enterprise integration layers*
- keep normative details RFC-first where the spec is still moving

Lift: large

## Overall lift assessment

This is no longer a pure architecture sketch. The local control plane, sandbox,
append-only audit, and morphling lifecycle substrate are real.

Why it is tractable:

- the local prompt/model boundary already exists
- the local continuity ledger and Loopgate memory boundary now both exist
- the control-plane split is implemented and tested
- the sandbox and morphling lifecycle contracts are now explicit in code

Why it is still large:

- continuity governance now has explicit review, tombstone, and purge state, but client-side projection/UX is still narrow
- auth, secrets, and restart safety remain cross-cutting concerns
- developer UX must improve without weakening authority boundaries
- integrations need typed contracts and quarantine discipline

## Immediate next slice

The next engineering slice should stay on continuity hardening and memory
governance, not more spawn schema churn and not broader public API surface.

Immediate focus:

- extend operator-facing projection for continuity review and lineage-governance status without moving authority into the client
- extend restart tests around sealed-but-uninspected and inspected-but-not-yet-acknowledged threads
- keep wake-state recall bounded and provenance-rich
- keep host writes, export, and durable-memory mutation behind existing approval and promotion boundaries

Do not:

- expose morphlings as a public network API
- widen extraction or browsing surface without a workflow need
- weaken append-only audit or state-machine ownership for convenience
