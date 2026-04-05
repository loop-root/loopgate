package loopgate

import (
	"path/filepath"
	"strings"
)

// Historical UI path mapping used by local workspace-facing routes.

const havenSharedPath = "shared"

var havenSandboxToHaven = map[string]string{
	"workspace": "projects",
	"imports":   "imports",
	"outputs":   "artifacts",
	"scratch":   "research",
	"agents":    "agents",
}

var havenHavenToSandbox = map[string]string{
	"projects":  "workspace",
	"imports":   "imports",
	"artifacts": "outputs",
	"research":  "scratch",
	"agents":    "agents",
}

// mapHavenPathToSandbox converts a UI-facing path to a sandbox-relative path.
func mapHavenPathToSandbox(havenPath string) string {
	cleaned := strings.TrimSpace(havenPath)
	if cleaned == "" || cleaned == "." {
		return ""
	}
	if cleaned == havenSharedPath {
		return "imports/shared"
	}
	if strings.HasPrefix(filepath.ToSlash(cleaned), havenSharedPath+"/") {
		return "imports/shared/" + strings.TrimPrefix(filepath.ToSlash(cleaned), havenSharedPath+"/")
	}

	parts := strings.SplitN(filepath.ToSlash(cleaned), "/", 2)
	if sandboxName, ok := havenHavenToSandbox[parts[0]]; ok {
		if len(parts) == 1 {
			return sandboxName
		}
		return sandboxName + "/" + parts[1]
	}
	return cleaned
}

// mapSandboxPathToHaven converts a sandbox-relative path to a UI-facing path.
func mapSandboxPathToHaven(sandboxPath string) string {
	cleaned := strings.TrimSpace(sandboxPath)
	if cleaned == "" || cleaned == "." {
		return ""
	}
	if cleaned == "imports/shared" {
		return havenSharedPath
	}
	if strings.HasPrefix(filepath.ToSlash(cleaned), "imports/shared/") {
		return havenSharedPath + "/" + strings.TrimPrefix(filepath.ToSlash(cleaned), "imports/shared/")
	}

	parts := strings.SplitN(filepath.ToSlash(cleaned), "/", 2)
	if havenName, ok := havenSandboxToHaven[parts[0]]; ok {
		if len(parts) == 1 {
			return havenName
		}
		return havenName + "/" + parts[1]
	}
	return cleaned
}
