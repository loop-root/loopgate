package loopgate

import (
	controlapipkg "loopgate/internal/loopgate/controlapi"
	"os"
	"path/filepath"
	"testing"
)

func TestExecuteHostFolderRead_DeniesSymlinkEscape(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := filepath.Join(repoRoot, "home")
	downloadsDir := filepath.Join(homeDir, "Downloads")
	outsideDir := filepath.Join(repoRoot, "outside")
	if err := os.MkdirAll(downloadsDir, 0o755); err != nil {
		t.Fatalf("mkdir downloads: %v", err)
	}
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outsideDir, "secret.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(filepath.Join(outsideDir, "secret.txt"), filepath.Join(downloadsDir, "escape.txt")); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	server := newHostPlanApplyPolicyTestServer(t, repoRoot, homeDir)
	seedGrantedDownloadsFolderAccess(t, server)

	resp := server.executeHostFolderReadCapability(
		capabilityToken{ControlSessionID: "cs-downloads", ActorLabel: "operator", ClientSessionLabel: "operator-session"},
		controlapipkg.CapabilityRequest{
			RequestID:  "req-host-read-symlink",
			Capability: "host.folder.read",
			Arguments: map[string]string{
				"folder_name": folderAccessDownloadsID,
				"path":        "escape.txt",
			},
		},
	)

	if resp.Status != controlapipkg.ResponseStatusError {
		t.Fatalf("expected read denial, got %#v", resp)
	}
	if resp.DenialCode != controlapipkg.DenialCodeInvalidCapabilityArguments {
		t.Fatalf("expected invalid-arguments denial, got %#v", resp)
	}
}

func TestExecuteHostFolderList_DeniesSymlinkDirectoryEscape(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := filepath.Join(repoRoot, "home")
	downloadsDir := filepath.Join(homeDir, "Downloads")
	outsideDir := filepath.Join(repoRoot, "outside")
	if err := os.MkdirAll(downloadsDir, 0o755); err != nil {
		t.Fatalf("mkdir downloads: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(outsideDir, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir outside nested: %v", err)
	}
	if err := os.Symlink(filepath.Join(outsideDir, "nested"), filepath.Join(downloadsDir, "escape")); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	server := newHostPlanApplyPolicyTestServer(t, repoRoot, homeDir)
	seedGrantedDownloadsFolderAccess(t, server)

	resp := server.executeHostFolderListCapability(
		capabilityToken{ControlSessionID: "cs-downloads", ActorLabel: "operator", ClientSessionLabel: "operator-session"},
		controlapipkg.CapabilityRequest{
			RequestID:  "req-host-list-symlink",
			Capability: "host.folder.list",
			Arguments: map[string]string{
				"folder_name": folderAccessDownloadsID,
				"path":        "escape",
			},
		},
	)

	if resp.Status != controlapipkg.ResponseStatusError {
		t.Fatalf("expected list denial, got %#v", resp)
	}
	if resp.DenialCode != controlapipkg.DenialCodeInvalidCapabilityArguments {
		t.Fatalf("expected invalid-arguments denial, got %#v", resp)
	}
}
