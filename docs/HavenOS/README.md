**Last updated:** 2026-04-01

# Haven OS product docs (`docs/HavenOS/`)

This folder holds **consumer-demo / Swift Haven** product intent: desktop shell, capabilities, host access, roadmaps, and security checklists for the **Haven** app (separate repository, e.g. `~/Dev/Haven`).

**Repository product name:** The **primary** system implemented in **this** repo is **Loopgate**. These docs describe how **Haven** presents and consumes Loopgate; they are not the enterprise MCP/proxy/admin narrative (see root `AGENTS.md` and `docs/roadmap/roadmap.md`).

## “Morph” in this folder vs Loopgate branding

In **Haven-facing** copy and UX specs under `docs/HavenOS/`, **Morph** often names the **in-app assistant persona** the user talks to (“what Morph is doing”, rail chrome, etc.). That is **intentional product language** for the consumer demo, **not** the name of this repository or the primary ship target.

When editing **engineering** or **cross-repo** docs outside `docs/HavenOS/`, prefer **Loopgate** (product), **Haven** (operator client), and **morphlings** (workers). Do not reinterpret Haven OS lines that describe the assistant as “wrong” branding—they describe **persona**, not repo identity.

## Entry points

- North star / MVP: `HavenOS_Northstar.md`, `MVP Experience Spec.md`
- Security + transport: `Haven_Loopgate_Security_and_Transport_Checklist.md`, `Haven_Loopgate_Local_Control_Plane_Posture.md`
- Capabilities: `Loopgate Capability System.md`, `App Surface and Capability Taxonomy.md`
- Wails **reference** shell map (in-repo): `Haven_Frontend_Source_Map.md`

See also `docs/docs_map.md` and (if present locally) `context_map.md`.
