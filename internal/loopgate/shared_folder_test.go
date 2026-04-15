package loopgate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"loopgate/internal/config"
	"loopgate/internal/ledger"
	"loopgate/internal/sandbox"
)

func TestSyncDefaultSharedFolderCreatesHostFolderAndMirrorsIntoSandbox(t *testing.T) {
	repoRoot := t.TempDir()
	userHomeDirectory := t.TempDir()
	sharedHostPath := filepath.Join(userHomeDirectory, defaultSharedFolderName)
	if err := os.MkdirAll(sharedHostPath, 0o700); err != nil {
		t.Fatalf("create shared host folder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sharedHostPath, "notes.txt"), []byte("from host"), 0o600); err != nil {
		t.Fatalf("write host file: %v", err)
	}

	auditedEventCount := 0
	server := &Server{
		sandboxPaths: sandbox.PathsForRepo(repoRoot),
		resolveUserHomeDir: func() (string, error) {
			return userHomeDirectory, nil
		},
		appendAuditEvent: func(string, ledger.Event) error {
			auditedEventCount++
			return nil
		},
		auditPath: filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"),
		now:       time.Now,
	}

	response, err := server.syncDefaultSharedFolder(capabilityToken{
		ControlSessionID:   "cs-test",
		ActorLabel:         "operator",
		ClientSessionLabel: "session-test",
	})
	if err != nil {
		t.Fatalf("sync shared folder: %v", err)
	}

	resolvedSharedHostPath, err := filepath.EvalSymlinks(sharedHostPath)
	if err != nil {
		t.Fatalf("resolve shared host path: %v", err)
	}
	if response.HostPath != resolvedSharedHostPath {
		t.Fatalf("expected host path %q, got %q", resolvedSharedHostPath, response.HostPath)
	}
	if response.SandboxRelativePath != "imports/shared" {
		t.Fatalf("expected sandbox path imports/shared, got %q", response.SandboxRelativePath)
	}
	if !response.MirrorReady {
		t.Fatal("expected shared mirror to be ready")
	}
	if auditedEventCount != 1 {
		t.Fatalf("expected one audit event, got %d", auditedEventCount)
	}

	mirroredBytes, err := os.ReadFile(filepath.Join(server.sandboxPaths.Imports, "shared", "notes.txt"))
	if err != nil {
		t.Fatalf("read mirrored file: %v", err)
	}
	if string(mirroredBytes) != "from host" {
		t.Fatalf("expected mirrored content %q, got %q", "from host", string(mirroredBytes))
	}
}

func TestSharedFolderStatusReportsMirrorState(t *testing.T) {
	repoRoot := t.TempDir()
	userHomeDirectory := t.TempDir()
	server := &Server{
		sandboxPaths: sandbox.PathsForRepo(repoRoot),
		resolveUserHomeDir: func() (string, error) {
			return userHomeDirectory, nil
		},
	}

	statusResponse, err := server.sharedFolderStatus()
	if err != nil {
		t.Fatalf("shared folder status: %v", err)
	}
	if statusResponse.HostExists {
		t.Fatal("expected missing host folder before creation")
	}
	if statusResponse.MirrorReady {
		t.Fatal("expected missing mirror before sync")
	}

	sharedHostPath := filepath.Join(userHomeDirectory, defaultSharedFolderName)
	if err := os.MkdirAll(sharedHostPath, 0o700); err != nil {
		t.Fatalf("create shared host folder: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(server.sandboxPaths.Imports, defaultSharedFolderSandboxName), 0o700); err != nil {
		t.Fatalf("create shared mirror: %v", err)
	}
	if err := os.WriteFile(filepath.Join(server.sandboxPaths.Imports, defaultSharedFolderSandboxName, "hello.txt"), []byte("hi"), 0o600); err != nil {
		t.Fatalf("write mirror file: %v", err)
	}

	statusResponse, err = server.sharedFolderStatus()
	if err != nil {
		t.Fatalf("shared folder status after setup: %v", err)
	}
	if !statusResponse.HostExists {
		t.Fatal("expected host folder to exist")
	}
	if !statusResponse.MirrorReady {
		t.Fatal("expected mirror to be ready")
	}
	if statusResponse.EntryCount != 1 {
		t.Fatalf("expected one mirrored entry, got %d", statusResponse.EntryCount)
	}
}

func TestSyncDefaultSharedFolderRestoresPreviousMirrorWhenAuditFails(t *testing.T) {
	repoRoot := t.TempDir()
	userHomeDirectory := t.TempDir()
	sharedHostPath := filepath.Join(userHomeDirectory, defaultSharedFolderName)
	if err := os.MkdirAll(sharedHostPath, 0o700); err != nil {
		t.Fatalf("create shared host folder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sharedHostPath, "fresh.txt"), []byte("fresh"), 0o600); err != nil {
		t.Fatalf("write host file: %v", err)
	}

	server := &Server{
		sandboxPaths: sandbox.PathsForRepo(repoRoot),
		resolveUserHomeDir: func() (string, error) {
			return userHomeDirectory, nil
		},
		appendAuditEvent: func(string, ledger.Event) error {
			return errors.New("audit unavailable")
		},
		auditPath: filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"),
		now:       time.Now,
	}
	if err := server.sandboxPaths.Ensure(); err != nil {
		t.Fatalf("ensure sandbox paths: %v", err)
	}
	mirrorPath := filepath.Join(server.sandboxPaths.Imports, defaultSharedFolderSandboxName)
	if err := os.MkdirAll(mirrorPath, 0o700); err != nil {
		t.Fatalf("create prior mirror: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mirrorPath, "stale.txt"), []byte("stale"), 0o600); err != nil {
		t.Fatalf("write prior mirror file: %v", err)
	}

	_, err := server.syncDefaultSharedFolder(capabilityToken{
		ControlSessionID:   "cs-test",
		ActorLabel:         "operator",
		ClientSessionLabel: "session-test",
	})
	if err == nil {
		t.Fatal("expected sync to fail when audit append fails")
	}

	restoredBytes, err := os.ReadFile(filepath.Join(mirrorPath, "stale.txt"))
	if err != nil {
		t.Fatalf("read restored mirror file: %v", err)
	}
	if string(restoredBytes) != "stale" {
		t.Fatalf("expected restored mirror content %q, got %q", "stale", string(restoredBytes))
	}

	if _, err := os.Stat(filepath.Join(mirrorPath, "fresh.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected new mirrored content to be rolled back, got %v", err)
	}
}

func TestUpdateFolderAccessPersistsGrantedPresetsAndHostDownloadsAccess(t *testing.T) {
	repoRoot := t.TempDir()
	userHomeDirectory := t.TempDir()
	downloadsPath := filepath.Join(userHomeDirectory, "Downloads")
	if err := os.MkdirAll(downloadsPath, 0o700); err != nil {
		t.Fatalf("create downloads folder: %v", err)
	}
	invoicePath := filepath.Join(downloadsPath, "invoice.pdf")
	if err := os.WriteFile(invoicePath, []byte("pdf"), 0o600); err != nil {
		t.Fatalf("write downloads file: %v", err)
	}

	server := &Server{
		sandboxPaths:   sandbox.PathsForRepo(repoRoot),
		configStateDir: filepath.Join(repoRoot, "runtime", "state", "config"),
		resolveUserHomeDir: func() (string, error) {
			return userHomeDirectory, nil
		},
		appendAuditEvent: func(string, ledger.Event) error { return nil },
		auditPath:        filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"),
		now:              time.Now,
	}
	if err := server.sandboxPaths.Ensure(); err != nil {
		t.Fatalf("ensure sandbox paths: %v", err)
	}

	statusResponse, err := server.updateFolderAccess(capabilityToken{
		ControlSessionID:   "cs-test",
		ActorLabel:         "operator",
		ClientSessionLabel: "session-test",
	}, FolderAccessUpdateRequest{GrantedIDs: []string{"downloads"}})
	if err != nil {
		t.Fatalf("update folder access: %v", err)
	}

	if len(statusResponse.Folders) != 4 {
		t.Fatalf("expected all folder presets in status, got %#v", statusResponse.Folders)
	}
	downloadsStatus := folderStatusByID(statusResponse.Folders, folderAccessDownloadsID)
	// Downloads is HostAccessOnly: no sandbox mirror; MirrorReady means granted + host folder exists.
	if !downloadsStatus.Granted || !downloadsStatus.MirrorReady || downloadsStatus.EntryCount != 1 || !downloadsStatus.HostAccessOnly {
		t.Fatalf("expected host-direct downloads status, got %#v", downloadsStatus)
	}
	sharedStatus := folderStatusByID(statusResponse.Folders, folderAccessSharedID)
	if !sharedStatus.Granted {
		t.Fatalf("expected shared space to remain always granted, got %#v", sharedStatus)
	}
	if _, err := os.Stat(invoicePath); err != nil {
		t.Fatalf("expected invoice on real Downloads path: %v", err)
	}

	configFile, err := config.LoadJSONConfig[folderAccessConfigFile](server.configStateDir, folderAccessConfigSection)
	if err != nil {
		t.Fatalf("load saved folder access config: %v", err)
	}
	if len(configFile.GrantedIDs) != 2 || configFile.GrantedIDs[0] != folderAccessDownloadsID || configFile.GrantedIDs[1] != folderAccessSharedID {
		t.Fatalf("unexpected saved granted ids: %#v", configFile.GrantedIDs)
	}
}

func TestSyncGrantedFolderAccessSkipsAuditUntilSourceChanges(t *testing.T) {
	repoRoot := t.TempDir()
	userHomeDirectory := t.TempDir()
	sharedHostPath := filepath.Join(userHomeDirectory, defaultSharedFolderName)
	if err := os.MkdirAll(sharedHostPath, 0o700); err != nil {
		t.Fatalf("create shared host folder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sharedHostPath, "notes.txt"), []byte("v1"), 0o600); err != nil {
		t.Fatalf("write shared file: %v", err)
	}
	downloadsPath := filepath.Join(userHomeDirectory, "Downloads")
	if err := os.MkdirAll(downloadsPath, 0o700); err != nil {
		t.Fatalf("create downloads folder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(downloadsPath, "invoice.pdf"), []byte("pdf"), 0o600); err != nil {
		t.Fatalf("write downloads file: %v", err)
	}

	auditedEventCount := 0
	server := &Server{
		sandboxPaths:   sandbox.PathsForRepo(repoRoot),
		configStateDir: filepath.Join(repoRoot, "runtime", "state", "config"),
		resolveUserHomeDir: func() (string, error) {
			return userHomeDirectory, nil
		},
		appendAuditEvent: func(string, ledger.Event) error {
			auditedEventCount++
			return nil
		},
		auditPath: filepath.Join(repoRoot, "runtime", "state", "loopgate_events.jsonl"),
		now:       time.Now,
	}
	if err := server.sandboxPaths.Ensure(); err != nil {
		t.Fatalf("ensure sandbox paths: %v", err)
	}
	if _, err := server.updateFolderAccess(capabilityToken{
		ControlSessionID:   "cs-test",
		ActorLabel:         "operator",
		ClientSessionLabel: "session-test",
	}, FolderAccessUpdateRequest{GrantedIDs: []string{"downloads"}}); err != nil {
		t.Fatalf("seed folder access config: %v", err)
	}

	initialAuditCount := auditedEventCount
	firstSync, err := server.syncGrantedFolderAccess(capabilityToken{
		ControlSessionID:   "cs-test",
		ActorLabel:         "operator",
		ClientSessionLabel: "session-test",
	})
	if err != nil {
		t.Fatalf("first granted-folder sync: %v", err)
	}
	if len(firstSync.ChangedIDs) != 0 {
		t.Fatalf("expected initial compare sync to see no changes after setup, got %#v", firstSync.ChangedIDs)
	}
	if auditedEventCount != initialAuditCount {
		t.Fatalf("expected no new audit writes for unchanged source, got %d -> %d", initialAuditCount, auditedEventCount)
	}

	if err := os.WriteFile(filepath.Join(sharedHostPath, "notes.txt"), []byte("v2"), 0o600); err != nil {
		t.Fatalf("update shared folder file: %v", err)
	}

	secondSync, err := server.syncGrantedFolderAccess(capabilityToken{
		ControlSessionID:   "cs-test",
		ActorLabel:         "operator",
		ClientSessionLabel: "session-test",
	})
	if err != nil {
		t.Fatalf("second granted-folder sync: %v", err)
	}
	if len(secondSync.ChangedIDs) != 1 || secondSync.ChangedIDs[0] != folderAccessSharedID {
		t.Fatalf("expected shared mirror to be reported as changed, got %#v", secondSync.ChangedIDs)
	}
	if auditedEventCount != initialAuditCount+1 {
		t.Fatalf("expected exactly one new audit write after source change, got %d -> %d", initialAuditCount, auditedEventCount)
	}
}

func folderStatusByID(folderStatuses []FolderAccessStatus, folderID string) FolderAccessStatus {
	for _, folderStatus := range folderStatuses {
		if folderStatus.ID == folderID {
			return folderStatus
		}
	}
	return FolderAccessStatus{}
}
