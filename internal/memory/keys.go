package memory

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"morph/internal/audit"
	"morph/internal/ledger"
)

// SessionState is the minimal interface required from runtime state.
type SessionState struct {
	SessionID    string
	StartedAtUTC string
	TurnCount    int
}

// Paths groups memory-related filesystem paths.
type Paths struct {
	KeysPath   string
	LedgerPath string
}

// FinalizeSession creates a resonate key if meaningful turns occurred.
func FinalizeSession(paths Paths, state SessionState) error {
	if state.TurnCount <= 0 {
		return nil
	}

	keyID := "rk-" + state.SessionID
	keyFilename := state.SessionID + ".json"
	keyPath := filepath.Join(paths.KeysPath, keyFilename)

	keyDocument := map[string]interface{}{
		"id":             keyID,
		"session_id":     state.SessionID,
		"scope":          MemoryScopeGlobal,
		"started_at_utc": state.StartedAtUTC,
		"ended_at_utc":   time.Now().UTC().Format(time.RFC3339),
		"turns":          state.TurnCount,
	}
	sessionTags, err := collectSessionResonateKeyTags(paths.LedgerPath, state.SessionID)
	if err != nil {
		return err
	}
	if len(sessionTags) > 0 {
		keyDocument["tags"] = sessionTags
	}

	if err := os.MkdirAll(paths.KeysPath, 0700); err != nil {
		return err
	}

	keyBytes, err := json.MarshalIndent(keyDocument, "", "  ")
	if err != nil {
		return err
	}

	if err := writePrivateJSONAtomically(keyPath, keyBytes); err != nil {
		return err
	}

	ledgerEventData := map[string]interface{}{
		"key_id":       keyID,
		"key_filename": keyFilename,
		"turns":        state.TurnCount,
	}
	ledgerEventData = AnnotateMemoryCandidate(
		ledgerEventData,
		MemoryCandidateTypeResonateKey,
		MemoryScopeGlobal,
		EpistemicFlavorRemembered,
		map[string]interface{}{
			"key_id": keyID,
			"turns":  state.TurnCount,
		},
	)
	ledgerEventData = AnnotateContinuityEvent(
		ledgerEventData,
		ContinuityEventTypeResonateKeyCreated,
		MemoryScopeGlobal,
		EpistemicFlavorRemembered,
		nil,
		map[string]interface{}{
			"key_id": keyID,
			"turns":  state.TurnCount,
		},
	)

	appendErr := audit.RecordMustPersist(paths.LedgerPath, ledger.Event{
		TS:      time.Now().UTC().Format(time.RFC3339),
		Type:    "memory.resonate_key.created",
		Session: state.SessionID,
		Data:    ledgerEventData,
	})
	if appendErr == nil {
		return nil
	}

	removeErr := os.Remove(keyPath)
	if removeErr != nil && !os.IsNotExist(removeErr) {
		return fmt.Errorf("append resonate key ledger event: %w (cleanup key file: %v)", appendErr, removeErr)
	}
	return fmt.Errorf("append resonate key ledger event: %w", appendErr)
}

func collectSessionResonateKeyTags(ledgerPath string, sessionID string) ([]string, error) {
	ledgerFileHandle, err := openVerifiedMemoryLedger(ledgerPath)
	if err != nil {
		return nil, err
	}
	defer ledgerFileHandle.Close()

	ledgerScanner := bufio.NewScanner(ledgerFileHandle)
	ledgerScanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	discoveredTags := map[string]struct{}{}
	for ledgerScanner.Scan() {
		var rawLedgerEvent map[string]interface{}
		if err := json.Unmarshal(ledgerScanner.Bytes(), &rawLedgerEvent); err != nil {
			return nil, fmt.Errorf("%w: malformed ledger line in resonate-key tag collection: %v", ErrLedgerIntegrity, err)
		}
		if strings.TrimSpace(stringValue(rawLedgerEvent["session"])) != strings.TrimSpace(sessionID) {
			continue
		}
		rawEventData, _ := rawLedgerEvent["data"].(map[string]interface{})
		continuityEvent, foundContinuityEvent := parseContinuityEvent(rawEventData)
		if !foundContinuityEvent {
			continue
		}
		recordContinuityTags(discoveredTags, strings.TrimSpace(continuityEvent.Scope))
		switch strings.TrimSpace(continuityEvent.Type) {
		case ContinuityEventTypeGoalOpened:
			recordContinuityTags(discoveredTags, stringValue(continuityEvent.Payload["goal_id"]), stringValue(continuityEvent.Payload["text"]))
		case ContinuityEventTypeUnresolvedItemOpened:
			recordContinuityTags(discoveredTags, stringValue(continuityEvent.Payload["item_id"]), stringValue(continuityEvent.Payload["text"]))
		case ContinuityEventTypeProviderFactObserved:
			candidateFacts, _ := continuityEvent.Payload["facts"].(map[string]interface{})
			factNames := make([]string, 0, len(candidateFacts))
			for factName := range candidateFacts {
				factNames = append(factNames, factName)
			}
			sort.Strings(factNames)
			for _, factName := range factNames {
				recordContinuityTags(discoveredTags, factName)
				if factValue, isString := candidateFacts[factName].(string); isString {
					recordContinuityTags(discoveredTags, factValue)
				}
			}
		}
	}
	if err := ledgerScanner.Err(); err != nil {
		return nil, err
	}

	normalizedTags := make([]string, 0, len(discoveredTags))
	for tag := range discoveredTags {
		normalizedTags = append(normalizedTags, tag)
	}
	sort.Strings(normalizedTags)
	if len(normalizedTags) > 12 {
		normalizedTags = append([]string(nil), normalizedTags[:12]...)
	}
	return normalizedTags, nil
}

func writePrivateJSONAtomically(targetPath string, fileContents []byte) error {
	parentDir := filepath.Dir(targetPath)
	temporaryPath := targetPath + ".tmp"

	fileHandle, err := os.OpenFile(temporaryPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}

	if _, err := fileHandle.Write(fileContents); err != nil {
		_ = fileHandle.Close()
		_ = os.Remove(temporaryPath)
		return err
	}
	if len(fileContents) == 0 || fileContents[len(fileContents)-1] != '\n' {
		if _, err := fileHandle.WriteString("\n"); err != nil {
			_ = fileHandle.Close()
			_ = os.Remove(temporaryPath)
			return err
		}
	}
	if err := fileHandle.Sync(); err != nil {
		_ = fileHandle.Close()
		_ = os.Remove(temporaryPath)
		return err
	}
	if err := fileHandle.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	if err := os.Rename(temporaryPath, targetPath); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}

	parentHandle, err := os.Open(parentDir)
	if err == nil {
		_ = parentHandle.Sync()
		_ = parentHandle.Close()
	}
	return nil
}
