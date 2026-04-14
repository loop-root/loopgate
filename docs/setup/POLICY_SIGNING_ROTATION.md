**Last updated:** 2026-04-12

# Policy signing rotation and emergency replacement

This runbook covers the two real operator workflows for Loopgate policy signing:

1. restore the **same** trusted signer key
2. rotate to a **new trust anchor**

Those are not the same operation.

## What is fixed by the binary

Loopgate does **not** trust arbitrary local private keys. It trusts the Ed25519 public key embedded in `internal/config/policy_signing.go`.

Today that trust anchor is:

- `key_id`: `loopgate-policy-root-2026-04`
- constant: `PolicySigningTrustAnchorKeyID`

Implication:

- If you still control the matching private key, you can keep signing policy with no binary change.
- If that private key is lost or suspected compromised, you do **not** have an in-place key swap. You must rotate the trust anchor and ship a new Loopgate binary that trusts the new public key.

## A. Restore the same trusted signer

Use this path only when the current signing key is still trusted and you are restoring the exact same private key from a secure backup.

Preferred operator path on macOS:

```text
~/Library/Application Support/Loopgate/policy-signing/loopgate-policy-root-2026-04.pem
```

Restore steps:

1. Recover the exact PKCS#8 PEM Ed25519 private key from your secure backup.
2. Install it with locked-down permissions:

```bash
install -d -m 700 "$HOME/Library/Application Support/Loopgate/policy-signing"
install -m 600 /path/from/secure-backup/loopgate-policy-root-2026-04.pem \
  "$HOME/Library/Application Support/Loopgate/policy-signing/loopgate-policy-root-2026-04.pem"
```

3. Re-sign the policy:

```bash
go run ./cmd/loopgate-policy-sign
```

4. Verify Loopgate accepts the signed policy:

```bash
go test ./cmd/loopgate ./internal/config -count=1
```

Do **not** generate a fresh key for this path. A new private key will not match the public key embedded in the current binary.

## B. Planned trust-anchor rotation

Use this path when you intentionally want a new policy-signing root, or when the current private key is no longer acceptable.

### 1. Generate the new signer

Generate a new Ed25519 private key outside the repo:

```bash
openssl genpkey -algorithm Ed25519 -out /tmp/loopgate-policy-root-next.pem
chmod 600 /tmp/loopgate-policy-root-next.pem
```

Derive the public key in PKIX DER base64 for the embedded trust anchor:

```bash
openssl pkey -in /tmp/loopgate-policy-root-next.pem -pubout -outform DER | base64
```

Choose a new `key_id` that is explicit and monotonic, for example `loopgate-policy-root-2026-05`.

### 2. Update the trusted public key in code

Edit `internal/config/policy_signing.go`:

- replace `PolicySigningTrustAnchorKeyID`
- replace `policySigningTrustAnchorDERBase64`

Do not reuse the old `key_id` for a different public key. That would blur audit meaning and rollback handling.

### 3. Move the new private key into operator custody

Install the new signer key under the standard operator path for its new `key_id`:

```bash
install -d -m 700 "$HOME/Library/Application Support/Loopgate/policy-signing"
install -m 600 /tmp/loopgate-policy-root-next.pem \
  "$HOME/Library/Application Support/Loopgate/policy-signing/loopgate-policy-root-2026-05.pem"
rm /tmp/loopgate-policy-root-next.pem
```

### 4. Re-sign the live policy with the new signer

Use either the default path or an explicit override:

```bash
LOOPGATE_POLICY_SIGNING_PRIVATE_KEY_FILE="$HOME/Library/Application Support/Loopgate/policy-signing/loopgate-policy-root-2026-05.pem" \
  go run ./cmd/loopgate-policy-sign -key-id loopgate-policy-root-2026-05
```

This must rewrite `core/policy/policy.yaml.sig` with the new `key_id`.

### 5. Validate before rollout

Run the smallest relevant verification set before restarting operators:

```bash
go test ./cmd/loopgate ./cmd/loopgate-policy-sign ./internal/config -count=1
```

Then start Loopgate normally:

```bash
go run ./cmd/loopgate
```

### 6. Retire the old signer

After the new binary and new signature are live everywhere that matters:

- remove the old private key from operator custody
- revoke any backup handling path for the old key
- keep an archival record of which `key_id` signed which rollout

## C. Emergency signer compromise

If the current policy-signing private key is suspected exposed:

1. Stop treating the current signer as authoritative for new policy changes.
2. Preserve the currently deployed `policy.yaml` and `policy.yaml.sig` as evidence; do not rewrite them with the compromised key.
3. Generate a new Ed25519 keypair.
4. Rotate the embedded trust anchor in `internal/config/policy_signing.go`.
5. Re-sign `core/policy/policy.yaml` with the new signer.
6. Roll out the new Loopgate binary and the new signature together.
7. Remove the compromised private key from every operator path and backup location you can control.

Important:

- A compromised signer is **not** fixed by moving the same file to a new path.
- A compromised signer is **not** fixed by generating a new local private key unless the binary trust anchor changes too.

## D. Rollback discipline

Treat these as a matched set:

- Loopgate binary
- `core/policy/policy.yaml`
- `core/policy/policy.yaml.sig`

Rollback rules:

- If you roll back the binary to an older embedded trust anchor, restore the matching signed policy file too.
- Do not mix a new binary with an old signature unless that old `key_id` is still trusted by that binary.
- Do not reuse a `key_id` across different public keys.

## E. Operator hygiene

- Keep signer keys out of the repo, `runtime/state`, and shell startup files.
- Do not leave signer keys in `/tmp` after installation.
- Prefer one active operator path per trusted `key_id`.
- Record signer rotation events in operator change logs or deployment notes alongside the policy change that required them.
