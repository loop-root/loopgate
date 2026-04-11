package loopgate

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"morph/internal/sandbox"
)

const havenJournalSandboxDir = "scratch/journal"

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
