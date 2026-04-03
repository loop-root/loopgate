package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const journalDirectoryRelativePath = "scratch/journal"

// UniqueJournalEntryFileName returns a new journal filename unique per write using the machine's
// local date and time of day. Legacy entries used YYYY-MM-DD.md (one file per day); new entries use
// local wall clock + unix-nano suffix so Haven and journal.list show one row per logical entry.
func UniqueJournalEntryFileName(now time.Time) string {
	local := now.In(time.Local)
	timePart := strings.ReplaceAll(local.Format("15:04:05"), ":", "-")
	return fmt.Sprintf("%sT%s-%d.md", local.Format("2006-01-02"), timePart, now.UnixNano())
}

// JournalList lists available journal entries in Morph's private journal.
type JournalList struct {
	Root string
}

func (tool *JournalList) Name() string      { return "journal.list" }
func (tool *JournalList) Category() string  { return "filesystem" }
func (tool *JournalList) Operation() string { return OpRead }

func (tool *JournalList) Schema() Schema {
	return Schema{
		Description: "List private journal entries in Morph's journal.",
	}
}

func (tool *JournalList) Execute(context.Context, map[string]string) (string, error) {
	journalDirectoryPath := filepath.Join(tool.Root, filepath.FromSlash(journalDirectoryRelativePath))
	directoryEntries, err := os.ReadDir(journalDirectoryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "No journal entries yet.", nil
		}
		return "", fmt.Errorf("list journal entries: %w", err)
	}

	type journalEntry struct {
		name       string
		modifiedAt time.Time
	}
	journalEntries := make([]journalEntry, 0, len(directoryEntries))
	for _, directoryEntry := range directoryEntries {
		if directoryEntry.IsDir() || !strings.HasSuffix(strings.ToLower(directoryEntry.Name()), ".md") {
			continue
		}
		info, infoErr := directoryEntry.Info()
		if infoErr != nil {
			continue
		}
		journalEntries = append(journalEntries, journalEntry{
			name:       directoryEntry.Name(),
			modifiedAt: info.ModTime().Local(),
		})
	}

	if len(journalEntries) == 0 {
		return "No journal entries yet.", nil
	}

	sort.Slice(journalEntries, func(leftIndex, rightIndex int) bool {
		return journalEntries[leftIndex].modifiedAt.After(journalEntries[rightIndex].modifiedAt)
	})

	var builder strings.Builder
	builder.WriteString("Journal entries:\n")
	for _, journalEntry := range journalEntries {
		builder.WriteString("- ")
		builder.WriteString(filepath.ToSlash(filepath.Join("research", "journal", journalEntry.name)))
		builder.WriteString(" (updated ")
		builder.WriteString(journalEntry.modifiedAt.Format(time.RFC3339))
		builder.WriteString(")\n")
	}
	return strings.TrimSpace(builder.String()), nil
}

// JournalRead reads a specific journal entry.
type JournalRead struct {
	Root string
}

func (tool *JournalRead) Name() string      { return "journal.read" }
func (tool *JournalRead) Category() string  { return "filesystem" }
func (tool *JournalRead) Operation() string { return OpRead }

func (tool *JournalRead) Schema() Schema {
	return Schema{
		Description: "Read a journal entry from Morph's private journal.",
		Args: []ArgDef{
			{
				Name:        "path",
				Description: "Journal entry path or date, such as research/journal/2026-03-18.md or 2026-03-18",
				Required:    true,
				Type:        "path",
				MaxLen:      128,
			},
		},
	}
}

func (tool *JournalRead) Execute(ctx context.Context, args map[string]string) (string, error) {
	normalizedPath, err := normalizeJournalEntryPath(args["path"])
	if err != nil {
		return "", err
	}
	journalReader := FSRead{
		RepoRoot:     tool.Root,
		AllowedRoots: []string{journalDirectoryRelativePath},
		DeniedPaths:  []string{},
	}
	return journalReader.Execute(ctx, map[string]string{"path": normalizedPath})
}

// JournalWrite creates a new private journal file per call (one logical entry per file).
type JournalWrite struct {
	Root string
}

func (tool *JournalWrite) Name() string      { return "journal.write" }
func (tool *JournalWrite) Category() string  { return "filesystem" }
func (tool *JournalWrite) Operation() string { return OpWrite }

func (tool *JournalWrite) Schema() Schema {
	return Schema{
		Description: "Write a private journal entry for Morph. Each call creates a new entry file. Use for reflection, processing, or leaving a thought in the journal.",
		Args: []ArgDef{
			{
				Name:        "title",
				Description: "Optional short title for the journal entry",
				Required:    false,
				Type:        "string",
				MaxLen:      80,
			},
			{
				Name:        "body",
				Description: "The journal entry text",
				Required:    true,
				Type:        "string",
				MaxLen:      4000,
			},
		},
	}
}

func (tool *JournalWrite) Execute(_ context.Context, args map[string]string) (string, error) {
	entryTitle := strings.TrimSpace(args["title"])
	entryBody := strings.TrimSpace(args["body"])
	if entryBody == "" {
		return "", fmt.Errorf("journal body is required")
	}
	if len(entryTitle) > 80 {
		return "", fmt.Errorf("journal title exceeds maximum length")
	}

	now := time.Now()
	journalDirectoryPath := filepath.Join(tool.Root, filepath.FromSlash(journalDirectoryRelativePath))
	if err := os.MkdirAll(journalDirectoryPath, 0o700); err != nil {
		return "", fmt.Errorf("create journal directory: %w", err)
	}

	entryFileName := UniqueJournalEntryFileName(now)
	entryFilePath := filepath.Join(journalDirectoryPath, entryFileName)
	entryHeader := fmt.Sprintf("--- %s ---\n", now.Local().Format(time.RFC3339Nano))

	var builder strings.Builder
	builder.WriteString(entryHeader)
	if entryTitle != "" {
		builder.WriteString(entryTitle)
		builder.WriteString("\n")
	}
	builder.WriteString(entryBody)
	builder.WriteString("\n")

	journalFile, err := os.OpenFile(entryFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", fmt.Errorf("open journal file: %w", err)
	}
	defer journalFile.Close()

	if _, err := journalFile.WriteString(builder.String()); err != nil {
		return "", fmt.Errorf("write journal entry: %w", err)
	}
	if err := journalFile.Sync(); err != nil {
		return "", fmt.Errorf("sync journal entry: %w", err)
	}

	return fmt.Sprintf("Journal entry saved to %s", filepath.ToSlash(filepath.Join("research", "journal", entryFileName))), nil
}

func normalizeJournalEntryPath(rawPath string) (string, error) {
	trimmedPath := strings.TrimSpace(rawPath)
	if trimmedPath == "" {
		return "", fmt.Errorf("journal path is required")
	}

	normalizedPath := strings.TrimPrefix(filepath.ToSlash(trimmedPath), "/")
	switch {
	case strings.HasPrefix(normalizedPath, "research/journal/"):
		normalizedPath = strings.TrimPrefix(normalizedPath, "research/journal/")
	case strings.HasPrefix(normalizedPath, journalDirectoryRelativePath+"/"):
		normalizedPath = strings.TrimPrefix(normalizedPath, journalDirectoryRelativePath+"/")
	}

	if normalizedPath == "" {
		return "", fmt.Errorf("journal path is required")
	}
	if !strings.HasSuffix(strings.ToLower(normalizedPath), ".md") {
		normalizedPath += ".md"
	}
	if strings.Contains(normalizedPath, "/") {
		return "", fmt.Errorf("journal path must reference a single entry")
	}
	return filepath.ToSlash(filepath.Join(journalDirectoryRelativePath, normalizedPath)), nil
}
