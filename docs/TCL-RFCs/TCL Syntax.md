**Last updated:** 2026-03-24

# Thought Compression Language (TCL) — Syntax v0.4

## 1. Canonical Compact Form

Canonical compact form:

```text
ACTION_EXPR [REL_CHAIN] [CERT]
```

Where:

```text
ACTION_EXPR := ACT "(" OBJ [ ":" QLF { ":" QLF } ] ")" [ "->" ACT ] "[" STA "]"
REL_CHAIN   := { REL_OP ( ACTION_EXPR | "@" MID ) }
CERT        := "%(" DIGIT ")"
```

Examples:

```text
ANL(REP:SEC)->SUM[ACT]%(8)
STR(MEM:PRI)->WRT[REV]%(9)
ASK(UNK)[AMB]%(1)
RDD(FIL)[ACT]~@M41%(7)
```

`->ACT` is optional.
`REL_CHAIN` is optional.
`CERT` is optional.

## 2. Token Sets

### 2.1 Action Tokens

| Token | Meaning |
|-------|---------|
| `ASK` | ask / inquire |
| `RDD` | read / inspect / open |
| `WRT` | write / create / modify |
| `SRH` | search / locate |
| `ANL` | analyze |
| `SUM` | summarize |
| `CMP` | compare |
| `PLN` | plan |
| `STR` | store / remember |
| `RCL` | recall |
| `APR` | approve |
| `DNY` | deny |

### 2.2 Object Tokens

| Token | Meaning |
|-------|---------|
| `USR` | user |
| `FIL` | file |
| `REP` | repository |
| `MEM` | memory |
| `TSK` | task |
| `POL` | policy |
| `IMG` | image |
| `COD` | code |
| `NTE` | note |
| `RES` | result |
| `SYS` | system |
| `UNK` | unknown / unresolved object |

### 2.3 Qualifier Tokens

| Token | Meaning |
|-------|---------|
| `SEC` | security-sensitive |
| `URG` | urgent |
| `DET` | detailed |
| `CON` | concise |
| `PRI` | private |
| `EXT` | external |
| `INT` | internal |
| `SPC` | speculative |
| `CNF` | confirmed |

### 2.4 State Tokens

| Token | Meaning |
|-------|---------|
| `ACT` | active / current |
| `PND` | pending |
| `DON` | done |
| `BLK` | blocked |
| `REV` | review required |
| `SPR` | superseded |
| `AMB` | ambiguous / uncertain |

### 2.5 Relation Tokens

| Token | Meaning |
|-------|---------|
| `SUP` | supports |
| `CNT` | contradicts |
| `REL` | related_to |
| `DRV` | derived_from |
| `DEP` | depends_on |
| `IMP` | importance / emphasis |

### 2.6 Trust Tokens

| Token | Meaning |
|-------|---------|
| `TSY` | system-derived |
| `TUS` | direct user-originated |
| `TEX` | external-source-derived |
| `TIF` | inferred / derived |

### 2.7 Disposition Tokens

These are part of the wider semantic node, not the minimal compact expression.

| Token | Meaning |
|-------|---------|
| `KEP` | keep as candidate signal |
| `DRP` | drop as low-value / non-durable |
| `FLG` | flag as risky |
| `QTN` | quarantine / isolate |
| `RVW` | send to review path |

## 3. Operators

| Form | Meaning | Relation Token |
|------|---------|----------------|
| `->` | resulting action flow inside `ACTION_EXPR` | n/a |
| `^`  | supports | `SUP` |
| `x`  | contradicts | `CNT` |
| `~`  | related_to | `REL` |
| `<-` | derived_from | `DRV` |
| `>>` | depends_on | `DEP` |
| `!`  | importance / emphasis | `IMP` |

Other delimiters:

| Form | Meaning |
|------|---------|
| `()` | object scope |
| `:`  | qualifier delimiter |
| `[]` | state annotation |
| `@MID` | memory reference |
| `%(N)` | certainty score where `N` is `0..9` |

## 4. Grammar

Practical grammar:

```text
ACT    := one valid action token
OBJ    := one valid object token
QLF    := one valid qualifier token
STA    := one valid state token
RELTOK := one valid relation token
MID    := "M" DIGIT { DIGIT }
DIGIT  := "0" | "1" | "2" | "3" | "4" | "5" | "6" | "7" | "8" | "9"
CERT   := "%(" DIGIT ")"

EXPR        := ACTION_EXPR [ REL_CHAIN ] [ CERT ]
ACTION_EXPR := ACT "(" OBJ [ ":" QLF { ":" QLF } ] ")" [ "->" ACT ] "[" STA "]"
REL_CHAIN   := { REL_OP ( ACTION_EXPR | REF_EXPR ) }
REF_EXPR    := "@" MID
REL_OP      := "^" | "x" | "~" | "<-" | ">>" | "!"
```

## 5. Validator Rules

- every token MUST exist in the defined vocabulary
- token meanings are category-specific and globally unambiguous
- no whitespace is permitted in canonical syntax
- object scope MUST NOT be empty
- state is required
- `->ACT` MAY be omitted, but if present it MUST end in a valid action token
- qualifiers per object MUST be `0..3`
- qualifiers within one expression MUST be unique
- certainty, if present, MUST be `%(N)` where `N` is integer `0..9`
- relation chains MUST connect valid `ACTION_EXPR` values or valid `@MID` references
- chained flow MUST NOT terminate with an operator
- unknown tokens are invalid
- malformed certainty annotations are invalid
- duplicate identical relations SHOULD be rejected or normalized

Parser behavior on invalid TCL:

- reject the expression
- do not persist it to durable memory
- optionally mark it for review in the translation / distillation layer

## 6. Valid Examples

```text
RDD(FIL)[ACT]%(8)
ANL(REP:SEC)->SUM[ACT]%(8)
PLN(TSK)->APR[PND]%(7)
STR(MEM:PRI)->WRT[REV]%(9)
ASK(UNK)[AMB]%(1)
RDD(FIL)[ACT]~@M41%(7)
ANL(REP:SEC)[ACT]^PLN(TSK)[PND]%(7)
```

## 7. Invalid Examples

Missing object:

```text
RDD()[ACT]
```

Duplicate qualifiers:

```text
ANL(REP:SEC:SEC)[ACT]
```

Missing state:

```text
ANL(REP:SEC)->SUM
```

Malformed certainty:

```text
RDD(FIL)[ACT]%(12)
```

Dangling flow operator:

```text
PLN(TSK)->[PND]
```

Dangling relation operator:

```text
RDD(FIL)[ACT]~
```

## 8. Notes

- TCL is intentionally compact.
- TCL may be lossy relative to authoritative continuity artifacts.
- The compact expression is a semantic sketch, not a full durable artifact.
- Continuity stream, distillates, and wake state remain authoritative.
- Compact syntax is valid input to validation, hashing, and signature derivation,
  but it is not sufficient by itself to authorize persistence.
- A parsed expression may still be dropped, reviewed, quarantined, or denied by
  downstream Loopgate policy.
