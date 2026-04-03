package tools

import (
	"context"
	"fmt"
)

// MemoryRemember stores a short explicit fact in durable continuity.
//
// Execution is routed through Loopgate's dedicated memory pipeline rather than
// generic tool execution. The registry entry exists so the capability can be
// discovered, granted, documented, and schema-validated like other tools.
type MemoryRemember struct{}

func (tool *MemoryRemember) Name() string      { return "memory.remember" }
func (tool *MemoryRemember) Category() string  { return "filesystem" }
func (tool *MemoryRemember) Operation() string { return OpWrite }

func (tool *MemoryRemember) Schema() Schema {
	return Schema{
		Description: "Store a short durable memory fact when the user asks to remember something or when you proactively save a stable preference or routine. Do not use this tool to list, read, or audit existing memories — those appear in the REMEMBERED CONTINUITY section of the prompt. Never use it for secrets, tokens, passwords, or long notes.",
		Args: []ArgDef{
			{
				Name:        "fact_key",
				Description: "Memory key such as name, preferred_name, preference.coffee_order, routine.friday_gym, or project.current_focus",
				Required:    true,
				Type:        "string",
				MaxLen:      64,
			},
			{
				Name:        "fact_value",
				Description: "Short single-line value to remember",
				Required:    true,
				Type:        "string",
				MaxLen:      256,
			},
			{
				Name:        "reason",
				Description: "Short rationale for why this should be remembered",
				Required:    false,
				Type:        "string",
				MaxLen:      200,
			},
		},
	}
}

func (tool *MemoryRemember) Execute(context.Context, map[string]string) (string, error) {
	return "", fmt.Errorf("memory.remember must be executed through loopgate memory handling")
}
