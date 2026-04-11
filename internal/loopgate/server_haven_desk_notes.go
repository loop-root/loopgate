package loopgate

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type havenDeskNoteStateFile struct {
	Notes []HavenDeskNote `json:"notes"`
}

func (server *Server) havenDeskNotesPath() string {
	return filepath.Join(server.repoRoot, "runtime", "state", "haven_desk_notes.json")
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
