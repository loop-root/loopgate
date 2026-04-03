**Last updated:** 2026-04-01

# Docs Index

**Product:** **Loopgate** is the primary system in this repository (governance engine, control plane). **Haven** (Swift, separate repo) is the consumer demo client. **Morphlings** are Loopgate-governed workers. **Public vs local-only:** [DOCUMENTATION_SCOPE.md](./DOCUMENTATION_SCOPE.md) — including gitignored **`AGENTS.md`**, **`docs/superpowers/`**, and optional **`context_map.md` / `*_map.md`** copies maintained locally for GitHub hygiene.

**Haven OS docs and the name “Morph”:** In **`docs/HavenOS/`**, *Morph* usually means the **in-app assistant persona** in the consumer demo, not this repository’s product name. Read **[`docs/HavenOS/README.md`](./HavenOS/README.md)** before “fixing” that wording.

**Maintainer archives (not in clone):** some long-form narratives live under **`~/Dev/projectDocs/morph/`**.

## Start here

- [Setup](./setup/SETUP.md)
- [**Loopgate HTTP API (local clients / Swift Haven)**](./setup/LOOPGATE_HTTP_API_FOR_LOCAL_CLIENTS.md) — Unix-socket HTTP, session open, signing, route inventory
- [Tool Usage](./setup/TOOL_USAGE.md)
- [Secrets](./setup/SECRETS.md)
- **Current priorities:** enterprise integration (MCP, proxy, `tenant_id`, admin console) — see root `AGENTS.md` and [Roadmap](./roadmap/roadmap.md)

## Architecture

- [Goals](./design_overview/goals.md)
- [Philosophy](./design_overview/philosophy.md)
- [Architecture](./design_overview/architecture.md)
- [Continuity Stream Architecture](./design_overview/continuity_stream_architecture.md)
- [How It Works](./design_overview/how_it_works.md) — reference **Wails** path in-repo; Swift Haven is canonical
- [**AMP (protocol)**](./AMP/README.md) — vendored RFCs, mapping, conformance
- [Integration Test Plan](./design_overview/integration_test_plan.md)
- [Operator planning model](./design_overview/morph_planning_model.md) *(file name historical)*
- [Workflow Milestone 1](./design_overview/workflow_milestone_1.md)
- [Loopgate](./design_overview/loopgate.md)
- [System Contract](./design_overview/systems_contract.md)
- [UI Surface Contract](./design_overview/ui_surface_contract.md)
- [Threat model](./loopgate-threat-model.md)
- [Roadmap](./roadmap/roadmap.md)

See `design_overview/architecture.md` for system architecture and control-plane design.

AMP documents are **vendored under `docs/AMP/`**; sync from a standalone AMP checkout when the neutral spec changes.

## RFCs

- [RFC 0001: Loopgate Token and Request Integrity Policy](./rfcs/0001-loopgate-token-policy.md)
- [RFC 0002: Delegated Session Refresh and Pipe Contract](./rfcs/0002-delegated-session-refresh.md)
- [RFC 0003: Loopgate Content Extraction, Provenance, and Taint Policy](./rfcs/0003-loopgate-content-extraction-and-taint.md)
- [RFC 0004: Quarantine Promotion and Retention](./rfcs/0004-quarantine-promotion-and-retention.md)
- [RFC 0005: Promotion Target Eligibility](./rfcs/0005-promotion-target-eligibility.md)
- [RFC 0006: Bounded Scalar Subclasses](./rfcs/0006-bounded-scalar-subclasses.md)
- [RFC 0007: Blob-Ref Dereference and View Semantics](./rfcs/0007-blob-ref-dereference-and-view.md)
- [RFC 0008: Selector Schema and Extractor Contracts](./rfcs/0008-selector-schema-and-extractor-contracts.md)
- [RFC 0009: Memory Continuity, Wake State, and Recall](./rfcs/0009-memory-continuity-and-recall.md)
- [RFC 0010: Memory Candidate Eligibility and Wake State Policy](./rfcs/0010-memory-candidate-eligibility-and-wake-state-policy.md)
- [RFC 0011: Swappable Memory Backends and Benchmark Harness](./rfcs/0011-swappable-memory-backends-and-benchmark-harness.md)
- [RFC 0012: Haven Scheduler and Background Agent Execution](./rfcs/0012-haven-scheduler-and-background-agent-execution.md)
- [RFC 0013: Continuity/TCL Storage and Query Backend](./rfcs/0013-continuity-tcl-storage-and-query-backend.md)
- [RFC 0014: TCL Conformance and Conservative Anchor Freeze](./rfcs/0014-tcl-conformance-and-anchor-freeze.md)

## Benchmarks

- [Haven memory plan & Phase D checklist](./HavenOS/Haven_Memory_Candidates_and_Loopgate_Plan.md)
- [Memorybench In Plain English](./memorybench_plain_english.md)
- [Memorybench Glossary](./memorybench_glossary.md)
- [Memorybench Benchmark Guide](./memorybench_benchmark_guide.md)
- [Memorybench Running Results](./memorybench_running_results.md)
- [RFC 0011](./rfcs/0011-swappable-memory-backends-and-benchmark-harness.md)

Internal methodology narrative (maintainer copy): `~/Dev/projectDocs/morph/memorybench-internal/memorybench_internal_report.md`.

## AMP Docs (neutral protocol)

Prefer the **[AMP README](./AMP/README.md)**. Quick links:

- [Implementation mapping](./AMP/design_overview/amp_implementation_mapping.md)
- [RFC 0001 — Local transport](./AMP/AMP-RFCs/0001-local-transport-profile.md) · [RFC 0004 — Canonical envelope](./AMP/AMP-RFCs/0004-canonical-envelope-and-integrity-binding.md)
- [RFC 0008 — Gaps and assumptions](./AMP/AMP-RFCs/0008-open-issues-gaps-and-assumptions.md)
- [local-uds-v1 conformance checklist](./AMP/conformance/local-uds-v1-checklist.md)

## Product RFCs (`RFC-MORPH-*` — stable IDs)

**Folder:** [`docs/product-rfcs/`](./product-rfcs/README.md). The **`RFC-MORPH-*`** prefix is **legacy naming**; specs describe **Loopgate**, **Haven**, sandbox, and **morphlings**.

- [RFC-MORPH-0001 — Haven operator client](./product-rfcs/RFC-MORPH-0001:%20Haven%20operator%20client%20architecture.md)
- [RFC-MORPH-0002 — Morphling task schema](./product-rfcs/RFC-MORPH-0002:%20Morphling%20Task%20Schema.md)
- [RFC-MORPH-0003 — Loopgate capability token model](./product-rfcs/RFC-MORPH-0003:%20Loopgate%20Capability%20Token%20Model.md)
- [RFC-MORPH-0004 — Sandbox filesystem policy (Loopgate-enforced)](./product-rfcs/RFC-MORPH-0004:%20Sandbox%20Filesystem%20Policy.md)
- [RFC-MORPH-0005 — Continuity & memory](./product-rfcs/RFC-MORPH-0005:%20Continuity%20and%20Memory%20Model.md)
- [RFC-MORPH-0006 — Approval & promotion (Loopgate)](./product-rfcs/RFC-MORPH-0006:%20Approval%20&%20Promotion%20Flow.md)
- [RFC-MORPH-0007 — Sandbox & morphling implementation plan](./product-rfcs/RFC-MORPH-0007:%20Sandbox%20&%20Morphling%20Implementation%20Plan.md)
- [RFC-MORPH-0008 — Morphling class schema & lifecycle](./product-rfcs/RFC-MORPH-0008:%20Morphling%20Class%20Schema%20and%20Lifecycle%20State%20Machine.md)
- [RFC-MORPH-0009 — Loopgate control plane architecture](./product-rfcs/RFC-MORPH-0009:%20Loopgate%20control%20plane%20architecture.md)

## Historical reports (local-only)

Point-in-time reviews under `docs/reports/` are **gitignored** when that rule is active (see [DOCUMENTATION_SCOPE.md](./DOCUMENTATION_SCOPE.md)). For current intent prefer [Threat model](./loopgate-threat-model.md), [Loopgate](./design_overview/loopgate.md), and numbered RFCs.
