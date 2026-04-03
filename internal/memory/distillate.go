package memory

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var ErrLedgerIntegrity = errors.New("ledger integrity anomaly")

// Distillate represents compressed long-term memory derived from ledger events.
// It is intentionally small, structured, and append-only (JSONL).
type Distillate struct {
	ID             string   `json:"id"`
	WindowStartUTC string   `json:"window_start_utc"`
	WindowEndUTC   string   `json:"window_end_utc"`
	Trigger        string   `json:"trigger,omitempty"`
	EventCount     int      `json:"event_count"`
	ToolsUsed      []string `json:"tools_used,omitempty"`
	FilesTouched   []string `json:"files_touched,omitempty"`
	ApprovalsCount int      `json:"approvals_count"`
	DenialsCount   int      `json:"denials_count"`
	PolicyEvents   []string `json:"policy_events,omitempty"`
	Importance     float64  `json:"importance"`
	CreatedAtUTC   string   `json:"created_at_utc"`
}

// DistillFromLedger reads ledger events starting from a 1-based line cursor.
// It appends at most a small chunk of new events into distillates.jsonl and
// returns the new cursor (the last processed ledger line).
//
// Design goals:
// - cursor-based (no overlap / no duplicates)
// - chunked (keeps distillates readable and "recent")
// - derived from the ledger (auditable, not vibes)
func DistillFromLedger(ledgerPath string, distillatePath string, startLine int, trigger string) (int, error) {
	const maxEventsPerDistillate = 50

	file, err := openVerifiedMemoryLedger(ledgerPath)
	if err != nil {
		return startLine, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// Ledger lines can grow; bump scanner buffer to 1MB.
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	currentLine := 0
	lastProcessedLine := startLine

	toolsSet := map[string]bool{}
	filesSet := map[string]bool{}
	policySet := map[string]bool{}
	approvals := 0
	denials := 0
	started := ""
	ended := ""
	count := 0

	for scanner.Scan() {
		currentLine++
		if currentLine <= startLine {
			continue
		}

		// Stop after a chunk; next trigger will continue from the updated cursor.
		if count >= maxEventsPerDistillate {
			break
		}

		var raw map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
			return startLine, fmt.Errorf("%w: malformed ledger line %d: %v", ErrLedgerIntegrity, currentLine, err)
		}

		count++
		lastProcessedLine = currentLine

		if timestamp, ok := raw["ts"].(string); ok {
			if started == "" {
				started = timestamp
			}
			ended = timestamp
		}

		typ, _ := raw["type"].(string)
		if typ == "" {
			continue
		}

		// Tools
		if strings.HasPrefix(typ, "tool.") {
			toolsSet[typ] = true
		}

		// Policy-relevant signals (we treat approvals/denials as policy events)
		if strings.HasSuffix(typ, ".approval") {
			approvals++
			policySet[typ] = true
		}
		if strings.Contains(typ, ".denied") || strings.HasSuffix(typ, ".denied") {
			denials++
			policySet[typ] = true
		}
		if strings.HasPrefix(typ, "policy.") {
			policySet[typ] = true
		}

		// Files touched: look for data.path (preferred) or data.abs.
		if data, ok := raw["data"].(map[string]interface{}); ok {
			if filePath, ok := data["path"].(string); ok && filePath != "" {
				filesSet[filePath] = true
			} else if abs, ok := data["abs"].(string); ok && abs != "" {
				filesSet[abs] = true
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return startLine, err
	}

	// Nothing new to distill.
	if count == 0 {
		return startLine, nil
	}

	tools := make([]string, 0, len(toolsSet))
	for toolName := range toolsSet {
		tools = append(tools, toolName)
	}
	sort.Strings(tools)

	files := make([]string, 0, len(filesSet))
	for filePath := range filesSet {
		files = append(files, filePath)
	}
	sort.Strings(files)

	policyEvents := make([]string, 0, len(policySet))
	for policyEvent := range policySet {
		policyEvents = append(policyEvents, policyEvent)
	}
	sort.Strings(policyEvents)

	distillate := Distillate{
		ID:             "dist-" + time.Now().UTC().Format("20060102150405"),
		WindowStartUTC: started,
		WindowEndUTC:   ended,
		Trigger:        strings.TrimSpace(trigger),
		EventCount:     count,
		ToolsUsed:      tools,
		FilesTouched:   files,
		ApprovalsCount: approvals,
		DenialsCount:   denials,
		PolicyEvents:   policyEvents,
		Importance:     0.5,
		CreatedAtUTC:   time.Now().UTC().Format(time.RFC3339),
	}

	if err := os.MkdirAll(filepath.Dir(distillatePath), 0700); err != nil {
		return startLine, err
	}

	f, err := os.OpenFile(distillatePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return startLine, err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	if err := enc.Encode(distillate); err != nil {
		return startLine, err
	}

	return lastProcessedLine, nil
}
