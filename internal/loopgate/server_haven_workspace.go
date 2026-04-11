package loopgate

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	maxHavenPreviewBytes     = 64 * 1024
	havenSharedWorkspacePath = "shared"
)

var havenWorkspaceSandboxToDisplay = map[string]string{
	"workspace": "projects",
	"imports":   "imports",
	"outputs":   "artifacts",
	"scratch":   "research",
	"agents":    "agents",
}

var havenWorkspaceDisplayToSandbox = map[string]string{
	"projects":  "workspace",
	"imports":   "imports",
	"artifacts": "outputs",
	"research":  "scratch",
	"agents":    "agents",
}

func (server *Server) handleHavenWorkspaceList(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityUIRead) {
		return
	}
	if !server.requireCapabilityScope(writer, tokenClaims, "fs_list") {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var listRequest HavenWorkspaceListRequest
	if err := decodeJSONBytes(requestBodyBytes, &listRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	listResponse, err := server.havenWorkspaceListResponse(tokenClaims, listRequest)
	if err != nil {
		server.writeJSON(writer, http.StatusOK, HavenWorkspaceListResponse{Path: strings.TrimSpace(listRequest.Path), Error: redactSandboxError(err)})
		return
	}
	server.writeJSON(writer, http.StatusOK, listResponse)
}

func (server *Server) handleHavenWorkspaceHostLayout(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityUIRead) {
		return
	}
	if !server.requireCapabilityScope(writer, tokenClaims, "fs_list") {
		return
	}
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}
	if err := server.sandboxPaths.Ensure(); err != nil {
		server.writeJSON(writer, http.StatusOK, HavenWorkspaceHostLayoutResponse{
			Error: "sandbox layout unavailable",
		})
		return
	}
	projectsPath := server.sandboxPaths.Workspace
	researchPath := server.sandboxPaths.Scratch
	projectsResolved, err := filepath.EvalSymlinks(projectsPath)
	if err != nil {
		projectsResolved = projectsPath
	}
	researchResolved, err := filepath.EvalSymlinks(researchPath)
	if err != nil {
		researchResolved = researchPath
	}
	if err := server.logEvent("haven.ui.workspace_host_layout", tokenClaims.ControlSessionID, map[string]interface{}{
		"control_session_id": tokenClaims.ControlSessionID,
		"actor_label":        tokenClaims.ActorLabel,
	}); err != nil {
		server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "control-plane audit is unavailable",
			DenialCode:   DenialCodeAuditUnavailable,
		})
		return
	}
	server.writeJSON(writer, http.StatusOK, HavenWorkspaceHostLayoutResponse{
		ProjectsHostPath: filepath.Clean(projectsResolved),
		ResearchHostPath: filepath.Clean(researchResolved),
	})
}

func (server *Server) havenWorkspaceListResponse(tokenClaims capabilityToken, listRequest HavenWorkspaceListRequest) (HavenWorkspaceListResponse, error) {
	requestedPath := strings.TrimSpace(listRequest.Path)
	if requestedPath == "" {
		return server.listHavenWorkspaceRoot(), nil
	}

	sandboxPath := havenMapWorkspacePathToSandbox(requestedPath)
	listResponse, err := server.listSandboxDirectory(tokenClaims, SandboxListRequest{SandboxPath: sandboxPath})
	if err != nil {
		return HavenWorkspaceListResponse{}, err
	}

	havenPath := havenMapWorkspacePathToDisplay(listResponse.SandboxPath)
	entries := make([]HavenWorkspaceListEntry, 0, len(listResponse.Entries))
	for _, entry := range listResponse.Entries {
		entryPath := entry.Name
		if havenPath != "" {
			entryPath = path.Join(havenPath, entry.Name)
		}
		entries = append(entries, HavenWorkspaceListEntry{
			Name:       entry.Name,
			Path:       entryPath,
			EntryType:  entry.EntryType,
			SizeBytes:  entry.SizeBytes,
			ModTimeUTC: entry.ModTimeUTC,
		})
	}
	return HavenWorkspaceListResponse{Path: havenPath, Entries: entries}, nil
}

func (server *Server) listHavenWorkspaceRoot() HavenWorkspaceListResponse {
	_ = server.sandboxPaths.Ensure()
	type rootEntry struct {
		name string
		path string
	}
	rootEntries := []rootEntry{
		{name: "projects", path: server.sandboxPaths.Workspace},
		{name: "imports", path: server.sandboxPaths.Imports},
		{name: "artifacts", path: server.sandboxPaths.Outputs},
		{name: "research", path: server.sandboxPaths.Scratch},
		{name: "agents", path: server.sandboxPaths.Agents},
	}
	sharedPath := filepath.Join(server.sandboxPaths.Imports, "shared")
	if info, err := os.Stat(sharedPath); err == nil && info.IsDir() {
		rootEntries = append(rootEntries, rootEntry{name: havenSharedWorkspacePath, path: sharedPath})
	}
	sort.Slice(rootEntries, func(leftIndex int, rightIndex int) bool {
		return rootEntries[leftIndex].name < rootEntries[rightIndex].name
	})

	entries := make([]HavenWorkspaceListEntry, 0, len(rootEntries))
	for _, rootEntry := range rootEntries {
		info, err := os.Stat(rootEntry.path)
		if err != nil {
			continue
		}
		entries = append(entries, HavenWorkspaceListEntry{
			Name:       rootEntry.name,
			Path:       rootEntry.name,
			EntryType:  "directory",
			SizeBytes:  info.Size(),
			ModTimeUTC: info.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
		})
	}

	return HavenWorkspaceListResponse{Path: "", Entries: entries}
}

func havenMapWorkspacePathToSandbox(havenPath string) string {
	cleanedPath := strings.TrimSpace(havenPath)
	if cleanedPath == "" || cleanedPath == "." {
		return ""
	}
	if cleanedPath == havenSharedWorkspacePath {
		return "imports/shared"
	}
	normalizedPath := filepath.ToSlash(cleanedPath)
	if strings.HasPrefix(normalizedPath, havenSharedWorkspacePath+"/") {
		return "imports/shared/" + strings.TrimPrefix(normalizedPath, havenSharedWorkspacePath+"/")
	}
	parts := strings.SplitN(normalizedPath, "/", 2)
	if sandboxName, ok := havenWorkspaceDisplayToSandbox[parts[0]]; ok {
		if len(parts) == 1 {
			return sandboxName
		}
		return sandboxName + "/" + parts[1]
	}
	return normalizedPath
}

func havenMapWorkspacePathToDisplay(sandboxPath string) string {
	cleanedPath := strings.TrimSpace(sandboxPath)
	if cleanedPath == "" || cleanedPath == "." {
		return ""
	}
	normalizedPath := filepath.ToSlash(cleanedPath)
	if normalizedPath == "imports/shared" {
		return havenSharedWorkspacePath
	}
	if strings.HasPrefix(normalizedPath, "imports/shared/") {
		return havenSharedWorkspacePath + "/" + strings.TrimPrefix(normalizedPath, "imports/shared/")
	}
	parts := strings.SplitN(normalizedPath, "/", 2)
	if displayName, ok := havenWorkspaceSandboxToDisplay[parts[0]]; ok {
		if len(parts) == 1 {
			return displayName
		}
		return displayName + "/" + parts[1]
	}
	return normalizedPath
}

func (server *Server) handleHavenWorkspacePreview(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityUIRead) {
		return
	}
	if !server.requireCapabilityScope(writer, tokenClaims, "fs_read") {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var previewRequest HavenWorkspacePreviewRequest
	if err := decodeJSONBytes(requestBodyBytes, &previewRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if strings.TrimSpace(previewRequest.Path) == "" {
		server.writeJSON(writer, http.StatusBadRequest, HavenWorkspacePreviewResponse{Error: "path is required"})
		return
	}

	sandboxPath := mapHavenPathToSandbox(previewRequest.Path)
	content, err := server.havenReadFileViaCapability(request.Context(), tokenClaims, sandboxPath)
	if err != nil {
		server.writeJSON(writer, http.StatusOK, HavenWorkspacePreviewResponse{Path: previewRequest.Path, Error: err.Error()})
		return
	}
	truncated := false
	if len(content) > maxHavenPreviewBytes {
		content = content[:maxHavenPreviewBytes]
		truncated = true
	}
	server.writeJSON(writer, http.StatusOK, HavenWorkspacePreviewResponse{
		Content:   content,
		Truncated: truncated,
		Path:      previewRequest.Path,
	})
}
