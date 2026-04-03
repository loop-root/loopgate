**Last updated:** 2026-03-24

# Execution Backlog

Status: active implementation backlog  
Authority: implementation-facing  
Scope: convert current review feedback into bounded, testable slices

## Guardrails

Every slice below must preserve the existing system invariants:

- Loopgate remains the authority boundary
- model output remains untrusted content, never authority
- privileged control-plane traffic stays local and socket-bound in v1
- append-only audit remains tamper-evident and cryptographically chained
- user-visible sandbox paths remain virtual paths, not runtime filesystem paths
- policy remains deny-by-default and deterministic

Protocol alignment rule:

- AMP is a vendor-neutral protocol track
- implementation names may remain product-specific if the structure and
  semantics match the RFC
- RFC `MUST` and `MUST NOT` requirements are binding even when product terms
  differ
- naming drift is acceptable; semantic drift on normative requirements is not

The ordering below is intentional. It front-loads runtime cleanup and boundary
tests before adding new product surface.

## Slice 0: Runtime Cleanup And Repo Hygiene

Goal:

- keep `cmd/morph/main.go` as bootstrap only
- move session control, scheduling, shutdown, and loop orchestration into small
  files
- fix the distillation cursor serialization race
- track the `cmd/morph` helper layer that was previously hidden by `.gitignore`

Key files:

- `cmd/morph/main.go`
- `cmd/morph/interactive_loop.go`
- `cmd/morph/session_controller.go`
- `cmd/morph/distillation_scheduler.go`
- `cmd/morph/shutdown.go`
- `cmd/morph/runtime_paths.go`
- `cmd/morph/*_helpers.go`
- `.gitignore`

Exit criteria:

- `main.go` is bootstrap and wiring only
- distillation cursor reads and writes are serialized behind the distillation
  lock
- helper files under `cmd/morph` are tracked source, not ignored artifacts
- `go test ./...` passes

## Slice 1: Real Boundary Integration Harness

Goal:

- prove the real Morph -> Loopgate Unix-socket boundary under live request flow

First tests:

1. signed-request replay rejection over the real socket
2. denied write over the real socket does not land on disk
3. append-only audit chain and redaction round trip

Target package:

- `internal/integration/`

Primary doc:

- `docs/design_overview/integration_test_plan.md`

Exit criteria:

- the harness uses a real Unix socket, temp repo fixture, real client, and real
  Loopgate server
- tests assert filesystem state and audit-chain state, not just handler return
  values
- no public TCP transport is introduced

## Slice 2: RFC Normalization Pass

Goal:

- resolve the highest-risk RFC ambiguities before more behavior depends on them

Required updates:

- tighten restart, replay, and high-risk execution semantics in RFC 0001
- make taint-laundering prohibitions and `model_derived` policy normative in
  RFC 0003
- define retry and prune semantics in RFC 0004
- define stronger promotion paths for `short_text_label` and future safe
  structured collections in RFC 0005
- bind `strict_identifier` to the exact validation rule in RFC 0006
- fix numbering and define required/optional field behavior and schema drift in
  RFC 0008
- reconcile RFC-MORPH-0005 with RFCs 0009 and 0010 so memory authority is not
  split across overlapping specs

New RFCs to add:

- capability provisioning flow
- failure taxonomy and recovery semantics
- remote transport profile
- end-to-end flow of model output -> Loopgate decision -> audit -> memory

Exit criteria:

- the implementation team has one authoritative answer for each boundary above
- no new implementation work depends on undefined fail-open behavior

## Slice 3: Capability Draft Provisioning DX

Goal:

- move integration burden onto the system without weakening provisioning
  authority

Operator flow:

1. first-run Morph wizard produces a draft local config
2. `/capability draft ...` creates an untrusted draft capability definition
3. Loopgate validates the draft against selector, extraction, and policy rules
4. operator explicitly approves provisioning
5. Loopgate records provision, reject, or revise events in append-only audit

Non-goals:

- no direct natural-language provisioning
- no model-authored capability becomes active without Loopgate validation and
  explicit operator approval

Exit criteria:

- draft, validate, reject, approve, and provision are distinct states
- each state transition is auditable
- draft artifacts are reviewable and diffable

## Slice 4: Memory Inspector MVP

Goal:

- make continuity useful without letting memory become an unbounded trust sink

v1 shape:

- landed baseline:
  - Morph-local three-thread continuity substrate
  - sealed-thread handoff to Loopgate
  - Loopgate-owned distillates, resonate keys, wake-state projection, discovery,
    and recall
  - deterministic threshold-based submission
- next hardening:
  - candidate outcome is `auto_approve`, `requires_review`, or `denied`
  - approved distillates include source refs, policy hash, inspector version,
    and creation timestamp
  - tombstone/purge semantics are explicit and test-covered
  - wake-state loading remains budget-based with a soft item cap
  - thread discovery stays deterministic and tag-based, not semantic

Scope activation order:

1. explicit resume of a thread or task scope
2. prompted resume after deterministic tag overlap match
3. global wake state only

Exit criteria:

- wake state stays bounded by token budget
- memory provenance is explicit and Loopgate-owned
- tombstoned artifacts cannot re-enter wake state or recall
- immutable `memory_candidate` denials have a defined derived-artifact escape
  path instead of silent reclassification

## Slice 5: Morphling Worker Handshake

Goal:

- turn the existing lifecycle-only morphling pool into a real governed worker
  demo

Required behavior:

- morphlings remain local-only and socket-bound
- each worker gets a bounded Loopgate identity/session
- workers may update only bounded `status_text`, bounded `memory_strings`, and
  staged artifact references
- default active morphling cap remains 5
- spawn, update, and termination events remain append-only and hash-linked

Exit criteria:

- Morph can render a live pool of morphlings with status and memory strings
- no public API surface is introduced
- worker authority remains narrower than operator authority

## Slice 6: Remote Profile Is RFC-First

Goal:

- design the remote deployment profile without weakening the local security
  model

v1 of this slice is documentation only.

Remote profile constraints:

- mTLS replaces local peer binding for deployment identity
- signed request envelopes remain mandatory
- tenant scoping is explicit in sessions, capability tokens, and audit storage
- secret handling moves to a real secret manager rather than local keychain

Exit criteria:

- remote transport is specified before it is implemented
- no current code path quietly expands from local-only to public network access

## Deferred Backburner: AMP Protocol-Conformance Alignment

Goal:

- converge Morph and Loopgate toward the AMP protocol track without forcing a
  cosmetic rename of product terms

Deferred rule:

- this slice stays behind the memory inspector work unless a normative AMP
  `MUST` blocks current implementation work

Most important deferred gaps:

- add exact AMP version and transport-profile negotiation during session
  establishment
- replace the current Loopgate-specific signed-request headers with the RFC 0004
  canonical envelope for both operator and morphling-worker requests
- add canonical `token_binding` semantics for scoped-token-bound actions
- replace generic approval metadata with the RFC 0005 approval manifest,
  subject binding, execution binding, and `single-use` consumed semantics
- move authoritative memory derivation and privileged recall behind a
  Loopgate-owned inspector path per AMP RFC 0006
- introduce neutral compact AMP objects for denial, event, artifact reference,
  memory reference, approval request, and approval decision

Constraints:

- no public network API is introduced as part of AMP convergence
- local Unix-socket transport remains the default authority path
- append-only audit and hash-chain integrity remain stronger than or equal to
  the current implementation
- vendor-specific field names may remain where the RFC does not require exact
  names, as long as the normative structure still matches

Exit criteria:

- the local transport path satisfies the AMP local-uds-v1 checklist for the
  normative `MUST` requirements that apply to the implemented surface
- approval, memory, and artifact/reference flows have an implementation-neutral
  shape even if product labels remain in the codebase
- conformance work does not weaken current security boundaries for convenience

## Priority Order

1. Slice 0
2. Slice 1
3. Slice 2
4. Slice 3
5. Slice 4
6. Slice 5
7. Slice 6

## Current Recommendation

The immediate implementation focus should stay on the memory path. AMP
protocol-conformance work is a deferred backlog item unless a normative AMP
`MUST` becomes blocking for the memory slice.
