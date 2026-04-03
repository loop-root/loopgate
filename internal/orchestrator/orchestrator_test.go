package orchestrator

import (
	"context"
	"errors"
	"testing"

	"morph/internal/config"
	"morph/internal/policy"
	"morph/internal/tools"
)

// mockTool for testing
type mockTool struct {
	name       string
	category   string
	operation  string
	executeErr error
	output     string
}

func (m *mockTool) Name() string     { return m.name }
func (m *mockTool) Category() string { return m.category }
func (m *mockTool) Operation() string {
	if m.operation == "" {
		return tools.OpRead // Default to read for backwards compat
	}
	return m.operation
}
func (m *mockTool) Schema() tools.Schema {
	return tools.Schema{
		Description: "mock tool",
		Args: []tools.ArgDef{
			{Name: "input", Required: true, Type: "string"},
		},
	}
}
func (m *mockTool) Execute(_ context.Context, args map[string]string) (string, error) {
	if m.executeErr != nil {
		return "", m.executeErr
	}
	return m.output, nil
}

// mockApprover for testing
type mockApprover struct {
	approved bool
	err      error
}

func (m *mockApprover) RequestApproval(call ToolCall, reason string) (bool, error) {
	return m.approved, m.err
}

// mockLogger for testing
type mockLogger struct {
	calls   []ToolCall
	results []ToolResult
}

func (m *mockLogger) LogToolCall(call ToolCall, decision policy.Decision, reason string) {
	m.calls = append(m.calls, call)
}
func (m *mockLogger) LogToolResult(call ToolCall, result ToolResult) {
	m.results = append(m.results, result)
}

func TestOrchestrator_UnknownTool(t *testing.T) {
	registry := tools.NewRegistry()
	pol := config.Policy{}
	checker := policy.NewChecker(pol)
	logger := &mockLogger{}

	orch := New(registry, checker, nil, logger)

	calls := []ToolCall{{ID: "1", Name: "nonexistent", Args: map[string]string{}}}
	results := orch.ProcessToolCalls(context.Background(), calls)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != StatusDenied {
		t.Errorf("expected StatusDenied, got %v", results[0].Status)
	}
	if results[0].Reason == "" {
		t.Error("expected denial reason")
	}
}

func TestOrchestrator_MissingRequiredArg(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(&mockTool{name: "test_tool", category: "filesystem", output: "ok"})

	pol := config.Policy{}
	pol.Tools.Filesystem.ReadEnabled = true
	checker := policy.NewChecker(pol)
	logger := &mockLogger{}

	orch := New(registry, checker, nil, logger)

	// Missing 'input' arg which is required
	calls := []ToolCall{{ID: "1", Name: "test_tool", Args: map[string]string{}}}
	results := orch.ProcessToolCalls(context.Background(), calls)

	if results[0].Status != StatusError {
		t.Errorf("expected StatusError for missing arg, got %v", results[0].Status)
	}
}

func TestOrchestrator_PolicyDenied(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(&mockTool{name: "fs_read", category: "filesystem", output: "content"})

	pol := config.Policy{}
	pol.Tools.Filesystem.ReadEnabled = false // Deny reads
	checker := policy.NewChecker(pol)
	logger := &mockLogger{}

	orch := New(registry, checker, nil, logger)

	calls := []ToolCall{{ID: "1", Name: "fs_read", Args: map[string]string{"input": "test"}}}
	results := orch.ProcessToolCalls(context.Background(), calls)

	if results[0].Status != StatusDenied {
		t.Errorf("expected StatusDenied, got %v", results[0].Status)
	}
}

func TestOrchestrator_ApprovalGranted(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(&mockTool{name: "fs_write", category: "filesystem", operation: tools.OpWrite, output: "wrote file"})

	pol := config.Policy{}
	pol.Tools.Filesystem.WriteEnabled = true
	pol.Tools.Filesystem.WriteRequiresApproval = true
	checker := policy.NewChecker(pol)
	approver := &mockApprover{approved: true}
	logger := &mockLogger{}

	orch := New(registry, checker, approver, logger)

	calls := []ToolCall{{ID: "1", Name: "fs_write", Args: map[string]string{"input": "test"}}}
	results := orch.ProcessToolCalls(context.Background(), calls)

	if results[0].Status != StatusSuccess {
		t.Errorf("expected StatusSuccess after approval, got %v", results[0].Status)
	}
}

func TestOrchestrator_ApprovalDenied(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(&mockTool{name: "fs_write", category: "filesystem", operation: tools.OpWrite, output: "wrote file"})

	pol := config.Policy{}
	pol.Tools.Filesystem.WriteEnabled = true
	pol.Tools.Filesystem.WriteRequiresApproval = true
	checker := policy.NewChecker(pol)
	approver := &mockApprover{approved: false}
	logger := &mockLogger{}

	orch := New(registry, checker, approver, logger)

	calls := []ToolCall{{ID: "1", Name: "fs_write", Args: map[string]string{"input": "test"}}}
	results := orch.ProcessToolCalls(context.Background(), calls)

	if results[0].Status != StatusDenied {
		t.Errorf("expected StatusDenied after user denial, got %v", results[0].Status)
	}
}

func TestOrchestrator_ExecutionError(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(&mockTool{
		name:       "failing_tool",
		category:   "filesystem",
		executeErr: errors.New("something went wrong"),
	})

	pol := config.Policy{}
	pol.Tools.Filesystem.ReadEnabled = true
	checker := policy.NewChecker(pol)
	logger := &mockLogger{}

	orch := New(registry, checker, nil, logger)

	calls := []ToolCall{{ID: "1", Name: "failing_tool", Args: map[string]string{"input": "test"}}}
	results := orch.ProcessToolCalls(context.Background(), calls)

	if results[0].Status != StatusError {
		t.Errorf("expected StatusError, got %v", results[0].Status)
	}
	if results[0].Output == "" {
		t.Error("expected error message in output")
	}
}

func TestOrchestrator_Success(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(&mockTool{name: "fs_read", category: "filesystem", output: "file contents"})

	pol := config.Policy{}
	pol.Tools.Filesystem.ReadEnabled = true
	checker := policy.NewChecker(pol)
	logger := &mockLogger{}

	orch := New(registry, checker, nil, logger)

	calls := []ToolCall{{ID: "1", Name: "fs_read", Args: map[string]string{"input": "test"}}}
	results := orch.ProcessToolCalls(context.Background(), calls)

	if results[0].Status != StatusSuccess {
		t.Errorf("expected StatusSuccess, got %v", results[0].Status)
	}
	if results[0].Output != "file contents" {
		t.Errorf("expected 'file contents', got %q", results[0].Output)
	}
}

func TestOrchestrator_ProcessModelOutput(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(&mockTool{name: "fs_read", category: "filesystem", output: "content"})

	pol := config.Policy{}
	pol.Tools.Filesystem.ReadEnabled = true
	checker := policy.NewChecker(pol)
	logger := &mockLogger{}

	orch := New(registry, checker, nil, logger)

	modelOutput := `Let me read that file for you.
<tool_call>
{"name": "fs_read", "args": {"input": "test.txt"}}
</tool_call>
I'll analyze the contents.`

	results, text, parseErrs := orch.ProcessModelOutput(context.Background(), modelOutput)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != StatusSuccess {
		t.Errorf("expected StatusSuccess, got %v", results[0].Status)
	}
	if text == "" {
		t.Error("expected non-empty text")
	}
	if len(parseErrs) != 0 {
		t.Fatalf("expected 0 parse errors, got %d", len(parseErrs))
	}
}

func TestOrchestrator_ProcessModelOutputReturnsParseErrors(t *testing.T) {
	registry := tools.NewRegistry()

	pol := config.Policy{}
	pol.Tools.Filesystem.ReadEnabled = true
	checker := policy.NewChecker(pol)
	logger := &mockLogger{}

	orch := New(registry, checker, nil, logger)

	modelOutput := `<tool_call>
{not valid json}
</tool_call>`

	results, text, parseErrs := orch.ProcessModelOutput(context.Background(), modelOutput)

	if len(results) != 0 {
		t.Fatalf("expected 0 tool results, got %d", len(results))
	}
	if text != "" {
		t.Fatalf("expected empty non-tool text, got %q", text)
	}
	if len(parseErrs) != 1 {
		t.Fatalf("expected 1 parse error, got %d", len(parseErrs))
	}
}

func TestOrchestrator_NoApprover(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(&mockTool{name: "fs_write", category: "filesystem", operation: tools.OpWrite, output: "wrote"})

	pol := config.Policy{}
	pol.Tools.Filesystem.WriteEnabled = true
	pol.Tools.Filesystem.WriteRequiresApproval = true
	checker := policy.NewChecker(pol)

	// No approver configured
	orch := New(registry, checker, nil, nil)

	calls := []ToolCall{{ID: "1", Name: "fs_write", Args: map[string]string{"input": "test"}}}
	results := orch.ProcessToolCalls(context.Background(), calls)

	// Should deny when approval needed but no approver available
	if results[0].Status != StatusDenied {
		t.Errorf("expected StatusDenied when no approver, got %v", results[0].Status)
	}
}
