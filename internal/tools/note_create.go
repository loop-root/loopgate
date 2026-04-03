package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const noteCreateMaxActiveNotes = 4

// NoteCreate writes a sticky note into Haven's desk-note state so it appears on the desktop.
type NoteCreate struct {
	StateDir string
}

func (tool *NoteCreate) Name() string      { return "note.create" }
func (tool *NoteCreate) Category() string  { return "filesystem" }
func (tool *NoteCreate) Operation() string { return OpWrite }

func (tool *NoteCreate) Schema() Schema {
	return Schema{
		Description: "Create a sticky note on Morph's desktop. Use this for handoffs, reminders, questions, or short notes to the user.",
		Args: []ArgDef{
			{
				Name:        "kind",
				Description: "Note kind: update or reminder",
				Required:    false,
				Type:        "string",
				MaxLen:      20,
			},
			{
				Name:        "title",
				Description: "Short note title",
				Required:    false,
				Type:        "string",
				MaxLen:      80,
			},
			{
				Name:        "body",
				Description: "Short note body",
				Required:    true,
				Type:        "string",
				MaxLen:      280,
			},
		},
	}
}

func (tool *NoteCreate) Execute(_ context.Context, args map[string]string) (string, error) {
	if strings.TrimSpace(tool.StateDir) == "" {
		return "", fmt.Errorf("desk note state directory is not configured")
	}
	noteKind := strings.TrimSpace(args["kind"])
	if noteKind == "" {
		noteKind = "update"
	}
	switch noteKind {
	case "update", "reminder":
	default:
		return "", fmt.Errorf("note kind must be update or reminder")
	}

	noteTitle := strings.TrimSpace(args["title"])
	if noteTitle == "" {
		noteTitle = "A note from Morph"
	}
	noteBody := strings.TrimSpace(args["body"])
	if noteBody == "" {
		return "", fmt.Errorf("note body is required")
	}

	statePath := tool.statePath()
	existingNotes, err := loadPersistedDeskNotes(statePath)
	if err != nil {
		return "", err
	}

	nowUTC := time.Now().UTC().Format(time.RFC3339Nano)
	existingNotes = append(existingNotes, persistedDeskNote{
		ID:           fmt.Sprintf("desk-note:%d", time.Now().UTC().UnixNano()),
		Kind:         noteKind,
		Title:        noteTitle,
		Body:         noteBody,
		CreatedAtUTC: nowUTC,
	})
	existingNotes = archiveOverflowPersistedDeskNotes(existingNotes, noteCreateMaxActiveNotes)

	if err := savePersistedDeskNotes(statePath, existingNotes); err != nil {
		return "", err
	}
	return fmt.Sprintf("Sticky note created: %s", noteTitle), nil
}

type persistedDeskNote struct {
	ID                  string                   `json:"id"`
	Kind                string                   `json:"kind"`
	Title               string                   `json:"title"`
	Body                string                   `json:"body"`
	Action              *persistedDeskNoteAction `json:"action,omitempty"`
	ActionExecutedAtUTC string                   `json:"action_executed_at_utc,omitempty"`
	ActionThreadID      string                   `json:"action_thread_id,omitempty"`
	CreatedAtUTC        string                   `json:"created_at_utc"`
	ArchivedAtUTC       string                   `json:"archived_at_utc,omitempty"`
}

type persistedDeskNoteAction struct {
	Kind    string `json:"kind"`
	Label   string `json:"label,omitempty"`
	Message string `json:"message,omitempty"`
}

type persistedDeskNoteStateFile struct {
	Notes []persistedDeskNote `json:"notes"`
}

func (tool *NoteCreate) statePath() string {
	return filepath.Join(tool.StateDir, "haven_desk_notes.json")
}

func loadPersistedDeskNotes(path string) ([]persistedDeskNote, error) {
	rawStateBytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read desk note state: %w", err)
	}

	var stateFile persistedDeskNoteStateFile
	decoder := json.NewDecoder(bytes.NewReader(rawStateBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stateFile); err != nil {
		return nil, fmt.Errorf("decode desk note state: %w", err)
	}
	return stateFile.Notes, nil
}

func savePersistedDeskNotes(path string, notes []persistedDeskNote) error {
	stateFile := persistedDeskNoteStateFile{Notes: notes}
	jsonBytes, err := json.MarshalIndent(stateFile, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal desk note state: %w", err)
	}
	if len(jsonBytes) == 0 || jsonBytes[len(jsonBytes)-1] != '\n' {
		jsonBytes = append(jsonBytes, '\n')
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create desk note state dir: %w", err)
	}

	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, jsonBytes, 0o600); err != nil {
		return fmt.Errorf("write temp desk note state: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("rename desk note state: %w", err)
	}
	return nil
}

func archiveOverflowPersistedDeskNotes(notes []persistedDeskNote, maxActive int) []persistedDeskNote {
	activeIndexes := make([]int, 0, len(notes))
	for noteIndex, currentNote := range notes {
		if currentNote.ArchivedAtUTC == "" {
			activeIndexes = append(activeIndexes, noteIndex)
		}
	}
	if len(activeIndexes) <= maxActive {
		return notes
	}

	sort.Slice(activeIndexes, func(leftIndex, rightIndex int) bool {
		return notes[activeIndexes[leftIndex]].CreatedAtUTC < notes[activeIndexes[rightIndex]].CreatedAtUTC
	})
	archiveCount := len(activeIndexes) - maxActive
	archivedAtUTC := time.Now().UTC().Format(time.RFC3339Nano)
	for _, noteIndex := range activeIndexes[:archiveCount] {
		if notes[noteIndex].ArchivedAtUTC == "" {
			notes[noteIndex].ArchivedAtUTC = archivedAtUTC
		}
	}
	return notes
}
