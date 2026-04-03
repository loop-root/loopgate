**Last updated:** 2026-03-24

# RFC-MORPH-0008: Morphling class schema & lifecycle (Loopgate authority)

- **Status:** Draft — implementation target (substantial portions implemented in Loopgate)
- **Primary authority:** **Loopgate**
- **Depends on:** RFC-MORPH-0001, RFC-MORPH-0009, RFC-MORPH-0002, RFC-MORPH-0003, RFC-MORPH-0004, RFC 0001 (`docs/rfcs/0001`)
- **Normative revision:** 2026-03-11

---

## 1. Purpose

This RFC defines the three things that must exist for a **sound** morphling
implementation (much of this now exists in Loopgate; the sections remain
normative for invariants and future changes):

1. The **class definition schema** — the policy config Loopgate loads at
   startup to validate spawn requests
2. The **formal lifecycle state machine** — legal states, legal transitions,
   illegal transitions, transition triggers, and the audit event at each
   transition
3. The **spawn request/response wire format** — what the client sends, what
   Loopgate returns

MORPH-RFC-0002 defines what a morphling is. This RFC defines what Loopgate
needs to validate, spawn, and govern one.

This RFC deliberately separates three concerns that must remain distinct in
implementation:

- **Class policy** — what a class of morphling is allowed to do (static,
  loaded at startup)
- **Runtime state** — the authoritative lifecycle record for a morphling
  instance (mutable, Loopgate-owned)
- **Audit/event projection** — the append-only record of what happened and the
  UI-safe view of current state (immutable once written)

Fusing these three concerns is the most common source of correctness bugs in
lifecycle systems. This RFC keeps them separate.

---

## 2. Core invariants

These align with the project’s documented security and lifecycle rules (see
`docs/loopgate-threat-model.md`, `docs/design_overview/loopgate.md`, and
maintainer-local `AGENTS.md` when present) and must be preserved in every
implementation decision:

- Working directories are derived by Loopgate from class policy, never
  accepted from the caller.
- Allowed paths are derived by Loopgate from class policy and validated sandbox
  inputs, never from caller parameters.
- Capability envelopes are derived by Loopgate from class policy, never from
  model claims or caller fields.
- Loopgate is the sole authority for lifecycle transitions.
- `terminated` is a final state. Dead morphlings cannot be resurrected.
- Capacity reservation must be atomic. Check-then-create is not acceptable.
- The `terminating` state record must be persisted to durable storage before
  any cleanup action begins. Emitting an in-memory event is not sufficient.
  If Loopgate crashes after persisting `terminating` but before cleanup
  finishes, restart must be able to continue cleanup from the persisted record.
- Capacity reservation release must be atomic with the persistence of the
  `terminating` state record. The slot is not freed by cleanup completing — it
  is freed by `terminating` being durably recorded.
- Default max active morphlings remains conservative unless explicitly
  configured.
- The sandbox directory must not be created until all authorization gates are
  passed and capacity is atomically reserved.

---

## 3. Class definition schema

A class definition is a policy record describing what a morphling of that class
is allowed to do, where it may operate, and what resource and time limits
apply. Loopgate loads these at startup. The class config is Loopgate-owned —
The client cannot modify it.

### 3.1 Design decisions

**Allow-only capability and zone lists.**
Each class declares only what is allowed. Denied is derived as "everything
else." An explicit denied list would be useful if class inheritance existed,
but this system has no inheritance. Maintaining both allowed and denied lists
adds validation complexity without value. If overlap validation ever fails,
the system will have a security defect that is invisible at runtime. Allow-only
eliminates that class of defect.

**TTL policy fields are class-level, not global.**
`spawn_approval_ttl_seconds`, `capability_token_ttl_seconds`, and
`review_ttl_seconds` belong in the class definition because different classes
have meaningfully different expected durations. A reviewer morphling and a
builder morphling should not share a token TTL. Punting these to a later RFC
forces schema churn against running instances.

### 3.2 Schema

```yaml
# /morph/policy/morphling_classes.yaml

version: "1"

# Global kill-switch. Must be explicitly set true to enable spawning.
spawn_enabled: false

# Conservative default. Operator must raise this explicitly.
max_active_morphlings: 3

classes:

  - name: reviewer
    description: "Read-only analysis, code review, and inspection"
    capabilities:
      allowed:
        - read_path
        - analyze_code
    sandbox:
      allowed_zones:
        - workspace
        - imports
        - scratch
    resource_limits:
      max_time_seconds: 300
      max_tokens: 50000
      max_disk_bytes: 52428800        # 50 MB
    ttl:
      spawn_approval_ttl_seconds: 300
      capability_token_ttl_seconds: 360  # slightly longer than max_time_seconds
      review_ttl_seconds: 86400          # 24 hours
    spawn_requires_approval: false
    completion_requires_review: true
    max_concurrent: 3

  - name: editor
    description: "Read and write files within allowed sandbox paths; propose patches"
    capabilities:
      allowed:
        - read_path
        - write_path
        - propose_patch
    sandbox:
      allowed_zones:
        - workspace
        - imports
        - outputs
        - scratch
        - agents
    resource_limits:
      max_time_seconds: 600
      max_tokens: 100000
      max_disk_bytes: 104857600       # 100 MB
    ttl:
      spawn_approval_ttl_seconds: 300
      capability_token_ttl_seconds: 660
      review_ttl_seconds: 86400
    spawn_requires_approval: true
    completion_requires_review: true
    max_concurrent: 2

  - name: tester
    description: "Run test commands and report results"
    capabilities:
      allowed:
        - read_path
        - execute_test
    sandbox:
      allowed_zones:
        - workspace
        - imports
        - scratch
        - logs
    resource_limits:
      max_time_seconds: 300
      max_tokens: 30000
      max_disk_bytes: 52428800
    ttl:
      spawn_approval_ttl_seconds: 300
      capability_token_ttl_seconds: 360
      review_ttl_seconds: 86400
    spawn_requires_approval: false
    completion_requires_review: true
    max_concurrent: 2

  - name: researcher
    description: "Query approved providers; results land in quarantine"
    capabilities:
      allowed:
        - read_path
        - provider_query
    sandbox:
      allowed_zones:
        - imports
        - scratch
        - quarantine
    resource_limits:
      max_time_seconds: 120
      max_tokens: 40000
      max_disk_bytes: 26214400        # 25 MB
    ttl:
      spawn_approval_ttl_seconds: 300
      capability_token_ttl_seconds: 180
      review_ttl_seconds: 86400
    spawn_requires_approval: false
    completion_requires_review: true
    max_concurrent: 3

  - name: builder
    description: "Build and compile artifacts from sandbox inputs"
    capabilities:
      allowed:
        - read_path
        - write_path
        - execute_build
    sandbox:
      allowed_zones:
        - workspace
        - imports
        - outputs
        - scratch
    resource_limits:
      max_time_seconds: 900
      max_tokens: 50000
      max_disk_bytes: 524288000       # 500 MB
    ttl:
      spawn_approval_ttl_seconds: 300
      capability_token_ttl_seconds: 960
      review_ttl_seconds: 86400
    spawn_requires_approval: true
    completion_requires_review: true
    max_concurrent: 1
```

### 3.3 Class schema validation rules (Loopgate, at startup)

Loopgate must reject a class definition and refuse to start if:

- `name` fails `identifiers.ValidateSafeIdentifier`
- Any capability name in `allowed` is unknown to the capability registry
- Any zone name in `allowed_zones` is not in the known sandbox zone set
- Any resource limit (`max_time_seconds`, `max_tokens`, `max_disk_bytes`) is
  zero or negative
- Any TTL field is zero or negative
- `max_concurrent` is zero or negative
- `name` is duplicated across the class list
- `capability_token_ttl_seconds` is less than `max_time_seconds` (the token
  must outlive the task)

---

## 4. Lifecycle state machine

### 4.1 Design decisions

**States model durable execution phases, not decision labels.**
`approved` and `rejected` are review outcomes, not execution phases. If
Loopgate crashes between `approved` and the next thing that happens, the
system must be able to reconstruct what occurred and what invariant holds. A
state named `approved` raises the question: is the morphling still active? Is
it consuming quota? Can a second approval happen? A state named `terminating`
has a clear invariant: cleanup is in progress. The morphling is shutting down.
That is unambiguous.

Review decisions are recorded as append-only events and stored as the terminal
`outcome` field on the `terminated` record. The lifecycle state machine
stays monotonic and kernel-shaped.

**`staged` is eliminated.**
`staged` had no invariant distinct from `completing` (artifacts being written)
or `pending_review` (artifacts ready for review). A state that means "between
two other states" is usually a diagram artifact, not an enforcement boundary.
`completing` now covers artifact staging. Completion ends when staging
succeeds. Then either `pending_review` or `terminating` follows directly.

**Two categories of spawn denial, with distinct outcome semantics.**
A spawn request that is rejected before a morphling record is created is a
request-level denial. No `morphling_id` is issued. No lifecycle entry exists.
The response carries `status: "denied"` against the original `request_id`.

Once a morphling record is instantiated (morphling_id minted), the request-
level `denied` vocabulary no longer applies. From that point, all termination
outcomes use lifecycle semantics: `approved`, `rejected`, `cancelled`, or
`failed`. The word `denied` must not appear as a lifecycle `outcome` field on
an instantiated morphling — it belongs to the request layer, not the lifecycle
layer. Blurring these two surfaces produces metric lies and confusing audit
queries about entities that may or may not have ever existed.

**Outcome and termination reason are separate fields.**
`outcome` answers "what happened at a high level." `termination_reason`
answers "what specific cause produced that outcome." Mixing normal completion
outcomes with fault causes in one field produces broken metrics and confusing
audit queries.

### 4.2 States

| State | Description |
|-------|-------------|
| `requested` | Spawn request received by Loopgate; validation not yet started |
| `authorizing` | Loopgate validating class, checking active limits, atomically reserving capacity, deriving sandbox paths |
| `pending_spawn_approval` | Class requires operator approval before spawn proceeds |
| `spawned` | All gates passed; capacity reserved; sandbox directory created; capability token issued |
| `running` | Morphling actively executing |
| `completing` | Execution finished; artifacts being staged to outputs zone; artifact manifest being finalized |
| `pending_review` | Artifact manifest finalized and immutable; awaiting operator review decision |
| `terminating` | Cleanup in progress; sandbox preserved as evidence |
| `terminated` | Final state; outcome and termination_reason recorded; artifacts and audit trail preserved |

`terminated` is the only terminal state. All paths lead here.

### 4.3 Legal transitions

```
requested            → authorizing

authorizing          → pending_spawn_approval
                       (spawn_requires_approval: true; capacity reserved)

authorizing          → spawned
                       (spawn_requires_approval: false; all gates passed;
                        capacity reserved; sandbox created; token issued)

pending_spawn_approval → spawned
                       (operator approved within spawn_approval_ttl_seconds)

pending_spawn_approval → terminating
                       (operator denied; or approval TTL expired)

spawned              → running
                       (execution started)

spawned              → terminating
                       (execution failed to start after token issued)

running              → completing
                       (task execution finished normally)

running              → terminating
                       (timeout / budget exceeded / operator cancel /
                        token expired / error)

completing           → pending_review
                       (completion_requires_review: true;
                        artifact manifest finalized and immutable)

completing           → terminating
                       (completion_requires_review: false; OR staging failed)

pending_review       → terminating
                       (operator reviewed — approved or rejected;
                        or review TTL expired)

terminating          → terminated
                       (cleanup complete; outcome and reason recorded)
```

### 4.4 Illegal transitions

Loopgate must deny these explicitly and append an audit event for any attempt:

- Any backward transition (`running → spawned`, `completing → running`, etc.)
- Any skip that bypasses mandatory phases (`running → terminated` directly)
- Any transition out of `terminated` — this is a final state
- Any caller-supplied state override

### 4.5 Outcome and termination reason

The `terminated` record carries two separate fields.

**`outcome`** describes what happened at a high level. This field only appears
on instantiated morphlings (those with a `morphling_id`). Request-level
denials use the request denial envelope instead and never produce an `outcome`
field.

| Outcome | When |
|---------|------|
| `approved` | Operator approved artifacts after review |
| `rejected` | Operator rejected artifacts after review |
| `cancelled` | Operator or system cancelled the morphling before or during execution, including spawn approval denied by operator, TTL expiry, or parent session termination |
| `failed` | Abnormal termination due to a fault condition: budget exceeded, execution error, staging error, token expiry, or restart |

**`termination_reason`** describes the specific cause:

| Reason | Outcome | When |
|--------|---------|------|
| `normal_completion` | `approved` or `rejected` | Task finished and operator review decision recorded |
| `review_ttl_expired` | `cancelled` | System-driven: `pending_review` TTL elapsed with no operator decision |
| `spawn_denied_by_operator` | `cancelled` | Operator explicitly denied the spawn approval request |
| `spawn_approval_ttl_expired` | `cancelled` | System-driven: spawn approval TTL elapsed with no operator decision |
| `operator_cancelled` | `cancelled` | Operator explicitly cancelled an active morphling |
| `parent_session_terminated` | `cancelled` | Parent client session closed while morphling was active |
| `execution_start_failed` | `failed` | `spawned → running` transition failed |
| `time_budget_exceeded` | `failed` | `max_time_seconds` enforced by Loopgate |
| `token_budget_exceeded` | `failed` | `max_tokens` enforced by Loopgate |
| `disk_quota_exceeded` | `failed` | `max_disk_bytes` enforced by Loopgate |
| `capability_token_expired` | `failed` | Token TTL elapsed mid-execution |
| `staging_failed` | `failed` | Artifact staging encountered unrecoverable error |
| `loopgate_restart` | `failed` | Loopgate restarted while morphling was active |

**Note on TTL-driven reasons vs. operator-driven reasons:** Reasons ending in
`_ttl_expired` are system-driven. No operator action occurred. These must be
clearly distinguished in audit records from operator decisions. The audit
events section below uses separate event types for each case to make the actor
unambiguous — a system timer firing is not the same thing as an operator
choosing to deny or cancel.

### 4.6 Audit events per transition

Every state transition must produce an append-only audit event before the
transition is considered complete. The event must be persisted before any
external response is returned.

| Event type | Fired at | Required fields |
|------------|----------|-----------------|
| `morphling.spawn_requested` | `→ requested` | `request_id`, `class`, `goal_hmac`, `parent_session_id` |
| `morphling.authorizing` | `→ authorizing` | `morphling_id`, `class` |
| `morphling.spawn_approval_pending` | `→ pending_spawn_approval` | `morphling_id`, `approval_id`, `approval_deadline_utc` |
| `morphling.spawn_approved` | operator decision → `spawned` | `morphling_id`, `approval_id`, `decision_nonce` |
| `morphling.spawn_denied_by_operator` | operator decision → `terminating` | `morphling_id`, `approval_id`, `decision_nonce` |
| `morphling.spawn_approval_expired` | system TTL → `terminating` | `morphling_id`, `approval_id`, `expired_at_utc` |
| `morphling.spawned` | `→ spawned` | `morphling_id`, `task_id`, `class`, `granted_capabilities`, `virtual_sandbox_path`, `token_expiry_utc` |
| `morphling.execution_started` | `→ running` | `morphling_id`, `task_id` |
| `morphling.execution_completed` | `→ completing` | `morphling_id`, `exit_reason` |
| `morphling.artifacts_staged` | `→ pending_review` | `morphling_id`, `artifact_count`, `artifact_manifest_hash` |
| `morphling.review_decision` | operator decision during `pending_review` | `morphling_id`, `decision` (approved/rejected), `decision_nonce` |
| `morphling.review_expired` | system TTL during `pending_review` | `morphling_id`, `expired_at_utc` |
| `morphling.terminating` | `→ terminating` | `morphling_id`, `outcome`, `termination_reason` |
| `morphling.terminated` | `→ terminated` | `morphling_id`, `outcome`, `termination_reason`, `preserved_artifact_refs`, `virtual_evidence_path` |

**Event type is the actor signal.** Operator-driven and system-driven paths use
distinct event types. `morphling.review_decision` carries a `decision_nonce`
— it was produced by an operator action. `morphling.review_expired` carries an
`expired_at_utc` — it was produced by a system timer. These must never share
an event type. Audit queries, metrics, and operator-facing history all depend
on this distinction being unambiguous in the ledger.

**On `goal_hmac`:** The morphling goal is model-adjacent user input and must
not appear raw in the append-only audit ledger. Use HMAC-SHA256 keyed with the
session MAC key for internal audit correlation. This preserves correlation
across a session without making repeated identical goals trivially linkable
across sessions via a plain content hash. The full goal text is stored
separately in the task record under Loopgate control, not in the ledger.

**On `virtual_sandbox_path` and `virtual_evidence_path`:** These are always
operator-visible virtual paths. Internal runtime paths must never appear in
audit events or UI projection.

**On `artifact_manifest_hash`:** The artifact manifest is finalized and hashed
at the point of the `morphling.artifacts_staged` event. The hash is the
integrity anchor for the review decision. A reviewer is approving a specific
manifest, not an open-ended artifact set. The manifest must not change after
this event.

The manifest must be serialized to canonical form before hashing: sorted
object keys, no insignificant whitespace, deterministic number representation.
Non-deterministic serialization produces different hashes for logically
identical manifests, which breaks hash-based review binding silently and with
no error message. Canonical JSON per RFC 8785 is the recommended baseline.

---

## 5. Capacity reservation

### 5.1 Atomicity requirement

Loopgate must check and reserve capacity atomically before returning a
`spawned` response or transitioning to `spawned` state. The reservation must be
persisted before any external success is returned.

This means:

- Loopgate maintains a protected counter for active morphling count (global)
  and per-class active count
- The reservation operation is: read current count, compare against limit,
  increment, persist — all inside a single critical section
- Concurrent spawn requests for the final available slot must not both succeed
- If the persistence of the reservation fails, the spawn must fail and the
  counter must be rolled back

This is not check-then-create. Check-then-create is a TOCTOU bug. Two parallel
spawns can both pass the limit check before either completes, which silently
exceeds the configured limit.

### 5.2 Reservation release

The capacity reservation is released atomically with the persistence of the
`terminating` state record. Not at the start of cleanup, not at `terminated`,
not when an in-memory event is emitted — at the exact moment the `terminating`
state is written to durable storage.

The required sequence for the `→ terminating` transition is:

```
1. Construct the terminating state record (outcome, termination_reason)
2. Atomically: persist state record AND decrement capacity counter
3. Append morphling.terminating audit event
4. Begin cleanup
```

Steps 2 and 3 must be ordered: state and counter persist before the audit
event, audit event before cleanup. If Loopgate crashes after step 2 but before
step 4, restart sees a `terminating` record and a released slot, and can
continue cleanup. If Loopgate crashes before step 2, restart sees the previous
state and the slot is still held — capacity is reconstructed from live records.

Two failure modes to avoid:

- **Leaked slot:** capacity decremented but `terminating` record not persisted.
  On restart, the slot appears free but the morphling is still active in the
  record. A new spawn can now exceed the actual limit.
- **Double release:** the `terminating` transition fires twice (e.g., two
  concurrent cancel requests). The critical section in 5.1 must also protect
  against duplicate `→ terminating` transitions on the same morphling_id.

### 5.3 Sandbox directory creation timing

The sandbox directory under `agents/` must not be created until:

1. Class validation has passed
2. Capacity is atomically reserved and persisted
3. Spawn approval is obtained (if required)

Creating the directory during `authorizing` would produce observable filesystem
side effects before admission is complete, leaving litter from denied or expired
approval requests. The directory is created as part of the `spawned` transition,
after all gates are cleared.

---

## 6. Capability grant rule

### 6.1 Grant computation

The granted capability scope is the intersection of the requested capabilities
and the class-allowed capabilities:

```
granted = requested_capabilities ∩ class.capabilities.allowed
```

### 6.2 Empty and omitted request handling

- If `requested_capabilities` is absent from the spawn request, the request
  is denied: `spawn_denied_by_policy` / reason `capabilities_not_specified`.
- If `requested_capabilities` is present but empty, the request is denied:
  `spawn_denied_by_policy` / reason `capabilities_empty`.
- If the intersection is empty (caller asked for things the class does not
  allow), the request is denied: `spawn_denied_by_policy` / reason
  `capability_intersection_empty`.

Omission and empty are explicit denials. Loopgate must not default to granting
all class-allowed capabilities when the caller omits the field. Otherwise a
Client bug or model-generated spawn request could over-provision a morphling by
accident.

---

## 7. Spawn request and response wire format

### 7.1 Spawn request (Client → Loopgate)

```json
{
  "request_id": "req_abc123",
  "class": "editor",
  "goal": "Modify the parser in foo.go to handle escaped quotes",
  "inputs": [
    {
      "sandbox_path": "/morph/home/imports/project/foo.go",
      "role": "primary"
    }
  ],
  "output_tag": "parser-fix",
  "requested_capabilities": ["read_path", "write_path", "propose_patch"],
  "requested_time_budget_seconds": 300,
  "requested_token_budget": 50000,
  "parent_session_id": "session_xyz"
}
```

**Fields The client must NOT supply** (Loopgate derives these):

- `working_dir`
- `allowed_paths`
- `morphling_id`
- `task_id`
- Any field that encodes runtime authority

Loopgate must reject any spawn request containing these fields with
`denial_reason: caller_supplied_authority_field_denied`. This is not a
validation warning. It is a hard denial.

### 7.2 Spawn responses

**Request-level denial** (no morphling instance created, no `morphling_id`
issued):

```json
{
  "request_id": "req_abc123",
  "status": "denied",
  "denial_reason": "max_active_limit_reached"
}
```

Using `state: "terminated"` here is wrong — it implies a morphling existed and
reached a terminal state, which is false. Request-level denials return a denial
envelope against the request, not a lifecycle record.

**Pending approval** (morphling instance created, awaiting operator):

```json
{
  "morphling_id": "morphling_abc123",
  "state": "pending_spawn_approval",
  "class": "editor",
  "approval_id": "approval_def789",
  "approval_deadline_utc": "2026-03-11T15:35:00Z"
}
```

**Spawned successfully:**

```json
{
  "morphling_id": "morphling_abc123",
  "task_id": "task_xyz456",
  "state": "spawned",
  "class": "editor",
  "granted_capabilities": ["read_path", "write_path", "propose_patch"],
  "virtual_sandbox_path": "/morph/home/agents/task_xyz456",
  "spawned_at_utc": "2026-03-11T15:25:00Z",
  "token_expiry_utc": "2026-03-11T15:36:00Z"
}
```

### 7.3 Projected status (Loopgate → client, for UI rendering)

This is what The client renders. It contains only information safe for display —
virtual paths, no internal runtime details, no raw goal text.

**Single morphling status:**

```json
{
  "morphling_id": "morphling_abc123",
  "class": "editor",
  "state": "running",
  "goal_hint": "Modify parser in foo.go",
  "spawned_at_utc": "2026-03-11T15:25:00Z",
  "last_event_at_utc": "2026-03-11T15:26:30Z",
  "token_expiry_utc": "2026-03-11T15:36:00Z",
  "virtual_sandbox_path": "/morph/home/agents/task_xyz456",
  "artifact_count": 0,
  "pending_review": false,
  "outcome": null,
  "termination_reason": null
}
```

`goal_hint` is the goal truncated to 80 characters. The full goal is not in
the projection.

**Pool summary:**

```json
{
  "spawn_enabled": true,
  "max_active": 3,
  "active_count": 1,
  "pending_review_count": 0,
  "morphlings": []
}
```

---

## 8. Restart and recovery invariants

When Loopgate restarts, it reads all persisted morphling lifecycle records and
resolves each one before accepting new requests.

**Resolution rules by state found on disk:**

- `running`, `completing`, or `pending_review`: transition to `terminating`
  with `outcome: failed`, `termination_reason: loopgate_restart`. Release
  capacity per section 5.2 (persist `terminating` record atomically with
  counter decrement, then append event, then begin cleanup).
- `pending_spawn_approval`: transition to `terminating` with
  `outcome: cancelled`, `termination_reason: loopgate_restart`. Release
  capacity.
- `terminating`: continue cleanup from the persisted record. The slot was
  already released when `terminating` was first persisted — do not release
  again. This is the double-release guard.
- `terminated`: leave as-is.
- `requested`, `authorizing`, `spawned`: these are pre-execution or
  just-spawned states. Treat as `failed` / `loopgate_restart` and transition
  to `terminating`. If capacity was reserved (i.e., state was `spawned`),
  release it.

The morphling lifecycle record on disk is the source of truth. The client must not
cache or infer morphling state independently. The client's projected view is always
derived from what Loopgate returns, never from client-local memory.

---

## 9. What this RFC unlocks, in order

1. **Replace registry.json** with `morphling_classes.yaml` as the
   authoritative class store. The JSON file was a placeholder. The YAML policy
   file is the format consistent with the rest of the config system. Loopgate
   loads and validates it at startup.

2. **Implement class validation** at startup. If any class fails the rules in
   section 3.3, Loopgate refuses to start. This is fail-closed behavior.

3. **Implement the state machine enforcer** — a Loopgate-internal module that
   takes `(authoritativeState, event)` and returns
   `(nextState, auditEvent, error)`. All lifecycle transitions go through it.
   Illegal transitions return an explicit typed error. No transition happens
   without a corresponding audit event being appended first.

4. **Implement atomic capacity tracking** per section 5. The counter and its
   reservation must be persisted before any external response is returned.

5. **Implement the spawn handler** — validates class, computes granted
   capabilities, checks and atomically reserves capacity, issues approval
   request if needed, then (after all gates pass) creates sandbox directory,
   issues capability token, appends `morphling.spawned`, returns response.

6. **Implement the terminate handler** — the required sequence is: (a) persist
   `terminating` state record to durable storage atomically with decrementing
   the capacity counter; (b) append `morphling.terminating` audit event; (c)
   begin cleanup: preserve sandbox artifacts, revoke capability token; (d)
   append `morphling.terminated` with outcome and termination_reason. Do not
   begin any cleanup action before step (a) is durably persisted. See section
   5.2 for the failure modes this ordering prevents.

7. **Wire the projected status endpoint** — `GET /v1/ui/morphlings` returns
   the pool summary and per-morphling projection. Virtual paths only.

---

## 10. Open questions for subsequent RFCs

- **Review expiry behavior:** When `review_ttl_seconds` elapses, the outcome
  is `cancelled` and reason is `review_ttl_expired`. Should artifacts be
  preserved or discarded? The conservative default is preserved. A subsequent
  RFC should make this a class-level policy field.

- **Parent session termination cascade:** If the parent client session closes,
  do active morphlings auto-terminate immediately or get a grace window to
  finish staging? The grace window approach is friendlier but adds complexity.
  Default is immediate termination with `outcome: cancelled` and reason
  `parent_session_terminated`.

- **Artifact retention TTL after termination:** The RFC says artifacts are
  preserved at `terminated`. For how long and under what cleanup policy? This
  connects to RFC 0004 quarantine retention semantics and should be addressed
  in a follow-on RFC before the first real morphling ships.

- **output_tag and chaining:** The `output_tag` field in the spawn request is
  a forward-looking hook for one morphling's output becoming another's input.
  The contract for that must be defined before implementing it. It must not
  become an implicit trust path between morphlings.
