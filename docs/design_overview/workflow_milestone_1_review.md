**Last updated:** 2026-03-24

# Workflow Milestone 1 Review

This document reviews the current codebase against
[Workflow Milestone 1](./workflow_milestone_1.md).

The goal is to answer three questions:

- what is already supported end-to-end
- what is only partially supported
- what the next smallest product gap is for each workflow

This is a product-fit review, not a roadmap for broader extractor or provider
surface area.

**Documentation note (2026-03-24):** Memory continuity and explicit-remember quality improved with TCL key normalization and anchor-aware supersession (master plan Phase 2). Milestone 1 “memory” gaps are narrower than when this review was first written, but **end-user phrasing** and **workflow polish** still apply. Re-run a product pass after dogfood (master plan Phase 4).

## 1. Current support map

| Workflow | Current fit | What is already real | Main remaining gap |
| --- | --- | --- | --- |
| Service Status Check | supported narrowly | `public_read` connections, site inspection + trust drafts, JSON nested extraction, HTML metadata extraction, direct operator client status planning, partial-success summaries, display-safe details | broader real-world source coverage and final answer polish under mixed outcomes |
| Repository / Issue Summary | supported narrowly | authenticated connections (`client_credentials`, `pkce`), `public_read`, typed provider-backed read capabilities, bounded JSON issue-list extraction, quarantine/provenance, direct operator client issues planning, partial-success summaries | one concrete end-user workflow validation pass plus richer answer shaping over real issue lists |
| Multi-Step Board / Work Queue Triage | partially supported | model prompt compilation, typed capability execution, explicit denials, multi-result summaries | stronger operator client-side orchestration and aggregation across multiple capability calls |
| Memory Continuity | partially supported | append-only ledger, explicit `memory_candidate` tagging, typed continuity events, bounded global wake state, startup wake-state loading, exact-key recall | clearer user-facing remembered-vs-fresh answer behavior inside real workflows |
| Safe Denial | supported in principle | policy denials, secret-export denial, fail-closed extraction, quarantine, display-safe CLI output | more end-to-end validation inside mixed workflows, not new kernel surface |

## 2. Workflow-by-workflow notes

### A. Service Status Check

Status checks are the strongest Milestone 1 workflow today.

What already exists:

- narrow `public_read` connection type
- checked-in example config:
  - `docs/setup/examples/public_status_github.yaml`
- runtime onboarding commands:
  - `/site inspect <url>`
  - `/site trust-draft <url>`
- deterministic JSON nested extraction for status fields
- narrow HTML metadata extraction when JSON is unavailable
- operator client-side capability-selection guidance for status requests
- direct narrow operator client status workflow planning for clear status-like requests
- operator client-side operator summary plus display-safe details

What is still missing:

- broader validation against a few real configured sources
- answer polish when one status source succeeds but a secondary check is denied
  or unavailable

Assessment:

- this is now the best real Milestone 1 demo path
- no broader HTML extraction is required yet

### B. Repository / Issue Summary

What already exists:

- secure Loopgate connection/auth flows
- `public_read` and authenticated provider-backed repo paths
- bounded JSON issue-list extraction
- quarantine/provenance and display-safe list rendering
- direct operator client issues workflow planning for clear repo/issues requests
- operator client partial-success/denial summaries

What is still missing:

- one real operator workflow validation pass against the checked-in example
  config
- final answer shaping tuned against larger but still bounded issue lists
- clearer distinction between "recent open issues" and anything inferred from
  them

Assessment:

- technically real
- still needs end-to-end workflow validation and product polish

### C. Multi-Step Board / Work Queue Triage

What already exists:

- typed capability execution
- explicit denials
- operator-facing aggregation of mixed tool outcomes

What is still missing:

- better operator client planning quality across multiple related capabilities
- workflow-aware aggregation over several successful/denied sub-steps
- one concrete board-style provider path

Assessment:

- kernel support exists
- shell-side orchestration remains the main gap

### D. Memory Continuity

What already exists:

- append-only local ledger
- explicit `memory_candidate` policy
- typed continuity events
- bounded global wake-state artifact
- startup wake-state loading
- exact-key historical recall
- explicit distinction between remembered continuity and fresh provider state in
  the planning model

What is still missing:

- more visible use of memory continuity inside live workflows
- answer formatting that consistently separates:
  - remembered context
  - derived continuity
  - newly checked provider state

Assessment:

- substrate is now strong
- the product gap is orchestration and answer behavior, not kernel semantics

### E. Safe Denial

What already exists:

- deny-by-default capability boundary
- explicit denial codes and reasons
- operator client partial-success summaries now keep denied sub-steps visible

What is still missing:

- broader workflow validation that denials remain helpful inside real
  multi-step tasks

Assessment:

- this is already a cross-cutting behavior
- no major kernel gap remains here

## 3. What the codebase is strongest at right now

The current system is strongest where the architecture is most explicit:

- Loopgate as sole privileged control plane
- typed capability execution
- quarantined remote content with explicit provenance/classification
- explicit operator flows for trust drafts, quarantine inspection, and pruning
- deterministic extractor contracts for:
  - structured JSON
  - markdown frontmatter
  - markdown section text
  - narrow HTML metadata
- operator client operator summaries for:
  - success
  - denial
  - partial success

## 4. What is still behind the product vision

The main product gaps are no longer “missing security model.”
They are mostly shell- and workflow-level:

- operator client still needs stronger orchestration over several related capability calls
- final answer shaping still needs more "what I checked / what I could not
  verify / whether this is partial" behavior
- repo/board workflows need real workflow validation, not broader extractor
  work
- memory continuity now needs visible product behavior more than new kernel
  primitives
- end-user setup should continue becoming easier without weakening explicit
  trust boundaries

## 5. Recommended next steps

Priority order:

1. Validate the public status-check workflow end-to-end against a few real
   configured sources.
2. Validate the repository/issues workflow end-to-end against the checked-in
   example path.
3. Improve operator client answer behavior so remembered vs fresh facts stay explicit
   inside real workflows.
4. Revisit board/work-queue orchestration only after 1–3 are behaving well.

Do not widen extraction beyond the current JSON + markdown + HTML metadata
surface unless one of those workflows is concretely blocked by that limit.
