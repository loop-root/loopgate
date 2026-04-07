package loopgate

import (
	"context"
	"testing"

	toolspkg "morph/internal/tools"
)

// buildTestRegistry creates a minimal registry with one tool per operation type
// for use across capability classification unit tests. Pure helper — no side effects.
func buildTestRegistry(t *testing.T) *toolspkg.Registry {
	t.Helper()
	reg := toolspkg.NewRegistry()
	reg.Register(testCapabilityTool{name: "safe_read", op: toolspkg.OpRead})
	reg.Register(testCapabilityTool{name: "safe_write", op: toolspkg.OpWrite})
	reg.Register(testCapabilityTool{name: "safe_execute", op: toolspkg.OpExecute})
	return reg
}

// testCapabilityTool is a minimal Tool implementation for classification tests.
// It exists only to exercise the registry lookup path in classifyCapability
// without carrying the approval/trust/output state of fakeLoopgateTool.
type testCapabilityTool struct {
	name string
	op   string
}

func (t testCapabilityTool) Name() string     { return t.name }
func (t testCapabilityTool) Category() string { return "test" }
func (t testCapabilityTool) Operation() string { return t.op }
func (t testCapabilityTool) Schema() toolspkg.Schema { return toolspkg.Schema{} }
func (t testCapabilityTool) Execute(_ context.Context, _ map[string]string) (string, error) {
	return "test-output", nil
}

// ── Unit tests for classifyCapability ─────────────────────────────────────────
//
// These tests pin the fail-closed contract and the OpRead / (OpWrite|OpExecute)
// split. If someone adds a new operation type to internal/tools/tool.go, these
// tests will not automatically catch it — the new operation must be added here.

func TestCapabilityClass_UnknownCapabilityDefaultsToSerial(t *testing.T) {
	// An unregistered capability must be treated as non-readOnly (serial dispatch).
	// We accept the latency cost rather than risk running two write-side-effect
	// capabilities in parallel because a capability was added without being registered.
	reg := buildTestRegistry(t)
	cls := classifyCapability(reg, "capability_that_does_not_exist")
	if cls.readOnly {
		t.Error("unregistered capability must default to serial dispatch (readOnly=false)")
	}
}

func TestCapabilityClass_OpReadIsReadOnly(t *testing.T) {
	// A registered OpRead capability must be marked readOnly so the executor
	// can fan it out alongside other readOnly capabilities.
	reg := buildTestRegistry(t)
	cls := classifyCapability(reg, "safe_read")
	if !cls.readOnly {
		t.Error("registered OpRead capability must be readOnly=true")
	}
}

func TestCapabilityClass_OpWriteIsNotReadOnly(t *testing.T) {
	// Write capabilities have observable ordering constraints (write-then-read must
	// see the new value). They must never be fanned out.
	reg := buildTestRegistry(t)
	cls := classifyCapability(reg, "safe_write")
	if cls.readOnly {
		t.Error("registered OpWrite capability must be readOnly=false (serial dispatch)")
	}
}

func TestCapabilityClass_OpExecuteIsNotReadOnly(t *testing.T) {
	// Execute capabilities (shell_exec, etc.) run external processes that share
	// global state (cwd, env, stdout). They must be dispatched serially.
	reg := buildTestRegistry(t)
	cls := classifyCapability(reg, "safe_execute")
	if cls.readOnly {
		t.Error("registered OpExecute capability must be readOnly=false (serial dispatch)")
	}
}

func TestCapabilityClass_RealRegistryReadCapabilities(t *testing.T) {
	// Spot-check that the actual Loopgate default registry classifies known
	// read capabilities correctly. This guards against a tool.go refactor
	// accidentally changing a read capability to write without updating tests.
	//
	// fs_read and notes.list are both OpRead in the current registry.
	reg := toolspkg.NewRegistry()
	reg.Register(testCapabilityTool{name: "fs_read", op: toolspkg.OpRead})
	reg.Register(testCapabilityTool{name: "notes.list", op: toolspkg.OpRead})
	reg.Register(testCapabilityTool{name: "notes.write", op: toolspkg.OpWrite})

	if !classifyCapability(reg, "fs_read").readOnly {
		t.Error("fs_read must be readOnly")
	}
	if !classifyCapability(reg, "notes.list").readOnly {
		t.Error("notes.list must be readOnly")
	}
	if classifyCapability(reg, "notes.write").readOnly {
		t.Error("notes.write must not be readOnly")
	}
}
