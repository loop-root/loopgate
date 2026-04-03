**Last updated:** 2026-03-24

# RFC 0006: Bounded Scalar Subclasses

- Status: Draft
- Applies to: Loopgate promotion policy, field metadata, derived artifact
  creation, and future prompt/memory eligibility rules

## 1. Purpose

This RFC refines the coarse `bounded_scalar` class from RFC 0005 into explicit
subclasses that can be reasoned about and validated deterministically.

The goal is to prevent "scalar" from becoming an over-broad trust bucket.

## 2. Core invariants

- Scalar shape alone does not grant trust.
- Prompt and memory eligibility MUST be based on explicit validation class, not
  on JSON type alone.
- User-controlled short labels MUST NOT be treated as equivalent to strict
  identifiers.
- Unknown scalar subclasses MUST fail closed.

## 3. Scalar subclasses

V1 bounded-scalar subclasses:

- `boolean`
  - literal `true` / `false`
- `validated_number`
  - integer or decimal that passed explicit range/type validation
- `enum`
  - value in a fixed allowlist declared by capability policy
- `timestamp`
  - parsed and normalized timestamp in an approved format
- `strict_identifier`
  - identifier validated against an explicit safe identifier rule
- `short_text_label`
  - bounded user-visible text label that is not a strict identifier

Anything else MUST NOT be treated as a bounded scalar by default.

## 4. Subclass semantics

### `boolean`

Low-risk structural value.

May be eligible for:

- `display`
- `memory`
- `prompt`

subject to policy.

### `validated_number`

May be eligible for:

- `display`
- `memory`
- `prompt`

subject to policy and explicit validation constraints such as range, integral
vs decimal, and unit expectations.

### `enum`

May be eligible for:

- `display`
- `memory`
- `prompt`

subject to policy and an explicit fixed value set.

### `timestamp`

May be eligible for:

- `display`
- `memory`
- `prompt`

subject to policy and normalization requirements.

### `strict_identifier`

May be eligible for:

- `display`
- `memory`
- `prompt`

subject to policy.

`strict_identifier` means the value is inert metadata, not arbitrary text.

### `short_text_label`

May be eligible for:

- `display`

Must be denied for:

- `memory`
- `prompt`

unless a future RFC adds a stronger trust process.

Rationale:

Short labels are still user-controlled or remote-origin text and can carry
instructional or socially manipulative content even when bounded in size.

## 5. Relationship to tainted scalar text

`short_text_label` is not the same as `tainted_scalar_text`.

Rules:

- `tainted_scalar_text` remains display-only in v1 per RFC 0005
- `short_text_label` is a bounded subclass for text-like labels that still do
  not automatically become prompt- or memory-safe
- capability policy MUST NOT silently upgrade `short_text_label` into
  `strict_identifier`

## 6. Metadata implications

Field metadata SHOULD eventually record scalar subclass explicitly.

Recommended future field metadata addition:

- `scalar_subclass`

Example values:

- `boolean`
- `validated_number`
- `enum`
- `timestamp`
- `strict_identifier`
- `short_text_label`

If `scalar_subclass` is absent for a field that is being considered for prompt
or memory promotion, the decision SHOULD fail closed.

## 7. Validation boundary

Scalar subclass assignment MUST come from deterministic validation logic inside
Loopgate or trusted configuration.

It MUST NOT come from:

- field name alone
- JSON type alone
- model inference
- UI inference
- operator guesswork without explicit promotion/transform policy

## 8. Initial implementation guidance

V1 implementation SHOULD treat the following as promotable beyond display,
subject to policy:

- `boolean`
- `validated_number`
- `enum`
- `timestamp`
- `strict_identifier`

V1 implementation SHOULD treat the following as display-only:

- `short_text_label`
- `tainted_scalar_text`

Unknown subclasses or unclassified scalar text SHOULD be denied for prompt and
memory targets.

## 9. Open questions

- whether future policy should distinguish between local and remote
  `strict_identifier`
- whether some tightly constrained labels may later become memory-eligible
- how subclass metadata should interact with future suspicious field-name hooks
