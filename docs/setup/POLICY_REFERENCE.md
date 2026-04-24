# Policy reference

**Last updated:** 2026-04-20

This page is the operator-facing reference for `core/policy/policy.yaml`.

Use it when you need to answer:

- what each policy block controls
- which fields are strict versus optional
- which defaults fail closed
- where the current policy model is intentionally narrow

For the signed-policy workflow, see [POLICY_SIGNING.md](./POLICY_SIGNING.md).
For a plain-language summary of the active signed policy, use:

```bash
./bin/loopgate-policy-admin explain
```

## Parsing and trust model

- `core/policy/policy.yaml` is the checked-in signed policy source.
- `core/policy/policy.yaml.sig` is a detached Ed25519 signature file.
- Loopgate startup fails closed if the signature file is missing, malformed, or does not verify.
- YAML parsing is strict: unknown fields are rejected.
- Policy defaults are conservative: enabling a surface without the required allowlist still fails.

## Top-level schema

```yaml
version: 0.1.0
tools: ...
operator_overrides: ...
logging: ...
safety: ...
```

## `tools.claude_code`

Controls Claude Code built-in tool governance before execution.

Fields:

- `deny_unknown_tools`
  - `true` means Claude tool names not declared in the builtin support set are denied.
- `tool_policies`
  - keyed by supported Claude tool name:
    - `Bash`
    - `Read`
    - `Write`
    - `Edit`
    - `MultiEdit`
    - `Glob`
    - `Grep`
    - `WebFetch`
    - `WebSearch`

Per-tool fields:

- `enabled`
- `requires_approval`
- `allowed_roots`
- `denied_paths`
- `allowed_domains`
- `allowed_command_prefixes`
- `denied_command_prefixes`

Notes:

- `allowed_roots` and `denied_paths` apply to filesystem-shaped Claude tools.
- `allowed_domains` applies to web-fetch shaped tools.
- `allowed_command_prefixes` and `denied_command_prefixes` apply only to `Bash`.
- Tool-specific values narrow or override the category defaults where the current hook surface supports it.

## `tools.mcp_gateway`

Controls the governed MCP broker surface.

Fields:

- `deny_unknown_servers`
- `servers`

Each `servers.<name>` entry supports:

- `enabled`
- `requires_approval`
- `transport`
  - currently only `stdio`
- `launch.command`
- `launch.args`
- `working_directory`
- `allowed_environment`
- `secret_environment`
- `tool_policies`

Each `tool_policies.<tool>` entry supports:

- `enabled`
- `requires_approval`
- `required_arguments`
- `allowed_arguments`
- `denied_arguments`
- `argument_value_kinds`

Current argument-value kinds:

- `string`
- `number`
- `boolean`
- `object`
- `array`
- `null`

## `tools.filesystem`

Controls Loopgate filesystem capabilities and the current host-path mediation surface.

Fields:

- `allowed_roots`
- `denied_paths`
- `read_enabled`
- `write_enabled`
- `write_requires_approval`

Important current limitation:

- host-category tools currently reuse `tools.filesystem.*`
  enablement and approval flags
- there is not yet a separate `tools.host` block

That means host-folder access docs must describe the coupling explicitly instead of implying an independent host policy surface.

## `tools.http`

Controls Loopgate-mediated HTTP fetch capability.

Fields:

- `enabled`
- `allowed_domains`
- `requires_approval`
- `timeout_seconds`

Notes:

- an empty `allowed_domains` list means domain fetches deny by default
- `timeout_seconds <= 0` normalizes to `10`

## `tools.shell`

Controls Loopgate shell execution capability.

Fields:

- `enabled`
- `allowed_commands`
- `requires_approval`

Notes:

- `allowed_commands` must be explicitly populated when shell is enabled
- Loopgate executes argv directly rather than going through a shell parser
- shell control operators and ambient expansion are intentionally unavailable in the governed path

## `logging`

Controls audit detail level, not whether must-persist security events exist.

Fields:

- `log_commands`
- `log_tool_calls`
- `audit_detail.hook_projection_level`
  - `full`
  - `minimal`

`minimal` reduces preview noise, but it does not remove mandatory audit events for approvals, denials, execution, or integrity checkpoints.

## `operator_overrides`

Declares which bounded operator-created exceptions the parent policy may allow.

Current v0.2 behavior:

- this block does **not** widen current tool access by itself
- absent or unconfigured classes default fail-closed to `none`
- permanent operator grants must be explicitly created, validated,
  signed, and hot-applied before they can affect the effective policy
- the current CLI supports permanent path-scoped grants only when the parent
  class has `max_delegation: persistent`
- session-scoped approvals are harness-owned; they are not written into the
  durable operator override document

Fields:

- `classes`
  - keyed by supported action class name:
    - `repo_read_search`
    - `repo_edit_safe`
    - `repo_write_safe`
    - `repo_bash_safe`
    - `web_access_trusted`

Each `classes.<name>` entry supports:

- `max_delegation`
  - `none`
  - `session`
  - `persistent`

Semantics:

- `none`
  - the parent policy does not delegate this action class to local operator exceptions
- `session`
  - the parent policy may allow future session-scoped operator exceptions for this class
- `persistent`
  - the parent policy may allow future permanent operator grants for this class
  - `persistent` is the serialized policy value; operator-facing CLI output
    calls this grant scope `permanent`

Permanent path-scoped grant command:

```bash
./bin/loopgate-policy-admin grants add repo_edit_safe -path docs -dry-run
./bin/loopgate-policy-admin grants add repo_edit_safe -path docs
./bin/loopgate-policy-admin grants revoke <grant-id>
```

Supported path-scoped classes are `repo_read_search`, `repo_edit_safe`,
`repo_write_safe`, and `repo_bash_safe`. `web_access_trusted` is not path-scoped
in the current CLI.

Important limitation:

- this is intentionally a constrained class-based declaration, not a second arbitrary policy tree
- the parent policy remains the authority boundary
- hard-denied or non-delegable classes must stay non-overridable even after a delegated override flow is introduced

## `safety`

Explicit high-risk authoring toggles.

Fields:

- `allow_persona_modification`
- `allow_policy_modification`

These should usually stay `false`.

## Starter policy guidance

The checked-in `core/policy/policy.yaml` is an intentionally strict starter policy for a fresh public checkout:

- Claude filesystem reads are allowed within the repo root
- Claude writes and edits require approval
- shell, HTTP, and governed MCP start disabled
- policy, persona, runtime state, `.git`, and Claude settings mutation stay denied

If you want a more permissive local-development starting point, render a reviewed template first:

```bash
./bin/loopgate-policy-admin render-template -preset balanced
```

Available starter profiles:
- `strict`
  - higher-sensitivity starter profile
  - repo reads and search stay open
  - Claude `Write`, `Edit`, and `MultiEdit` require approval
  - Bash and HTTP stay disabled
  - only `repo_edit_safe` is delegated, and only up to `session`
- `balanced`
  - recommended daily-driver for local engineering work
  - Claude `Read`, `Glob`, `Grep`, `Edit`, and `MultiEdit` are allowed inside the repo root
  - Claude `Write` and allowed Bash commands require approval
  - HTTP stays disabled
  - `repo_edit_safe` can receive permanent grants; `repo_write_safe` and `repo_bash_safe` are session-only
- `read-only`
  - lowest-friction evaluation profile
  - Claude `Read`, `Glob`, and `Grep` are allowed inside the repo root
  - Claude `Write`, `Edit`, and `MultiEdit` stay disabled
  - Bash and HTTP stay disabled
  - no delegated operator override classes are enabled

Experimental manual template:
- `developer`
  - broader development shell commands plus HTTP enabled; not part of the supported guided v1 setup path

Then inspect, sign, and apply the chosen profile through the normal signed-policy workflow, or use `./bin/loopgate setup` for the guided path.
