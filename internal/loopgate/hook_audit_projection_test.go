package loopgate

import (
	"path/filepath"
	"testing"

	controlapipkg "loopgate/internal/loopgate/controlapi"
)

func TestBuildHookAuditProjection_LogsInsideRepoCWD(t *testing.T) {
	repoRoot := t.TempDir()
	cwd := filepath.Join(repoRoot, "docs")

	projection := buildHookAuditProjection(controlapipkg.HookPreValidateRequest{
		ToolName: "Grep",
		CWD:      cwd,
		ToolInput: map[string]interface{}{
			"path": ".",
		},
	}, repoRoot, false)

	if projection["hook_cwd_state"] != "inside_repo" {
		t.Fatalf("expected inside repo cwd state, got %#v", projection["hook_cwd_state"])
	}
	if projection["hook_cwd"] != cwd {
		t.Fatalf("expected hook cwd %q, got %#v", cwd, projection["hook_cwd"])
	}
	if projection["hook_cwd_sha256"] == "" {
		t.Fatalf("expected hook cwd hash, got %#v", projection)
	}
}

func TestBuildHookAuditProjection_DoesNotLogOutsideRepoCWDPath(t *testing.T) {
	repoRoot := t.TempDir()
	outsideCWD := filepath.Join(t.TempDir(), "outside")

	projection := buildHookAuditProjection(controlapipkg.HookPreValidateRequest{
		ToolName: "Grep",
		CWD:      outsideCWD,
		ToolInput: map[string]interface{}{
			"path": ".",
		},
	}, repoRoot, false)

	if projection["hook_cwd_state"] != "outside_repo" {
		t.Fatalf("expected outside repo cwd state, got %#v", projection["hook_cwd_state"])
	}
	if _, found := projection["hook_cwd"]; found {
		t.Fatalf("expected outside repo cwd path to be omitted, got %#v", projection)
	}
	if projection["hook_cwd_sha256"] == "" {
		t.Fatalf("expected hook cwd hash, got %#v", projection)
	}
}

func TestBuildHookAuditProjection_DoesNotLogRelativeCWDPath(t *testing.T) {
	repoRoot := t.TempDir()

	projection := buildHookAuditProjection(controlapipkg.HookPreValidateRequest{
		ToolName: "Grep",
		CWD:      "../relative",
		ToolInput: map[string]interface{}{
			"path": ".",
		},
	}, repoRoot, false)

	if projection["hook_cwd_state"] != "invalid_relative" {
		t.Fatalf("expected invalid relative cwd state, got %#v", projection["hook_cwd_state"])
	}
	if _, found := projection["hook_cwd"]; found {
		t.Fatalf("expected relative cwd path to be omitted, got %#v", projection)
	}
}
