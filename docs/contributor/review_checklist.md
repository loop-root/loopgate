**Last updated:** 2026-04-20

# Review Checklist

## Change-management questions

Before proposing or making a change, evaluate:

1. What invariant could this weaken?
2. Does it expand trust, permissions, reachable files, executable paths,
   transport exposure, or mutation surface?
3. Does it introduce shared state, races, hidden background work, or non-atomic
   transitions?
4. Will failures and denials still be visible and explainable?
5. What docs must change to keep the repo honest?
6. What tests are required?

If the change is hard to reason about, prefer the smaller change.

## Testing requirements

Any security boundary or invariant change should include tests.

Must-test areas:

- policy allow and deny behavior
- path normalization and symlink behavior
- append-only ledger behavior
- audit integrity behavior where present
- atomic write or crash-safe behavior where relevant
- malformed input handling
- concurrency-sensitive state transitions
- denial paths, not just success paths
- secret redaction and non-leakage paths
- UI projection paths that must not leak internal-only details

Testing philosophy:

- prove unsafe behavior is denied
- prove allowed behavior still works
- prove edge cases do not silently degrade into permissive behavior

When fixing a bug, add the regression test for that exact boundary.

## Code-review self-check

Before finalizing a change, ask:

- Does this weaken any boundary?
- Does this introduce fail-open behavior?
- Am I treating model output as trusted when I should not?
- Did I rename things so trust and state are clearer?
- Could this race?
- Could this create an ambiguous audit trail?
- Are denials explicit and test-covered?
- Did I preserve append-only semantics and audit integrity?
- Did I update docs where the operator or architecture contract changed?

## Output expectations for meaningful changes

When summarizing a real change, be explicit about:

1. what changed
2. why it changed
3. which invariant it touches
4. security implications
5. concurrency implications
6. documentation updated
7. tests added or updated
