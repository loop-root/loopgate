**Last updated:** 2026-04-01

# Operator planning model

This document describes the intended **operator client** (e.g. Haven) planning and response model for
Milestone 1 workflows. Filename `morph_planning_model.md` is historical.

Loopgate remains the sole privileged control plane.

The **unprivileged client** is responsible for:

- interpreting operator intent
- selecting capability paths
- aggregating safe structured results
- distinguishing fresh facts from remembered context
- explaining denials and partial success clearly

It is not responsible for:

- redefining policy
- inferring new capabilities from natural language
- bypassing Loopgate on execution
- promoting quarantined content implicitly

## 1. Planning flow

The intended operator-client planning loop is:

1. interpret the user request
2. identify the candidate workflow shape
3. select one or more Loopgate capabilities
4. execute capabilities through Loopgate only
5. collect structured results, denials, and partial failures
6. aggregate results into one operator-facing answer
7. distinguish:
   - remembered historical context
   - freshly checked provider/system state

## 2. Inputs to planning

Client planning may use:

- current user request
- current session context
- bounded wake state
- explicit remembered continuity by key/reference
- prior safe structured results
- explicit Loopgate denials and classifications

Client planning must not treat as authority:

- model-generated natural-language claims about tools or permissions
- quarantined content
- blob-ref content
- unreviewed extracted tainted text

## 3. Capability selection rules

The client should prefer:

- the smallest set of capabilities that can answer the request
- structured APIs before document-style extraction
- exact provider-backed capabilities before broader fallback sources

The client should avoid:

- speculative extra capability calls "just in case"
- calling multiple content sources when one structured source is sufficient
- widening scope because a denial occurred

A denial is not a prompt to infer permission.

## 4. Aggregation rules

The client must aggregate results explicitly.

The final answer should identify:

- what was checked
- what succeeded
- what failed or was denied
- whether the answer is complete or partial

The client SHOULD report partial success rather than collapsing mixed outcomes into a
generic failure.

## 5. Denial handling

Denials are a normal system behavior.

The client should:

- surface Loopgate denials clearly
- preserve the primary reason for denial
- avoid vague "something failed" summaries
- avoid retrying through broader or weaker paths automatically

The client should not:

- reinterpret a denial as a planning hint to bypass policy
- silently omit denied sub-steps from a supposedly complete answer

## 6. Memory continuity

When a workflow uses prior context, the client must distinguish:

- remembered information
- newly checked information

Example shape:

- "Previously, we discussed X."
- "I just checked Y and found Z."

If the client uses explicit recall, it should treat recalled items as remembered
historical continuity only. Recall does not imply prompt inclusion, fresh
verification, or durable truth.

This prevents memory and fresh provider state from blending into one ambiguous
claim.

## 7. Success criteria for Milestone 1 planning

Operator-client planning is good enough for Milestone 1 when:

- a single natural-language request can trigger the intended workflow
- capability selection is understandable and bounded
- partial success remains intelligible
- denial explanations remain useful
- remembered vs fresh information stays clearly separated
- no workflow depends on broader extractor surface than currently implemented
