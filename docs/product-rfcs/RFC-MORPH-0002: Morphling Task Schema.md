**Last updated:** 2026-03-24

# RFC-MORPH-0002: Morphling task schema

- **Status:** Draft — task envelope and invariants (lifecycle authority is Loopgate; see RFC-MORPH-0008)
- **Primary authority:** **Loopgate** (morphlings are control-plane objects)
- **Normative revision:** 2026-03-09

---

# Summary

Morphlings are disposable execution workers spawned under **Loopgate** authority (requested via **Haven**) to perform a **single bounded task**. They operate within strict capability and filesystem constraints issued by Loopgate.

Morphlings do not possess persistent authority, secrets, or unrestricted system access. They exist only long enough to complete their assigned task and return artifacts for review.

This RFC defines the **task schema**, lifecycle constraints, and security invariants governing morphling execution.

**Implementation note (2026-03-24):** Morphlings exist in Loopgate with class policy and lifecycle per RFC-MORPH-0008; this RFC remains the reference for **task schema** and envelope constraints as the runner surface evolves.

---

# Design Goals

Morphlings must be:

- single-purpose
- short-lived
- capability-limited
- sandbox-bound
- easy to terminate
- incapable of silently mutating global state

Morphlings produce **artifacts and proposals**, not direct side effects.

---

# Morphling Identity

Each morphling must have a unique identity issued by Loopgate.

Example:

```
morphling_id: morphling-uuid
parent_session: morph-session-id
spawned_by: morph
```

This ensures auditability and traceability for all morphling actions.

---

# Task Schema

All morphlings must be spawned using a strict task schema.

Example JSON representation:

```json
{
  "task_id": "uuid",
  "class": "editor",
  "goal": "Modify lines 10-55 in foo.go to implement feature X",
  "inputs": [
    "/morph/home/imports/project/foo.go"
  ],
  "working_dir": "/morph/home/agents/task-uuid",
  "allowed_paths": [
    "/morph/home/agents/task-uuid",
    "/morph/home/workspace/project-x"
  ],
  "capabilities": [
    "read_path",
    "write_path",
    "propose_patch"
  ],
  "time_budget_seconds": 120,
  "token_budget": 30000,
  "requires_review": true
}
```

---

# Task Fields

## task_id

Unique identifier for the morphling task.

## class

Defines the morphling's role template. See Morphling Classes.

## goal

Human-readable description of the objective.

## inputs

Explicit list of input files or resources.

All inputs must exist inside `/morph/home`.

## working_dir

Sandbox directory used by the morphling.

Loopgate must create this directory prior to execution.

## allowed_paths

Paths the morphling may read or write.

Access outside these paths must be denied.

## capabilities

Explicit capability set issued by Loopgate.

Capabilities are never inferred.

## time_budget_seconds

Maximum wall-clock execution time.

Loopgate must terminate morphlings exceeding this limit.

## token_budget

Maximum model token usage for the task.

## requires_review

If true, all artifacts must be staged before promotion.

---

# Morphling Classes

Morphlings should use predefined capability templates.

Suggested classes:

### Reviewer

Capabilities:

- read_path
- analyze_code

Purpose:

Code review, static analysis, inspection.

---

### Editor

Capabilities:

- read_path
- write_path
- propose_patch

Purpose:

Modify files inside sandbox.

---

### Tester

Capabilities:

- read_path
- execute_test

Purpose:

Run test commands and summarize results.

---

### Researcher

Capabilities:

- read_path
- provider_query

Purpose:

Search or gather information from approved providers.

---

### Refactorer

Capabilities:

- read_path
- write_path
- propose_patch
- analyze_code

Purpose:

Refactor code while preserving behavior.

---

### Builder

Capabilities:

- read_path
- write_path
- execute_build

Purpose:

Build artifacts or compile outputs.

---

# Capability Tokens

Morphlings must receive a **scoped capability token** issued by Loopgate.

Tokens bind:

- task_id
- morphling_id
- allowed_paths
- capability set
- expiration time

Morphlings must not operate without a valid token.

In the current Loopgate architecture, capability tokens are server-issued opaque
credentials validated server-side and bound to signed request envelopes. This
RFC uses a task-centric schema to describe the target morphling scope model,
not a requirement that tokens become self-describing bearer blobs.

---

# Artifact Output

Morphlings must return structured artifacts.

Examples:

- patch files
- generated code
- reports
- analysis summaries
- test results

Artifacts must be written to:

```
/morph/home/outputs
```

Artifacts must not be automatically exported.

---

# Staging

All morphling outputs must be staged before promotion.

Example staging output:

```
/morph/home/outputs/patch.diff
```

Morph summarizes staged outputs for user review.

---

# Lifecycle

Morphlings follow a strict lifecycle:

```
Requested
→ Authorized
→ Spawned
→ Running
→ Produced artifacts
→ Staged
→ Reviewed
→ Approved / Rejected
→ Terminated
```

Morphlings must terminate after completion.

---

# Resource Limits

Morphlings must enforce resource limits:

- token budgets
- execution time limits
- sandbox disk quotas

Loopgate must terminate morphlings exceeding limits.

---

# Security Invariants

Morphlings must never:

- access `/morph` outside `/morph/home`
- mutate policy or configuration
- access provider secrets directly
- write to audit logs
- bypass Loopgate token checks
- modify trust or integration configs

Morphlings must not escalate privileges.

---

# Audit Requirements

Loopgate must record the following events:

- morphling spawn
- capability token issuance
- task completion
- artifact staging
- termination reason

These events must append to the audit ledger.

---

# Future Work

Possible future improvements:

- morphling cooperative workflows
- distributed morphlings
- adaptive resource scheduling
- sandbox snapshot rollback

These are explicitly **out of scope for v1**.

---

# Conclusion

Morphlings provide a safe execution model by separating **planning (Morph)** from **execution (Morphlings)** and **authority (Loopgate)**.

This architecture allows complex tasks to be executed while maintaining strict control over capabilities, filesystem scope, and side effects.
