package loopgate

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"morph/internal/sandbox"
)

const havenWorkingNotesSandboxDir = "scratch/notes"

func (server *Server) handleHavenWorkingNotes(writer http.ResponseWriter, request *http.Request) {
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

	listResponse, err := server.listSandboxDirectory(tokenClaims, SandboxListRequest{SandboxPath: havenWorkingNotesSandboxDir})
	if err != nil {
		if errors.Is(err, sandbox.ErrSandboxSourceUnavailable) {
			server.writeJSON(writer, http.StatusOK, HavenWorkingNotesResponse{Notes: nil})
			return
		}
		server.writeJSON(writer, sandboxHTTPStatus(err), CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: redactSandboxError(err),
			DenialCode:   sandboxDenialCode(err),
			Redacted:     true,
		})
		return
	}

	notes := make([]HavenWorkingNoteSummary, 0, len(listResponse.Entries))
	for _, entry := range listResponse.Entries {
		if entry.EntryType != "file" || !havenIsWorkingNoteFile(entry.Name) {
			continue
		}
		sandboxPath := filepath.ToSlash(filepath.Join(havenWorkingNotesSandboxDir, entry.Name))
		content, readErr := server.havenReadFileViaCapability(request.Context(), tokenClaims, sandboxPath)
		if readErr != nil {
			content = ""
		}
		preview, title := havenSummarizeWorkingNoteContent(content, entry.Name)
		notes = append(notes, HavenWorkingNoteSummary{
			Path:         fmt.Sprintf("research/notes/%s", entry.Name),
			Title:        title,
			Preview:      preview,
			UpdatedAtUTC: entry.ModTimeUTC,
		})
	}
	sort.Slice(notes, func(i, j int) bool {
		return notes[i].UpdatedAtUTC > notes[j].UpdatedAtUTC
	})
	server.writeJSON(writer, http.StatusOK, HavenWorkingNotesResponse{Notes: notes})
}

func (server *Server) handleHavenWorkingNotesEntry(writer http.ResponseWriter, request *http.Request) {
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
	if !server.requireCapabilityScope(writer, tokenClaims, "fs_read") {
		return
	}
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	havenPath := strings.TrimSpace(request.URL.Query().Get("path"))
	if havenPath == "" {
		server.writeJSON(writer, http.StatusBadRequest, HavenWorkingNoteResponse{Error: "path is required"})
		return
	}
	sandboxPath := mapHavenPathToSandbox(havenPath)
	if !strings.HasPrefix(sandboxPath, havenWorkingNotesSandboxDir+"/") {
		server.writeJSON(writer, http.StatusBadRequest, HavenWorkingNoteResponse{Error: "working note path is required"})
		return
	}

	content, err := server.havenReadFileViaCapability(request.Context(), tokenClaims, sandboxPath)
	if err != nil {
		server.writeJSON(writer, http.StatusOK, HavenWorkingNoteResponse{Path: havenPath, Error: err.Error()})
		return
	}
	_, title := havenSummarizeWorkingNoteContent(content, filepath.Base(havenPath))
	server.writeJSON(writer, http.StatusOK, HavenWorkingNoteResponse{
		Path:    havenPath,
		Title:   title,
		Content: content,
	})
}

func (server *Server) handleHavenWorkingNotesSave(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityUIWrite) {
		return
	}
	if !server.requireCapabilityScope(writer, tokenClaims, "notes.write") {
		return
	}
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var saveRequest HavenWorkingNoteSaveRequest
	if err := decodeJSONBytes(requestBodyBytes, &saveRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	if strings.TrimSpace(saveRequest.Content) == "" {
		server.writeJSON(writer, http.StatusBadRequest, HavenWorkingNoteSaveResponse{Error: "note content is required"})
		return
	}

	capReq := CapabilityRequest{
		RequestID:  fmt.Sprintf("haven-ui-notes-save-%d", server.now().UnixNano()),
		Capability: "notes.write",
		Arguments: map[string]string{
			"path":  strings.TrimSpace(saveRequest.Path),
			"title": strings.TrimSpace(saveRequest.Title),
			"body":  strings.TrimSpace(saveRequest.Content),
		},
	}
	resp := server.executeCapabilityRequest(request.Context(), tokenClaims, capReq, false)
	if resp.Status != ResponseStatusSuccess {
		reason := resp.DenialReason
		if reason == "" {
			reason = "working note could not be saved"
		}
		server.writeJSON(writer, httpStatusForResponse(resp), HavenWorkingNoteSaveResponse{Error: reason})
		return
	}

	savedPath := havenDeriveWorkingNoteSavedPath(saveRequest.Path, saveRequest.Title, resp)
	title := havenWorkingNoteTitleFromFilename(filepath.Base(savedPath))
	server.writeJSON(writer, http.StatusOK, HavenWorkingNoteSaveResponse{
		Saved: true,
		Path:  savedPath,
		Title: title,
	})
}

func havenDeriveWorkingNoteSavedPath(requestPath string, requestTitle string, resp CapabilityResponse) string {
	if strings.TrimSpace(requestPath) != "" {
		return filepath.ToSlash(filepath.Join("research", "notes", filepath.Base(strings.TrimSpace(requestPath))))
	}
	if resp.StructuredResult != nil {
		if out, ok := resp.StructuredResult["output"].(string); ok {
			const savedPrefix = "Working note saved to "
			if strings.HasPrefix(out, savedPrefix) {
				saved := strings.TrimSpace(strings.TrimPrefix(out, savedPrefix))
				if strings.HasPrefix(saved, "research/notes/") {
					return saved
				}
			}
		}
	}
	fallback := strings.TrimSpace(requestTitle)
	if fallback == "" {
		fallback = "untitled-note"
	}
	normalized := strings.ToLower(strings.ReplaceAll(fallback, " ", "-"))
	return filepath.ToSlash(filepath.Join("research", "notes", normalized+".md"))
}

func havenSummarizeWorkingNoteContent(content string, filename string) (preview string, title string) {
	title = havenWorkingNoteTitleFromFilename(filename)
	if strings.TrimSpace(content) == "" {
		return "No note text yet.", title
	}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	previewLines := make([]string, 0, 3)
	for _, rawLine := range lines {
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "# ") && title == havenWorkingNoteTitleFromFilename(filename) {
			title = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			continue
		}
		previewLines = append(previewLines, trimmed)
		if len(previewLines) == 3 {
			break
		}
	}
	preview = strings.Join(previewLines, " ")
	if preview == "" {
		preview = "No note text yet."
	}
	if len(preview) > 160 {
		preview = preview[:157] + "..."
	}
	return preview, title
}

func havenWorkingNoteTitleFromFilename(filename string) string {
	baseName := strings.TrimSuffix(filename, filepath.Ext(filename))
	baseName = strings.ReplaceAll(baseName, "-", " ")
	baseName = strings.TrimSpace(baseName)
	if baseName == "" {
		return "Untitled Note"
	}
	words := strings.Fields(baseName)
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

func havenIsWorkingNoteFile(filename string) bool {
	lower := strings.ToLower(strings.TrimSpace(filename))
	return strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".txt")
}
