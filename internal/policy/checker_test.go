package policy

import (
	"testing"

	"morph/internal/config"
)

func TestChecker_FilesystemRead_Allowed(t *testing.T) {
	pol := config.Policy{}
	pol.Tools.Filesystem.ReadEnabled = true

	checker := NewChecker(pol)
	tool := &mockTool{name: "fs_read", category: "filesystem", operation: "read"}

	result := checker.Check(tool)

	if result.Decision != Allow {
		t.Errorf("expected Allow, got %v with reason: %s", result.Decision, result.Reason)
	}
}

func TestChecker_FilesystemRead_Denied(t *testing.T) {
	pol := config.Policy{}
	pol.Tools.Filesystem.ReadEnabled = false

	checker := NewChecker(pol)
	tool := &mockTool{name: "fs_read", category: "filesystem", operation: "read"}

	result := checker.Check(tool)

	if result.Decision != Deny {
		t.Errorf("expected Deny, got %v", result.Decision)
	}
	if result.Reason == "" {
		t.Error("expected denial reason")
	}
}

func TestChecker_FilesystemWrite_NeedsApproval(t *testing.T) {
	pol := config.Policy{}
	pol.Tools.Filesystem.WriteEnabled = true
	pol.Tools.Filesystem.WriteRequiresApproval = true

	checker := NewChecker(pol)
	tool := &mockTool{name: "fs_write", category: "filesystem", operation: "write"}

	result := checker.Check(tool)

	if result.Decision != NeedsApproval {
		t.Errorf("expected NeedsApproval, got %v", result.Decision)
	}
}

func TestChecker_FilesystemWrite_Allowed(t *testing.T) {
	pol := config.Policy{}
	pol.Tools.Filesystem.WriteEnabled = true
	pol.Tools.Filesystem.WriteRequiresApproval = false

	checker := NewChecker(pol)
	tool := &mockTool{name: "fs_write", category: "filesystem", operation: "write"}

	result := checker.Check(tool)

	if result.Decision != Allow {
		t.Errorf("expected Allow, got %v with reason: %s", result.Decision, result.Reason)
	}
}

func TestChecker_FilesystemWrite_Disabled(t *testing.T) {
	pol := config.Policy{}
	pol.Tools.Filesystem.WriteEnabled = false

	checker := NewChecker(pol)
	tool := &mockTool{name: "fs_write", category: "filesystem", operation: "write"}

	result := checker.Check(tool)

	if result.Decision != Deny {
		t.Errorf("expected Deny, got %v", result.Decision)
	}
}

func TestChecker_HostRead_Allowed(t *testing.T) {
	pol := config.Policy{}
	pol.Tools.Filesystem.ReadEnabled = true
	checker := NewChecker(pol)
	tool := &mockTool{name: "host.folder.list", category: "host", operation: "read"}
	result := checker.Check(tool)
	if result.Decision != Allow {
		t.Fatalf("expected Allow, got %v: %s", result.Decision, result.Reason)
	}
}

func TestChecker_HostRead_DeniedWhenFilesystemReadOff(t *testing.T) {
	pol := config.Policy{}
	pol.Tools.Filesystem.ReadEnabled = false
	checker := NewChecker(pol)
	tool := &mockTool{name: "host.folder.list", category: "host", operation: "read"}
	result := checker.Check(tool)
	if result.Decision != Deny {
		t.Fatalf("expected Deny, got %v", result.Decision)
	}
}

func TestChecker_HostWrite_AlwaysNeedsApproval(t *testing.T) {
	pol := config.Policy{}
	pol.Tools.Filesystem.WriteEnabled = true
	checker := NewChecker(pol)
	tool := &mockTool{name: "host.plan.apply", category: "host", operation: "write"}
	result := checker.Check(tool)
	if result.Decision != NeedsApproval {
		t.Fatalf("expected NeedsApproval, got %v: %s", result.Decision, result.Reason)
	}
}

func TestChecker_HostWrite_DeniedWhenFilesystemWriteOff(t *testing.T) {
	pol := config.Policy{}
	pol.Tools.Filesystem.WriteEnabled = false
	checker := NewChecker(pol)
	tool := &mockTool{name: "host.plan.apply", category: "host", operation: "write"}
	result := checker.Check(tool)
	if result.Decision != Deny {
		t.Fatalf("expected Deny, got %v", result.Decision)
	}
}

func TestChecker_UnknownCategory(t *testing.T) {
	pol := config.Policy{}
	checker := NewChecker(pol)
	tool := &mockTool{name: "unknown_tool", category: "unknown", operation: "read"}

	result := checker.Check(tool)

	if result.Decision != Deny {
		t.Errorf("expected Deny for unknown category, got %v", result.Decision)
	}
}

func TestChecker_Shell_DisabledByDefault(t *testing.T) {
	pol := config.Policy{}
	checker := NewChecker(pol)
	tool := &mockTool{name: "shell_exec", category: "shell", operation: "execute"}

	result := checker.Check(tool)

	if result.Decision != Deny {
		t.Errorf("expected Deny for shell when disabled, got %v", result.Decision)
	}
}

func TestChecker_Shell_EnabledRequiresApproval(t *testing.T) {
	pol := config.Policy{}
	pol.Tools.Shell.Enabled = true
	pol.Tools.Shell.RequiresApproval = true
	checker := NewChecker(pol)
	tool := &mockTool{name: "shell_exec", category: "shell", operation: "execute"}

	result := checker.Check(tool)
	if result.Decision != NeedsApproval {
		t.Fatalf("expected NeedsApproval, got %v", result.Decision)
	}
}

func TestChecker_Shell_EnabledWithoutApprovalAllows(t *testing.T) {
	pol := config.Policy{}
	pol.Tools.Shell.Enabled = true
	pol.Tools.Shell.RequiresApproval = false
	checker := NewChecker(pol)
	tool := &mockTool{name: "shell_exec", category: "shell", operation: "execute"}

	result := checker.Check(tool)
	if result.Decision != Allow {
		t.Fatalf("expected Allow, got %v", result.Decision)
	}
}

func TestChecker_OperationBased_NotToolName(t *testing.T) {
	// This test verifies that policy is based on operation type, not tool name
	pol := config.Policy{}
	pol.Tools.Filesystem.ReadEnabled = true
	pol.Tools.Filesystem.WriteEnabled = false

	checker := NewChecker(pol)

	// A tool with a different name but write operation should be denied
	tool := &mockTool{name: "custom_file_modifier", category: "filesystem", operation: "write"}
	result := checker.Check(tool)

	if result.Decision != Deny {
		t.Error("expected write operation to be denied regardless of tool name")
	}

	// A tool with read operation should be allowed
	tool2 := &mockTool{name: "custom_file_viewer", category: "filesystem", operation: "read"}
	result2 := checker.Check(tool2)

	if result2.Decision != Allow {
		t.Error("expected read operation to be allowed")
	}
}

func TestChecker_UnknownOperation(t *testing.T) {
	pol := config.Policy{}
	pol.Tools.Filesystem.ReadEnabled = true
	pol.Tools.Filesystem.WriteEnabled = true

	checker := NewChecker(pol)
	tool := &mockTool{name: "weird_tool", category: "filesystem", operation: "delete"}

	result := checker.Check(tool)

	// Unknown operations should be denied by default
	if result.Decision != Deny {
		t.Errorf("expected Deny for unknown operation, got %v", result.Decision)
	}
}

// mockTool implements ToolInfo for testing
type mockTool struct {
	name      string
	category  string
	operation string
}

func (m *mockTool) Name() string      { return m.name }
func (m *mockTool) Category() string  { return m.category }
func (m *mockTool) Operation() string { return m.operation }
