---
name: loopgate-policy-admin
description: Use when validating, explaining, diffing, applying, or administering Loopgate policy, approvals, and signed operator grants with loopgate-policy-admin.
---

# Loopgate Policy Admin

Use `loopgate-policy-admin` for policy inspection and bounded operator
administration. Some subcommands are read-only; others mutate live authority.

## Guardrails

- Natural language never grants authority.
- Validate and explain policy before signing or applying it.
- `diff` is a normalized effective-policy diff, not a literal YAML source diff.
- `apply`, approval decisions, and grant mutations require a running local
  Loopgate daemon over the Unix socket.
- Permanent operator grants must be allowed by the parent signed policy and must
  be signed before they can affect behavior.
- Use `-dry-run` before adding or revoking grants unless the operator already
  gave explicit approval.
- Do not include secrets in approval or grant reason text; reason text is
  recorded in audit.

## Command choice

Read-only policy inspection:

- Validate active signed policy: `./bin/loopgate-policy-admin validate`
- Explain all supported Claude tools: `./bin/loopgate-policy-admin explain`
- Explain one tool: `./bin/loopgate-policy-admin explain -tool Bash`
- Compare a candidate policy:
  `./bin/loopgate-policy-admin diff -right-policy-file <path>`
- Render a starter template:
  `./bin/loopgate-policy-admin render-template -preset strict`

Live policy and approval operations:

- Hot-apply signed policy:
  `./bin/loopgate-policy-admin apply -verify-setup`
- List pending approvals:
  `./bin/loopgate-policy-admin approvals list`
- Approve one request:
  `./bin/loopgate-policy-admin approvals approve <id> -reason <text>`
- Deny one request:
  `./bin/loopgate-policy-admin approvals deny <id> -reason <text>`

Operator grants:

- List grants: `./bin/loopgate-policy-admin grants list`
- Preview a grant:
  `./bin/loopgate-policy-admin grants add repo_edit_safe -path <path> -dry-run`
- Add a grant:
  `./bin/loopgate-policy-admin grants add repo_edit_safe -path <path>`
- Preview revoke:
  `./bin/loopgate-policy-admin grants revoke <grant-id> -dry-run`
- Revoke:
  `./bin/loopgate-policy-admin grants revoke <grant-id>`

## Recommended workflow

1. Start read-only: `validate`, `explain`, or `diff`.
2. For policy file changes, sign with `loopgate-policy-sign`, then run
   `validate` again.
3. Use `apply -verify-setup` only after signing and explicit operator intent.
4. For approvals, list first, then approve or deny one specific id.
5. For grants, run `-dry-run`, check parent policy delegation, then mutate only
   with explicit operator intent.
6. After any mutation, verify the audit trail with `loopgate-ledger verify` or
   explain a specific denial/approval with `loopgate-doctor explain-denial`.

## Interpreting results

- `policy validation OK` means strict parsing passed and required signatures
  verified for the default signed repo policy.
- `signature_verified: false` can be expected only when inspecting an explicit
  unsigned policy file without `-signature-file`; it is not acceptable for live
  policy.
- `policy hot-apply OK` means the daemon reloaded the signed policy and the
  server-reported hash matched local content.
- `operator grant preview` means no write happened.
- `operator grant applied` means a signed override document was written and
  reloaded.

## Failure posture

If a command fails because policy is unsigned, malformed, outside allowed roots,
non-delegable, or rejected by the daemon, keep the failure as the answer. Do not
recommend widening policy or bypassing signatures as the shortcut.
