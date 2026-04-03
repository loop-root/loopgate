**Last updated:** 2026-03-26

# RFC 0014: TCL Conformance and Conservative Anchor Freeze

Status: draft

## 1. Summary

TCL now needs one canonical semantic source and one conservative anchor policy.

This RFC proposes:

- the TCL RFC set under `docs/TCL-RFCs/` is the canonical semantic source
- conformance fixtures must prove code matches that source
- anchor derivation stays narrow, explicit, versioned, and fail-closed

The goal is to stop semantic drift before wider benchmark and storage work makes
that drift expensive.

## 2. Motivation

The continuity/TCL design only works if two things stay stable:

1. the meaning of a validated TCL node
2. the conditions under which a durable memory candidate gets an anchor

If either drifts silently:

- contradiction handling drifts
- supersession behavior drifts
- benchmark results become hard to trust
- security policy comparisons become less meaningful

## 3. Canonical source

The canonical semantic source is the TCL RFC set:

- [Thought Compression Language](../TCL-RFCs/Thought%20Compression%20Language.md)
- [TCL Syntax](../TCL-RFCs/TCL%20Syntax.md)
- [TCL Semantic Node Schema](../TCL-RFCs/TCL%20Memory%20Node%20Schema.md)
- [English to TCL](../TCL-RFCs/English%20to%20TCL.md)

Code is not the semantic source.
Code is an implementation that must conform to the RFCs.

If the RFCs are not explicit enough for an implementation choice, add to the
RFCs or add a supporting RFC. Do not let code invent semantic policy quietly.

## 4. Conformance model

Conformance should be enforced with checked-in fixtures rather than only prose.

The first checked-in fixture corpus lives under `internal/tcl/` and should grow
monotonically with RFC-approved syntax, validation, and anchor cases.

Required fixture classes:

- valid TCL nodes
- invalid TCL nodes
- valid compact syntax examples
- invalid compact syntax examples
- English-to-TCL normalization examples
- anchor derivation examples
- poisoning-shaped examples expected to review, flag, or quarantine

Each fixture should record:

- source input
- expected validated node or error
- expected compact rendering where applicable
- expected anchor tuple or explicit absence of anchor
- expected disposition / poisoning flags where applicable

## 5. Conservative anchor policy

Anchors must stay narrow and explicit.

Default rule:

- if a semantic class is not explicitly approved for anchoring, it is unanchored

Anchors should initially be limited to stable contradiction slots such as:

- explicit profile facts
- bounded stable preferences

Anchors should not be created for:

- broad narrative summaries
- unstable intent
- speculative or ambiguous content
- generic workflow context unless explicitly approved

## 6. Anchor versioning and change control

Anchor semantics must be versioned.

Rules:

- `v1` anchor meanings are frozen once published
- semantic changes require a new version, not silent reinterpretation
- widening anchor coverage requires:
  - RFC update
  - registry update
  - regression fixtures
  - benchmark review for contradiction/supersession effects

Do not silently retarget an existing anchor key to a new meaning.

## 7. Registry rules

The anchor registry should be the only place that defines:

- allowed anchored slot families
- canonical key normalization
- alias normalization for anchorable classes

Unknown semantic classes must not synthesize new anchors opportunistically.

## 8. Security and benchmarking implications

Conservative anchor freeze is part of the security model.

It reduces:

- false contradiction
- false supersession
- hostile overwrite of stable slots
- benchmark ambiguity about what the backend believed was “the same fact”

The benchmark harness should explicitly measure:

- false contradiction count
- false suppression count
- stale intrusion count
- stale suppression count

## 9. Rollout order

1. declare the TCL RFC set canonical
2. add conformance fixtures
3. freeze the initial anchor registry
4. require version bump for anchor meaning changes
5. expand only after benchmark evidence justifies it
