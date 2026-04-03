**Last updated:** 2026-03-26

# Thought Compression Language (TCL) v0.4

## 1. Purpose

TCL is a Loopgate-owned semantic intermediate representation.

It exists to translate raw natural language, model output, or tool text into a compact, structured semantic sketch that is easier to:

- classify
- validate
- audit
- hash
- compare
- distill
- policy-check

TCL is:

- optional
- derived
- assistive

TCL is not:

- a human language
- canonical durable memory
- a replacement for the continuity stream
- a replacement for distillates or wake state
- an authority source

## 1.1 Current implementation status

TCL is currently a design and integration target, not an authoritative storage
format and not yet a complete production subsystem in the Go tree.

The intended implementation order is:

1. explicit memory-write normalization and risk checking
2. dangerous-pattern fingerprinting
3. broader memory-candidate classification
4. distillation assistance and semantic compression for older memory

Phase 1 should wire TCL only to explicit memory writes. Broader continuity
candidate ingestion comes later.

## 2. Intended Use

TCL is useful when Loopgate needs a compact semantic layer for:

- intent normalization
- memory candidate classification
- keep / drop / flag / quarantine decisions
- dangerous-pattern fingerprinting
- distillation assistance

TCL may be lossy relative to authoritative artifacts.

That is acceptable because authoritative continuity artifacts remain the source of truth.

## 3. Architecture Boundary

Recommended pipeline:

```text
Raw English / model output / tool text
    ↓
intent extraction
    ↓
object + qualifier normalization
    ↓
TCL semantic sketch
    ↓
validation + risk analysis + signature lookup
    ↓
decision:
  keep / drop / flag / quarantine / review
    ↓
authoritative downstream action
```

Examples of authoritative downstream actions:

- continuity distillate creation
- durable fact write
- task creation
- approval prompt creation
- denial
- audit enrichment

Important:

- TCL may recommend a disposition, but Loopgate still decides the authoritative
  outcome.
- A TCL `KEP` result is permission to continue evaluation, not permission to
  persist by itself.
- A TCL hard-block is valid only for curated high-confidence known-bad semantic
  families.

## 4. Design Goals

TCL should help answer:

- what is this content trying to do?
- is it durable signal or disposable noise?
- is it risky?
- is it poison-like?
- is it similar to a known dangerous pattern?

## 5. Core Principles

1. Meaning over wording
2. Deterministic enough for Go implementation
3. Compact but inspectable
4. Policy-friendly
5. Derived, never self-authorizing
6. Safe to reject on parse or validation failure

## 6. Token Vocabulary

All semantic tokens are globally unambiguous across categories.

### 6.1 Actions

```text
ASK  ask / inquire
RDD  read / inspect / open
WRT  write / create / modify
SRH  search / locate
ANL  analyze
SUM  summarize
CMP  compare
PLN  plan
STR  store / remember
RCL  recall
APR  approve
DNY  deny
```

### 6.2 Objects

```text
USR  user
FIL  file
REP  repository
MEM  memory
TSK  task
POL  policy
IMG  image
COD  code
NTE  note
RES  result
SYS  system
UNK  unknown / unresolved object
```

### 6.3 Qualifiers

```text
SEC  security-sensitive
URG  urgent
DET  detailed
CON  concise
PRI  private
EXT  external
INT  internal
SPC  speculative
CNF  confirmed
```

### 6.4 States

```text
ACT  active / current
PND  pending
DON  done
BLK  blocked
REV  review required
SPR  superseded
AMB  ambiguous / uncertain
```

### 6.5 Relations

```text
SUP  supports
CNT  contradicts
REL  related_to
DRV  derived_from
DEP  depends_on
IMP  importance / emphasis
```

### 6.6 Trust

```text
TSY  system-derived
TUS  direct user-originated
TEX  external-source-derived
TIF  inferred / derived
```

### 6.7 Disposition

```text
KEP  keep as candidate signal
DRP  drop as low-value / non-durable
FLG  flag as risky
QTN  quarantine / isolate
RVW  send to review path
```

## 7. Node Model

A validated TCL result may be represented as a semantic node with:

- normalized action
- normalized object
- normalized qualifiers
- optional output action
- lifecycle state
- relations
- trust / provenance metadata
- optional contradiction anchor
- downstream disposition
- optional semantic signature

See [TCL Memory Node Schema](./TCL%20Memory%20Node%20Schema.md) for the strict node structure.

## 8. Compact Expression

Canonical compact form:

```text
ACT(OBJ[:QLF])->OUT[STA]%(N)
```

Examples:

```text
ANL(REP:SEC)->SUM[ACT]%(8)
STR(MEM:PRI)->WRT[REV]%(9)
ASK(UNK)[AMB]%(1)
```

The compact expression is:

- intentionally compact
- machine-friendly
- suitable for hashing and comparison
- not sufficient by itself as authoritative memory

Relation targets in compact syntax may point at either:

- `@MID` references
- inline nested TCL expressions

Contradiction anchors are not encoded directly in the compact expression; they
belong to the validated node shape and downstream semantic analysis.

## 9. Dangerous-Pattern Use

TCL is especially valuable when different phrasings normalize into the same risky intent family.

Example pattern families:

- secret persistence requests
- memory poisoning attempts
- policy-escape attempts
- prompt injection patterns
- hostile self-modification requests

The normalized node may be hashed into a semantic signature and compared against a datastore of known dangerous patterns.

Recommended signature tiers:

1. exact semantic signature
   - hash of the fully normalized semantic node
2. family signature
   - hash of a reduced normalized family shape such as `ACT + OBJ + QUAL + OUT`
3. risk motif signature
   - hash or rule-family identifier for curated dangerous semantic motifs such
     as secret persistence or policy-override memory attempts

These signatures are assistive and do not replace policy evaluation.

Only a small curated deny set should hard-block automatically in early
implementations. Everything else should route to review, quarantine, or
explicit denial paths owned by Loopgate.

## 10. Recommended First Uses

The best first implementation targets are:

1. explicit memory-write normalization
2. host-action planning normalization
3. dangerous-pattern fingerprinting
4. memory candidate keep / drop / flag / quarantine decisions

## 11. Non-goals

TCL should not be treated as:

- sole memory truth
- sole audit truth
- a public model-facing language contract
- a replacement planning engine
- a permission system
- a hidden semantic-search layer over all prior history

## 12. Resonance and compression note

TCL is a good source for semantic compression, not prose replay.

When TCL later informs distillates and resonate keys:

- distillates should remain the richer authoritative derived artifacts with
  provenance
- resonate keys should act as compact semantic handles that reconstruct the
  general contour of memory
- neither TCL nor resonate keys should reconstruct arbitrary prose, raw hostile
  text, or full hidden prompts

The goal is semantic continuity, not raw text recovery.
