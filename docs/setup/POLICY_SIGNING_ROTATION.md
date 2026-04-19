**Last updated:** 2026-04-12

# Policy signing rotation and emergency replacement

This runbook covers the two real operator workflows for Loopgate policy signing:

1. restore the **same** trusted signer key
2. rotate to a **new trust anchor**

Those are not the same operation.

## What Loopgate trusts

Loopgate trusts:

- the compiled fallback public key in `internal/config/policy_signing.go`
- any operator-installed public keys in the local trust directory

Default operator trust directory on macOS:

```text
~/Library/Application Support/Loopgate/policy-signing/trusted/
```

Implication:

- if you still control the matching private key, you can keep signing policy with no binary change
- if you want a new signer, you can install a new public key under a new `key_id` in the trust directory without editing source
- if the compiled fallback key itself must change, that still requires a binary change

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
./bin/loopgate-policy-sign
```

4. Verify Loopgate accepts the signed policy:

```bash
go test ./cmd/loopgate ./internal/config -count=1
```

Do **not** generate a fresh key for this path unless you also replace the matching public key in the operator trust directory for the same `key_id`.

## B. Planned trust-anchor rotation

Use this path when you intentionally want a new policy-signing root, or when the current private key is no longer acceptable.

### 1. Generate the new signer

Generate a new Ed25519 private key outside the repo:

```bash
openssl genpkey -algorithm Ed25519 -out /tmp/loopgate-policy-root-next.pem
chmod 600 /tmp/loopgate-policy-root-next.pem
```

Choose a new `key_id` that is explicit and monotonic, for example `loopgate-policy-root-2026-05`.

Derive the public key PEM for the operator trust directory:

```bash
openssl pkey -in /tmp/loopgate-policy-root-next.pem -pubout
```

### 2. Install the new public key in the operator trust directory

Install the public key under that `key_id`:

```bash
install -d -m 700 "$HOME/Library/Application Support/Loopgate/policy-signing/trusted"
openssl pkey \
  -in /tmp/loopgate-policy-root-next.pem \
  -pubout \
  -out "$HOME/Library/Application Support/Loopgate/policy-signing/trusted/loopgate-policy-root-2026-05.pub.pem"
```

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
  ./bin/loopgate-policy-sign -key-id loopgate-policy-root-2026-05
```

This must rewrite `core/policy/policy.yaml.sig` with the new `key_id`.

### 5. Validate before rollout

Run the smallest relevant verification set before restarting operators:

```bash
go test ./cmd/loopgate ./cmd/loopgate-policy-sign ./internal/config -count=1
```

Then start Loopgate normally:

```bash
./bin/loopgate
```

For keychain-backed flows, prefer the stable `./bin/...` binaries over
`go run`; a fresh `go run` build changes the executable identity and can cause
repeated macOS approval prompts.

### 6. Retire the old signer

After the new public key and new signature are live everywhere that matters:

- remove the old private key from operator custody
- revoke any backup handling path for the old key
- keep an archival record of which `key_id` signed which rollout

## C. Emergency signer compromise

If the current policy-signing private key is suspected exposed:

1. Stop treating the current signer as authoritative for new policy changes.
2. Preserve the currently deployed `policy.yaml` and `policy.yaml.sig` as evidence; do not rewrite them with the compromised key.
3. Generate a new Ed25519 keypair.
4. Install a replacement public key under a new `key_id` in the operator trust directory.
5. Re-sign `core/policy/policy.yaml` with the new signer.
6. Roll out the updated signed policy and keep operator trust stores consistent everywhere that matters.
7. Remove the compromised private key from every operator path and backup location you can control.

Important:

- A compromised signer is **not** fixed by moving the same file to a new path.
- A compromised signer is **not** fixed by generating a new local private key unless the trusted public key for the intended `key_id` changes too.

## D. Rollback discipline

Treat these as a matched set:

- Loopgate binary
- `core/policy/policy.yaml`
- `core/policy/policy.yaml.sig`

Rollback rules:

- If you rely on the compiled fallback trust anchor and roll back the binary, restore the matching signed policy file too.
- Do not mix a new binary with an old signature unless that old `key_id` is still trusted by that binary.
- Do not reuse a `key_id` across different public keys.

## E. Operator hygiene

- Keep signer keys out of the repo, `runtime/state`, and shell startup files.
- Do not leave signer keys in `/tmp` after installation.
- Prefer one active operator path per trusted `key_id`.
- Record signer rotation events in operator change logs or deployment notes alongside the policy change that required them.
