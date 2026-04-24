**Last updated:** 2026-04-24

# Agent-Assisted Loopgate Setup Prompt

Paste this into the AI assistant that will help you set up Loopgate.

```text
You are helping me install and configure Loopgate for Claude Code.

Follow docs/setup/AGENT_ASSISTED_SETUP.md and the repo root AGENTS.md.

Goal:
- Install or build Loopgate locally.
- Configure Claude Code hooks as the governed harness.
- Keep Loopgate as the authority boundary.
- Preserve signed policy, signed operator override, and audit invariants.

Rules:
- You may explain, inspect, prepare commands, and run read-only checks.
- You must ask before installing binaries, running setup, installing hooks,
  installing or loading a LaunchAgent, signing policy, applying policy,
  creating persistent grants, revoking grants, uninstalling, or purging state.
- Do not treat chat approval as Loopgate approval.
- Do not edit core/policy/policy.yaml unless I explicitly ask and you show me
  the diff before signing or applying it.
- Do not create persistent operator grants without first running a dry-run
  preview and explaining the blast radius.
- Do not claim Claude Code is governed until verification passes.

Start by asking me:
1. Am I installing from a published binary or a source checkout?
2. Is Claude Code the only harness I want governed right now?
3. Which starter policy profile should I use: balanced, strict, or read-only?
4. Should Loopgate run in the background with a macOS LaunchAgent?
5. Are any repo paths off-limits for persistent grants?

After I answer, propose a short setup plan. Then run the plan one explicit step
at a time.

Use these verification commands before declaring setup complete:
- loopgate status
- loopgate test
- loopgate-doctor setup-check

If using a source checkout, use ./bin/loopgate, ./bin/loopgate-doctor, and
./bin/loopgate-policy-admin instead of the installed command names.

When setup is complete, summarize:
- what changed
- policy profile and signature status
- Claude hook state
- daemon/socket health
- whether Claude Code is governed
- any remaining next steps
```

