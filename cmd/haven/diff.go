package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"morph/internal/loopgate"
)

// DiffLine represents a single line in a unified diff.
type DiffLine struct {
	Type string `json:"type"` // "context", "add", "remove", "header"
	Text string `json:"text"`
}

// DiffResponse wraps the result of a file diff.
type DiffResponse struct {
	Path       string     `json:"path"` // Haven-facing path
	HasChanges bool       `json:"has_changes"`
	Lines      []DiffLine `json:"lines"`
	Error      string     `json:"error,omitempty"`
}

// WorkspaceRestoreResponse wraps the result of restoring an imported file.
type WorkspaceRestoreResponse struct {
	Restored bool   `json:"restored"`
	Error    string `json:"error,omitempty"`
}

// WorkspaceDiff computes a unified diff between the original imported version
// and the current sandbox version of a file.
func (app *HavenApp) WorkspaceDiff(havenPath string) DiffResponse {
	if strings.TrimSpace(havenPath) == "" {
		return DiffResponse{Error: "path is required"}
	}

	// Read current from sandbox via fs_read
	sandboxPath := mapHavenPathToSandbox(havenPath)
	response, err := app.loopgateClient.ExecuteCapability(context.Background(), loopgate.CapabilityRequest{
		RequestID:  fmt.Sprintf("diff-%d", time.Now().UnixNano()),
		Actor:      "haven",
		Capability: "fs_read",
		Arguments:  map[string]string{"path": sandboxPath},
	})
	if err != nil {
		return DiffResponse{Path: havenPath, Error: fmt.Sprintf("read current file: %v", err)}
	}
	if response.Status != loopgate.ResponseStatusSuccess {
		return DiffResponse{Path: havenPath, Error: response.DenialReason}
	}
	currentContent, _ := response.StructuredResult["content"].(string)

	// Read original from haven data dir — if none exists, show the
	// current file content as all-additions so the user can still
	// review what Morph created.
	origPath := app.originalFilePath(havenPath)
	origBytes, err := os.ReadFile(origPath)
	if err != nil {
		lines := []DiffLine{
			{Type: "header", Text: fmt.Sprintf("+++ %s (created in Haven)", havenPath)},
		}
		for _, line := range splitLines(currentContent) {
			lines = append(lines, DiffLine{Type: "add", Text: line})
		}
		return DiffResponse{
			Path:       havenPath,
			HasChanges: true,
			Lines:      lines,
		}
	}

	originalContent := string(origBytes)
	if originalContent == currentContent {
		return DiffResponse{Path: havenPath, HasChanges: false, Lines: []DiffLine{
			{Type: "header", Text: "No changes detected."},
		}}
	}

	// Compute unified diff
	lines := unifiedDiff(originalContent, currentContent, havenPath)

	return DiffResponse{
		Path:       havenPath,
		HasChanges: true,
		Lines:      lines,
	}
}

// WorkspaceRestoreOriginal restores an imported file to its original content.
// The write still routes through Loopgate so Haven does not bypass the kernel boundary.
func (app *HavenApp) WorkspaceRestoreOriginal(havenPath string) WorkspaceRestoreResponse {
	if strings.TrimSpace(havenPath) == "" {
		return WorkspaceRestoreResponse{Error: "path is required"}
	}

	originalBytes, err := os.ReadFile(app.originalFilePath(havenPath))
	if err != nil {
		return WorkspaceRestoreResponse{Error: "no original version found — file may not have been imported"}
	}

	sandboxPath := mapHavenPathToSandbox(havenPath)
	response, err := app.loopgateClient.ExecuteCapability(context.Background(), loopgate.CapabilityRequest{
		RequestID:  fmt.Sprintf("restore-%d", time.Now().UnixNano()),
		Actor:      "haven",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    sandboxPath,
			"content": string(originalBytes),
		},
	})
	if err != nil {
		return WorkspaceRestoreResponse{Error: fmt.Sprintf("restore file: %v", err)}
	}
	if response.Status != loopgate.ResponseStatusSuccess {
		denialReason := response.DenialReason
		if denialReason == "" {
			denialReason = "restore denied"
		}
		return WorkspaceRestoreResponse{Error: denialReason}
	}

	if app.emitter != nil {
		app.emitter.Emit("haven:file_changed", map[string]interface{}{
			"path":   havenPath,
			"action": "restore",
		})
	}

	return WorkspaceRestoreResponse{Restored: true}
}

// StoreOriginal saves the original content of an imported file for later diffing.
// Called internally during import.
func (app *HavenApp) StoreOriginal(havenPath, content string) error {
	origPath := app.originalFilePath(havenPath)
	if err := os.MkdirAll(filepath.Dir(origPath), 0o700); err != nil {
		return fmt.Errorf("create originals dir: %w", err)
	}
	return os.WriteFile(origPath, []byte(content), 0o600)
}

// originalFilePath returns the path where an original file version is stored.
func (app *HavenApp) originalFilePath(havenPath string) string {
	// Hash the path to create a flat lookup key
	h := sha256.Sum256([]byte(havenPath))
	key := hex.EncodeToString(h[:8]) // 16-char hex
	return filepath.Join(app.originalsDir, key+".orig")
}

// unifiedDiff produces a simple unified diff between two texts.
func unifiedDiff(original, current, filename string) []DiffLine {
	origLines := splitLines(original)
	currLines := splitLines(current)

	var result []DiffLine
	result = append(result, DiffLine{Type: "header", Text: fmt.Sprintf("--- %s (original)", filename)})
	result = append(result, DiffLine{Type: "header", Text: fmt.Sprintf("+++ %s (modified)", filename)})

	// Simple LCS-based diff
	lcs := longestCommonSubsequence(origLines, currLines)

	oi, ci, li := 0, 0, 0
	for li < len(lcs) {
		// Emit removals (lines in original but not matching LCS)
		for oi < len(origLines) && origLines[oi] != lcs[li] {
			result = append(result, DiffLine{Type: "remove", Text: origLines[oi]})
			oi++
		}
		// Emit additions (lines in current but not matching LCS)
		for ci < len(currLines) && currLines[ci] != lcs[li] {
			result = append(result, DiffLine{Type: "add", Text: currLines[ci]})
			ci++
		}
		// Emit context (matching line)
		result = append(result, DiffLine{Type: "context", Text: lcs[li]})
		oi++
		ci++
		li++
	}
	// Remaining removals
	for oi < len(origLines) {
		result = append(result, DiffLine{Type: "remove", Text: origLines[oi]})
		oi++
	}
	// Remaining additions
	for ci < len(currLines) {
		result = append(result, DiffLine{Type: "add", Text: currLines[ci]})
		ci++
	}

	return result
}

// longestCommonSubsequence computes the LCS of two string slices.
func longestCommonSubsequence(a, b []string) []string {
	m, n := len(a), len(b)
	// For very large files, limit to prevent excessive memory use
	if m > 5000 || n > 5000 {
		// Fall back to simple line-by-line comparison
		return nil
	}

	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	// Backtrack to get the LCS
	lcs := make([]string, dp[m][n])
	i, j, k := m, n, dp[m][n]-1
	for i > 0 && j > 0 {
		if a[i-1] == b[j-1] {
			lcs[k] = a[i-1]
			i--
			j--
			k--
		} else if dp[i-1][j] >= dp[i][j-1] {
			i--
		} else {
			j--
		}
	}

	return lcs
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	// Remove trailing empty line from final newline
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
