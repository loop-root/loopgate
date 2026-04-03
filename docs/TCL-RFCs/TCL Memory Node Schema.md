**Last updated:** 2026-03-26

# TCL Semantic Node Schema — v0.4

## 1. Scope

This document defines the strict schema for a validated TCL semantic node.

Despite the historical filename, this is not a canonical memory-storage format.

It is an optional IR node used for:

- semantic normalization
- candidate classification
- risk analysis
- signature generation
- distillation assistance

Authoritative continuity artifacts may derive from this node, but they remain the source of truth.

## 1.1 Implementation note

This schema describes the strict validated TCL node shape. Early implementation
phases do not require every downstream memory path to persist full TCL nodes.

Phase 1 uses this schema primarily for:

- explicit memory-write normalization
- candidate classification
- dangerous-pattern fingerprinting
- keep / drop / flag / quarantine / review decisions before persistence

Later phases may let distillates and resonate keys derive bounded semantic
compression from validated nodes, but TCL still remains assistive.

## 2. Node Structure

```text
NODE {
  ID
  ACT
  OBJ
  QUAL
  OUT
  STA
  REL
  META
  ANCHOR
  DECISION
}
```

## 3. Field Definitions

| Field | Type | Required | Description |
|------|------|----------|-------------|
| `ID` | `MID` | yes | unique semantic node identifier |
| `ACT` | `ACT` | yes | primary action token |
| `OBJ` | `OBJ` | yes | primary object token |
| `QUAL` | `list[QLF]` | no | normalized qualifiers |
| `OUT` | `ACT` | no | resulting action token |
| `STA` | `STA` | yes | lifecycle / interpretation state |
| `REL` | `list[RELATION]` | no | explicit relation records |
| `META` | `META` | yes | trust, certainty, and provenance metadata |
| `ANCHOR` | `ANCHOR` | no | stable contradiction-slot anchor when derivable |
| `DECISION` | `DECISION` | no | downstream keep / drop / flag guidance |

## 4. Identifier Format

```text
MID := "M" 1*DIGIT
```

Examples:

- `M1`
- `M42`
- `M1007`

## 5. Relation Structure

```text
RELATION {
  TYPE: RELTOK
  TARGET_MID?: MID
  TARGET_EXPR?: NODE_EXPR
}
```

Exactly one of `TARGET_MID` or `TARGET_EXPR` must be present.

`TYPE` accepts any member of the relation vocabulary:

- `SUP`
- `CNT`
- `REL`
- `DRV`
- `DEP`
- `IMP`

This field is not a generic string. It is constrained to the relation token set.

`TARGET_EXPR` allows a relation to point at an inline nested TCL expression
instead of only a prior `MID` reference. This matches the current compact
syntax/parser behavior.

## 5.1 Anchor structure

```text
ANCHOR {
  VERSION: string
  DOMAIN: string
  ENTITY: string
  SLOT_KIND: string
  SLOT_NAME: string
  FACET?: string
}
```

The canonical contradiction key derived from `ANCHOR` is:

```text
VERSION + canonical_key
```

Where `canonical_key` is:

```text
DOMAIN ":" ENTITY ":" SLOT_KIND ":" SLOT_NAME [ ":" FACET ]
```

This field is optional because not every validated TCL node has a stable
contradiction slot.

## 6. Metadata Structure

```text
META {
  ACTOR: OBJ
  TRUST: TRUST
  CONF: int
  TS: unix_ts
  SOURCE: string
  SIG: string?
}
```

| Field | Type | Required | Description |
|------|------|----------|-------------|
| `ACTOR` | `OBJ` | yes | originating actor/object class |
| `TRUST` | `TRUST` | yes | trust classification |
| `CONF` | `int` | yes | certainty score from `0..9` |
| `TS` | `unix_ts` | yes | creation timestamp |
| `SOURCE` | `string` | yes | source channel, e.g. `user_input`, `tool_output`, `model_output` |
| `SIG` | `string` | no | canonical exact semantic signature or risk fingerprint |

Valid `TRUST` values:

- `TSY`
- `TUS`
- `TEX`
- `TIF`

## 7. Decision Structure

```text
DECISION {
  DISP: DSP
  REVIEW_REQUIRED: bool
  RISKY: bool
  POISON_CANDIDATE: bool
  REASON: string?
}
```

| Field | Type | Required | Description |
|------|------|----------|-------------|
| `DISP` | `DSP` | yes | downstream disposition |
| `REVIEW_REQUIRED` | `bool` | yes | whether review is required |
| `RISKY` | `bool` | yes | whether the content appears dangerous |
| `POISON_CANDIDATE` | `bool` | yes | whether the content resembles poisoning / injection / hostile steering |
| `REASON` | `string` | no | short explanation |

Valid `DSP` values:

- `KEP`
- `DRP`
- `FLG`
- `QTN`
- `RVW`

## 8. Canonical Compact Form

```text
ACT(OBJ[:QLF])->OUT[STA]%(N)
```

This compact form must be derivable from node fields.

It is a semantic sketch, not the full node.

## 9. Validation Rules

- `ID` MUST match `MID`
- `ACT`, `OBJ`, and `STA` are required
- `QUAL` count MUST be `0..3`
- qualifiers MUST be unique
- `OUT`, if present, MUST be a valid action token
- `REL.TYPE` MUST be a valid relation token
- each relation MUST target exactly one of:
  - a valid `MID`
  - a valid nested target expression
- `ANCHOR`, if present, MUST be structurally valid
- `CONF` MUST be integer `0..9`
- `TRUST` MUST be one of `TSY`, `TUS`, `TEX`, `TIF`
- `DISP`, if present, MUST be one of `KEP`, `DRP`, `FLG`, `QTN`, `RVW`
- unknown tokens MUST fail validation

## 10. Normalization Rules

- all semantic tokens MUST be uppercase
- compact form MUST NOT contain placeholder category labels
- semantic signatures SHOULD be derived from normalized fields, not raw English
- duplicate identical relations SHOULD be rejected or normalized
- invalid nodes MUST NOT drive durable memory, policy, or safety decisions

## 10.1 Signature tiers

Implementations may derive multiple signature tiers from one validated node:

- exact semantic signature
- family signature
- risk motif signature

The node-local `META.SIG` field should be treated as the canonical exact
signature when only one signature is embedded directly on the node. Additional
family or risk signatures may live alongside analysis results rather than inside
the node itself.

## 11. Example Node

```text
NODE {
  ID: M42
  ACT: ANL
  OBJ: REP
  QUAL: [SEC]
  OUT: SUM
  STA: ACT
  REL: [
    { TYPE: DRV, TARGET_MID: M41 }
  ]
  META: {
    ACTOR: USR
    TRUST: TUS
    CONF: 8
    TS: 1700000000
    SOURCE: "user_input"
    SIG: "sha256:abcd1234"
  }
  ANCHOR: null
  DECISION: {
    DISP: KEP
    REVIEW_REQUIRED: false
    RISKY: false
    POISON_CANDIDATE: false
    REASON: "high-signal repository analysis request"
  }
}
```

## 12. Dangerous Example Node

```text
NODE {
  ID: M77
  ACT: STR
  OBJ: MEM
  QUAL: [PRI, EXT]
  OUT: WRT
  STA: REV
  REL: []
  META: {
    ACTOR: USR
    TRUST: TUS
    CONF: 9
    TS: 1700000100
    SOURCE: "user_input"
    SIG: "sha256:danger-pattern-01"
  }
  ANCHOR: {
    VERSION: "v1"
    DOMAIN: "usr_memory"
    ENTITY: "export"
    SLOT_KIND: "fact"
    SLOT_NAME: "private_external_write"
  }
  DECISION: {
    DISP: QTN
    REVIEW_REQUIRED: true
    RISKY: true
    POISON_CANDIDATE: true
    REASON: "private external memory write pattern"
  }
}
```

## 13. Boundary Note

This schema is assistive.

Continuity stream, distillates, and wake state remain authoritative.
Validated TCL may inform semantic compression for later recall, but it must not
be treated as canonical durable memory or as an authority token.
