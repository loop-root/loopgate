package memory

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type GlobalContinuityState struct {
	GoalsByID           map[string]string
	UnresolvedItemsByID map[string]WakeStateOpenItem
}

func LoadGlobalContinuityState(ledgerPath string) (GlobalContinuityState, error) {
	ledgerFileHandle, err := openVerifiedMemoryLedger(ledgerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return GlobalContinuityState{
				GoalsByID:           map[string]string{},
				UnresolvedItemsByID: map[string]WakeStateOpenItem{},
			}, nil
		}
		return GlobalContinuityState{}, err
	}
	defer ledgerFileHandle.Close()

	continuityState := GlobalContinuityState{
		GoalsByID:           map[string]string{},
		UnresolvedItemsByID: map[string]WakeStateOpenItem{},
	}

	ledgerScanner := bufio.NewScanner(ledgerFileHandle)
	ledgerScanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for ledgerScanner.Scan() {
		var rawLedgerEvent map[string]interface{}
		if err := json.Unmarshal(ledgerScanner.Bytes(), &rawLedgerEvent); err != nil {
			return GlobalContinuityState{}, fmt.Errorf("%w: malformed ledger line in continuity-state load: %v", ErrLedgerIntegrity, err)
		}
		rawEventData, _ := rawLedgerEvent["data"].(map[string]interface{})
		continuityEvent, foundContinuityEvent := parseContinuityEvent(rawEventData)
		if !foundContinuityEvent {
			continue
		}
		if strings.TrimSpace(continuityEvent.Scope) != MemoryScopeGlobal {
			continue
		}
		switch strings.TrimSpace(continuityEvent.Type) {
		case ContinuityEventTypeGoalOpened:
			goalID := strings.TrimSpace(stringValue(continuityEvent.Payload["goal_id"]))
			goalText := strings.TrimSpace(stringValue(continuityEvent.Payload["text"]))
			if goalID == "" || goalText == "" {
				continue
			}
			continuityState.GoalsByID[goalID] = goalText
		case ContinuityEventTypeGoalClosed:
			goalID := strings.TrimSpace(stringValue(continuityEvent.Payload["goal_id"]))
			if goalID == "" {
				continue
			}
			delete(continuityState.GoalsByID, goalID)
		case ContinuityEventTypeUnresolvedItemOpened:
			itemID := strings.TrimSpace(stringValue(continuityEvent.Payload["item_id"]))
			itemText := strings.TrimSpace(stringValue(continuityEvent.Payload["text"]))
			if itemID == "" || itemText == "" {
				continue
			}
			continuityState.UnresolvedItemsByID[itemID] = WakeStateOpenItem{
				ID:   itemID,
				Text: itemText,
			}
		case ContinuityEventTypeUnresolvedItemClosed:
			itemID := strings.TrimSpace(stringValue(continuityEvent.Payload["item_id"]))
			if itemID == "" {
				continue
			}
			delete(continuityState.UnresolvedItemsByID, itemID)
		}
	}
	if err := ledgerScanner.Err(); err != nil {
		return GlobalContinuityState{}, err
	}

	return continuityState, nil
}
