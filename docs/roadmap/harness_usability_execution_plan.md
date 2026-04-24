**Last updated:** 2026-04-24

# Harness usability execution plan

This plan keeps the near-term Loopgate work focused on a clear Claude Code
operator experience while preserving a generic harness contract for Codex,
Pi, and other future integrations.

## Goal

Make Loopgate obvious in the first 10 minutes:

- what action was proposed
- why Loopgate allowed, asked, or blocked it
- who owns the approval prompt
- what root policy permits the operator to delegate
- how to update policy or operator grants safely

Claude Code remains the only supported harness in the current implementation.
Future harnesses should integrate through the same decision contract instead of
getting custom authority paths.

## Invariants

- Loopgate remains the authority for policy evaluation and hard denials.
- Harnesses own approval prompts for `ask` decisions.
- A root policy deny is final and cannot be overridden by operator policy.
- Operator grants can only narrow or pre-approve within the root policy ceiling.
- Signed policy and signed operator override documents remain the durable
  control inputs.
- Audit records must distinguish enforced decisions from harness-owned prompts.

## Phase 1: Harness Decision Contract

Add explicit decision metadata to hook responses and audit projection.

Deliverables:

- `approval_owner`, with initial value `harness` for `ask` decisions
- `operator_override_class`
- `operator_override_max_delegation`
- explicit approval option metadata for one-time, session, and permanent grant scopes
- stable reason codes for common allow, ask, and block outcomes
- tests proving Claude Code still works through the generic contract

Initial implementation status:

- hook responses include `reason_code`, `approval_owner`, and
  `approval_options`
- hook audit records include the same decision metadata
- Claude Code remains the only supported harness

Non-goals:

- no new harness implementation
- no remote admin surface
- no UI-only authority

## Phase 2: Explainability Commands

Add commands that make policy behavior inspectable without requiring a live
Claude session.

Deliverables:

- `loopgate explain --tool Grep --path .`
- `loopgate explain --tool Write --path README.md`
- `loopgate explain --tool Bash --command "grep -R Loopgate ."`
- output showing decision, root reason, operator override class, max delegation,
  active grant match, and controlling policy fields

Initial implementation status:

- `loopgate explain` evaluates signed local policy without starting a daemon,
  appending audit, or creating approval state
- supported first-slice examples include `Grep`, `Write`, and `Bash`
- output includes decision, reason code, approval owner/options, and operator
  override class/delegation metadata

Tests:

- explain output matches hook enforcement for representative read, search,
  write, bash, and denied-path cases
- denied-path examples remain hard denies even with operator grants

## Phase 3: Doctor as the Main Setup UX

Make `loopgate doctor` the fastest way to understand setup state.

Deliverables:

- daemon/socket status
- signed root policy status
- signed operator override status
- Claude hook install status
- sample policy probes for read/search/write/bash
- clear repair commands for missing or stale setup

Initial implementation status:

- `loopgate-doctor setup-check` prints human-readable setup readiness
- `loopgate-doctor setup-check --json` prints the same readiness projection plus
  machine-readable `next_steps`
- output includes daemon/socket health, signed root policy status, signed
  operator override status, Claude hook install status, sample read/search,
  write, and bash policy probes, plus repair commands
- last real hook event status remains a later refinement

Tests:

- missing hooks produce a clear remediation
- invalid policy signatures fail closed with a useful message
- offline daemon state is reported without pretending Loopgate is healthy

## Phase 4: Safe Policy and Grant Editing

Make common operator changes explicit, signed, and reversible.

Deliverables:

- `loopgate policy show`
- `loopgate policy explain <tool-or-class>`
- `loopgate-policy-admin grants add <class> -path <path> [-dry-run]`
- `loopgate-policy-admin grants revoke <grant-id> [-dry-run]`
- preview-before-write behavior for policy and operator override mutations

Initial implementation status:

- `loopgate-policy-admin grants add <class> -path <path>` supports
  permanent path-scoped grants for `repo_read_search`, `repo_edit_safe`,
  `repo_write_safe`, and `repo_bash_safe`
- permanent grants are refused unless the signed root policy gives that class
  `max_delegation: persistent`
- `-dry-run` previews the grant without writing or reloading operator overrides
- `loopgate-policy-admin grants revoke <grant-id> -dry-run` previews revocation
  without writing or reloading operator overrides
- `loopgate-policy-admin grants list -all` includes revoked grant records for
  lifecycle history
- `overrides grant`, `overrides revoke`, and `grant-edit-path` remain
  compatibility aliases; `grant-edit-path` maps to `repo_edit_safe`

Tests:

- grant creation cannot exceed root max delegation
- revoked grants do not resurrect after reload
- malformed or unsigned operator override documents fail closed

## Phase 4.5: Agent-Assisted Setup

Make Loopgate easier to install with help from another agent that is not itself
governed by Loopgate yet.

Deliverables:

- canonical agent-assisted setup contract
- copy-paste prompt for Codex, Claude, Pi, or another setup assistant
- explicit human-confirmation boundaries for signing, hook install, LaunchAgent
  install, hot apply, and permanent grants
- verification sequence that proves Claude Code is actually governed

Initial implementation status:

- `docs/setup/AGENT_ASSISTED_SETUP.md` defines the setup contract
- `docs/setup/agent_assisted_prompt.md` provides a reusable prompt
- docs index and setup docs link to the agent-assisted path

## Phase 5: Local Admin Console

Keep the console as a readable local admin surface over existing authority APIs.

Deliverables:

- health and hook status
- active policy profile and signature status
- active operator grants
- recent allows, asks, and blocks
- audit integrity status

Non-goals:

- no separate approval authority
- no remote fleet management
- no business directory/group model yet

## Later Harnesses

Codex, Pi, and other harnesses should come after the Claude path is crisp.

Candidate integration requirements:

- ability to preflight a proposed action before execution
- ability to honor `allow`, `ask`, and `block`
- ability to render harness-owned prompts using Loopgate decision metadata
- ability to preserve Loopgate audit identifiers in harness logs if available

Pi is a plausible later candidate because custom integrations can route through
Loopgate before tool execution. That should be treated as a separate integration
slice after the generic contract is stable.
