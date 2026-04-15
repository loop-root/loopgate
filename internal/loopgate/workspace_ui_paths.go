package loopgate

import (
	"path/filepath"
	"strings"
)

// Historical workspace path mapping used by local workspace-facing routes.

const sharedWorkspacePath = "shared"

var sandboxToWorkspacePath = map[string]string{
	"workspace": "projects",
	"imports":   "imports",
	"outputs":   "artifacts",
	"scratch":   "research",
	"agents":    "agents",
}

var workspaceToSandboxPath = map[string]string{
	"projects":  "workspace",
	"imports":   "imports",
	"artifacts": "outputs",
	"research":  "scratch",
	"agents":    "agents",
}

// mapWorkspacePathToSandbox converts a UI-facing path to a sandbox-relative path.
func mapWorkspacePathToSandbox(workspacePath string) string {
	cleaned := strings.TrimSpace(workspacePath)
	if cleaned == "" || cleaned == "." {
		return ""
	}
	if cleaned == sharedWorkspacePath {
		return "imports/shared"
	}
	if strings.HasPrefix(filepath.ToSlash(cleaned), sharedWorkspacePath+"/") {
		return "imports/shared/" + strings.TrimPrefix(filepath.ToSlash(cleaned), sharedWorkspacePath+"/")
	}

	parts := strings.SplitN(filepath.ToSlash(cleaned), "/", 2)
	if sandboxName, ok := workspaceToSandboxPath[parts[0]]; ok {
		if len(parts) == 1 {
			return sandboxName
		}
		return sandboxName + "/" + parts[1]
	}
	return cleaned
}

// mapSandboxPathToWorkspace converts a sandbox-relative path to a UI-facing path.
func mapSandboxPathToWorkspace(sandboxPath string) string {
	cleaned := strings.TrimSpace(sandboxPath)
	if cleaned == "" || cleaned == "." {
		return ""
	}
	if cleaned == "imports/shared" {
		return sharedWorkspacePath
	}
	if strings.HasPrefix(filepath.ToSlash(cleaned), "imports/shared/") {
		return sharedWorkspacePath + "/" + strings.TrimPrefix(filepath.ToSlash(cleaned), "imports/shared/")
	}

	parts := strings.SplitN(filepath.ToSlash(cleaned), "/", 2)
	if workspaceName, ok := sandboxToWorkspacePath[parts[0]]; ok {
		if len(parts) == 1 {
			return workspaceName
		}
		return workspaceName + "/" + parts[1]
	}
	return cleaned
}
