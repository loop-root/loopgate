# Control Runtime Map

`internal/controlruntime` owns extracted control-plane runtime primitives that
do not require HTTP handlers, audit append wiring, or the `loopgate.Server`
type.

Dependency direction is one-way:

- `internal/loopgate` may import `internal/controlruntime`
- `internal/controlruntime` must not import `internal/loopgate`
- wire response shaping remains in `internal/loopgate/controlapi` and adapter
  code until that boundary is explicitly moved

## Current Files

- `session_mac.go`
  - 12-hour session MAC epoch math
  - per-epoch key derivation from the server-held rotation master
  - per-control-session derived MAC key generation
  - previous/current/next candidate key selection for rotation tolerance
  - private no-follow rotation-master file load/create behavior
- `session_mac_test.go`
  - epoch alignment coverage
  - deterministic key derivation coverage
  - response-model coverage that proves epoch key material is not exposed
  - private master-file permissions and symlink rejection coverage
- `nonce_replay.go`
  - snapshot and append-only JSONL nonce replay persistence
  - legacy snapshot fallback when the append-only log has not been created yet
  - truncated-tail tolerance for crash recovery
  - deterministic compaction of live nonce replay entries
  - private state-file writes for replay persistence
- `nonce_replay_test.go`
  - snapshot round-trip and pruning coverage
  - append-only log round-trip coverage
  - truncated-tail and malformed-middle-record coverage
  - legacy snapshot fallback coverage
  - compaction coverage

## Invariants

- The rotation master never leaves process memory except as the private
  runtime state file.
- The master file is created with `0600` permissions and opened with
  no-follow semantics.
- Clients receive only derived per-session MAC keys, never epoch key material
  or the rotation master.
- Candidate signing keys are derived from previous/current/next epochs only.
- Nonce replay storage preserves fail-closed caller semantics by returning
  persistence errors instead of hiding them.
- The append-only nonce replay log may ignore a truncated final line, but
  malformed middle records remain hard failures.
- Replay storage owns persistence only; Loopgate still owns the authoritative
  duplicate/saturation decisions under `server.mu`.

## Adapter Boundary

`internal/loopgate/session_mac_rotation.go` remains a thin adapter that:

- stores the loaded master on `Server`
- maps `controlruntime.SessionMACKeys` into `controlapi.SessionMACKeysResponse`
- keeps signed-request verification tied to Loopgate's existing request-auth
  denial behavior

`internal/loopgate/control_plane_state.go` remains the adapter for nonce replay
decisions:

- checks duplicates and capacity under `server.mu`
- rolls back in-memory nonce state if persistence fails
- maps persistence failure to the existing fail-closed control-plane denial
