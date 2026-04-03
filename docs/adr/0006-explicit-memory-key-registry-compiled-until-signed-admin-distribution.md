**Date:** 2026-04-02  
**Status:** accepted

## Context + decision

We keep the explicit-memory key registry compiled for now because its key families, anchor semantics, and runtime hints are currently coupled in code, and we do not yet have a signed admin-distributed config path that preserves the same fail-closed guarantees.

## Tradeoff

Operators cannot add new explicit-memory families without a code change and deploy, which slows extensibility in exchange for conservative, reviewable semantics.

## Escape hatch

If operator-managed memory namespaces become necessary, migrate to an admin-distributed signed registry that version-locks canonicalization, anchoring, and deny behavior instead of allowing clients or models to define durable key families.
