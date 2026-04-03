package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"morph/internal/loopgate"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Haven workspace directory name mapping.
// The sandbox uses internal names; Haven presents user-friendly names.
var (
	sandboxToHaven = map[string]string{
		"workspace": "projects",
		"imports":   "imports",
		"outputs":   "artifacts",
		"scratch":   "research",
		"agents":    "agents",
	}
	havenToSandbox = map[string]string{
		"projects":  "workspace",
		"imports":   "imports",
		"artifacts": "outputs",
		"research":  "scratch",
		"agents":    "agents",
	}
)

const sharedHavenPath = "shared"

// WorkspaceListEntry is a directory entry with Haven-friendly naming.
type WorkspaceListEntry struct {
	Name       string `json:"name"`
	EntryType  string `json:"entry_type"`
	SizeBytes  int64  `json:"size_bytes"`
	ModTimeUTC string `json:"mod_time_utc"`
}

// WorkspaceListResponse wraps a sandbox directory listing with mapped names.
type WorkspaceListResponse struct {
	Path    string               `json:"path"`
	Entries []WorkspaceListEntry `json:"entries"`
}

// WorkspaceImportResponse wraps the result of a file/directory import.
type WorkspaceImportResponse struct {
	Imported bool   `json:"imported"`
	Name     string `json:"name,omitempty"`
	Path     string `json:"path,omitempty"`
	Error    string `json:"error,omitempty"`
}

// WorkspaceExportResponse wraps the result of an export.
type WorkspaceExportResponse struct {
	Exported bool   `json:"exported"`
	HostPath string `json:"host_path,omitempty"`
	Error    string `json:"error,omitempty"`
}

// WorkspaceList lists a sandbox directory, mapping sandbox names to Haven names.
// An empty path lists the home root.
func (app *HavenApp) WorkspaceList(path string) (WorkspaceListResponse, error) {
	sandboxPath := mapHavenPathToSandbox(path)

	// Root listing: scan sandbox home directly since Loopgate rejects empty paths.
	if sandboxPath == "" || sandboxPath == "." {
		return app.listSandboxRoot()
	}

	resp, err := app.loopgateClient.SandboxList(context.Background(), loopgate.SandboxListRequest{
		SandboxPath: sandboxPath,
	})
	if err != nil {
		return WorkspaceListResponse{}, fmt.Errorf("list workspace: %w", err)
	}

	entries := make([]WorkspaceListEntry, 0, len(resp.Entries))
	for _, entry := range resp.Entries {
		entries = append(entries, WorkspaceListEntry{
			Name:       entry.Name,
			EntryType:  entry.EntryType,
			SizeBytes:  entry.SizeBytes,
			ModTimeUTC: entry.ModTimeUTC,
		})
	}

	return WorkspaceListResponse{
		Path:    mapSandboxPathToHaven(resp.SandboxPath),
		Entries: entries,
	}, nil
}

// listSandboxRoot lists the sandbox home directory directly, mapping
// sandbox directory names to Haven display names and filtering to
// only show directories that exist.
func (app *HavenApp) listSandboxRoot() (WorkspaceListResponse, error) {
	scanDir := app.sandboxHome
	if scanDir == "" {
		return WorkspaceListResponse{}, fmt.Errorf("sandbox home not configured")
	}

	dirEntries, err := os.ReadDir(scanDir)
	if err != nil {
		return WorkspaceListResponse{}, fmt.Errorf("read sandbox home: %w", err)
	}

	entries := make([]WorkspaceListEntry, 0, len(dirEntries))
	for _, de := range dirEntries {
		if strings.HasPrefix(de.Name(), ".") {
			continue
		}
		// Only show user-facing directories at root level; skip internal
		// sandbox directories (tmp, logs) that are not meant for the user.
		if _, isUserFacing := sandboxToHaven[de.Name()]; !isUserFacing {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		displayName := sandboxToHaven[de.Name()]
		entryType := "file"
		if de.IsDir() {
			entryType = "directory"
		}
		entries = append(entries, WorkspaceListEntry{
			Name:       displayName,
			EntryType:  entryType,
			SizeBytes:  info.Size(),
			ModTimeUTC: info.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
		})
	}

	sharedMirrorPath := filepath.Join(scanDir, "imports", "shared")
	if sharedInfo, err := os.Stat(sharedMirrorPath); err == nil && sharedInfo.IsDir() {
		entries = append(entries, WorkspaceListEntry{
			Name:       sharedHavenPath,
			EntryType:  "directory",
			SizeBytes:  sharedInfo.Size(),
			ModTimeUTC: sharedInfo.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
		})
	}

	return WorkspaceListResponse{
		Path:    "",
		Entries: entries,
	}, nil
}

// WorkspaceImportFile opens a native file dialog and imports the selected file.
func (app *HavenApp) WorkspaceImportFile() WorkspaceImportResponse {
	if app.wailsCtx == nil {
		return WorkspaceImportResponse{Error: "wails context not ready"}
	}

	selection, err := wailsruntime.OpenFileDialog(app.wailsCtx, wailsruntime.OpenDialogOptions{
		Title: "Import File into Workspace",
	})
	if err != nil {
		return WorkspaceImportResponse{Error: fmt.Sprintf("file dialog: %v", err)}
	}
	if selection == "" {
		return WorkspaceImportResponse{Imported: false}
	}

	return app.doImport(selection)
}

// WorkspaceImportDirectory opens a native directory dialog and imports the selection.
func (app *HavenApp) WorkspaceImportDirectory() WorkspaceImportResponse {
	if app.wailsCtx == nil {
		return WorkspaceImportResponse{Error: "wails context not ready"}
	}

	selection, err := wailsruntime.OpenDirectoryDialog(app.wailsCtx, wailsruntime.OpenDialogOptions{
		Title: "Import Directory into Workspace",
	})
	if err != nil {
		return WorkspaceImportResponse{Error: fmt.Sprintf("directory dialog: %v", err)}
	}
	if selection == "" {
		return WorkspaceImportResponse{Imported: false}
	}

	return app.doImport(selection)
}

// WorkspaceImportPath programmatically imports a host path (for drag-and-drop or chat attach).
func (app *HavenApp) WorkspaceImportPath(hostPath string) WorkspaceImportResponse {
	if strings.TrimSpace(hostPath) == "" {
		return WorkspaceImportResponse{Error: "host path is required"}
	}
	return app.doImport(hostPath)
}

// WorkspaceExport exports a sandbox artifact to the host via a save dialog.
func (app *HavenApp) WorkspaceExport(sandboxPath string) WorkspaceExportResponse {
	if strings.TrimSpace(sandboxPath) == "" {
		return WorkspaceExportResponse{Error: "sandbox path is required"}
	}
	if app.wailsCtx == nil {
		return WorkspaceExportResponse{Error: "wails context not ready"}
	}

	resolvedSandboxPath := mapHavenPathToSandbox(sandboxPath)

	destination, err := wailsruntime.SaveFileDialog(app.wailsCtx, wailsruntime.SaveDialogOptions{
		Title: "Export from Workspace",
	})
	if err != nil {
		return WorkspaceExportResponse{Error: fmt.Sprintf("save dialog: %v", err)}
	}
	if destination == "" {
		return WorkspaceExportResponse{Exported: false}
	}

	resp, err := app.loopgateClient.SandboxExport(context.Background(), loopgate.SandboxExportRequest{
		SandboxSourcePath:   resolvedSandboxPath,
		HostDestinationPath: destination,
	})
	if err != nil {
		return WorkspaceExportResponse{Error: fmt.Sprintf("export: %v", err)}
	}

	return WorkspaceExportResponse{
		Exported: true,
		HostPath: resp.HostPath,
	}
}

// WorkspacePreviewFile reads a small file from the sandbox for inline preview.
// Returns the file contents (truncated to maxPreviewBytes) and metadata.
func (app *HavenApp) WorkspacePreviewFile(path string) WorkspacePreviewResponse {
	if strings.TrimSpace(path) == "" {
		return WorkspacePreviewResponse{Error: "path is required"}
	}

	sandboxPath := mapHavenPathToSandbox(path)

	// Read via fs_read capability.
	response, err := app.loopgateClient.ExecuteCapability(context.Background(), loopgate.CapabilityRequest{
		RequestID:  fmt.Sprintf("preview-%d", time.Now().UnixNano()),
		Actor:      "haven",
		Capability: "fs_read",
		Arguments:  map[string]string{"path": sandboxPath},
	})
	if err != nil {
		return WorkspacePreviewResponse{Error: fmt.Sprintf("read file: %v", err)}
	}
	if response.Status != loopgate.ResponseStatusSuccess {
		reason := response.DenialReason
		if reason == "" {
			reason = "file not readable"
		}
		return WorkspacePreviewResponse{Error: reason}
	}

	content, _ := response.StructuredResult["content"].(string)

	truncated := false
	if len(content) > maxPreviewBytes {
		content = content[:maxPreviewBytes]
		truncated = true
	}

	return WorkspacePreviewResponse{
		Content:   content,
		Truncated: truncated,
		Path:      path,
	}
}

// WorkspacePreviewResponse wraps the result of a file preview.
type WorkspacePreviewResponse struct {
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
	Path      string `json:"path"`
	Error     string `json:"error,omitempty"`
}

const maxPreviewBytes = 64 * 1024 // 64 KB preview for editing

// WorkspaceWriteFile writes content to a file in the sandbox.
func (app *HavenApp) WorkspaceWriteFile(path string, content string) WorkspaceWriteResponse {
	if strings.TrimSpace(path) == "" {
		return WorkspaceWriteResponse{Error: "path is required"}
	}

	sandboxPath := mapHavenPathToSandbox(path)

	_, err := app.loopgateClient.ExecuteCapability(context.Background(), loopgate.CapabilityRequest{
		RequestID:  fmt.Sprintf("write-%d", time.Now().UnixNano()),
		Actor:      "haven",
		Capability: "fs_write",
		Arguments:  map[string]string{"path": sandboxPath, "content": content},
	})
	if err != nil {
		return WorkspaceWriteResponse{Error: fmt.Sprintf("write file: %v", err)}
	}

	app.emitter.Emit("haven:file_changed", map[string]interface{}{
		"action": "write",
		"path":   sandboxPath,
	})

	return WorkspaceWriteResponse{Written: true}
}

// WorkspaceWriteResponse wraps the result of a file write.
type WorkspaceWriteResponse struct {
	Written bool   `json:"written"`
	Error   string `json:"error,omitempty"`
}

// WorkspaceCreateDir creates a new directory in the sandbox.
func (app *HavenApp) WorkspaceCreateDir(path string) WorkspaceCreateDirResponse {
	if strings.TrimSpace(path) == "" {
		return WorkspaceCreateDirResponse{Error: "path is required"}
	}

	sandboxPath := mapHavenPathToSandbox(path)
	response, err := app.loopgateClient.ExecuteCapability(context.Background(), loopgate.CapabilityRequest{
		RequestID:  fmt.Sprintf("mkdir-%d", time.Now().UnixNano()),
		Actor:      "haven",
		Capability: "fs_mkdir",
		Arguments:  map[string]string{"path": sandboxPath},
	})
	if err != nil {
		return WorkspaceCreateDirResponse{Error: fmt.Sprintf("create directory: %v", err)}
	}
	if response.Status != loopgate.ResponseStatusSuccess {
		reason := response.DenialReason
		if reason == "" {
			reason = "directory not created"
		}
		return WorkspaceCreateDirResponse{Error: reason}
	}

	app.emitter.Emit("haven:file_changed", map[string]interface{}{
		"action": "create_dir",
		"path":   path,
	})

	return WorkspaceCreateDirResponse{Created: true}
}

// WorkspaceCreateDirResponse wraps the result of creating a directory.
type WorkspaceCreateDirResponse struct {
	Created bool   `json:"created"`
	Error   string `json:"error,omitempty"`
}

// WorkspaceDelete deletes a file or empty directory in the sandbox.
func (app *HavenApp) WorkspaceDelete(path string) WorkspaceDeleteResponse {
	if strings.TrimSpace(path) == "" {
		return WorkspaceDeleteResponse{Error: "path is required"}
	}

	sandboxPath := mapHavenPathToSandbox(path)
	absPath := filepath.Join(app.sandboxHome, filepath.FromSlash(sandboxPath))

	// TODO: Route through Loopgate fs_delete capability once implemented.
	// Currently bypasses policy and audit — do not use in production without fixing.
	// Atomic: we do not check-then-delete. os.Remove fails with ENOTEMPTY if the
	// directory has contents, avoiding a TOCTOU race.
	err := os.Remove(absPath)
	if err != nil {
		if isNotEmptyError(err) {
			return WorkspaceDeleteResponse{Error: "directory is not empty"}
		}
		return WorkspaceDeleteResponse{Error: fmt.Sprintf("delete: %v", err)}
	}

	app.emitter.Emit("haven:file_changed", map[string]interface{}{
		"action": "delete",
		"path":   sandboxPath,
	})

	return WorkspaceDeleteResponse{Deleted: true}
}

// WorkspaceDeleteResponse wraps the result of a delete operation.
type WorkspaceDeleteResponse struct {
	Deleted bool   `json:"deleted"`
	Error   string `json:"error,omitempty"`
}

// WorkspaceRename renames a file or directory in the sandbox.
func (app *HavenApp) WorkspaceRename(oldPath string, newName string) WorkspaceRenameResponse {
	if strings.TrimSpace(oldPath) == "" || strings.TrimSpace(newName) == "" {
		return WorkspaceRenameResponse{Error: "path and new name are required"}
	}
	if strings.Contains(newName, "/") || strings.Contains(newName, "\\") {
		return WorkspaceRenameResponse{Error: "new name must not contain path separators"}
	}

	sandboxOld := mapHavenPathToSandbox(oldPath)
	absOld := filepath.Join(app.sandboxHome, filepath.FromSlash(sandboxOld))
	absNew := filepath.Join(filepath.Dir(absOld), newName)

	// TODO: Route through Loopgate fs_rename capability once implemented.
	// Currently bypasses policy and audit — do not use in production without fixing.
	if err := os.Rename(absOld, absNew); err != nil {
		return WorkspaceRenameResponse{Error: fmt.Sprintf("rename: %v", err)}
	}

	app.emitter.Emit("haven:file_changed", map[string]interface{}{
		"action": "rename",
		"path":   sandboxOld,
	})

	return WorkspaceRenameResponse{Renamed: true}
}

func isNotEmptyError(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == syscall.ENOTEMPTY || errno == syscall.Errno(145)
	}
	return false
}

// WorkspaceRenameResponse wraps the result of a rename operation.
type WorkspaceRenameResponse struct {
	Renamed bool   `json:"renamed"`
	Error   string `json:"error,omitempty"`
}

func (app *HavenApp) doImport(hostPath string) WorkspaceImportResponse {
	destinationName := filepath.Base(hostPath)

	// Read original content before import (for diff tracking)
	origContent, readErr := os.ReadFile(hostPath)

	resp, err := app.loopgateClient.SandboxImport(context.Background(), loopgate.SandboxImportRequest{
		HostSourcePath:  hostPath,
		DestinationName: destinationName,
	})
	if err != nil {
		return WorkspaceImportResponse{Error: fmt.Sprintf("import: %v", err)}
	}

	havenPath := mapSandboxPathToHaven(resp.SandboxRelativePath)

	// Store original for diff tracking (best-effort, non-fatal)
	if readErr == nil && len(origContent) <= 1<<20 { // only track files under 1MB
		_ = app.StoreOriginal(havenPath, string(origContent))
	}

	return WorkspaceImportResponse{
		Imported: true,
		Name:     destinationName,
		Path:     havenPath,
	}
}

// mapHavenPathToSandbox converts a Haven-facing path to sandbox path.
// e.g. "projects/myapp" -> "workspace/myapp"
func mapHavenPathToSandbox(havenPath string) string {
	cleaned := strings.TrimSpace(havenPath)
	if cleaned == "" || cleaned == "." {
		return ""
	}
	if cleaned == sharedHavenPath {
		return "imports/shared"
	}
	if strings.HasPrefix(filepath.ToSlash(cleaned), sharedHavenPath+"/") {
		return "imports/shared/" + strings.TrimPrefix(filepath.ToSlash(cleaned), sharedHavenPath+"/")
	}

	parts := strings.SplitN(filepath.ToSlash(cleaned), "/", 2)
	if sandboxName, ok := havenToSandbox[parts[0]]; ok {
		if len(parts) == 1 {
			return sandboxName
		}
		return sandboxName + "/" + parts[1]
	}
	return cleaned
}

// mapSandboxPathToHaven converts a sandbox path to Haven-facing path.
// e.g. "workspace/myapp" -> "projects/myapp"
func mapSandboxPathToHaven(sandboxPath string) string {
	cleaned := strings.TrimSpace(sandboxPath)
	if cleaned == "" || cleaned == "." {
		return ""
	}
	if cleaned == "imports/shared" {
		return sharedHavenPath
	}
	if strings.HasPrefix(filepath.ToSlash(cleaned), "imports/shared/") {
		return sharedHavenPath + "/" + strings.TrimPrefix(filepath.ToSlash(cleaned), "imports/shared/")
	}

	parts := strings.SplitN(filepath.ToSlash(cleaned), "/", 2)
	if havenName, ok := sandboxToHaven[parts[0]]; ok {
		if len(parts) == 1 {
			return havenName
		}
		return havenName + "/" + parts[1]
	}
	return cleaned
}
