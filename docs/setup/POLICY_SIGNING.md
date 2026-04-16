**Last updated:** 2026-04-12

# Policy signing

Loopgate now requires a detached signature for the live policy file.

Required files:

- `core/policy/policy.yaml`
- `core/policy/policy.yaml.sig`

Startup fails closed if:

- the signature file is missing
- the signature file is malformed
- the signature does not verify against the trusted Ed25519 public key set

## Why

The old hash-acknowledgment flow only proved that `policy.yaml` matched another file in the same repo state. A same-user attacker who could edit the repo could change both files together.

Detached signatures raise that bar: the attacker now needs a trusted signing key, not just write access to the checkout.

## Trusted public keys

Loopgate trusts:

- the compiled fallback public key shipped with the binary
- any operator-installed public keys under the local trust directory

Default operator trust directory on macOS:

```text
~/Library/Application Support/Loopgate/policy-signing/trusted/
```

You can override the trust directory with:

```text
LOOPGATE_POLICY_SIGNING_TRUST_DIR
```

Public keys in that directory must be PEM-encoded Ed25519 public keys named:

```text
<key_id>.pub.pem
```

Example:

```text
~/Library/Application Support/Loopgate/policy-signing/trusted/loopgate-local-root-2026-04.pub.pem
```

## Sign a policy

Keep the private key outside the repo. A PKCS#8 PEM-encoded Ed25519 key works with the built-in signer.

The signer resolves the private key in this order:

1. `-private-key-file`
2. `LOOPGATE_POLICY_SIGNING_PRIVATE_KEY_FILE`
3. the default operator path from `os.UserConfigDir()`

On macOS, the default operator path is:

```text
~/Library/Application Support/Loopgate/policy-signing/loopgate-policy-root-2026-04.pem
```

For a fresh local signer, choose a new `key_id` and install both the private key and its public key:

```bash
KEY_ID="loopgate-local-root-2026-04"
install -d -m 700 "$HOME/Library/Application Support/Loopgate/policy-signing"
install -d -m 700 "$HOME/Library/Application Support/Loopgate/policy-signing/trusted"
openssl genpkey -algorithm Ed25519 \
  -out "$HOME/Library/Application Support/Loopgate/policy-signing/$KEY_ID.pem"
chmod 600 "$HOME/Library/Application Support/Loopgate/policy-signing/$KEY_ID.pem"
openssl pkey \
  -in "$HOME/Library/Application Support/Loopgate/policy-signing/$KEY_ID.pem" \
  -pubout \
  -out "$HOME/Library/Application Support/Loopgate/policy-signing/trusted/$KEY_ID.pub.pem"
```

If you are intentionally using the compiled fallback signer instead, keep using its matching private key under the default operator path.

Example signing flow with a custom operator key:

```bash
KEY_ID="loopgate-local-root-2026-04"
go run ./cmd/loopgate-policy-sign -key-id "$KEY_ID"
```

Or override the key location explicitly:

```bash
go run ./cmd/loopgate-policy-sign \
  -key-id "$KEY_ID" \
  -private-key-file ~/loopgate-policy-signing-key.pem
```

That writes:

```text
core/policy/policy.yaml.sig
```

Verify the active rollout inputs before restarting operators:

```bash
go run ./cmd/loopgate-policy-sign -key-id "$KEY_ID" -verify-setup
```

Before signing, you can validate or explain the policy surface locally with:

```bash
go run ./cmd/loopgate-policy-admin validate
go run ./cmd/loopgate-policy-admin explain -tool Bash
go run ./cmd/loopgate-policy-admin diff -right-policy-file ./candidate-policy.yaml
```

`diff` is intentionally a **normalized effective-policy diff**, not a raw YAML
text diff. It compares what Loopgate will enforce after strict parsing,
normalization, and defaults. Comments, key ordering, and formatting changes do
not appear there; use your normal VCS diff for literal source review.

That check fails closed unless all three line up:

- the requested trusted `key_id`
- the current `core/policy/policy.yaml.sig`
- the operator private key resolved from the active signer path

## Operational note

`cmd/loopgate` no longer supports `--accept-policy`. Policy changes must be signed before restart.

If you intentionally change `core/policy/policy.yaml`, re-run the signer before starting Loopgate again.

## Custody and rotation

- Treat the policy signing key as an operator authority key, not a runtime convenience secret.
- Keep exactly one active signer path per trusted `key_id`, outside the repo and outside `runtime/state`.
- Keep the matching public key in the operator trust directory, not in the repo checkout.
- Do not leave copies in `/tmp`, shell history, or repo-local `.env` files after installation.
- To rotate, generate a new Ed25519 keypair, install the new public key under a new `key_id` in the trust directory, re-sign `core/policy/policy.yaml`, then retire the old private key.

For the concrete restore-vs-rotate workflow, see [POLICY_SIGNING_ROTATION.md](./POLICY_SIGNING_ROTATION.md).
