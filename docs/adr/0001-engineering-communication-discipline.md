# ADR 0001: Engineering communication discipline

**Date:** 2026-04-01  
**Status:** accepted

## Decision

We require **why-oriented comments** at non-obvious decision points (about two sentences), maintain **timestamped sprint plans** under `sprints/`, and record durable architecture choices in **`docs/adr/`** instead of relying on chat history or blame.

## Tradeoff

This adds a small tax on every meaningful PR; the alternative is slower onboarding, riskier refactors, and agents that reverse correct decisions because context died.

## Consequences

If docs drift from code, **code wins** — update or supersede ADRs and sprint appendices when behavior changes deliberately. If commentary becomes noise, tighten the bar to “non-obvious only” rather than deleting the practice.
