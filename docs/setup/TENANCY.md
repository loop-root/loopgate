# Tenancy (future / archived direction)

**Last updated:** 2026-04-13

This document is **not part of the current Loopgate product story**.

Current Loopgate scope is:
- local-first
- single-user / local operator
- one local governance layer
- one local audit authority

The repository still contains some tenancy-related fields and compatibility work, but **multi-tenant deployment is not a near-term supported product shape**.

## Current practical guidance

For the current product:
- leave tenancy-style deployment identity fields empty
- do not plan around tenant partitioning
- do not treat `tenant_id` / `user_id` behavior in the repo as a supported enterprise boundary

If you are operating Loopgate locally today, this document can be ignored.

## Why this file still exists

The codebase still contains partial groundwork and compatibility traces for:
- deployment identity tagging
- tenant-like audit annotations
- partition-style memory layout ideas

Those traces are cleanup debt, not the active product center.

## Cleanup intent

This topic should eventually move to one of two places:
- archived design material
- a future enterprise-specific design track, if that product ever becomes real

Until then, keep the active setup/operator story centered on:
- local signed policy
- local approvals
- local audit
- local governed tool execution

Related cleanup tracking:
- [Loopgate cleanup plan](../roadmap/loopgate_cleanup_plan.md)
