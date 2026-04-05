# Model Map

This file maps the model integration layer under `internal/model/`.

Use it when changing:

- native structured tool definitions
- model request/response types
- native tool allowlist growth
- prompt adaptation between runtime and provider

## Core Role

`internal/model/` is the provider-facing abstraction layer.

Its main job here is converting tool registry schemas into native tool definitions the model can actually call when an operator client builds a request.

## Key Files

- `types.go`
  - shared model request and response types
- `toolschema.go`
  - native tool allowlist
  - conversion from `internal/tools` schemas to provider tool schemas
  - important for keeping registry-backed native tools visible to the model
  - also filters native tool defs down to the actually granted capability set when a client assembles a model request
- `toolschema_test.go`
  - regression coverage for native tool eligibility and schema shape
- `prompt_adapter.go`
  - converts internal conversation/tool structures into prompt-layer types
- `anthropic/` and `openai/`
  - provider implementations consuming compiled prompts and native tool defs

## Current Constraint

`toolschema.go` currently mirrors the existing flat tool-schema contract.
If a tool needs nested arrays or objects, the current builder cannot express it.

That is why new tools on the native schema path should start with simple flat args.

## Current Sprint Focus

The current working set in this directory is:

- `toolschema.go`
- `toolschema_test.go`

These files must change together with the tool registry and client-injected runtime facts so every operator surface sees the same capability truth.

The newest examples are `memory.remember` and `todo.*`:

- they must exist in `internal/tools`
- they must be allowed here
- they must be described honestly in the injected runtime facts used by local chat surfaces
- and Loopgate still remains the authority for execution

## Important Watchouts

- Adding a tool to the native allowlist does not grant authority by itself.
- Keep native tool definitions aligned with the real registry and real execution path.
- Native tool definitions should stay aligned with the granted Loopgate capability set, not just the sandbox registry.
- When the allowlist changes, update tests in the same pass.
