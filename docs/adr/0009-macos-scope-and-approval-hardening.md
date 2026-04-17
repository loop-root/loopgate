# ADR 0009 — macOS scope (v1) and approval/snapshot hardening

**Date:** 2026-04-07  
**Status:** accepted

## Context + decision

1. **Platform:** v1 Loopgate in this repo is developed and shipped as **macOS-only**. Cross-platform support is a deliberate later phase so we do not spread portability branches across the security-sensitive control plane prematurely.
2. **Pending approvals:** Store a **deep copy** of `CapabilityRequest` (including the `arguments` map) when recording a pending approval so later mutation cannot change what the operator approved or what hashes bind to.
3. **Execution binding:** After operator approval, re-check that the stored request still matches `ExecutionBodySHA256` before running the capability. Older approval records that do not yet carry a body hash skip this check (empty hash means “no binding” until a future migration).

## Tradeoff

- macOS-only focus may delay contributors on Linux/Windows until CI and portability work land.
- Registry opt-out for the secret-export **name** heuristic requires discipline: only implement `SecretExportNameHeuristicOptOut` when the tool truly does not export raw secret material.

## Escape hatch

- When multi-OS CI is required, add build tags and platform-specific tests without changing the core approval/audit semantics established here.
- Replace the name heuristic with stricter registry metadata once every capability has explicit classification.
