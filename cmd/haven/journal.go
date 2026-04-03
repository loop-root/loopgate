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

const journalSandboxDirectory = "scratch/journal"

// JournalEntrySummary is the sidebar view of a journal file inside Haven.
type JournalEntrySummary struct {
	Path         string `json:"path"`
	Title        string `json:"title"`
	Preview      string `json:"preview"`
	UpdatedAtUTC string `json:"updated_at_utc"`
	EntryCount   int    `json:"entry_count"`
}

// JournalEntryResponse is the full content of a journal file.
type JournalEntryResponse struct {
	Path       string `json:"path"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	EntryCount int    `json:"entry_count"`
	Error      string `json:"error,omitempty"`
}

// ListJournalEntries returns Morph's journal files, newest first.
func (app *HavenApp) ListJournalEntries() ([]JournalEntrySummary, error) {
	journalRuntimeDirectory := filepath.Join(app.sandboxHome, "scratch", "journal")
	if app.sandboxHome != "" {
		if _, err := os.Stat(journalRuntimeDirectory); err != nil {
			if os.IsNotExist(err) {
				return []JournalEntrySummary{}, nil
			}
			return nil, fmt.Errorf("stat journal directory: %w", err)
		}
	}

	listResponse, err := app.loopgateClient.SandboxList(context.Background(), loopgate.SandboxListRequest{
		SandboxPath: journalSandboxDirectory,
	})
	if err != nil {
		if isMissingJournalDirectoryError(err) {
			return []JournalEntrySummary{}, nil
		}
		return nil, fmt.Errorf("list journal entries: %w", err)
	}

	journalEntries := make([]JournalEntrySummary, 0, len(listResponse.Entries))
	for _, entry := range listResponse.Entries {
		if entry.EntryType != "file" || !strings.HasSuffix(strings.ToLower(entry.Name), ".md") {
			continue
		}

		sandboxPath := filepath.ToSlash(filepath.Join(journalSandboxDirectory, entry.Name))
		content, readErr := app.readJournalFile(context.Background(), sandboxPath)
		if readErr != nil {
			content = ""
		}
		preview, entryCount := summarizeJournalContent(content)

		journalEntries = append(journalEntries, JournalEntrySummary{
			Path:         fmt.Sprintf("research/journal/%s", entry.Name),
			Title:        journalTitleFromFilename(entry.Name),
			Preview:      preview,
			UpdatedAtUTC: entry.ModTimeUTC,
			EntryCount:   entryCount,
		})
	}

	sort.Slice(journalEntries, func(leftIndex, rightIndex int) bool {
		return journalEntries[leftIndex].UpdatedAtUTC > journalEntries[rightIndex].UpdatedAtUTC
	})

	return journalEntries, nil
}

// ReadJournalEntry returns the full contents of a specific journal file.
func (app *HavenApp) ReadJournalEntry(havenPath string) JournalEntryResponse {
	if strings.TrimSpace(havenPath) == "" {
		return JournalEntryResponse{Error: "path is required"}
	}

	sandboxPath := mapHavenPathToSandbox(havenPath)
	if !strings.HasPrefix(sandboxPath, journalSandboxDirectory+"/") {
		return JournalEntryResponse{Error: "journal path is required"}
	}

	content, err := app.readJournalFile(context.Background(), sandboxPath)
	if err != nil {
		return JournalEntryResponse{Path: havenPath, Error: err.Error()}
	}

	_, entryCount := summarizeJournalContent(content)
	return JournalEntryResponse{
		Path:       havenPath,
		Title:      journalTitleFromFilename(filepath.Base(havenPath)),
		Content:    content,
		EntryCount: entryCount,
	}
}

func (app *HavenApp) readJournalFile(ctx context.Context, sandboxPath string) (string, error) {
	response, err := app.loopgateClient.ExecuteCapability(ctx, loopgate.CapabilityRequest{
		RequestID:  fmt.Sprintf("journal-read-%d", time.Now().UnixNano()),
		Actor:      "haven",
		Capability: "fs_read",
		Arguments:  map[string]string{"path": sandboxPath},
	})
	if err != nil {
		return "", fmt.Errorf("read journal entry: %w", err)
	}
	if response.Status != loopgate.ResponseStatusSuccess {
		denialReason := response.DenialReason
		if denialReason == "" {
			denialReason = "journal entry is unavailable"
		}
		return "", fmt.Errorf("%s", denialReason)
	}
	content, _ := response.StructuredResult["content"].(string)
	return content, nil
}

func summarizeJournalContent(content string) (string, int) {
	if strings.TrimSpace(content) == "" {
		return "No journal text yet.", 0
	}

	normalizedContent := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalizedContent, "\n")

	entryCount := 0
	currentEntryLines := make([]string, 0, 4)
	latestEntryLines := make([]string, 0, 4)

	for _, rawLine := range lines {
		trimmedLine := strings.TrimSpace(rawLine)
		if isJournalTimeHeader(trimmedLine) {
			if len(currentEntryLines) > 0 {
				latestEntryLines = append([]string(nil), currentEntryLines...)
				currentEntryLines = currentEntryLines[:0]
			}
			entryCount++
			continue
		}
		if trimmedLine == "" {
			continue
		}
		currentEntryLines = append(currentEntryLines, trimmedLine)
	}
	if len(currentEntryLines) > 0 {
		latestEntryLines = append([]string(nil), currentEntryLines...)
	}
	if entryCount == 0 {
		entryCount = 1
	}

	preview := strings.Join(latestEntryLines, " ")
	if preview == "" {
		preview = "No journal text yet."
	}
	if len(preview) > 160 {
		preview = preview[:157] + "..."
	}
	return preview, entryCount
}

func isJournalTimeHeader(line string) bool {
	return strings.HasPrefix(line, "--- ") && strings.HasSuffix(line, " ---")
}

func journalTitleFromFilename(filename string) string {
	baseName := strings.TrimSuffix(filename, filepath.Ext(filename))
	parsedDate, err := time.Parse("2006-01-02", baseName)
	if err != nil {
		return baseName
	}
	return parsedDate.Format("January 2, 2006")
}

func isMissingJournalDirectoryError(err error) bool {
	lowerError := strings.ToLower(err.Error())
	return strings.Contains(lowerError, "not found") || strings.Contains(lowerError, "no such file") || strings.Contains(lowerError, "does not exist")
}
