package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const maxActiveDeskNotes = 4

type DeskNote struct {
	ID                  string          `json:"id"`
	Kind                string          `json:"kind"`
	Title               string          `json:"title"`
	Body                string          `json:"body"`
	Action              *DeskNoteAction `json:"action,omitempty"`
	ActionExecutedAtUTC string          `json:"action_executed_at_utc,omitempty"`
	ActionThreadID      string          `json:"action_thread_id,omitempty"`
	CreatedAtUTC        string          `json:"created_at_utc"`
	ArchivedAtUTC       string          `json:"archived_at_utc,omitempty"`
}

type DeskNoteAction struct {
	Kind    string `json:"kind"`
	Label   string `json:"label,omitempty"`
	Message string `json:"message,omitempty"`
}

type DeskNoteDraft struct {
	Kind   string
	Title  string
	Body   string
	Action *DeskNoteAction
}

type DeskNoteActionResponse struct {
	Success  bool   `json:"success"`
	Error    string `json:"error,omitempty"`
	ThreadID string `json:"thread_id,omitempty"`
}

type deskNoteStateFile struct {
	Notes []DeskNote `json:"notes"`
}

func (app *HavenApp) ListDeskNotes() ([]DeskNote, error) {
	app.deskNotesMu.Lock()
	defer app.deskNotesMu.Unlock()

	deskNotes, err := app.loadDeskNotesLocked()
	if err != nil {
		return nil, err
	}
	activeDeskNotes := activeDeskNotesFromAll(deskNotes)
	return cloneDeskNotes(activeDeskNotes), nil
}

func (app *HavenApp) DismissDeskNote(noteID string) DeskNoteActionResponse {
	trimmedNoteID := strings.TrimSpace(noteID)
	if trimmedNoteID == "" {
		return DeskNoteActionResponse{Error: "desk note id is required"}
	}

	app.deskNotesMu.Lock()
	defer app.deskNotesMu.Unlock()

	deskNotes, err := app.loadDeskNotesLocked()
	if err != nil {
		return DeskNoteActionResponse{Error: fmt.Sprintf("load desk notes: %v", err)}
	}

	foundActiveNote := false
	archivedAtUTC := time.Now().UTC().Format(time.RFC3339Nano)
	for index := range deskNotes {
		if deskNotes[index].ID != trimmedNoteID {
			continue
		}
		if deskNotes[index].ArchivedAtUTC != "" {
			return DeskNoteActionResponse{Error: "desk note is already dismissed"}
		}
		deskNotes[index].ArchivedAtUTC = archivedAtUTC
		foundActiveNote = true
		break
	}
	if !foundActiveNote {
		return DeskNoteActionResponse{Error: "desk note not found"}
	}

	if err := app.saveDeskNotesLocked(deskNotes); err != nil {
		return DeskNoteActionResponse{Error: fmt.Sprintf("save desk notes: %v", err)}
	}
	app.emitDeskNotesChanged()
	return DeskNoteActionResponse{Success: true}
}

func (app *HavenApp) ExecuteDeskNoteAction(noteID string) DeskNoteActionResponse {
	trimmedNoteID := strings.TrimSpace(noteID)
	if trimmedNoteID == "" {
		return DeskNoteActionResponse{Error: "desk note id is required"}
	}

	app.deskNotesMu.Lock()
	deskNotes, err := app.loadDeskNotesLocked()
	if err != nil {
		app.deskNotesMu.Unlock()
		return DeskNoteActionResponse{Error: fmt.Sprintf("load desk notes: %v", err)}
	}

	var selectedDeskNote *DeskNote
	for noteIndex := range deskNotes {
		if deskNotes[noteIndex].ID != trimmedNoteID {
			continue
		}
		selectedDeskNote = &deskNotes[noteIndex]
		break
	}
	if selectedDeskNote == nil {
		app.deskNotesMu.Unlock()
		return DeskNoteActionResponse{Error: "desk note not found"}
	}
	if selectedDeskNote.ArchivedAtUTC != "" {
		app.deskNotesMu.Unlock()
		return DeskNoteActionResponse{Error: "desk note is already dismissed"}
	}
	if selectedDeskNote.Action == nil {
		app.deskNotesMu.Unlock()
		return DeskNoteActionResponse{Error: "desk note has no action"}
	}
	if selectedDeskNote.ActionExecutedAtUTC != "" {
		threadID := strings.TrimSpace(selectedDeskNote.ActionThreadID)
		app.deskNotesMu.Unlock()
		return DeskNoteActionResponse{Success: true, ThreadID: threadID}
	}

	action := *selectedDeskNote.Action
	app.deskNotesMu.Unlock()

	if app.idleManager != nil {
		app.idleManager.NotifyActivity()
	}

	switch action.Kind {
	case "send_message":
		threadSummary, err := app.NewThread()
		if err != nil {
			return DeskNoteActionResponse{Error: fmt.Sprintf("create thread: %v", err)}
		}
		chatResponse := app.SendMessage(threadSummary.ThreadID, action.Message)
		if !chatResponse.Accepted {
			return DeskNoteActionResponse{Error: chatResponse.Reason}
		}

		app.deskNotesMu.Lock()
		defer app.deskNotesMu.Unlock()

		reloadedDeskNotes, err := app.loadDeskNotesLocked()
		if err != nil {
			return DeskNoteActionResponse{Error: fmt.Sprintf("reload desk notes: %v", err), ThreadID: threadSummary.ThreadID}
		}
		for noteIndex := range reloadedDeskNotes {
			if reloadedDeskNotes[noteIndex].ID != trimmedNoteID || reloadedDeskNotes[noteIndex].ArchivedAtUTC != "" {
				continue
			}
			reloadedDeskNotes[noteIndex].ActionExecutedAtUTC = time.Now().UTC().Format(time.RFC3339Nano)
			reloadedDeskNotes[noteIndex].ActionThreadID = threadSummary.ThreadID
			break
		}
		if err := app.saveDeskNotesLocked(reloadedDeskNotes); err != nil {
			return DeskNoteActionResponse{Error: fmt.Sprintf("save desk notes: %v", err), ThreadID: threadSummary.ThreadID}
		}
		app.emitDeskNotesChanged()
		return DeskNoteActionResponse{Success: true, ThreadID: threadSummary.ThreadID}
	default:
		return DeskNoteActionResponse{Error: fmt.Sprintf("unsupported desk note action %q", action.Kind)}
	}
}

func (app *HavenApp) hasActiveDeskNoteTitle(title string) bool {
	trimmedTitle := strings.TrimSpace(title)
	if trimmedTitle == "" {
		return false
	}

	app.deskNotesMu.Lock()
	defer app.deskNotesMu.Unlock()

	deskNotes, err := app.loadDeskNotesLocked()
	if err != nil {
		return false
	}
	for _, deskNote := range deskNotes {
		if deskNote.ArchivedAtUTC != "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(deskNote.Title), trimmedTitle) {
			return true
		}
	}
	return false
}

func (app *HavenApp) createDeskNote(draft DeskNoteDraft) (DeskNote, error) {
	normalizedDraft, err := normalizeDeskNoteDraft(draft)
	if err != nil {
		return DeskNote{}, err
	}

	app.deskNotesMu.Lock()
	defer app.deskNotesMu.Unlock()

	deskNotes, err := app.loadDeskNotesLocked()
	if err != nil {
		return DeskNote{}, err
	}

	createdAtUTC := time.Now().UTC().Format(time.RFC3339Nano)
	deskNote := DeskNote{
		ID:           fmt.Sprintf("desk-note:%d", time.Now().UTC().UnixNano()),
		Kind:         normalizedDraft.Kind,
		Title:        normalizedDraft.Title,
		Body:         normalizedDraft.Body,
		Action:       cloneDeskNoteAction(normalizedDraft.Action),
		CreatedAtUTC: createdAtUTC,
	}
	deskNotes = append(deskNotes, deskNote)
	deskNotes = archiveOverflowDeskNotes(deskNotes, maxActiveDeskNotes)

	if err := app.saveDeskNotesLocked(deskNotes); err != nil {
		return DeskNote{}, err
	}
	app.emitDeskNotesChanged()
	return deskNote, nil
}

func normalizeDeskNoteDraft(draft DeskNoteDraft) (DeskNoteDraft, error) {
	normalizedDraft := DeskNoteDraft{
		Kind:   strings.TrimSpace(draft.Kind),
		Title:  strings.TrimSpace(draft.Title),
		Body:   strings.TrimSpace(draft.Body),
		Action: cloneDeskNoteAction(draft.Action),
	}
	if normalizedDraft.Kind == "" {
		normalizedDraft.Kind = "update"
	}
	switch normalizedDraft.Kind {
	case "update", "reminder":
	default:
		return DeskNoteDraft{}, fmt.Errorf("unsupported desk note kind %q", normalizedDraft.Kind)
	}
	if normalizedDraft.Title == "" {
		normalizedDraft.Title = "A note from Morph"
	}
	if len(normalizedDraft.Title) > 80 {
		return DeskNoteDraft{}, fmt.Errorf("desk note title exceeds maximum length")
	}
	if normalizedDraft.Body == "" {
		return DeskNoteDraft{}, fmt.Errorf("desk note body is required")
	}
	if len(normalizedDraft.Body) > 280 {
		return DeskNoteDraft{}, fmt.Errorf("desk note body exceeds maximum length")
	}
	if normalizedDraft.Action != nil {
		normalizedAction, err := normalizeDeskNoteAction(*normalizedDraft.Action)
		if err != nil {
			return DeskNoteDraft{}, err
		}
		normalizedDraft.Action = &normalizedAction
	}
	return normalizedDraft, nil
}

func normalizeDeskNoteAction(action DeskNoteAction) (DeskNoteAction, error) {
	normalizedAction := DeskNoteAction{
		Kind:    strings.TrimSpace(action.Kind),
		Label:   strings.TrimSpace(action.Label),
		Message: strings.TrimSpace(action.Message),
	}
	switch normalizedAction.Kind {
	case "send_message":
	default:
		return DeskNoteAction{}, fmt.Errorf("unsupported desk note action %q", normalizedAction.Kind)
	}
	if normalizedAction.Label == "" {
		normalizedAction.Label = "Yes, do it"
	}
	if len(normalizedAction.Label) > 40 {
		return DeskNoteAction{}, fmt.Errorf("desk note action label exceeds maximum length")
	}
	if normalizedAction.Message == "" {
		return DeskNoteAction{}, fmt.Errorf("desk note action message is required")
	}
	if len(normalizedAction.Message) > 600 {
		return DeskNoteAction{}, fmt.Errorf("desk note action message exceeds maximum length")
	}
	return normalizedAction, nil
}

func archiveOverflowDeskNotes(deskNotes []DeskNote, maxActive int) []DeskNote {
	activeIndexes := make([]int, 0, len(deskNotes))
	for index, deskNote := range deskNotes {
		if deskNote.ArchivedAtUTC == "" {
			activeIndexes = append(activeIndexes, index)
		}
	}
	if len(activeIndexes) <= maxActive {
		return deskNotes
	}

	archiveCount := len(activeIndexes) - maxActive
	archivedAtUTC := time.Now().UTC().Format(time.RFC3339Nano)
	for _, index := range activeIndexes[:archiveCount] {
		if deskNotes[index].ArchivedAtUTC == "" {
			deskNotes[index].ArchivedAtUTC = archivedAtUTC
		}
	}
	return deskNotes
}

func activeDeskNotesFromAll(deskNotes []DeskNote) []DeskNote {
	activeDeskNotes := make([]DeskNote, 0, len(deskNotes))
	for _, deskNote := range deskNotes {
		if deskNote.ArchivedAtUTC == "" {
			activeDeskNotes = append(activeDeskNotes, deskNote)
		}
	}
	sort.Slice(activeDeskNotes, func(leftIndex int, rightIndex int) bool {
		return activeDeskNotes[leftIndex].CreatedAtUTC > activeDeskNotes[rightIndex].CreatedAtUTC
	})
	return activeDeskNotes
}

func cloneDeskNotes(deskNotes []DeskNote) []DeskNote {
	clonedDeskNotes := make([]DeskNote, len(deskNotes))
	for noteIndex := range deskNotes {
		clonedDeskNotes[noteIndex] = deskNotes[noteIndex]
		clonedDeskNotes[noteIndex].Action = cloneDeskNoteAction(deskNotes[noteIndex].Action)
	}
	return clonedDeskNotes
}

func cloneDeskNoteAction(action *DeskNoteAction) *DeskNoteAction {
	if action == nil {
		return nil
	}
	clonedAction := *action
	return &clonedAction
}

func (app *HavenApp) emitDeskNotesChanged() {
	if app.emitter != nil {
		app.emitter.Emit("haven:desk_notes_changed", map[string]interface{}{})
	}
}

func (app *HavenApp) loadDeskNotesLocked() ([]DeskNote, error) {
	path := app.deskNotesPath()
	rawDeskNoteBytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read desk notes: %w", err)
	}

	var stateFile deskNoteStateFile
	decoder := json.NewDecoder(bytes.NewReader(rawDeskNoteBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stateFile); err != nil {
		return nil, fmt.Errorf("decode desk notes: %w", err)
	}
	return stateFile.Notes, nil
}

func (app *HavenApp) saveDeskNotesLocked(deskNotes []DeskNote) error {
	path := app.deskNotesPath()
	stateFile := deskNoteStateFile{Notes: deskNotes}
	jsonBytes, err := json.MarshalIndent(stateFile, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal desk notes: %w", err)
	}
	if len(jsonBytes) == 0 || jsonBytes[len(jsonBytes)-1] != '\n' {
		jsonBytes = append(jsonBytes, '\n')
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create desk notes dir: %w", err)
	}

	tempPath := path + ".tmp"
	deskNoteFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open temp desk notes file: %w", err)
	}
	defer func() { _ = deskNoteFile.Close() }()

	if _, err := deskNoteFile.Write(jsonBytes); err != nil {
		return fmt.Errorf("write temp desk notes file: %w", err)
	}
	if err := deskNoteFile.Sync(); err != nil {
		return fmt.Errorf("sync temp desk notes file: %w", err)
	}
	if err := deskNoteFile.Close(); err != nil {
		return fmt.Errorf("close temp desk notes file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("rename temp desk notes file: %w", err)
	}
	if noteDir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = noteDir.Sync()
		_ = noteDir.Close()
	}
	_ = os.Chmod(path, 0o600)
	return nil
}

func (app *HavenApp) deskNotesPath() string {
	return filepath.Join(app.setupRepoRoot(), "runtime", "state", "haven_desk_notes.json")
}
