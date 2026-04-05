# Prompt Map

This file maps the prompt assembly layer under `internal/prompt/`.

Use it when changing:

- system prompt structure
- self-description rules
- tool-use instructions
- runtime contract language

## Core Role

`internal/prompt/` turns persona, policy, runtime facts, conversation, and tool metadata into the system instruction seen by the model provider.

This is where product truth and model truth either align or drift.

## Key Files

- `compiler.go`
  - builds the system instruction
  - defines sections such as:
    - `RUNTIME CONTRACT`
    - `AVAILABLE TOOLS`
    - `SELF-DESCRIPTION RULES`
    - `TOOL USE` / `TOOL CALL PROTOCOL`
- `compiler_test.go`
  - prompt contract tests
  - first place to update when changing self-description or runtime language

## Relationship Notes

Legacy shell behavior does not define the active product path.
Operator clients should inject runtime facts and available tools through their own assembly layer; prompt behavior here should stay Loopgate-agnostic.

That means prompt fixes often require coordinated edits across:

- `internal/prompt/compiler.go`
- `internal/model/toolschema.go`

## Current Sprint Focus

The current prompt risk is simple:

- if registry-backed native tools grow faster than runtime facts, the client prompt falls back to thinking in files plus shell again

The next prompt pass should:

- stop hardcoding the old file-plus-shell self-description
- describe the actual native-schema tools that are available, including Todo as a carry-over surface
- explain the explicit remember path honestly
- stop overstating memory reliability
- keep legacy shell- or slash-command language out of the active generic system prompt

## Important Watchouts

- Do not claim product features that do not exist.
- Do not let friendly copy contradict the actual tool surface.
- If memory is probabilistic or incomplete, say so honestly.
- If a durable memory path exists, name it clearly enough that the model can reliably choose it when asked.
