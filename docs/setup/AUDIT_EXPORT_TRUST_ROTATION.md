**Last updated:** 2026-04-13

# Audit Export Trust Rotation (optional / future-facing)

This document covers the optional downstream audit export path that currently exists in the codebase.

It is **not** the center of the current Loopgate product story.

Current Loopgate product scope is still:
- local-first
- single-user / local operator
- local authoritative audit ledger
- local governance over AI-assisted work

Use this document only if you are intentionally testing or maintaining the current optional remote export path.

## Current reality

Downstream export support exists, including:
- read-only trust preflight
- request-driven flush
- bearer-auth + mTLS hardening for the current remote sink path

But this is still secondary to the core product.

If you are trying to understand or operate Loopgate locally, start with:
- [Operator guide](./OPERATOR_GUIDE.md)
- [Setup](./SETUP.md)
- [Ledger and audit integrity](./LEDGER_AND_AUDIT_INTEGRITY.md)

## What this document is for

If you are explicitly working on the optional downstream export path, this file is where trust-rotation procedures belong.

That includes:
- client certificate rotation
- root CA rotation
- optional pinned server public-key updates
- checking the trust preflight and diagnostic projections

## Cleanup note

This document remains in the tree because the implementation exists and still needs an honest maintenance note.

Longer term, it should either:
- move under a clearly labeled optional/advanced section
- or be archived with other non-core surfaces if downstream export is removed from the near-term product

Related operator docs:
- [Operator guide](./OPERATOR_GUIDE.md)
- [Doctor and ledger tools](./DOCTOR_AND_LEDGER.md)
