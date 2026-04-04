**Last updated:** 2026-04-03

# RFC 0012: Operator-client scheduling and background agent execution

Status: draft

## 1. Summary

The **operator experience** (IDE, MCP host, or other unprivileged client) should become more capable of multi-step work without moving authority out of Loopgate.

This RFC proposes:

- the **operator client** owns job scheduling, retries, checkpoints, and background progress UX
- Loopgate remains the authority for approvals, leases, capabilities, secrets,
  sandbox mediation, and audit
- worker processes execute only through Loopgate-issued scoped authority
- the model proposes work, but never directly holds authority

This is the intended path from “interactive assistant with bounded loops” to
“durable local agent that can keep working safely.”

## 2. Motivation

Today the system is mostly foreground and request-driven:

- the operator client runs a bounded tool loop during a chat turn
- Loopgate remains synchronous and request-driven
- `run_in_background` is only a stored client preference today
- morphlings and task plans prove mediated worker execution, but not yet a
  durable background scheduler

That is a good safety baseline, but it does not yet feel like an agent that
can carry work forward.

## 3. Goals

- let work continue across multiple steps and over time
- support pause/resume/retry after app restart
- preserve explicit approvals and policy checks
- keep Loopgate simple, synchronous, and authoritative
- prevent the model from directly owning tokens or permissions

## 4. Non-goals

This RFC does not:

- turn Loopgate into an autonomous daemon brain
- give the model open-ended reusable authority
- expose morphlings as a public API
- allow silent background execution without auditable state
- remove user approvals for actions that still require approval

## 5. Core principle

The correct split is:

- model proposes
- operator client schedules
- Loopgate authorizes
- workers execute through Loopgate
- operator client renders progress and manages resumption

The trusted scheduler is the **operator client**, not the model.

## 6. Why the operator client should own scheduling

This fits the existing architecture:

- the operator client already owns UX, goals, and local session continuity
- Loopgate is explicitly the control plane, not the planner of all user work
- background orchestration needs retries, inboxing, and checkpoint UX
- those are client concerns, not kernel concerns

Loopgate should issue narrow authority for a step.
The operator client should decide when to request that authority, when to pause, and when to
ask the user for more input.

## 7. Authority model

The model never receives authority-bearing material directly.

Correct flow:

1. the model proposes a plan or next step
2. the operator client converts that into structured requests
3. Loopgate validates policy, approval state, and scope
4. Loopgate issues one of:
   - approval requirement
   - task lease
   - morphling worker launch/session
   - scoped capability token where appropriate
5. the operator client passes the scoped authority only to trusted local worker code
6. workers call back into Loopgate for actual execution

Important rule:

- the token/lease/session is for the operator client and worker processes
- it is not a model-owned key

## 8. Proposed runtime objects

### 8.1 Client-owned scheduler objects

Suggested shapes:

```go
type SchedulerJob struct {
	JobID               string
	GoalText            string
	Scope               string
	Status              string // queued|planning|awaiting_approval|ready|running|paused|completed|failed|cancelled
	CurrentRunID        string
	CreatedAtUTC        time.Time
	UpdatedAtUTC        time.Time
	LastCheckpointRef   string
	OutstandingPrompt   string
}

type SchedulerRun struct {
	RunID               string
	JobID               string
	Status              string // planning|running|awaiting_approval|blocked|completed|failed
	PlannedStepCount    int
	CompletedStepCount  int
	CreatedAtUTC        time.Time
	UpdatedAtUTC        time.Time
}

type StepAttempt struct {
	AttemptID           string
	RunID               string
	StepIndex           int
	Status              string // queued|leased|running|completed|failed|expired
	LeaseRef            string
	CheckpointRef       string
	CreatedAtUTC        time.Time
	UpdatedAtUTC        time.Time
}
```

These are **operator-client** orchestration records.
They are not authority records.

### 8.2 Loopgate-owned authority objects

Loopgate continues to own:

- approvals
- capability tokens
- task leases
- morphling lifecycle records
- worker sessions
- audit events

## 9. Execution flow

### 9.1 Planning

1. user creates or accepts a goal
2. the operator client asks the model for a structured plan or next step
3. the operator client submits the structured plan to Loopgate
4. Loopgate validates allowed capability set and approval requirements

### 9.2 Authorization

If approval is required:

1. Loopgate creates the approval
2. the operator client marks the job `awaiting_approval`
3. background execution stops cleanly until the user approves

If approval is not required:

1. Loopgate issues a task lease or worker session
2. the operator client marks the step `ready`

### 9.3 Execution

1. the operator client spawns a trusted local worker or morphling process
2. worker receives only:
   - lease/session material
   - scoped input
   - checkpoint references
3. worker executes through Loopgate only
4. Loopgate mediates capabilities, secrets, and sandbox
5. worker reports status/progress back through Loopgate
6. the operator client records checkpoints and updates UI

### 9.4 Completion

1. worker completes the step
2. Loopgate finalizes the lease/session state
3. the operator client advances the job
4. if more steps remain, repeat planning/authorization/execution
5. otherwise mark completed and surface the result

## 10. Background modes

The scheduler should support three modes:

### 10.1 Foreground assisted

- user is present
- the operator client drives the loop actively in the current thread

### 10.2 Background allowed

- standing approval or safe class permits execution
- the operator client may continue work without a fresh user tap for each step

### 10.3 Background paused

- approval is needed
- ambiguity is too high
- worker failed and needs human input

This is the state that makes the experience feel agentic without pretending the client has
infinite autonomy.

## 11. Checkpoints and restart recovery

The operator client should persist scheduler state so it can recover after restart.

Required checkpoint content:

- current plan/run id
- completed steps
- latest checkpoint ref
- whether approval is pending
- whether a lease expired mid-step
- last user-visible summary

On restart:

- the operator client reloads scheduler jobs
- queries Loopgate for active authority state
- reconciles incomplete runs
- either resumes, re-plans, or marks blocked

## 12. Worker model

Workers should remain boring.

Recommended worker properties:

- one job or one step at a time
- scoped authority only
- no secret persistence
- no direct filesystem authority beyond Loopgate-mediated paths
- no model-owned self-expansion of scope

Good execution substrates:

- local subprocess worker
- morphling-runner style worker
- future supervised worker helper

Not recommended:

- long-lived autonomous Loopgate goroutines
- model-controlled arbitrary subprocess trees

## 13. Relationship to morphlings and task plans

This RFC does not replace morphlings or task plans.
It gives them a home.

Suggested role split:

- the operator client’s scheduler owns job/run lifecycle
- task plans are Loopgate-validated structured execution envelopes
- morphlings/workers are the bounded execution substrate for a step

In other words:

- scheduler = orchestration
- task plan = approved structured intent
- morphling/worker = execution container
- Loopgate = authority boundary

## 14. Security invariants

This design must preserve:

- no natural-language authority
- no model-owned permission expansion
- explicit approval when required
- no public morphling API
- auditable step execution
- session-bound worker authority
- fail-closed behavior on lease/session/audit failure

## 15. UX consequences

If this is implemented correctly, operators should see:

- jobs carried forward across steps
- interrupted work resumed
- clean pauses for approval
- clear “waiting on …” states
- multi-step tasks without restarting from zero each turn

But it should still feel governable because:

- approvals remain explicit
- background progress is visible
- user can pause/cancel
- Loopgate remains the choke point

## 16. Rollout order

1. define client scheduler records and persistence
2. wire scheduler state into status surfaces
3. route one narrow class of jobs through task plans and leased workers
4. add checkpoint/resume after restart
5. add standing-approval-aware background continuation
6. expand carefully to more job classes
