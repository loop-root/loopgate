package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"morph/internal/loopgate"
)

const workingNotesSandboxDirectory = "scratch/notes"

type WorkingNoteSummary struct {
	Path         string `json:"path"`
	Title        string `json:"title"`
	Preview      string `json:"preview"`
	UpdatedAtUTC string `json:"updated_at_utc"`
}

type WorkingNoteResponse struct {
	Path         string `json:"path"`
	Title        string `json:"title"`
	Content      string `json:"content"`
	UpdatedAtUTC string `json:"updated_at_utc,omitempty"`
	Error        string `json:"error,omitempty"`
}

type WorkingNoteSaveRequest struct {
	Path    string `json:"path,omitempty"`
	Title   string `json:"title,omitempty"`
	Content string `json:"content"`
}

type WorkingNoteSaveResponse struct {
	Saved bool   `json:"saved"`
	Path  string `json:"path,omitempty"`
	Title string `json:"title,omitempty"`
	Error string `json:"error,omitempty"`
}

func (app *HavenApp) ListWorkingNotes() ([]WorkingNoteSummary, error) {
	notesRuntimeDirectory := filepath.Join(app.sandboxHome, "scratch", "notes")
	if app.sandboxHome != "" {
		if _, err := os.Stat(notesRuntimeDirectory); err != nil {
			if os.IsNotExist(err) {
				return []WorkingNoteSummary{}, nil
			}
			return nil, fmt.Errorf("stat notes directory: %w", err)
		}
	}

	listResponse, err := app.loopgateClient.SandboxList(context.Background(), loopgate.SandboxListRequest{
		SandboxPath: workingNotesSandboxDirectory,
	})
	if err != nil {
		if isMissingWorkingNotesDirectoryError(err) {
			return []WorkingNoteSummary{}, nil
		}
		return nil, fmt.Errorf("list working notes: %w", err)
	}

	workingNotes := make([]WorkingNoteSummary, 0, len(listResponse.Entries))
	for _, entry := range listResponse.Entries {
		if entry.EntryType != "file" || !isWorkingNoteFile(entry.Name) {
			continue
		}

		sandboxPath := filepath.ToSlash(filepath.Join(workingNotesSandboxDirectory, entry.Name))
		content, readErr := app.readWorkingNoteFile(context.Background(), sandboxPath)
		if readErr != nil {
			content = ""
		}
		preview, resolvedTitle := summarizeWorkingNoteContent(content, entry.Name)

		workingNotes = append(workingNotes, WorkingNoteSummary{
			Path:         fmt.Sprintf("research/notes/%s", entry.Name),
			Title:        resolvedTitle,
			Preview:      preview,
			UpdatedAtUTC: entry.ModTimeUTC,
		})
	}

	sort.Slice(workingNotes, func(leftIndex, rightIndex int) bool {
		return workingNotes[leftIndex].UpdatedAtUTC > workingNotes[rightIndex].UpdatedAtUTC
	})

	return workingNotes, nil
}

func (app *HavenApp) ReadWorkingNote(havenPath string) WorkingNoteResponse {
	if strings.TrimSpace(havenPath) == "" {
		return WorkingNoteResponse{Error: "path is required"}
	}

	sandboxPath := mapHavenPathToSandbox(havenPath)
	if !strings.HasPrefix(sandboxPath, workingNotesSandboxDirectory+"/") {
		return WorkingNoteResponse{Error: "working note path is required"}
	}

	content, err := app.readWorkingNoteFile(context.Background(), sandboxPath)
	if err != nil {
		return WorkingNoteResponse{Path: havenPath, Error: err.Error()}
	}

	_, resolvedTitle := summarizeWorkingNoteContent(content, filepath.Base(havenPath))
	return WorkingNoteResponse{
		Path:    havenPath,
		Title:   resolvedTitle,
		Content: content,
	}
}

func (app *HavenApp) SaveWorkingNote(request WorkingNoteSaveRequest) WorkingNoteSaveResponse {
	normalizedContent := strings.TrimSpace(request.Content)
	if normalizedContent == "" {
		return WorkingNoteSaveResponse{Error: "note content is required"}
	}
	if app.idleManager != nil {
		app.idleManager.NotifyActivity()
	}

	response, err := app.loopgateClient.ExecuteCapability(context.Background(), loopgate.CapabilityRequest{
		RequestID:  fmt.Sprintf("notes-write-%d", time.Now().UTC().UnixNano()),
		Actor:      "haven",
		Capability: "notes.write",
		Arguments: map[string]string{
			"path":  strings.TrimSpace(request.Path),
			"title": strings.TrimSpace(request.Title),
			"body":  normalizedContent,
		},
	})
	if err != nil {
		return WorkingNoteSaveResponse{Error: fmt.Sprintf("save working note: %v", err)}
	}
	if response.Status != loopgate.ResponseStatusSuccess {
		denialReason := response.DenialReason
		if denialReason == "" {
			denialReason = "working note could not be saved"
		}
		return WorkingNoteSaveResponse{Error: denialReason}
	}

	savedPath := deriveWorkingNoteResultPath(request.Path, request.Title, response.StructuredResult["content"])
	app.emitter.Emit("haven:file_changed", map[string]interface{}{
		"action": "notes_write",
		"path":   mapHavenPathToSandbox(savedPath),
	})
	return WorkingNoteSaveResponse{
		Saved: true,
		Path:  savedPath,
		Title: workingNoteTitleFromFilename(filepath.Base(savedPath)),
	}
}

func (app *HavenApp) readWorkingNoteFile(ctx context.Context, sandboxPath string) (string, error) {
	response, err := app.loopgateClient.ExecuteCapability(ctx, loopgate.CapabilityRequest{
		RequestID:  fmt.Sprintf("notes-read-%d", time.Now().UnixNano()),
		Actor:      "haven",
		Capability: "notes.read",
		Arguments:  map[string]string{"path": sandboxPath},
	})
	if err != nil {
		return "", fmt.Errorf("read working note: %w", err)
	}
	if response.Status != loopgate.ResponseStatusSuccess {
		denialReason := response.DenialReason
		if denialReason == "" {
			denialReason = "working note is unavailable"
		}
		return "", fmt.Errorf("%s", denialReason)
	}
	content, _ := response.StructuredResult["content"].(string)
	if content == "" {
		content, _ = response.StructuredResult["body"].(string)
	}
	return content, nil
}

func summarizeWorkingNoteContent(content string, filename string) (preview string, title string) {
	resolvedTitle := workingNoteTitleFromFilename(filename)
	if strings.TrimSpace(content) == "" {
		return "No note text yet.", resolvedTitle
	}

	normalizedContent := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalizedContent, "\n")
	previewLines := make([]string, 0, 3)
	for _, rawLine := range lines {
		trimmedLine := strings.TrimSpace(rawLine)
		if trimmedLine == "" {
			continue
		}
		if strings.HasPrefix(trimmedLine, "# ") && resolvedTitle == workingNoteTitleFromFilename(filename) {
			resolvedTitle = strings.TrimSpace(strings.TrimPrefix(trimmedLine, "# "))
			continue
		}
		previewLines = append(previewLines, trimmedLine)
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
	return preview, resolvedTitle
}

func workingNoteTitleFromFilename(filename string) string {
	baseName := strings.TrimSuffix(filename, filepath.Ext(filename))
	baseName = strings.ReplaceAll(baseName, "-", " ")
	baseName = strings.TrimSpace(baseName)
	if baseName == "" {
		return "Untitled Note"
	}
	words := strings.Fields(baseName)
	for index, word := range words {
		if word == "" {
			continue
		}
		words[index] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

func isWorkingNoteFile(filename string) bool {
	lowerFilename := strings.ToLower(strings.TrimSpace(filename))
	return strings.HasSuffix(lowerFilename, ".md") || strings.HasSuffix(lowerFilename, ".txt")
}

func isMissingWorkingNotesDirectoryError(err error) bool {
	lowerError := strings.ToLower(err.Error())
	return strings.Contains(lowerError, "not found") || strings.Contains(lowerError, "no such file") || strings.Contains(lowerError, "does not exist")
}

func deriveWorkingNoteResultPath(requestPath string, requestTitle string, rawContent interface{}) string {
	if strings.TrimSpace(requestPath) != "" {
		return filepath.ToSlash(filepath.Join("research", "notes", filepath.Base(strings.TrimSpace(requestPath))))
	}

	contentText := fmt.Sprint(rawContent)
	const savedPrefix = "Working note saved to "
	if strings.HasPrefix(contentText, savedPrefix) {
		savedPath := strings.TrimSpace(strings.TrimPrefix(contentText, savedPrefix))
		if strings.HasPrefix(savedPath, "research/notes/") {
			return savedPath
		}
	}

	fallbackTitle := strings.TrimSpace(requestTitle)
	if fallbackTitle == "" {
		fallbackTitle = "untitled-note"
	}
	normalizedTitle := strings.ToLower(strings.ReplaceAll(fallbackTitle, " ", "-"))
	return filepath.ToSlash(filepath.Join("research", "notes", normalizedTitle+".md"))
}
