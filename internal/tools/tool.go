package tools

import (
	"context"
	"strings"
)

// OperationType describes what kind of operation a tool performs.
// This is used by the policy checker to determine access rules.
// Using type alias so it's compatible with policy.ToolInfo interface.
type OperationType = string

const (
	OpRead    OperationType = "read"    // Read-only operations
	OpWrite   OperationType = "write"   // Creates, modifies, or deletes data
	OpExecute OperationType = "execute" // Executes external commands
)

// Tool is the interface all tools implement.
type Tool interface {
	// Name returns the tool's identifier (e.g., "fs_read").
	Name() string

	// Category returns the policy category (e.g., "filesystem", "http", "shell").
	Category() string

	// Operation returns what kind of operation this tool performs.
	// Used by policy checker to determine access rules.
	// Returns "read", "write", or "execute".
	Operation() string

	// Schema returns argument definitions for validation and documentation.
	Schema() Schema

	// Execute runs the tool with pre-validated arguments.
	// Policy checks happen before this is called.
	Execute(ctx context.Context, args map[string]string) (output string, err error)
}

// Schema describes a tool's arguments.
type Schema struct {
	Description string   `json:"description"`
	Args        []ArgDef `json:"args"`
}

// ArgDef describes a single argument.
type ArgDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Type        string `json:"type"`   // "path", "string", "int", "bool"
	MaxLen      int    `json:"maxlen"` // 0 = unlimited
}

// Validate checks that required arguments are present and within limits.
func (s Schema) Validate(args map[string]string) error {
	for _, arg := range s.Args {
		val, exists := args[arg.Name]

		if arg.Required {
			if !exists || strings.TrimSpace(val) == "" {
				return &MissingArgError{ArgName: arg.Name}
			}
		}

		if exists && arg.MaxLen > 0 && len(val) > arg.MaxLen {
			return &ArgTooLongError{ArgName: arg.Name, MaxLen: arg.MaxLen, ActualLen: len(val)}
		}
	}
	return nil
}

// MissingArgError is returned when a required argument is missing.
type MissingArgError struct {
	ArgName string
}

func (e *MissingArgError) Error() string {
	return "missing required argument: " + e.ArgName
}

// ArgTooLongError is returned when an argument exceeds max length.
type ArgTooLongError struct {
	ArgName   string
	MaxLen    int
	ActualLen int
}

func (e *ArgTooLongError) Error() string {
	return "argument too long: " + e.ArgName
}
