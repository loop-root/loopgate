package loopgate

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	tclpkg "morph/internal/tcl"
)

func (store *continuitySQLiteStore) replaceProjectedNodes(authoritativeState continuityMemoryState) error {
	transaction, err := store.database.Begin()
	if err != nil {
		return fmt.Errorf("begin projected node sync: %w", err)
	}
	defer func() {
		_ = transaction.Rollback()
	}()

	if _, err := transaction.Exec(
		`DELETE FROM memory_nodes WHERE node_kind IN (?, ?, ?)`,
		sqliteNodeKindExplicitRememberedFact,
		sqliteNodeKindExplicitTaskMetadata,
		sqliteNodeKindWorkflowTransition,
	); err != nil {
		return fmt.Errorf("clear projected nodes: %w", err)
	}

	if err := store.insertExplicitRememberedFactNodes(transaction, authoritativeState); err != nil {
		return err
	}
	if err := store.insertExplicitTaskMetadataNodes(transaction, authoritativeState); err != nil {
		return err
	}
	if err := store.insertWorkflowTransitionNodes(transaction, authoritativeState); err != nil {
		return err
	}

	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit projected node sync: %w", err)
	}
	return nil
}

func (store *continuitySQLiteStore) insertExplicitRememberedFactNodes(transaction *sql.Tx, authoritativeState continuityMemoryState) error {
	explicitDistillates := explicitRememberedFactDistillates(authoritativeState)
	for _, distillateRecord := range explicitDistillates {
		explicitFactRecord, found := explicitProfileFactFromDistillate(authoritativeState, distillateRecord)
		if !found {
			continue
		}
		inspectionRecord, found := authoritativeState.Inspections[distillateRecord.InspectionID]
		if !found {
			return fmt.Errorf("explicit remembered fact inspection %q not found", distillateRecord.InspectionID)
		}
		inspectionRecord = normalizeContinuityInspectionRecordMust(inspectionRecord)
		factRecord := distillateRecord.Facts[0]
		semanticProjection := cloneSemanticProjection(factRecord.SemanticProjection)
		if semanticProjection == nil {
			semanticProjection = &tclpkg.SemanticProjection{}
		}
		tclCoreJSONBytes, err := json.Marshal(map[string]interface{}{
			"object":           "MEM",
			"source_kind":      explicitProfileFactSourceKind,
			"fact_key":         explicitFactRecord.FactKey,
			"epistemic_flavor": factRecord.EpistemicFlavor,
		})
		if err != nil {
			return fmt.Errorf("marshal explicit remembered fact tcl core: %w", err)
		}
		riskMotifsJSONBytes, err := json.Marshal(semanticProjection.RiskMotifs)
		if err != nil {
			return fmt.Errorf("marshal explicit remembered fact risk motifs: %w", err)
		}
		hintID := "hint_" + explicitFactRecord.DistillateID
		if _, err := transaction.Exec(
			`INSERT INTO memory_nodes(
				node_id, created_at_utc, scope, node_kind, anchor_version, anchor_key, pattern_family_id,
				certainty_score, epistemic_flavor, state, current_hint_id, provenance_event_id
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			explicitFactRecord.DistillateID,
			explicitFactRecord.CreatedAtUTC,
			distillateRecord.Scope,
			sqliteNodeKindExplicitRememberedFact,
			semanticProjectionAnchorVersion(semanticProjection),
			semanticProjectionAnchorKey(semanticProjection),
			semanticProjection.FamilySignature,
			factRecord.CertaintyScore,
			factRecord.EpistemicFlavor,
			sqliteNodeStateForLineageStatus(inspectionRecord.Lineage.Status),
			hintID,
			inspectionRecord.InspectionID,
		); err != nil {
			return fmt.Errorf("insert explicit remembered fact node %q: %w", explicitFactRecord.DistillateID, err)
		}
		if _, err := transaction.Exec(
			`INSERT INTO memory_hints(hint_id, node_id, hint_kind, hint_text, byte_count, created_at_utc)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			hintID,
			explicitFactRecord.DistillateID,
			sqliteHintKindExactValue,
			explicitFactRecord.FactValue,
			len([]byte(explicitFactRecord.FactValue)),
			explicitFactRecord.CreatedAtUTC,
		); err != nil {
			return fmt.Errorf("insert explicit remembered fact hint %q: %w", hintID, err)
		}
		if _, err := transaction.Exec(
			`INSERT INTO semantic_projections(
				node_id, tcl_core_json, exact_signature, family_signature, risk_motifs_json, confidence
			) VALUES (?, ?, ?, ?, ?, ?)`,
			explicitFactRecord.DistillateID,
			string(tclCoreJSONBytes),
			semanticProjection.ExactSignature,
			semanticProjection.FamilySignature,
			string(riskMotifsJSONBytes),
			nil,
		); err != nil {
			return fmt.Errorf("insert explicit remembered fact semantic projection %q: %w", explicitFactRecord.DistillateID, err)
		}
	}
	return nil
}

func (store *continuitySQLiteStore) insertExplicitTaskMetadataNodes(transaction *sql.Tx, authoritativeState continuityMemoryState) error {
	taskMetadataDistillates := explicitTodoTaskMetadataDistillates(authoritativeState)
	for _, distillateRecord := range taskMetadataDistillates {
		inspectionRecord, found := authoritativeState.Inspections[distillateRecord.InspectionID]
		if !found {
			return fmt.Errorf("explicit task metadata inspection %q not found", distillateRecord.InspectionID)
		}
		inspectionRecord = normalizeContinuityInspectionRecordMust(inspectionRecord)
		for _, factRecord := range distillateRecord.Facts {
			if !strings.HasPrefix(strings.TrimSpace(factRecord.Name), "task.") {
				continue
			}
			semanticProjection := cloneSemanticProjection(factRecord.SemanticProjection)
			if semanticProjection == nil {
				semanticProjection = &tclpkg.SemanticProjection{}
			}
			factValue, valueIsString := factRecord.Value.(string)
			if !valueIsString {
				continue
			}
			nodeID := distillateRecord.DistillateID + "::" + strings.TrimSpace(factRecord.Name)
			hintID := "hint_" + nodeID
			tclCoreJSONBytes, err := json.Marshal(map[string]interface{}{
				"object":           "TSK",
				"source_kind":      explicitTodoSourceKind,
				"fact_key":         strings.TrimSpace(factRecord.Name),
				"epistemic_flavor": factRecord.EpistemicFlavor,
			})
			if err != nil {
				return fmt.Errorf("marshal explicit task metadata tcl core: %w", err)
			}
			riskMotifsJSONBytes, err := json.Marshal(semanticProjection.RiskMotifs)
			if err != nil {
				return fmt.Errorf("marshal explicit task metadata risk motifs: %w", err)
			}
			if _, err := transaction.Exec(
				`INSERT INTO memory_nodes(
					node_id, created_at_utc, scope, node_kind, anchor_version, anchor_key, pattern_family_id,
					certainty_score, epistemic_flavor, state, current_hint_id, provenance_event_id
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				nodeID,
				distillateRecord.CreatedAtUTC,
				distillateRecord.Scope,
				sqliteNodeKindExplicitTaskMetadata,
				semanticProjectionAnchorVersion(semanticProjection),
				semanticProjectionAnchorKey(semanticProjection),
				semanticProjection.FamilySignature,
				factRecord.CertaintyScore,
				factRecord.EpistemicFlavor,
				sqliteNodeStateForLineageStatus(inspectionRecord.Lineage.Status),
				hintID,
				inspectionRecord.InspectionID,
			); err != nil {
				return fmt.Errorf("insert explicit task metadata node %q: %w", nodeID, err)
			}
			if _, err := transaction.Exec(
				`INSERT INTO memory_hints(hint_id, node_id, hint_kind, hint_text, byte_count, created_at_utc)
				 VALUES (?, ?, ?, ?, ?, ?)`,
				hintID,
				nodeID,
				sqliteHintKindExactValue,
				factValue,
				len([]byte(factValue)),
				distillateRecord.CreatedAtUTC,
			); err != nil {
				return fmt.Errorf("insert explicit task metadata hint %q: %w", hintID, err)
			}
			if _, err := transaction.Exec(
				`INSERT INTO semantic_projections(
					node_id, tcl_core_json, exact_signature, family_signature, risk_motifs_json, confidence
				) VALUES (?, ?, ?, ?, ?, ?)`,
				nodeID,
				string(tclCoreJSONBytes),
				semanticProjection.ExactSignature,
				semanticProjection.FamilySignature,
				string(riskMotifsJSONBytes),
				nil,
			); err != nil {
				return fmt.Errorf("insert explicit task metadata semantic projection %q: %w", nodeID, err)
			}
		}
	}
	return nil
}

func explicitRememberedFactDistillates(authoritativeState continuityMemoryState) []continuityDistillateRecord {
	explicitFactDistillates := make([]continuityDistillateRecord, 0, len(authoritativeState.Distillates))
	for _, distillateRecord := range authoritativeState.Distillates {
		if !isExplicitProfileFactDistillate(distillateRecord) || len(distillateRecord.Facts) != 1 {
			continue
		}
		explicitFactDistillates = append(explicitFactDistillates, cloneContinuityDistillateRecord(distillateRecord))
	}
	sort.Slice(explicitFactDistillates, func(leftIndex int, rightIndex int) bool {
		if explicitFactDistillates[leftIndex].CreatedAtUTC != explicitFactDistillates[rightIndex].CreatedAtUTC {
			return explicitFactDistillates[leftIndex].CreatedAtUTC < explicitFactDistillates[rightIndex].CreatedAtUTC
		}
		return explicitFactDistillates[leftIndex].DistillateID < explicitFactDistillates[rightIndex].DistillateID
	})
	return explicitFactDistillates
}

func explicitTodoTaskMetadataDistillates(authoritativeState continuityMemoryState) []continuityDistillateRecord {
	taskMetadataDistillates := make([]continuityDistillateRecord, 0, len(authoritativeState.Distillates))
	for _, distillateRecord := range authoritativeState.Distillates {
		if !isExplicitTodoDistillate(distillateRecord) {
			continue
		}
		taskMetadataDistillates = append(taskMetadataDistillates, cloneContinuityDistillateRecord(distillateRecord))
	}
	sort.Slice(taskMetadataDistillates, func(leftIndex int, rightIndex int) bool {
		if taskMetadataDistillates[leftIndex].CreatedAtUTC != taskMetadataDistillates[rightIndex].CreatedAtUTC {
			return taskMetadataDistillates[leftIndex].CreatedAtUTC < taskMetadataDistillates[rightIndex].CreatedAtUTC
		}
		return taskMetadataDistillates[leftIndex].DistillateID < taskMetadataDistillates[rightIndex].DistillateID
	})
	return taskMetadataDistillates
}

func (store *continuitySQLiteStore) insertWorkflowTransitionNodes(transaction *sql.Tx, authoritativeState continuityMemoryState) error {
	workflowTransitionDistillates := workflowTransitionDistillates(authoritativeState)
	for _, distillateRecord := range workflowTransitionDistillates {
		inspectionRecord, found := authoritativeState.Inspections[distillateRecord.InspectionID]
		if !found {
			return fmt.Errorf("workflow transition inspection %q not found", distillateRecord.InspectionID)
		}
		inspectionRecord = normalizeContinuityInspectionRecordMust(inspectionRecord)
		for goalOpIndex, goalOp := range distillateRecord.GoalOps {
			nodeID := fmt.Sprintf("%s::goal_op::%d", distillateRecord.DistillateID, goalOpIndex)
			if err := store.insertWorkflowTransitionNode(transaction, distillateRecord, inspectionRecord, nodeID, "goal_op", goalOp.Action, goalOp.Text, goalOp.SemanticProjection); err != nil {
				return err
			}
		}
		for itemOpIndex, itemOp := range distillateRecord.UnresolvedItemOps {
			nodeID := fmt.Sprintf("%s::item_op::%d", distillateRecord.DistillateID, itemOpIndex)
			hintText := strings.TrimSpace(itemOp.Text)
			if hintText == "" {
				hintText = strings.TrimSpace(itemOp.Status)
			}
			if err := store.insertWorkflowTransitionNode(transaction, distillateRecord, inspectionRecord, nodeID, "item_op", itemOp.Action, hintText, itemOp.SemanticProjection); err != nil {
				return err
			}
		}
	}
	return nil
}

func (store *continuitySQLiteStore) insertWorkflowTransitionNode(
	transaction *sql.Tx,
	distillateRecord continuityDistillateRecord,
	inspectionRecord continuityInspectionRecord,
	nodeID string,
	operationKind string,
	action string,
	hintText string,
	semanticProjection *tclpkg.SemanticProjection,
) error {
	clonedProjection := cloneSemanticProjection(semanticProjection)
	if clonedProjection == nil {
		clonedProjection = &tclpkg.SemanticProjection{}
	}
	tclCoreJSONBytes, err := json.Marshal(map[string]interface{}{
		"object":      "TSK",
		"operation":   operationKind,
		"action":      strings.TrimSpace(action),
		"source_kind": "workflow_transition",
	})
	if err != nil {
		return fmt.Errorf("marshal workflow transition tcl core: %w", err)
	}
	riskMotifsJSONBytes, err := json.Marshal(clonedProjection.RiskMotifs)
	if err != nil {
		return fmt.Errorf("marshal workflow transition risk motifs: %w", err)
	}
	hintID := "hint_" + nodeID
	if _, err := transaction.Exec(
		`INSERT INTO memory_nodes(
			node_id, created_at_utc, scope, node_kind, anchor_version, anchor_key, pattern_family_id,
			certainty_score, epistemic_flavor, state, current_hint_id, provenance_event_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nodeID,
		distillateRecord.CreatedAtUTC,
		distillateRecord.Scope,
		sqliteNodeKindWorkflowTransition,
		semanticProjectionAnchorVersion(clonedProjection),
		semanticProjectionAnchorKey(clonedProjection),
		clonedProjection.FamilySignature,
		0,
		"workflow_transition",
		sqliteNodeStateForLineageStatus(inspectionRecord.Lineage.Status),
		hintID,
		inspectionRecord.InspectionID,
	); err != nil {
		return fmt.Errorf("insert workflow transition node %q: %w", nodeID, err)
	}
	if _, err := transaction.Exec(
		`INSERT INTO memory_hints(hint_id, node_id, hint_kind, hint_text, byte_count, created_at_utc)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		hintID,
		nodeID,
		sqliteHintKindExactValue,
		hintText,
		len([]byte(hintText)),
		distillateRecord.CreatedAtUTC,
	); err != nil {
		return fmt.Errorf("insert workflow transition hint %q: %w", hintID, err)
	}
	if _, err := transaction.Exec(
		`INSERT INTO semantic_projections(
			node_id, tcl_core_json, exact_signature, family_signature, risk_motifs_json, confidence
		) VALUES (?, ?, ?, ?, ?, ?)`,
		nodeID,
		string(tclCoreJSONBytes),
		clonedProjection.ExactSignature,
		clonedProjection.FamilySignature,
		string(riskMotifsJSONBytes),
		nil,
	); err != nil {
		return fmt.Errorf("insert workflow transition semantic projection %q: %w", nodeID, err)
	}
	return nil
}

func workflowTransitionDistillates(authoritativeState continuityMemoryState) []continuityDistillateRecord {
	workflowDistillates := make([]continuityDistillateRecord, 0, len(authoritativeState.Distillates))
	for _, distillateRecord := range authoritativeState.Distillates {
		if len(distillateRecord.GoalOps) == 0 && len(distillateRecord.UnresolvedItemOps) == 0 {
			continue
		}
		workflowDistillates = append(workflowDistillates, cloneContinuityDistillateRecord(distillateRecord))
	}
	sort.Slice(workflowDistillates, func(leftIndex int, rightIndex int) bool {
		if workflowDistillates[leftIndex].CreatedAtUTC != workflowDistillates[rightIndex].CreatedAtUTC {
			return workflowDistillates[leftIndex].CreatedAtUTC < workflowDistillates[rightIndex].CreatedAtUTC
		}
		return workflowDistillates[leftIndex].DistillateID < workflowDistillates[rightIndex].DistillateID
	})
	return workflowDistillates
}

func sqliteNodeStateForLineageStatus(lineageStatus string) string {
	switch lineageStatus {
	case continuityLineageStatusEligible:
		return "active"
	case continuityLineageStatusTombstoned:
		return "tombstoned"
	case continuityLineageStatusPurged:
		return "purged"
	default:
		return "unknown"
	}
}
