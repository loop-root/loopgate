package loopgate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"morph/internal/sandbox"
	"morph/internal/secrets"
)

const (
	havenJournalSandboxDir      = "scratch/journal"
	havenWorkingNotesSandboxDir = "scratch/notes"
	maxHavenPreviewBytes        = 64 * 1024
	havenSharedWorkspacePath    = "shared"
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

var havenPresenceAllowedStates = map[string]string{
	"blocked":   "Morph is blocked",
	"focused":   "Morph is focused",
	"idle":      "Morph is idle",
	"paused":    "Morph is paused",
	"planning":  "Morph is planning",
	"reviewing": "Morph is reviewing",
	"sleeping":  "Morph is sleeping",
	"working":   "Morph is working",
}

var havenPresenceAllowedAnchors = map[string]string{
	"chat":      "in chat",
	"desk":      "at desk",
	"memory":    "in memory",
	"project":   "in a project",
	"review":    "in review",
	"settings":  "in settings",
	"task":      "on a task",
	"workspace": "in workspace",
}

type havenDeskNoteStateFile struct {
	Notes []HavenDeskNote `json:"notes"`
}

func (server *Server) havenDeskNotesPath() string {
	return filepath.Join(server.repoRoot, "runtime", "state", "haven_desk_notes.json")
}

func (server *Server) havenPresencePath() string {
	return filepath.Join(server.repoRoot, "runtime", "state", "haven_presence.json")
}

func (server *Server) handleHavenDeskNotes(writer http.ResponseWriter, request *http.Request) {
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
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	server.havenDeskNotesMu.Lock()
	defer server.havenDeskNotesMu.Unlock()

	notes, err := server.loadHavenDeskNotesLocked()
	if err != nil {
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "desk notes unavailable",
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	active := activeHavenDeskNotes(notes)
	out := make([]HavenDeskNote, len(active))
	copy(out, active)
	for i := range out {
		if out[i].Action != nil {
			cloned := *out[i].Action
			out[i].Action = &cloned
		}
	}
	server.writeJSON(writer, http.StatusOK, HavenDeskNotesResponse{Notes: out})
}

func (server *Server) handleHavenDeskNotesDismiss(writer http.ResponseWriter, request *http.Request) {
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
	requestBodyBytes, denialResponse, ok := server.readAndVerifySignedBody(writer, request, maxCapabilityBodyBytes, tokenClaims.ControlSessionID)
	if !ok {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	var dismissRequest HavenDeskNoteDismissRequest
	if err := decodeJSONBytes(requestBodyBytes, &dismissRequest); err != nil {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: err.Error(),
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}
	noteID := strings.TrimSpace(dismissRequest.NoteID)
	if noteID == "" {
		server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "note_id is required",
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	server.havenDeskNotesMu.Lock()
	defer server.havenDeskNotesMu.Unlock()

	deskNotes, err := server.loadHavenDeskNotesLocked()
	if err != nil {
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "desk notes unavailable",
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	archivedAtUTC := server.now().UTC().Format(time.RFC3339Nano)
	found := false
	for i := range deskNotes {
		if deskNotes[i].ID != noteID {
			continue
		}
		if deskNotes[i].ArchivedAtUTC != "" {
			server.writeJSON(writer, http.StatusBadRequest, CapabilityResponse{
				Status:       ResponseStatusError,
				DenialReason: "desk note is already dismissed",
				DenialCode:   DenialCodeMalformedRequest,
			})
			return
		}
		deskNotes[i].ArchivedAtUTC = archivedAtUTC
		found = true
		break
	}
	if !found {
		server.writeJSON(writer, http.StatusNotFound, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "desk note not found",
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	if err := server.saveHavenDeskNotesLocked(deskNotes); err != nil {
		server.writeJSON(writer, http.StatusInternalServerError, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "could not save desk notes",
			DenialCode:   DenialCodeMalformedRequest,
		})
		return
	}

	if err := server.logEvent("haven.desk_note.dismissed", tokenClaims.ControlSessionID, map[string]interface{}{
		"control_session_id":   tokenClaims.ControlSessionID,
		"actor_label":          tokenClaims.ActorLabel,
		"client_session_label": tokenClaims.ClientSessionLabel,
		"note_id":              noteID,
	}); err != nil {
		server.writeJSON(writer, http.StatusServiceUnavailable, CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: "control-plane audit is unavailable",
			DenialCode:   DenialCodeAuditUnavailable,
		})
		return
	}

	server.writeJSON(writer, http.StatusOK, map[string]interface{}{"success": true})
}

func (server *Server) loadHavenDeskNotesLocked() ([]HavenDeskNote, error) {
	path := server.havenDeskNotesPath()
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var stateFile havenDeskNoteStateFile
	decoder := json.NewDecoder(bytes.NewReader(rawBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stateFile); err != nil {
		return nil, err
	}
	return stateFile.Notes, nil
}

func (server *Server) saveHavenDeskNotesLocked(deskNotes []HavenDeskNote) error {
	path := server.havenDeskNotesPath()
	stateFile := havenDeskNoteStateFile{Notes: deskNotes}
	jsonBytes, err := json.MarshalIndent(stateFile, "", "  ")
	if err != nil {
		return err
	}
	if len(jsonBytes) == 0 || jsonBytes[len(jsonBytes)-1] != '\n' {
		jsonBytes = append(jsonBytes, '\n')
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tempPath := path + ".tmp"
	f, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(jsonBytes); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return os.Chmod(path, 0o600)
}

func activeHavenDeskNotes(deskNotes []HavenDeskNote) []HavenDeskNote {
	active := make([]HavenDeskNote, 0, len(deskNotes))
	for _, note := range deskNotes {
		if note.ArchivedAtUTC == "" {
			active = append(active, note)
		}
	}
	sort.Slice(active, func(i, j int) bool {
		return active[i].CreatedAtUTC > active[j].CreatedAtUTC
	})
	return active
}

func parseSandboxJournalModTime(modTimeField string) time.Time {
	s := strings.TrimSpace(modTimeField)
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
	}
	if err != nil {
		return time.Time{}
	}
	return t
}

func journalModTimeLocalRFC3339Nano(modTime time.Time) string {
	if modTime.IsZero() {
		return ""
	}
	return modTime.Local().Format(time.RFC3339Nano)
}

// havenJournalFileModTimeUTC returns the journal file mod time for the UI using the Loopgate host's local timezone.
// (JSON field remains updated_at_utc for wire compatibility; value is RFC3339 with local offset.)
func havenJournalFileModTimeUTC(server *Server, tokenClaims capabilityToken, sandboxPath string) string {
	wantBase := filepath.Base(sandboxPath)
	listResponse, err := server.listSandboxDirectory(tokenClaims, SandboxListRequest{SandboxPath: havenJournalSandboxDir})
	if err != nil {
		return ""
	}
	for _, entry := range listResponse.Entries {
		if entry.EntryType == "file" && entry.Name == wantBase {
			return journalModTimeLocalRFC3339Nano(parseSandboxJournalModTime(entry.ModTimeUTC))
		}
	}
	return ""
}

func (server *Server) handleHavenJournalEntries(writer http.ResponseWriter, request *http.Request) {
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

	listResponse, err := server.listSandboxDirectory(tokenClaims, SandboxListRequest{SandboxPath: havenJournalSandboxDir})
	if err != nil {
		if errors.Is(err, sandbox.ErrSandboxSourceUnavailable) {
			server.writeJSON(writer, http.StatusOK, HavenJournalEntriesResponse{Entries: nil})
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

	type journalEntryBuild struct {
		modTime time.Time
		summary HavenJournalEntrySummary
	}
	build := make([]journalEntryBuild, 0, len(listResponse.Entries))
	for _, entry := range listResponse.Entries {
		if entry.EntryType != "file" || !strings.HasSuffix(strings.ToLower(entry.Name), ".md") {
			continue
		}
		sandboxPath := filepath.ToSlash(filepath.Join(havenJournalSandboxDir, entry.Name))
		content, readErr := server.havenReadFileViaCapability(request.Context(), tokenClaims, sandboxPath)
		if readErr != nil {
			content = ""
		}
		preview, entryCount := havenSummarizeJournalContent(content)
		modTime := parseSandboxJournalModTime(entry.ModTimeUTC)
		build = append(build, journalEntryBuild{
			modTime: modTime,
			summary: HavenJournalEntrySummary{
				Path:         fmt.Sprintf("research/journal/%s", entry.Name),
				Title:        havenJournalTitleFromFilename(entry.Name),
				Preview:      preview,
				UpdatedAtUTC: journalModTimeLocalRFC3339Nano(modTime),
				EntryCount:   entryCount,
			},
		})
	}
	sort.Slice(build, func(i, j int) bool {
		zi, zj := build[i].modTime.IsZero(), build[j].modTime.IsZero()
		if zi && zj {
			return false
		}
		if zi {
			return false
		}
		if zj {
			return true
		}
		return build[i].modTime.After(build[j].modTime)
	})
	entries := make([]HavenJournalEntrySummary, 0, len(build))
	for _, row := range build {
		entries = append(entries, row.summary)
	}
	server.writeJSON(writer, http.StatusOK, HavenJournalEntriesResponse{Entries: entries})
}

func (server *Server) handleHavenJournalEntry(writer http.ResponseWriter, request *http.Request) {
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
		server.writeJSON(writer, http.StatusBadRequest, HavenJournalEntryResponse{Error: "path is required"})
		return
	}
	sandboxPath := mapHavenPathToSandbox(havenPath)
	if !strings.HasPrefix(sandboxPath, havenJournalSandboxDir+"/") {
		server.writeJSON(writer, http.StatusBadRequest, HavenJournalEntryResponse{Path: havenPath, Error: "journal path is required"})
		return
	}

	content, err := server.havenReadFileViaCapability(request.Context(), tokenClaims, sandboxPath)
	if err != nil {
		server.writeJSON(writer, http.StatusOK, HavenJournalEntryResponse{Path: havenPath, Error: err.Error()})
		return
	}
	_, entryCount := havenSummarizeJournalContent(content)
	updatedAtUTC := havenJournalFileModTimeUTC(server, tokenClaims, sandboxPath)
	server.writeJSON(writer, http.StatusOK, HavenJournalEntryResponse{
		Path:         havenPath,
		Title:        havenJournalTitleFromFilename(filepath.Base(havenPath)),
		Content:      content,
		EntryCount:   entryCount,
		UpdatedAtUTC: updatedAtUTC,
	})
}

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

func (server *Server) handleHavenPresence(writer http.ResponseWriter, request *http.Request) {
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
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	server.writeJSON(writer, http.StatusOK, server.loadHavenPresenceSnapshot())
}

func (server *Server) handleHavenMorphSleep(writer http.ResponseWriter, request *http.Request) {
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
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	presence := server.loadHavenPresenceSnapshot()
	isSleeping := presence.State == "sleeping"
	server.writeJSON(writer, http.StatusOK, HavenMorphSleepResponse{
		State:      presence.State,
		StatusText: presence.StatusText,
		DetailText: presence.DetailText,
		Anchor:     presence.Anchor,
		IsSleeping: isSleeping,
		IsResting:  isSleeping,
	})
}

func (server *Server) loadHavenPresenceSnapshot() HavenPresenceResponse {
	path := server.havenPresencePath()
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		return normalizedHavenPresenceSnapshot(HavenPresenceResponse{})
	}
	var snapshot HavenPresenceResponse
	if err := json.Unmarshal(rawBytes, &snapshot); err != nil {
		return normalizedHavenPresenceSnapshot(HavenPresenceResponse{})
	}
	return normalizedHavenPresenceSnapshot(snapshot)
}

func normalizedHavenPresenceSnapshot(rawSnapshot HavenPresenceResponse) HavenPresenceResponse {
	normalizedState := normalizeHavenPresenceState(rawSnapshot.State)
	normalizedAnchor := normalizeHavenPresenceAnchor(rawSnapshot.Anchor)
	normalizedSnapshot := HavenPresenceResponse{
		State:      normalizedState,
		StatusText: havenPresenceAllowedStates[normalizedState],
		Anchor:     normalizedAnchor,
	}
	if detailText, found := havenPresenceAllowedAnchors[normalizedAnchor]; found && detailText != "" {
		normalizedSnapshot.DetailText = detailText
	}
	return normalizedSnapshot
}

func normalizeHavenPresenceState(rawState string) string {
	normalizedState := strings.ToLower(strings.TrimSpace(rawState))
	if _, found := havenPresenceAllowedStates[normalizedState]; found {
		return normalizedState
	}
	return "idle"
}

func normalizeHavenPresenceAnchor(rawAnchor string) string {
	normalizedAnchor := strings.ToLower(strings.TrimSpace(rawAnchor))
	if _, found := havenPresenceAllowedAnchors[normalizedAnchor]; found {
		return normalizedAnchor
	}
	return "desk"
}

func (server *Server) havenReadFileViaCapability(ctx context.Context, tokenClaims capabilityToken, sandboxPath string) (string, error) {
	capReq := CapabilityRequest{
		RequestID:  fmt.Sprintf("haven-ui-fs-read-%d", server.now().UnixNano()),
		Capability: "fs_read",
		Arguments: map[string]string{
			"path": sandboxPath,
		},
	}
	resp := server.executeCapabilityRequest(ctx, tokenClaims, capReq, false)
	if resp.Status != ResponseStatusSuccess {
		reason := resp.DenialReason
		if reason == "" {
			reason = "read denied"
		}
		return "", fmt.Errorf("%s", secrets.RedactText(reason))
	}
	content, _ := resp.StructuredResult["content"].(string)
	return content, nil
}

func havenSummarizeJournalContent(content string) (preview string, entryCount int) {
	if strings.TrimSpace(content) == "" {
		return "No journal text yet.", 0
	}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	entryCount = 0
	currentEntryLines := make([]string, 0, 4)
	latestEntryLines := make([]string, 0, 4)

	for _, rawLine := range lines {
		trimmed := strings.TrimSpace(rawLine)
		if havenIsJournalTimeHeader(trimmed) {
			if len(currentEntryLines) > 0 {
				latestEntryLines = append([]string(nil), currentEntryLines...)
				currentEntryLines = currentEntryLines[:0]
			}
			entryCount++
			continue
		}
		if trimmed == "" {
			continue
		}
		currentEntryLines = append(currentEntryLines, trimmed)
	}
	if len(currentEntryLines) > 0 {
		latestEntryLines = append([]string(nil), currentEntryLines...)
	}
	if entryCount == 0 {
		entryCount = 1
	}
	preview = strings.Join(latestEntryLines, " ")
	if preview == "" {
		preview = "No journal text yet."
	}
	if len(preview) > 160 {
		preview = preview[:157] + "..."
	}
	return preview, entryCount
}

func havenIsJournalTimeHeader(line string) bool {
	return strings.HasPrefix(line, "--- ") && strings.HasSuffix(line, " ---")
}

func havenJournalTitleFromFilename(filename string) string {
	baseName := strings.TrimSuffix(filename, filepath.Ext(filename))
	if parsedDate, err := time.ParseInLocation("2006-01-02", baseName, time.Local); err == nil {
		return parsedDate.Format("January 2, 2006")
	}
	// Per-entry files: local 2006-01-02T15-04-05-<unix_nano>.md
	lastDash := strings.LastIndex(baseName, "-")
	if lastDash <= 0 || !strings.Contains(baseName, "T") {
		return baseName
	}
	dateTimePart := baseName[:lastDash]
	dateTimePart = strings.Replace(dateTimePart, "T", " ", 1)
	parsedInstant, err := time.ParseInLocation("2006-01-02 15-04-05", dateTimePart, time.Local)
	if err != nil {
		return baseName
	}
	return parsedInstant.Format("Jan 2, 2006 · 15:04 MST")
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
