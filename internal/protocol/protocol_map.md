# Protocol Package Map

This file maps `internal/protocol/`, the canonical request envelope
package for capability execution and approval decisions.

Use it when changing:

- capability request identity and validation
- request-body hashing
- approval decision request validation
- provider-native metadata stripping

## Core Role

`protocol/` defines the small canonical request shapes that Loopgate binds to
policy, approval manifests, and request signatures.

The package exists to keep provider-specific tool metadata out of authority
paths. `CapabilityRequest.Capability` is canonical; echoed provider-native tool
names are accepted only so they can be ignored and stripped from the wire.

## Key Files

- `capability.go`
  - `CapabilityRequest`
  - custom `MarshalJSON` that emits only canonical fields
  - `Validate` for safe identifiers and argument names
  - `CloneCapabilityRequest`
  - `ApprovalDecisionRequest`
  - `RequestBodySHA256`

- `capability_test.go`
  - validation and canonical JSON behavior

## Relationship Notes

- `internal/loopgate/controlapi/core.go` aliases `CapabilityRequest` for the
  local control-plane contract.
- `internal/approvalruntime/` hashes `CapabilityRequest` into approval
  manifests.
- `internal/loopgate/request_body_runtime.go` handles signed body verification
  around these payloads.
- This package must not import `internal/loopgate`.

## Important Watchouts

- Natural-language tool names, provider metadata, and echoed native fields must
  never override `Capability`.
- Keep validation strict and deterministic; invalid identifiers should fail
  before policy or execution.
- Do not add nested or arbitrary authority-bearing structures without reviewing
  request signing, approval manifests, and audit summaries together.
