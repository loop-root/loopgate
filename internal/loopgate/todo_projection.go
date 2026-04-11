package loopgate

import (
	"sort"
	"strings"
)

func goalTextsFromContinuityState(currentState continuityMemoryState) []string {
	goalSnapshot := buildGoalsCurrentSnapshot(currentState)
	rawGoals, ok := goalSnapshot["goals"].([]map[string]string)
	if !ok || len(rawGoals) == 0 {
		return nil
	}
	texts := make([]string, 0, len(rawGoals))
	for _, goalEntry := range rawGoals {
		texts = append(texts, goalEntry["text"])
	}
	return texts
}

func explicitTodoOpenDistillateForItemID(currentState continuityMemoryState, itemID string) (continuityDistillateRecord, bool) {
	distillates := make([]continuityDistillateRecord, 0, len(currentState.Distillates))
	for _, distillateRecord := range currentState.Distillates {
		distillates = append(distillates, distillateRecord)
	}
	sort.Slice(distillates, func(leftIndex int, rightIndex int) bool {
		if distillates[leftIndex].CreatedAtUTC != distillates[rightIndex].CreatedAtUTC {
			return distillates[leftIndex].CreatedAtUTC < distillates[rightIndex].CreatedAtUTC
		}
		return distillates[leftIndex].DistillateID < distillates[rightIndex].DistillateID
	})
	for _, distillateRecord := range distillates {
		if !isExplicitTodoDistillate(distillateRecord) {
			continue
		}
		for _, itemOp := range distillateRecord.UnresolvedItemOps {
			if itemOp.Action == "opened" && itemOp.ItemID == itemID {
				return distillateRecord, true
			}
		}
	}
	return continuityDistillateRecord{}, false
}

func isExplicitTodoCompletionDistillate(distillateRecord continuityDistillateRecord) bool {
	if !isExplicitTodoDistillate(distillateRecord) {
		return false
	}
	if len(distillateRecord.UnresolvedItemOps) != 1 {
		return false
	}
	return distillateRecord.UnresolvedItemOps[0].Action == "closed"
}

func recentExplicitTodoCompletionUIEntries(currentState continuityMemoryState, maxItems int) []UITasksItemEntry {
	if maxItems <= 0 {
		return nil
	}
	completions := make([]continuityDistillateRecord, 0)
	for _, distillateRecord := range currentState.Distillates {
		if isExplicitTodoCompletionDistillate(distillateRecord) {
			completions = append(completions, distillateRecord)
		}
	}
	sort.Slice(completions, func(leftIndex int, rightIndex int) bool {
		leftCreated := completions[leftIndex].CreatedAtUTC
		rightCreated := completions[rightIndex].CreatedAtUTC
		if leftCreated != rightCreated {
			return leftCreated > rightCreated
		}
		return completions[leftIndex].DistillateID > completions[rightIndex].DistillateID
	})
	if len(completions) > maxItems {
		completions = completions[:maxItems]
	}
	entries := make([]UITasksItemEntry, 0, len(completions))
	for _, completionDistillate := range completions {
		closeOp := completionDistillate.UnresolvedItemOps[0]
		itemID := closeOp.ItemID
		openDistillate, foundOpen := explicitTodoOpenDistillateForItemID(currentState, itemID)
		meta := explicitTodoTaskMetadataFromDistillate(completionDistillate)
		createdAt := completionDistillate.CreatedAtUTC
		if foundOpen {
			meta = explicitTodoTaskMetadataFromDistillate(openDistillate)
			createdAt = openDistillate.CreatedAtUTC
		}
		entries = append(entries, UITasksItemEntry{
			ID:           itemID,
			Text:         closeOp.Text,
			TaskKind:     meta.TaskKind,
			SourceKind:   meta.SourceKind,
			NextStep:     meta.NextStep,
			Status:       explicitTodoWorkflowStatusDone,
			CreatedAtUTC: createdAt,
		})
	}
	return entries
}

func buildUITasksResponseFromContinuityState(currentState continuityMemoryState) UITasksResponse {
	response := UITasksResponse{
		Goals: goalTextsFromContinuityState(currentState),
		Items: make([]UITasksItemEntry, 0, 8),
	}
	for _, activeRecord := range activeExplicitTodoItems(currentState) {
		status := activeRecord.Status
		if status == "" {
			status = explicitTodoWorkflowStatusTodo
		}
		response.Items = append(response.Items, UITasksItemEntry{
			ID:           activeRecord.ItemID,
			Text:         activeRecord.Text,
			TaskKind:     activeRecord.TaskKind,
			SourceKind:   activeRecord.SourceKind,
			NextStep:     activeRecord.NextStep,
			Status:       status,
			CreatedAtUTC: activeRecord.CreatedAtUTC,
		})
	}
	response.Items = append(response.Items, recentExplicitTodoCompletionUIEntries(currentState, maxUIRecentCompletedTodoItems)...)
	return response
}

func activeExplicitTodoItemByID(currentState continuityMemoryState, itemID string) (explicitTodoItemRecord, bool) {
	for _, activeTodoItem := range activeExplicitTodoItems(currentState) {
		if activeTodoItem.ItemID == itemID {
			return activeTodoItem, true
		}
	}
	return explicitTodoItemRecord{}, false
}

func activeExplicitTodoItemByText(currentState continuityMemoryState, text string) (explicitTodoItemRecord, bool) {
	normalizedText := normalizeTodoText(text)
	for _, activeTodoItem := range activeExplicitTodoItems(currentState) {
		if normalizeTodoText(activeTodoItem.Text) == normalizedText {
			return activeTodoItem, true
		}
	}
	return explicitTodoItemRecord{}, false
}

func activeExplicitTodoItems(currentState continuityMemoryState) []explicitTodoItemRecord {
	activeTodoByID := make(map[string]explicitTodoItemRecord)
	activeTodoOrder := make([]string, 0)

	for _, distillateRecord := range activeLoopgateDistillates(currentState) {
		if !isExplicitTodoDistillate(distillateRecord) {
			continue
		}
		resonateKeyID := resonateKeyIDForDistillate(currentState, distillateRecord.DistillateID)
		taskMetadata := explicitTodoTaskMetadataFromDistillate(distillateRecord)
		for _, itemOp := range distillateRecord.UnresolvedItemOps {
			switch itemOp.Action {
			case "opened":
				activeTodoByID[itemOp.ItemID] = explicitTodoItemRecord{
					InspectionID:    distillateRecord.InspectionID,
					DistillateID:    distillateRecord.DistillateID,
					ResonateKeyID:   resonateKeyID,
					ItemID:          itemOp.ItemID,
					Text:            itemOp.Text,
					TaskKind:        taskMetadata.TaskKind,
					SourceKind:      taskMetadata.SourceKind,
					NextStep:        taskMetadata.NextStep,
					ScheduledForUTC: taskMetadata.ScheduledForUTC,
					ExecutionClass:  taskMetadata.ExecutionClass,
					CreatedAtUTC:    distillateRecord.CreatedAtUTC,
					Status:          explicitTodoWorkflowStatusTodo,
				}
				activeTodoOrder = appendWithoutDuplicate(activeTodoOrder, itemOp.ItemID)
			case "closed":
				delete(activeTodoByID, itemOp.ItemID)
				activeTodoOrder = removeStringValue(activeTodoOrder, itemOp.ItemID)
			case todoItemOpStatusSet:
				if existingTodo, ok := activeTodoByID[itemOp.ItemID]; ok {
					if normalized := normalizeExplicitTodoWorkflowStatus(itemOp.Status); normalized != "" {
						existingTodo.Status = normalized
						activeTodoByID[itemOp.ItemID] = existingTodo
					}
				}
			}
		}
	}

	activeTodos := make([]explicitTodoItemRecord, 0, len(activeTodoOrder))
	for _, itemID := range activeTodoOrder {
		activeTodoItem, found := activeTodoByID[itemID]
		if !found {
			continue
		}
		activeTodos = append(activeTodos, activeTodoItem)
	}
	sort.Slice(activeTodos, func(leftIndex int, rightIndex int) bool {
		leftTodo := activeTodos[leftIndex]
		rightTodo := activeTodos[rightIndex]
		if leftTodo.CreatedAtUTC != rightTodo.CreatedAtUTC {
			return leftTodo.CreatedAtUTC < rightTodo.CreatedAtUTC
		}
		return leftTodo.ItemID < rightTodo.ItemID
	})
	return activeTodos
}

func resonateKeyIDForDistillate(currentState continuityMemoryState, distillateID string) string {
	for _, resonateKeyRecord := range currentState.ResonateKeys {
		if resonateKeyRecord.DistillateID == distillateID {
			return resonateKeyRecord.KeyID
		}
	}
	return ""
}

func isExplicitTodoDistillate(distillateRecord continuityDistillateRecord) bool {
	for _, sourceRef := range distillateRecord.SourceRefs {
		if sourceRef.Kind == explicitTodoSourceKind {
			return true
		}
	}
	return false
}

func explicitTodoTaskMetadataFromDistillate(distillateRecord continuityDistillateRecord) MemoryWakeStateOpenItem {
	taskMetadata := MemoryWakeStateOpenItem{
		TaskKind:       taskKindCarryOver,
		SourceKind:     taskSourceContinuity,
		ExecutionClass: TaskExecutionClassApprovalRequired,
		CreatedAtUTC:   distillateRecord.CreatedAtUTC,
	}
	if isExplicitTodoDistillate(distillateRecord) {
		taskMetadata.SourceKind = taskSourceUser
	}
	for _, factRecord := range distillateRecord.Facts {
		factValue, _ := factRecord.Value.(string)
		switch strings.TrimSpace(factRecord.Name) {
		case taskFactKind:
			normalizedTaskKind := normalizeTaskKind(factValue)
			if validateTaskKind(normalizedTaskKind) == nil {
				taskMetadata.TaskKind = normalizedTaskKind
			}
		case taskFactSourceKind:
			normalizedSourceKind := normalizeTaskSourceKind(factValue)
			if validateTaskSourceKind(normalizedSourceKind) == nil {
				taskMetadata.SourceKind = normalizedSourceKind
			}
		case taskFactNextStep:
			taskMetadata.NextStep = normalizeTodoText(factValue)
		case taskFactScheduledForUT:
			taskMetadata.ScheduledForUTC = strings.TrimSpace(factValue)
		case taskFactExecutionClass:
			normalizedExecutionClass := normalizeTaskExecutionClass(factValue)
			if validateTaskExecutionClass(normalizedExecutionClass) == nil {
				taskMetadata.ExecutionClass = normalizedExecutionClass
			}
		}
	}
	return taskMetadata
}
