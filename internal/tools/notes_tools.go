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

const notesDirectoryRelativePath = "scratch/notes"

// NotesList lists working notes in Morph's private notebook.
type NotesList struct {
	Root string
}

func (tool *NotesList) Name() string      { return "notes.list" }
func (tool *NotesList) Category() string  { return "filesystem" }
func (tool *NotesList) Operation() string { return OpRead }

func (tool *NotesList) Schema() Schema {
	return Schema{
		Description: "List private working notes in Morph's notebook.",
	}
}

func (tool *NotesList) Execute(context.Context, map[string]string) (string, error) {
	notesDirectoryPath := filepath.Join(tool.Root, filepath.FromSlash(notesDirectoryRelativePath))
	directoryEntries, err := os.ReadDir(notesDirectoryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "No working notes yet.", nil
		}
		return "", fmt.Errorf("list working notes: %w", err)
	}

	type workingNote struct {
		name       string
		modifiedAt time.Time
	}
	workingNotes := make([]workingNote, 0, len(directoryEntries))
	for _, directoryEntry := range directoryEntries {
		if directoryEntry.IsDir() || !isWorkingNoteFilename(directoryEntry.Name()) {
			continue
		}
		info, infoErr := directoryEntry.Info()
		if infoErr != nil {
			continue
		}
		workingNotes = append(workingNotes, workingNote{
			name:       directoryEntry.Name(),
			modifiedAt: info.ModTime().UTC(),
		})
	}

	if len(workingNotes) == 0 {
		return "No working notes yet.", nil
	}

	sort.Slice(workingNotes, func(leftIndex, rightIndex int) bool {
		return workingNotes[leftIndex].modifiedAt.After(workingNotes[rightIndex].modifiedAt)
	})

	var builder strings.Builder
	builder.WriteString("Working notes:\n")
	for _, workingNote := range workingNotes {
		builder.WriteString("- ")
		builder.WriteString(filepath.ToSlash(filepath.Join("research", "notes", workingNote.name)))
		builder.WriteString(" (updated ")
		builder.WriteString(workingNote.modifiedAt.Format(time.RFC3339))
		builder.WriteString(")\n")
	}
	return strings.TrimSpace(builder.String()), nil
}

// NotesRead reads a specific working note.
type NotesRead struct {
	Root string
}

func (tool *NotesRead) Name() string      { return "notes.read" }
func (tool *NotesRead) Category() string  { return "filesystem" }
func (tool *NotesRead) Operation() string { return OpRead }

func (tool *NotesRead) Schema() Schema {
	return Schema{
		Description: "Read a private working note from Morph's notebook.",
		Args: []ArgDef{
			{
				Name:        "path",
				Description: "Working note path or filename, such as research/notes/inbox.md or inbox",
				Required:    true,
				Type:        "path",
				MaxLen:      160,
			},
		},
	}
}

func (tool *NotesRead) Execute(ctx context.Context, args map[string]string) (string, error) {
	normalizedPath, err := normalizeWorkingNotePath(args["path"])
	if err != nil {
		return "", err
	}
	notesReader := FSRead{
		RepoRoot:     tool.Root,
		AllowedRoots: []string{notesDirectoryRelativePath},
		DeniedPaths:  []string{},
	}
	return notesReader.Execute(ctx, map[string]string{"path": normalizedPath})
}

// NotesWrite writes a private working note for Morph.
type NotesWrite struct {
	Root string
}

func (tool *NotesWrite) Name() string      { return "notes.write" }
func (tool *NotesWrite) Category() string  { return "filesystem" }
func (tool *NotesWrite) Operation() string { return OpWrite }

func (tool *NotesWrite) Schema() Schema {
	return Schema{
		Description: "Create or update a private working note for Morph. Use this for plans, scratch work, reminders to self, or research notes that should persist inside Haven without going into the journal.",
		Args: []ArgDef{
			{
				Name:        "path",
				Description: "Optional note path or filename, such as research/notes/inbox.md. If omitted, one will be created from the title.",
				Required:    false,
				Type:        "path",
				MaxLen:      160,
			},
			{
				Name:        "title",
				Description: "Optional short title used when creating a new note without a path",
				Required:    false,
				Type:        "string",
				MaxLen:      80,
			},
			{
				Name:        "body",
				Description: "Full note content to save",
				Required:    true,
				Type:        "string",
				MaxLen:      8000,
			},
		},
	}
}

func (tool *NotesWrite) Execute(ctx context.Context, args map[string]string) (string, error) {
	normalizedBody := strings.TrimSpace(args["body"])
	if normalizedBody == "" {
		return "", fmt.Errorf("note body is required")
	}

	normalizedPath, err := resolveWorkingNoteWritePath(args["path"], args["title"])
	if err != nil {
		return "", err
	}

	notesDirectoryPath := filepath.Join(tool.Root, filepath.FromSlash(notesDirectoryRelativePath))
	if err := os.MkdirAll(notesDirectoryPath, 0o700); err != nil {
		return "", fmt.Errorf("create notes directory: %w", err)
	}

	notesWriter := FSWrite{
		RepoRoot:     tool.Root,
		AllowedRoots: []string{notesDirectoryRelativePath},
		DeniedPaths:  []string{},
	}
	if _, err := notesWriter.Execute(ctx, map[string]string{
		"path":    normalizedPath,
		"content": normalizedBody + "\n",
	}); err != nil {
		return "", err
	}

	return fmt.Sprintf("Working note saved to %s", filepath.ToSlash(filepath.Join("research", "notes", filepath.Base(normalizedPath)))), nil
}

func normalizeWorkingNotePath(rawPath string) (string, error) {
	trimmedPath := strings.TrimSpace(rawPath)
	if trimmedPath == "" {
		return "", fmt.Errorf("note path is required")
	}

	normalizedPath := strings.TrimPrefix(filepath.ToSlash(trimmedPath), "/")
	switch {
	case strings.HasPrefix(normalizedPath, "research/notes/"):
		normalizedPath = strings.TrimPrefix(normalizedPath, "research/notes/")
	case strings.HasPrefix(normalizedPath, notesDirectoryRelativePath+"/"):
		normalizedPath = strings.TrimPrefix(normalizedPath, notesDirectoryRelativePath+"/")
	}

	if normalizedPath == "" {
		return "", fmt.Errorf("note path is required")
	}
	if strings.Contains(normalizedPath, "/") {
		return "", fmt.Errorf("note path must reference a single note")
	}
	if !strings.Contains(filepath.Base(normalizedPath), ".") {
		normalizedPath += ".md"
	}
	if !isWorkingNoteFilename(normalizedPath) {
		return "", fmt.Errorf("note path must end in .md or .txt")
	}
	return filepath.ToSlash(filepath.Join(notesDirectoryRelativePath, normalizedPath)), nil
}

func resolveWorkingNoteWritePath(rawPath string, rawTitle string) (string, error) {
	if strings.TrimSpace(rawPath) != "" {
		return normalizeWorkingNotePath(rawPath)
	}

	normalizedTitle := strings.TrimSpace(rawTitle)
	if normalizedTitle == "" {
		normalizedTitle = "untitled note"
	}
	slug := slugifyWorkingNoteTitle(normalizedTitle)
	if slug == "" {
		slug = "untitled-note"
	}
	return filepath.ToSlash(filepath.Join(notesDirectoryRelativePath, slug+".md")), nil
}

func slugifyWorkingNoteTitle(rawTitle string) string {
	lowerTitle := strings.ToLower(strings.TrimSpace(rawTitle))
	var builder strings.Builder
	previousWasDash := false
	for _, r := range lowerTitle {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
			previousWasDash = false
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
			previousWasDash = false
		default:
			if previousWasDash || builder.Len() == 0 {
				continue
			}
			builder.WriteByte('-')
			previousWasDash = true
		}
		if builder.Len() >= 48 {
			break
		}
	}
	return strings.Trim(builder.String(), "-")
}

func isWorkingNoteFilename(filename string) bool {
	lowerFilename := strings.ToLower(strings.TrimSpace(filename))
	return strings.HasSuffix(lowerFilename, ".md") || strings.HasSuffix(lowerFilename, ".txt")
}
