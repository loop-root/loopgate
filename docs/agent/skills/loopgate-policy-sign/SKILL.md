---
name: loopgate-policy-sign
description: Use when signing Loopgate policy.yaml, verifying local policy signing setup, checking signer key custody, or explaining detached Ed25519 policy signatures.
---

# Loopgate Policy Signing

Use `loopgate-policy-sign` when the task is about the detached signature for
`core/policy/policy.yaml`.

## Guardrails

- Editing `policy.yaml` does not create authority. The policy must be signed by
  a trusted Ed25519 key before Loopgate will treat it as live policy.
- Never print, copy, move into the repo, or otherwise expose private key bytes.
- Treat `LOOPGATE_POLICY_SIGNING_PRIVATE_KEY_FILE` as sensitive configuration.
- Prefer `./bin/loopgate-policy-sign` over `go run ./cmd/loopgate-policy-sign`
  for operator workflows.
- Do not suggest `--accept-policy`; that legacy bypass is intentionally gone.

## Command choice

- Verify signer setup: `./bin/loopgate-policy-sign -verify-setup`
- Sign the repo policy with the current/default key: `./bin/loopgate-policy-sign`
- Sign with a specific key id: `./bin/loopgate-policy-sign -key-id <key-id>`
- Sign with an explicit private key path:
  `./bin/loopgate-policy-sign -key-id <key-id> -private-key-file <path>`

Use `-repo-root <dir>` when the repo is not the current working directory. Use
`-policy-file <path>` only when intentionally signing a non-default policy file.

## Recommended workflow

1. Use `loopgate-policy-admin validate` before signing.
2. Run `loopgate-policy-sign -verify-setup` to check that the current signature,
   trusted public key, and private signer key line up.
3. Sign only after the operator has intentionally accepted the policy change.
4. Run `loopgate-policy-admin validate` again after signing.
5. Use `loopgate-policy-admin apply -verify-setup` only when the running daemon
   should hot-reload the signed policy.

## Interpreting results

- `Policy signing setup OK` means the requested key id, current policy
  signature, trusted public key, and resolved private key match.
- `Wrote core/policy/policy.yaml.sig` means the detached signature file was
  written. It does not mean a running daemon has reloaded the policy.
- Key paths and file modes may be reported. Private key contents must never be
  reported.

## Failure posture

If verification fails, say the signing setup is not trusted yet. The next safe
step is to fix key custody or signature mismatch, not to bypass policy signing.
