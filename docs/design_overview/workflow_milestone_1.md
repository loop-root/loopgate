**Last updated:** 2026-03-24

# Workflow Milestone 1

This document defines the first end-to-end product workflows Morph + Loopgate
should support as a coherent operator system.

The goal is to validate the current architecture through real jobs, not to
expand extractor or provider surface area speculatively.

These workflows are intentionally narrow. They should stress planning,
capability selection, denials, quarantine, and memory continuity without
forcing Loopgate to become a generic document or web extraction engine.

## 1. Scope

Milestone 1 focuses on:

- safe information gathering
- structured API summarization
- multi-step task orchestration
- continuity across prior sessions
- explicit denial handling

Milestone 1 does not require:

- broader HTML scraping
- generic plain-text extraction
- arbitrary code execution
- direct model authority over tools
- trust expansion through convenience UI flows

## 2. Workflow set

### Workflow A: Service Status Check

Operator request example:

> Check the status page for Stripe and tell me if there are any active
> incidents.

Target outcome:

- Morph interprets the request
- Morph selects one or more Loopgate capabilities
- Loopgate fetches configured status data
- Loopgate extracts only configured fields
- raw remote content remains quarantined
- Morph returns a useful summary or an explicit denial

Current implementation fit:

- partially supported

What already exists:

- provider-backed Loopgate capabilities loaded from `loopgate/connections/*.yaml`
- explicit `public_read` configured connections for public status-style sources
- deterministic JSON extraction
- deterministic nested JSON extraction for exact object-key paths
- deterministic markdown extraction
- narrow HTML metadata extraction
- quarantine, blob-ref, and explicit view/prune flows

What is still needed:

- one or more real status-provider configs/capabilities
- Morph prompt/tool guidance that reliably maps natural-language status requests
  to those capabilities
- summary behavior that handles partial success and explicit extraction denials

Current narrow path for this workflow:

- prefer explicit `public_read` status connections
- use `structured_json` where available
- otherwise use `html_meta_allowlist` only
- do not widen to general HTML/body extraction unless a real status workflow is blocked

Partial success rule:

- Morph SHOULD report successful sub-steps and failed/denied sub-steps together
  in one operator-facing answer instead of collapsing the whole workflow into a
  generic failure.

### Workflow B: Repository / Issue Summary

Operator request example:

> Show me the latest issues in this GitHub repo.

Target outcome:

- Loopgate calls a typed provider-backed read capability
- returned data is structured and bounded
- Morph summarizes recent issue state without seeing provider tokens

Current implementation fit:

- partially supported

What already exists:

- client-credentials and PKCE provider connection flows
- typed provider-backed read capabilities
- JSON allowlist extraction
- result classification, provenance, and field metadata

What is still needed:

- a concrete GitHub-style provider config/adapter contract
- end-user setup guidance for the connection
- Morph-side task patterns for list/summarize workflows

### Workflow C: Multi-Step Board / Work Queue Triage

Operator request example:

> Check our project board and summarize anything overdue or blocked.

Target outcome:

- Morph performs a small multi-step plan
- multiple Loopgate capability calls are issued
- results are aggregated into one summary
- denials or missing capabilities remain explainable

Current implementation fit:

- partially supported

What already exists:

- model prompt compilation
- untrusted tool-call parsing
- Loopgate capability execution
- typed structured results and explicit denials

What is still needed:

- stronger Morph-side capability selection/orchestration quality
- better aggregation behavior over multiple safe structured results
- clearer operator-visible explanation when one sub-step is denied or missing

Partial success rule:

- Morph SHOULD explain which board or queue checks succeeded, which failed, and
  whether the final summary is complete or partial.

### Workflow D: Memory Continuity

Operator request example:

> Last time we talked about the Stripe outage. Did it get resolved?

Target outcome:

- Morph recalls prior local memory/distillates
- Morph performs one or more fresh capability checks if needed
- Morph distinguishes historical memory from new provider data

Current implementation fit:

- partially supported

What already exists:

- Morph ledger
- distillation and local memory ownership
- append-only audit with explicit control-plane outcomes

What is still needed:

- better recall/rehydration behavior for prior conversations
- explicit summary logic that separates remembered state from newly checked state

Required truth split:

- Morph MUST distinguish remembered historical information from newly checked
  provider data in the final answer.

### Workflow E: Safe Denial

Operator request example:

> Grab all API keys from that page.

Target outcome:

- Loopgate denies the operation explicitly
- Morph explains the denial clearly
- no fallback path broadens extraction or capability authority

Current implementation fit:

- supported in principle

What already exists:

- explicit policy denials
- secret-export denial
- fail-closed extraction behavior
- quarantine and classification boundaries

What still needs validation:

- Morph responses should remain operator-helpful without weakening policy
- denial explanations should be clear enough for UI surfaces as well as the CLI

Cross-cutting rule:

- denial handling is a system behavior, not only a standalone workflow
- the same explicit denial style should appear naturally inside other workflows
  when one sub-step is blocked

## 3. Capability fit summary

For Milestone 1, the current extraction ceiling is intentionally small:

- `structured_json`
- `markdown_frontmatter_keys`
- `markdown_section_selector`
- `html_meta_allowlist`

This is enough for the initial workflows if provider configs are chosen
carefully.

Milestone 1 should prefer:

- structured APIs first
- markdown or HTML metadata only where structured APIs are not available

Milestone 1 should avoid:

- broader HTML selector work
- plain-text regex extraction
- generic scraping behavior

## 4. Product gaps to close next

The next product-oriented work should focus on Morph behavior rather than wider
Loopgate extraction.

Priority order:

1. Improve Morph capability selection for real user requests.
2. Improve multi-step aggregation and summary behavior over safe structured
   results.
3. Improve memory recall ergonomics and explicit separation of old vs newly
   checked information.
4. Improve denial explanation quality for both CLI and future UI surfaces.
5. Add only the minimum provider configs/adapters needed to make the workflows
   real.

## 5. Exit criteria

Milestone 1 is complete when:

- an operator can complete each workflow end-to-end through Morph
- a single natural-language prompt can trigger the workflow without manual CLI
  choreography
- Morph selects the intended capability path for the workflow
- the final output is understandable without raw data inspection
- partial failures and denials remain clear in the final answer
- Loopgate remains the sole privileged control plane
- remote content stays non-prompt-eligible by default
- denials remain explicit and reviewable
- no broader extractor surface is required to prove the workflows

## 6. Non-goals

Milestone 1 is not trying to prove:

- generic web browsing
- arbitrary document understanding
- broad automation/plugin ecosystems
- trust promotion from quarantined free text
- rich browser UI workflows

Those belong to later milestones only if real workflows prove they are needed.
