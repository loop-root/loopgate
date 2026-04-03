**Date:** 2026-04-03  
**Status:** accepted

## Context + decision

We keep preference anchor derivation narrow and explicit in the current implementation because model-emitted TCL candidates are not yet the authoritative normalization path for all preference writes.

## Tradeoff

The tradeoff is lower recall for free-form preference language, but it keeps contradiction slots conservative and prevents phrase heuristics from silently broadening supersession semantics.

## Escape hatch

When TCL candidate generation becomes the primary path, these phrase-level rules should be reduced to secondary fallback or removed; any widening before then must happen through explicit tested facet rules, not heuristic free-text anchoring.
