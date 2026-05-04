package loopgate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHookPreValidate_DeniesReadThroughRepoSymlinkEscape(t *testing.T) {
	repoRoot := t.TempDir()
	outsideRoot := t.TempDir()
	outsideFile := filepath.Join(outsideRoot, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret\n"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	requireSymlinkForHookTest(t, outsideFile, filepath.Join(repoRoot, "looks-safe.txt"))

	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	response := postHookPreValidateForTest(t, server, map[string]interface{}{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Read",
		"tool_input": map[string]interface{}{
			"file_path": filepath.Join(repoRoot, "looks-safe.txt"),
		},
		"cwd":        repoRoot,
		"session_id": "session-hook",
	})
	if response.Decision != "block" {
		t.Fatalf("expected symlink escape read to block, got %#v", response)
	}
	if !strings.Contains(response.Reason, "outside allowed roots") {
		t.Fatalf("expected outside allowed roots reason, got %#v", response)
	}
}

func TestHookPreValidate_DeniesWriteThroughRepoSymlinkEscape(t *testing.T) {
	repoRoot := t.TempDir()
	outsideRoot := t.TempDir()
	outsideFile := filepath.Join(outsideRoot, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret\n"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	requireSymlinkForHookTest(t, outsideFile, filepath.Join(repoRoot, "looks-safe.txt"))

	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	response := postHookPreValidateForTest(t, server, map[string]interface{}{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Write",
		"tool_input": map[string]interface{}{
			"file_path": filepath.Join(repoRoot, "looks-safe.txt"),
			"content":   "changed\n",
		},
		"cwd":        repoRoot,
		"session_id": "session-hook",
	})
	if response.Decision != "block" {
		t.Fatalf("expected symlink escape write to block, got %#v", response)
	}
	if !strings.Contains(response.Reason, "outside allowed roots") {
		t.Fatalf("expected outside allowed roots reason, got %#v", response)
	}
}

func TestHookPreValidate_DeniesEditAndMultiEditThroughSymlinkPath(t *testing.T) {
	repoRoot := t.TempDir()
	docsDir := filepath.Join(repoRoot, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	realPath := filepath.Join(docsDir, "real.md")
	if err := os.WriteFile(realPath, []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("write real file: %v", err)
	}
	linkPath := filepath.Join(docsDir, "link.md")
	requireSymlinkForHookTest(t, realPath, linkPath)

	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	for _, toolName := range []string{"Edit", "MultiEdit"} {
		response := postHookPreValidateForTest(t, server, map[string]interface{}{
			"hook_event_name": "PreToolUse",
			"tool_name":       toolName,
			"tool_input": map[string]interface{}{
				"file_path":  linkPath,
				"old_string": "hello",
				"new_string": "hi",
			},
			"cwd":        repoRoot,
			"session_id": "session-hook-" + toolName,
		})
		if response.Decision != "block" {
			t.Fatalf("expected %s through symlink path to block, got %#v", toolName, response)
		}
		if !strings.Contains(response.Reason, "uses a symlink path") {
			t.Fatalf("expected symlink path reason for %s, got %#v", toolName, response)
		}
	}
}

func TestHookPreValidate_DeniesGrepAndGlobThroughSymlinkedDirectoryEscape(t *testing.T) {
	repoRoot := t.TempDir()
	outsideRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(outsideRoot, "secret.txt"), []byte("secret\n"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	requireSymlinkForHookTest(t, outsideRoot, filepath.Join(repoRoot, "search-here"))

	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	for _, toolName := range []string{"Grep", "Glob"} {
		response := postHookPreValidateForTest(t, server, map[string]interface{}{
			"hook_event_name": "PreToolUse",
			"tool_name":       toolName,
			"tool_input": map[string]interface{}{
				"path": filepath.Join(repoRoot, "search-here"),
			},
			"cwd":        repoRoot,
			"session_id": "session-hook-" + toolName,
		})
		if response.Decision != "block" {
			t.Fatalf("expected %s through symlinked directory to block, got %#v", toolName, response)
		}
		if !strings.Contains(response.Reason, "outside allowed roots") {
			t.Fatalf("expected outside allowed roots reason for %s, got %#v", toolName, response)
		}
	}
}

func TestHookPreValidate_DeniesSymlinkIntoDeniedPath(t *testing.T) {
	repoRoot := t.TempDir()
	deniedDir := filepath.Join(repoRoot, "runtime", "state")
	if err := os.MkdirAll(deniedDir, 0o755); err != nil {
		t.Fatalf("mkdir denied dir: %v", err)
	}
	deniedFile := filepath.Join(deniedDir, "secret.jsonl")
	if err := os.WriteFile(deniedFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write denied file: %v", err)
	}
	docsDir := filepath.Join(repoRoot, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	requireSymlinkForHookTest(t, deniedDir, filepath.Join(docsDir, "state-link"))

	policyYAML := strings.Replace(loopgatePolicyYAML(false), "    denied_paths: []\n", "    denied_paths:\n      - \"runtime/state\"\n      - \"core/policy\"\n", 1)
	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	writeSignedTestPolicyYAML(t, repoRoot, policyYAML)
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	response := postHookPreValidateForTest(t, server, map[string]interface{}{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Read",
		"tool_input": map[string]interface{}{
			"file_path": filepath.Join(docsDir, "state-link", "secret.jsonl"),
		},
		"cwd":        repoRoot,
		"session_id": "session-hook",
	})
	if response.Decision != "block" {
		t.Fatalf("expected symlink into denied path to block, got %#v", response)
	}
	if !strings.Contains(response.Reason, "matches denied path policy") {
		t.Fatalf("expected denied path policy reason, got %#v", response)
	}
}

func TestHookPreValidate_DeniesNewFileUnderSymlinkedParentDirectory(t *testing.T) {
	repoRoot := t.TempDir()
	realDir := filepath.Join(repoRoot, "real-parent")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("mkdir real parent: %v", err)
	}
	requireSymlinkForHookTest(t, realDir, filepath.Join(repoRoot, "linked-parent"))

	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	response := postHookPreValidateForTest(t, server, map[string]interface{}{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Write",
		"tool_input": map[string]interface{}{
			"file_path": filepath.Join(repoRoot, "linked-parent", "new.txt"),
			"content":   "new\n",
		},
		"cwd":        repoRoot,
		"session_id": "session-hook",
	})
	if response.Decision != "block" {
		t.Fatalf("expected new file under symlinked parent to block, got %#v", response)
	}
	if !strings.Contains(response.Reason, "uses a symlink path") {
		t.Fatalf("expected symlink path reason, got %#v", response)
	}
}

func TestHookPreValidate_AllowsNormalSafeReadAndWritePaths(t *testing.T) {
	repoRoot := t.TempDir()
	readPath := filepath.Join(repoRoot, "README.md")
	if err := os.WriteFile(readPath, []byte("Loopgate\n"), 0o600); err != nil {
		t.Fatalf("write read target: %v", err)
	}

	socketPath := filepath.Join(t.TempDir(), "loopgate.sock")
	writeSignedTestPolicyYAML(t, repoRoot, loopgatePolicyYAML(false))
	server, err := NewServer(repoRoot, socketPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	readResponse := postHookPreValidateForTest(t, server, map[string]interface{}{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Read",
		"tool_input": map[string]interface{}{
			"file_path": readPath,
		},
		"cwd":        repoRoot,
		"session_id": "session-hook-read",
	})
	if readResponse.Decision != "allow" {
		t.Fatalf("expected normal read to allow, got %#v", readResponse)
	}

	writeResponse := postHookPreValidateForTest(t, server, map[string]interface{}{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Write",
		"tool_input": map[string]interface{}{
			"file_path": filepath.Join(repoRoot, "notes.md"),
			"content":   "hello\n",
		},
		"cwd":        repoRoot,
		"session_id": "session-hook-write",
	})
	if writeResponse.Decision != "allow" {
		t.Fatalf("expected normal write to allow, got %#v", writeResponse)
	}
}
