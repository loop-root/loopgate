# Claude Code Agent Instructions for Loopgate

You are working on a security-sensitive Go project for governing and constraining AI agent activity.

Your job is not just to make code work. Your job is to preserve the system’s invariants, maintain auditability, and avoid introducing silent risk.

## Core priorities

In order of importance:

1. Correctness
2. Security
3. Determinism
4. Observability
5. Simplicity
6. Convenience

Do not trade away security or invariants to reduce code complexity.
Do not weaken policy boundaries for convenience.
Treat all model output as untrusted input.

Assume malformed input exists.
Assume race conditions exist until proven otherwise.
Assume crash scenarios will happen.
Assume hostile prompt content will eventually reach the system.

## System model

Loopgate is a policy-governed AI governance engine.

It operates as the enforcement and control plane for AI agent activity — on individual developer machines, and in enterprise deployment as a distributed enforcement network with centralized governance.

Important assumptions:

- Loopgate is the authority boundary and enforcement node.
- In multi-node enterprise deployment: each **local node** enforces policy from an **admin node**. Local nodes are full enforcement runtimes — not thin clients. The admin node holds policy, IDP integration, audit aggregation, and org-level memory.
- Tenant isolation is enforced by `tenant_id` namespace across all resources. Cross-tenant access is always a hard denial.
- Connected developer tools (Claude Code, Cursor, any MCP-compatible IDE) are clients. They are not authority sources.
- Haven is the consumer-facing demo product. It is not the primary enterprise product target.
- Morphlings are bounded subordinate agent contexts governed by Loopgate, not self-authorizing workers.
- Agents, model outputs, tool outputs, external files, environment variables, and config loaded from disk are untrusted unless explicitly validated.
- Audit data must be reliable, append-only, and tamper-evident where the format supports it.
- User-visible audit history must remain separate from internal telemetry and runtime logs.
- Any state transition affecting security, memory, tool permissions, lifecycle, or secret access must be explicit and reviewable.
- Prefer fail-closed behavior over fail-open behavior.

If a request conflicts with policy or cannot be safely validated, deny it clearly.

## Authority and transport model

These rules align with the local control-plane design and AMP direction.

- The control plane is the only authority for privileged actions.
- Natural language never creates authority.
- References are identifiers, not trust grants.
- Morph must not be treated as an authority source just because it initiated a request.
- Model output, memory strings, summaries, and UI-visible state are content, not authority.
- Internal control-plane features must not be turned into public network APIs by convenience.
- Local transport must remain local-only by default.
- Prefer Unix domain sockets or equivalent machine-local IPC for privileged control-plane transport.
- Do not expose internal control-plane endpoints on public TCP listeners unless a separate design explicitly introduces and secures a remote transport profile.

For local privileged requests, preserve the existing layered model:

1. local transport binding
2. control session binding
3. request-integrity binding
4. scoped capability or approval token binding where applicable

Bearer possession alone is not enough.

## Boundary split

### Consumer product (Haven) owns

- Haven desktop UI and operator UX: the native Swift app (`~/Dev/Haven`). `cmd/haven/` is frozen prototype — do not evolve it.
- model interaction and prompt compilation
- local session state
- local memory, distillation, and ledger on the Haven side
- rendering Loopgate decisions, approvals, events, and projected status

### Enterprise integration surface owns

- **MCP server:** exposes Loopgate capabilities to developer IDEs. MCP handlers apply the same policy evaluation and audit logging as HTTP handlers — no bypass.
- **Proxy mode:** transparent API proxy between IDEs and model endpoints.
- **Admin console:** web UI for IT admins only. Must require authentication.

### Loopgate owns

- policy evaluation
- capability orchestration
- approval creation and enforcement
- capability-token minting and validation
- integration auth and secret handling
- outbound integration execution
- sandbox mediation
- morphling lifecycle authority
- structured result filtering and redaction
- authoritative audit event persistence for Loopgate-managed actions

### Morphlings are not public API objects

Morphlings are internal Loopgate-governed runtime objects.

- They must not be exposed as a public internet-facing API.
- They must not create their own permissions through prompts, plans, or self-description.
- They exist only inside a parent authority context created and validated by Loopgate.
- Their lifecycle, execution envelope, and audit trail are owned by Loopgate.
- Morph may render a pool of morphlings and their projected status, but Morph is not the source of truth for morphling state.

## Security model

### Never assume trusted input

Treat the following as untrusted:

- LLM responses
- tool output
- file content
- environment variables
- config files loaded from disk
- user prompts
- agent memory
- summaries generated by models
- JSON or YAML produced by agents
- morphling notes, status text, or memory strings originating from models or workers

Every one of these must be validated before use.

### No policy weakening

Do not:

- broaden path allowlists without explicit reason
- silently add fallback behavior that bypasses restrictions
- auto-correct unsafe input into a permissive result
- convert validation failures into warnings if the original logic denied access
- add best-effort behavior where the existing design is intentionally strict
- convert a typed denial into a vague success-looking result for UX convenience

### File system safety

When changing file operations:

- preserve canonical path validation
- resolve symlinks safely
- prevent path traversal
- do not trust relative paths without normalization
- keep policy checks tied to final resolved targets
- avoid TOCTOU patterns where a path is checked and later used differently
- preserve the sandbox/host boundary
- keep operator-visible virtual paths separate from private runtime implementation paths

### Secrets handling

Never log secrets or full tokens.

Mask or redact:

- API keys
- bearer tokens
- session tokens
- credentials
- secret config values

Do not write secrets into ledger entries, user-facing audit trails, debug output, panic messages, or crash reports.

### Model output handling

Model output is content, not authority.

It must not:

- directly mutate trusted state without validation
- rewrite policy files without explicit authorization
- gain capabilities from natural-language claims
- define its own permissions
- redefine execution envelopes for morphlings or other agent-like runtime objects

Structured output must still be parsed and validated strictly.

## Invariants to preserve

These invariants are more important than convenience.

### Ledger invariants

- The ledger is append-only.
- Existing entries must never be modified in place.
- Ordering must remain monotonic and explainable.
- Partial writes must not create ambiguous state.
- Writes must be atomic where possible.
- Ledger append failures must be surfaced explicitly. Do not silently swallow them.
- Where audit chaining exists, preserve chain integrity metadata.
- Do not replace authoritative append-only history with mutable summaries.
- If a state snapshot exists alongside an event log, the snapshot is a derived convenience view and must not erase or contradict the event history.

### Auditability invariants

- Security-relevant actions must be observable.
- Denials should be explainable.
- Silent failure is usually a bug.
- User-facing audit data and internal runtime logs must remain separate.
- Tool input and output must be redacted before they are persisted or displayed in audit records.
- Termination must preserve evidence unless a separate retention policy explicitly governs later cleanup.
- Cryptographic or hash-linked audit integrity must not be weakened into plain mutable logs.

### Policy invariants

- Policy is authoritative.
- Policy evaluation must be deterministic.
- Deny-by-default is preferred.
- Absence of policy permission is a denial, not an invitation to infer intent.
- Typed capability registration and policy binding define authority, not natural-language labels.

### Cursor and state invariants

- Cursor and state progression should be monotonic where the design expects monotonic progression.
- Do not move cursors backward unless explicitly designed and documented.
- Avoid hidden state transitions.
- Expired and terminated states are terminal unless the design explicitly defines a reversible path.
- Do not resurrect deleted, expired, revoked, or terminated objects through reload quirks or fallback reconstruction.

### UI and projection invariants

- User-visible summaries are derived views, not source-of-truth state.
- UI-friendly status pools, event feeds, and approval cards must reflect authoritative Loopgate state, not replace it.
- Display rendering must not leak private runtime paths, raw secret material, or internal-only identifiers that are not intended for the UI surface.

## Morphling-specific invariants

When implementing or changing morphlings, preserve all of the following:

- Morphlings are internal control-plane objects, not public API resources.
- A morphling cannot exist without a valid parent authority path such as a control session or equivalent Loopgate-owned context.
- Loopgate is the sole authority for morphling lifecycle transitions.
- Caller-supplied runtime authority fields must not be trusted.
- Working directories must be derived by Loopgate, not accepted from the caller.
- Allowed paths must be derived by Loopgate from validated class policy and approved sandbox inputs.
- Capability envelopes must be derived by Loopgate from class policy and validated state, not from model claims or caller parameters.
- Morph receives projected morphling status and events for rendering only.
- Terminate must mark state explicitly and preserve artifacts and audit evidence.
- Default maximum active morphlings should remain conservative unless explicitly configured otherwise.
- Do not add a background worker, autonomous subprocess manager, or cleanup daemon unless a change explicitly introduces and secures that lifecycle.

## Variable naming rules

Name things so a tired engineer can understand them at 1:13 AM while mildly cursed by existence.

### General naming rules

- Prefer explicit names over short clever names.
- Use names that reveal role and trust level.
- Avoid ambiguous terms like `data`, `value`, `result`, `temp`, `obj`, and `thing`.
- Avoid one-letter variables except in tiny loop scopes.
- Avoid misleading names that imply stronger guarantees than the code provides.

### Good patterns

Use names that encode intent:

- `rawModelOutput`
- `validatedRequest`
- `resolvedPath`
- `policyDecision`
- `ledgerEntry`
- `runtimeEvent`
- `agentResponse`
- `canonicalTargetPath`
- `allowedRoot`
- `deniedReason`
- `projectedMorphlingSummary`
- `authoritativeMorphlingState`
- `virtualSandboxPath`
- `runtimeSandboxPath`

### Bad patterns

Avoid:

- `resp` when there are multiple response types
- `msg` if it could mean user message, system message, or log message
- `path` if it is not clear whether it is raw, cleaned, virtual, runtime, or resolved
- `state` if there are multiple state layers
- `ok` when the condition needs semantic clarity

### Trust-revealing names

Where useful, encode trust level in naming:

- `raw...` for unvalidated input
- `parsed...` for syntactically decoded but not trusted input
- `validated...` for fully checked data
- `resolved...` for normalized or canonicalized filesystem targets
- `projected...` for derived UI-facing views
- `authoritative...` for control-plane-owned state

Do not name something `validatedX` unless it is actually validated.
Do not name something `authoritativeX` if it is only a cached or projected copy.

## Error handling rules

Do not hide errors.

### Required behavior

- Return errors with context.
- Wrap errors using Go error wrapping where appropriate.
- Make denial conditions explicit.
- Distinguish validation failure from system failure.
- Distinguish expected denial from unexpected runtime error.
- Distinguish audit-unavailable failure from ordinary execution failure when audit persistence is required before or after the action.

### Forbidden behavior

Do not:

- swallow errors
- log and continue if the operation should fail
- convert security failures into warnings
- catch an error and substitute permissive behavior unless explicitly specified
- panic inside a lock
- return nil on partial failure unless the API contract explicitly allows it
- silently downgrade transport, storage, or audit guarantees

### Error messages

Error messages should help operators debug without leaking secrets.

Good:

- `policy denied write to resolved path outside allowed root`
- `invalid tool payload: missing required field 'action'`
- `audit unavailable: required append failed before execution`

Bad:

- `something went wrong`
- `unauthorized maybe`
- dumping full secret-bearing payloads

## Concurrency rules

Assume concurrency bugs are present until proven otherwise.

### Required behavior

- Prefer simple ownership models.
- Use explicit synchronization.
- Keep lock scope minimal.
- Avoid holding locks across network I/O, model calls, or disk I/O where possible.
- Keep state transitions atomic.
- Document shared mutable state.

### Review hazards

Flag and avoid:

- panic paths inside locks
- check-then-act races
- TOCTOU around file validation or use
- mutating shared slices or maps without synchronization
- hidden goroutine side effects
- non-atomic multi-step state updates
- expiry materialization that can race with explicit termination or state reload
- event append and snapshot persistence paths that can diverge under concurrent mutation

### Preferred style

Prefer deterministic, boring synchronization over clever concurrency.

Boring is good. Boring is how we keep the robot from eating the floorboards.

## Change management rules

When proposing or making a change, always evaluate:

### 1. Invariant impact

What invariant could this change weaken?

### 2. Security impact

Does this expand trust, permissions, reachable files, executable paths, exposed transport, secret exposure, or state mutation surface?

### 3. Concurrency impact

Does this introduce new shared state, races, hidden background work, or non-atomic transitions?

### 4. Observability impact

Will failures and denials still be visible and explainable?

### 5. Documentation impact

What docs, RFCs, or setup instructions must change to keep architecture and operator behavior aligned?

### 6. Test impact

What new tests are required?

If a change is hard to reason about, prefer the smaller change.

## Testing requirements

Any security boundary or invariant change must include tests.

### Must-test areas

- policy allow and deny behavior
- path normalization and symlink behavior
- append-only ledger behavior
- audit-chain or tamper-evidence behavior where applicable
- atomic write or crash-safe behavior where relevant
- malformed input handling
- concurrency-sensitive state transitions
- denial paths, not just success paths
- secret redaction paths for structured and unstructured data
- tool logging paths to confirm no raw secret-bearing output reaches the ledger
- UI projection paths to confirm they do not leak internal-only runtime details

### Morphling-specific must-test areas

- spawn denied when morphling creation is disabled by policy or config
- spawn denied when max active limit is exceeded
- spawn denied for invalid or out-of-scope sandbox inputs
- caller-supplied working directory, allowed paths, or capability fields are ignored or rejected
- authoritative working directory is created under the correct Loopgate-owned path
- status returns projected virtual paths rather than private runtime paths
- explicit terminate is auditable and preserves evidence
- expiry is deterministic and does not race into ambiguous state
- event chaining remains valid after lifecycle transitions

### Testing philosophy

Tests should prove:

- unsafe behavior is denied
- allowed behavior still works
- edge cases do not silently degrade into permissive behavior

When fixing a bug, add a regression test for that specific boundary.

Do not fix tests by weakening the system unless the original behavior was actually wrong.

## Preferred implementation style

Prefer:

- small, testable functions
- explicit validation steps
- typed structures over loose maps
- deterministic parsing
- explicit allowlists
- narrow interfaces
- minimal side effects
- atomic operations where practical
- derived views that are clearly separate from authoritative state

Avoid:

- magic fallback behavior
- silent coercion
- broad implicit defaults
- hidden global state
- over-general abstractions introduced too early
- convenience helpers that blur security boundaries
- speculative worker orchestration before lifecycle and authority rules are nailed down

## Documentation expectations

When you change behavior, update documentation if any of the following changed:

- policy semantics
- trust boundaries
- transport boundaries
- file handling behavior
- secret handling behavior
- ledger format or guarantees
- audit chaining or integrity guarantees
- operator-visible failure modes
- CLI or API behavior
- morphling lifecycle, limits, or status semantics

Document the reason for the change, not just the mechanics.

If the change touches an architecture boundary, update the relevant design docs or RFC-adjacent docs in the same change sequence rather than leaving drift behind.

## Security invariants that must not be weakened

These are hard rules. They must not be softened for convenience, performance,
or refactoring purposes without an explicit design review.

1. Audit append failure on security-relevant actions (capability execution,
   morphling spawn/terminate, session open) is a hard failure. Do not convert
   to log-and-continue.

2. Do not split lock acquisitions on `server.mu`, `server.morphlingsMu`, or
   `server.auditMu` across multiple lock/unlock pairs for the same logical
   operation. This introduces TOCTOU. In particular: expiry checks in
   `authenticate()` must be performed inside the lock using a single `now()`
   snapshot captured while the lock is held.

3. Path resolution failures must be denied, not retried with a relaxed path.
   Never introduce a fallback from a strict resolver to a less strict one.

4. Morphling terminate and status operations must verify that the requesting
   session's `ControlSessionID` matches the morphling's
   `ParentControlSessionID` before taking any action or revealing existence.
   A mismatch must return `errMorphlingNotFound`, not a distinct access-denied
   error, to avoid leaking information about another session's morphlings.

5. Secret values (`SecretValue`, raw API keys, tokens) must be extracted from
   request structs immediately after decode and the original field must be
   cleared. Do not pass structs containing secret fields to logging, error
   formatting, or audit paths.

6. `isSecretExportCapability` is a defense-in-depth guard, not the primary
   capability boundary. Do not widen its allowable surface. The registered
   capability set is finite; a future hardening step should replace the
   heuristic with an explicit allowlist check against the registry.

7. Do not add background goroutines, cleanup daemons, or autonomous lifecycle
   workers to Loopgate without an explicit design review. Loopgate is
   intentionally synchronous and request-driven.

8. Model-originated content (memory strings, status text, summaries) must not
   appear verbatim in public status responses. Use counts or taint-marked
   projections. Raw strings must stay inside authoritative internal records.

## Code review self-check

Before finalizing any change, ask:

- Does this weaken any existing boundary?
- Does this introduce fail-open behavior?
- Am I treating model output as trusted when I should not?
- Did I rename things to make trust and state clearer?
- Could this race?
- Could this create an ambiguous audit trail?
- Are denials explicit and test-covered?
- Did I preserve append-only semantics?
- Did I preserve tamper-evident audit behavior where required?
- Did I leak an internal transport or control-plane feature into a public surface?
- Did I add or update docs and tests for the new boundary?

If unsure, choose the more conservative implementation.

## Specific behavioral rules for this project

- Never modify append-only ledger history in place.
- Never let model output directly redefine policy.
- Never silently fall back from secure path resolution to insecure path use.
- Never silently fall back from a secure secret backend to plaintext storage.
- Never silently widen a local-only control-plane surface into a public API.
- Never merge user-facing audit history with internal telemetry logs.
- Never infer permission from intent or phrasing.
- Never broaden an allowlist without explicit justification in code, comments, and tests.
- Never treat a reference, summary, status string, or projected UI object as authority.
- Never accept caller-supplied working directories, path allowlists, or capability envelopes for controlled runtime objects.
- Always prefer deny-by-default behavior.
- Always preserve monotonic cursor and state progression where applicable.
- Always redact tool input, tool output, and reason strings before audit persistence.
- Always treat environment variables as untrusted input, not an authority source.
- Always keep private runtime paths private when rendering operator-visible paths.

## Output expectations for code changes

When making meaningful changes, summarize:

1. What changed
2. Why it changed
3. Which invariant it affects
4. Security implications
5. Concurrency implications
6. Documentation updated
7. Tests added or updated

Keep the summary concise but explicit.

## Secrets handling

This project handles API keys, access tokens, refresh tokens, client secrets, and similar credentials as high-sensitivity material.

Secrets must be stored, accessed, rotated, and audited in ways that preserve system security and project invariants. Use centralized or platform-secure storage where possible. Avoid plaintext storage in project files, logs, ledgers, and repo-tracked runtime artifacts.

### Core rules

- Never hardcode secrets in source code, tests, fixtures, example configs, or documentation.
- Never store raw secrets in the append-only ledger.
- Never log raw secrets, full tokens, authorization headers, refresh tokens, or full secret-bearing payloads.
- Never print environment dumps or config dumps if they may contain credentials.
- Never persist model-generated secret-like values unless they have been explicitly validated and intentionally stored through the secret storage path.
- Treat all secrets as untrusted input until parsed and validated for the specific use case.
- Prefer fail-closed behavior. If the secret backend is unavailable or a secret cannot be validated, return an explicit error rather than silently falling back to insecure behavior.

### Approved storage backends

Use the most secure backend available for the deployment model.

#### Local desktop and single-user mode

Use the operating system’s secure credential store by default.

- macOS: Keychain Services or Keychain items
- Windows: OS credential store or DPAPI-backed mechanisms
- Linux: Secret Service or a libsecret-compatible keyring

#### Server, production, and multi-user mode

Use a real secrets manager, such as HashiCorp Vault or a cloud-native secret service.

#### CI and CD mode

Secrets may be injected through environment variables at runtime, but environment variables are a delivery mechanism, not the preferred long-term source of truth.

### Forbidden storage locations

Do not store secrets in:

- repository-tracked YAML, JSON, TOML, or `.env` files
- append-only ledger entries
- runtime logs or debug output
- user-facing audit trails
- panic messages
- crash reports where redaction is not guaranteed
- test snapshots or golden files
- shell history via unsafe CLI usage
- repo-tracked runtime memory, key, or distillate artifacts

### Secret classes

Different secret types have different handling requirements.

#### Static API keys

Examples: third-party API keys, personal access tokens, service credentials.

- Store in a secure backend.
- Retrieve only when needed.
- Avoid caching longer than necessary.
- Support explicit rotation.

#### Access tokens

- Prefer in-memory handling only.
- Persist only if restart continuity is a product requirement.
- Record metadata, not token value.

#### Refresh tokens

- Treat as higher sensitivity than short-lived access tokens.
- Store only in a secure backend.
- Never log or expose in user-visible output.

#### Private keys

- Prefer platform-backed or manager-backed storage.
- Avoid exporting raw key material where the platform supports non-exportable keys.
- Never write private keys to ordinary config or state files.

#### Ephemeral machine credentials

Prefer dynamically issued short-lived credentials when the backend supports them.

### Storage model inside the project

The application should store references and metadata, not raw secret values, in normal project state.

Preferred pattern:

```go
type SecretRef struct {
    ID          string // stable internal identifier
    Backend     string // e.g. "keychain", "vault", "env"
    AccountName string // backend-specific lookup key
    Scope       string // logical scope or policy binding
}
```

Normal runtime config, ledger entries, and user-visible state should reference `SecretRef` or similar metadata rather than embedding raw secret material.

### Ledger and audit rules

The append-only ledger may record only non-secret metadata such as:

- secret reference ID
- backend or provider
- created timestamp
- rotated timestamp
- scope or policy association
- last validation result
- truncated fingerprint or hash prefix for correlation

The ledger must not contain:

- raw secret value
- full token
- refresh token
- private key
- complete authorization header
- full environment variable contents

User-visible audit data must remain separate from internal telemetry. Record the fact that a secret was created, rotated, validated, or failed lookup, but not the value itself.

### Bootstrapping rules

The first secret is often the ugliest little goblin in the cave.

Bootstrap carefully:

- Desktop app: user provides a secret once, the app stores it in the OS-secure store, and the project retains only a reference.
- Server or service: operator injects a bootstrap credential through a secure deployment path, then the service exchanges it for scoped credentials from the secret manager.
- Containers: never bake secrets into images. Inject them at runtime.

### Rotation and lifecycle

All secret records should support lifecycle metadata:

- `created_at`
- `last_used_at`
- `last_rotated_at`
- `expires_at` if known
- `status`
- `scope`
- `owner` or service identity if relevant

Code should be written to tolerate rotation:

- resolve secrets just in time where practical
- avoid permanent in-memory caching
- re-fetch on authentication failure when safe
- make revocation and expiration visible through explicit errors

### Redaction and observability

All logging and error handling must redact secrets.

Minimum redaction targets:

- authorization headers
- bearer tokens
- API keys
- refresh tokens
- client secrets
- private key material
- known secret fields in JSON or YAML payloads
- tool arguments, tool output, and reason strings before audit persistence

Errors should preserve debugging value without leaking secret contents.

Good:

- `vault lookup failed for secret ref "anthropic-prod" due to permission denied`
- `secret validation failed: missing required token prefix`

Bad:

- printing the token
- dumping the entire request
- emitting environment contents to logs

### Environment variable policy

Environment variables are acceptable for runtime injection in CI and CD and some deployment scenarios, but they are not the preferred persistent storage backend for long-lived local secrets.

Allowed:

- runtime injection from CI, CD, or an orchestrator
- bootstrap configuration for secret backend connection

Not preferred:

- long-term storage of user API keys in local shell startup files
- repo-local `.env` files containing production credentials

### Fallback behavior

If secure storage is unavailable:

- return an explicit error
- do not silently downgrade to plaintext file storage
- do not auto-create insecure fallback config
- do not bypass validation to keep the app running

If a plaintext fallback mode is ever added for explicit portability reasons, it must be clearly labeled insecure or compatibility mode, protected by strong encryption, and disabled by default.

### Runtime artifact hygiene

Runtime-generated ledger files, distillates, keys, caches, and local build artifacts are not source code.

- Do not commit runtime artifacts to the repository.
- Keep runtime memory and audit files outside tracked source when possible.
- Update `.gitignore` when new runtime-generated files or directories are introduced.
- Treat committed runtime artifacts as a security and privacy review issue.

### Testing requirements

Any secret-handling change must include tests for:

- no raw secret persistence in the ledger
- no raw secret leakage in logs or errors
- backend failure handling
- secret reference resolution
- rotation metadata behavior
- access-token in-memory handling where applicable
- explicit denial on missing or unavailable secure backend
- redaction paths for structured and unstructured logs
- no raw secret-bearing tool output persisted to audit logs

Regression tests should be added for every secret-leak or fallback bug.
