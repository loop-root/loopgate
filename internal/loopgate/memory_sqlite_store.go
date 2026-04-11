package loopgate

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tclpkg "morph/internal/tcl"

	_ "modernc.org/sqlite"
)

const continuitySQLiteSchemaVersion = "1"

const sqliteNodeKindExplicitRememberedFact = "explicit_remembered_fact"
const sqliteNodeKindExplicitTaskMetadata = "explicit_task_metadata"
const sqliteNodeKindWorkflowTransition = "workflow_transition"
const sqliteNodeKindBenchmarkFixtureStep = "benchmark_fixture_step"
const sqliteHintKindExactValue = "exact_value"

type continuitySQLiteStore struct {
	databasePath string
	database     *sql.DB
}

type continuitySQLiteProjectedNode struct {
	NodeID          string
	CreatedAtUTC    string
	Scope           string
	NodeKind        string
	AnchorVersion   string
	AnchorKey       string
	State           string
	HintText        string
	ExactSignature  string
	FamilySignature string
	TCLCoreJSON     string
	ProvenanceEvent string
	MatchCount      int
}

type BenchmarkProjectedNodeSeed struct {
	NodeID          string
	CreatedAtUTC    string
	Scope           string
	NodeKind        string
	State           string
	HintText        string
	ExactSignature  string
	FamilySignature string
	ProvenanceEvent string
}

func openContinuitySQLiteStore(databasePath string) (*continuitySQLiteStore, error) {
	if databasePath == "" {
		return nil, fmt.Errorf("sqlite database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o700); err != nil {
		return nil, fmt.Errorf("ensure sqlite store directory: %w", err)
	}
	databaseHandle, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	databaseHandle.SetMaxOpenConns(1)
	store := &continuitySQLiteStore{
		databasePath: databasePath,
		database:     databaseHandle,
	}
	if err := store.initialize(); err != nil {
		_ = databaseHandle.Close()
		return nil, err
	}
	return store, nil
}

func (store *continuitySQLiteStore) initialize() error {
	connectionStatements := []string{
		`PRAGMA foreign_keys = ON;`,
		`PRAGMA journal_mode = WAL;`,
		`PRAGMA synchronous = FULL;`,
	}
	for _, statement := range connectionStatements {
		if _, err := store.database.Exec(statement); err != nil {
			return fmt.Errorf("initialize sqlite connection: %w", err)
		}
	}

	schemaStatements := []string{
		`CREATE TABLE IF NOT EXISTS memory_store_meta (
			meta_key TEXT PRIMARY KEY,
			meta_value TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS memory_nodes (
			node_id TEXT PRIMARY KEY,
			created_at_utc TEXT NOT NULL,
			scope TEXT NOT NULL,
			node_kind TEXT NOT NULL,
			anchor_version TEXT,
			anchor_key TEXT,
			pattern_family_id TEXT,
			certainty_score INTEGER NOT NULL,
			epistemic_flavor TEXT NOT NULL,
			state TEXT NOT NULL,
			current_hint_id TEXT,
			provenance_event_id TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS memory_hints (
			hint_id TEXT PRIMARY KEY,
			node_id TEXT NOT NULL,
			hint_kind TEXT NOT NULL,
			hint_text TEXT NOT NULL,
			byte_count INTEGER NOT NULL,
			created_at_utc TEXT NOT NULL,
			FOREIGN KEY(node_id) REFERENCES memory_nodes(node_id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS semantic_projections (
			node_id TEXT PRIMARY KEY,
			tcl_core_json TEXT NOT NULL,
			exact_signature TEXT,
			family_signature TEXT,
			risk_motifs_json TEXT,
			confidence REAL,
			FOREIGN KEY(node_id) REFERENCES memory_nodes(node_id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS memory_edges (
			edge_id TEXT PRIMARY KEY,
			from_node_id TEXT NOT NULL,
			edge_type TEXT NOT NULL,
			to_node_id TEXT NOT NULL,
			weight REAL NOT NULL,
			created_at_utc TEXT NOT NULL,
			FOREIGN KEY(from_node_id) REFERENCES memory_nodes(node_id) ON DELETE CASCADE,
			FOREIGN KEY(to_node_id) REFERENCES memory_nodes(node_id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS wake_snapshots (
			snapshot_id TEXT PRIMARY KEY,
			scope TEXT NOT NULL,
			created_at_utc TEXT NOT NULL,
			payload_json TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS pattern_families (
			pattern_family_id TEXT PRIMARY KEY,
			version TEXT NOT NULL,
			tcl_shape_json TEXT NOT NULL,
			description TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_memory_nodes_anchor ON memory_nodes(anchor_version, anchor_key, state);`,
		`CREATE INDEX IF NOT EXISTS idx_memory_nodes_family ON memory_nodes(pattern_family_id, state);`,
		`CREATE INDEX IF NOT EXISTS idx_memory_edges_from ON memory_edges(from_node_id, edge_type);`,
		`CREATE INDEX IF NOT EXISTS idx_memory_edges_to ON memory_edges(to_node_id, edge_type);`,
		`CREATE INDEX IF NOT EXISTS idx_memory_nodes_scope_time ON memory_nodes(scope, created_at_utc);`,
		`INSERT INTO memory_store_meta(meta_key, meta_value)
			VALUES ('schema_version', ?)
			ON CONFLICT(meta_key) DO UPDATE SET meta_value = excluded.meta_value;`,
	}

	transaction, err := store.database.Begin()
	if err != nil {
		return fmt.Errorf("begin sqlite initialization: %w", err)
	}
	defer func() {
		_ = transaction.Rollback()
	}()
	for _, statement := range schemaStatements[:len(schemaStatements)-1] {
		if _, err := transaction.Exec(statement); err != nil {
			return fmt.Errorf("initialize sqlite schema: %w", err)
		}
	}
	if _, err := transaction.Exec(schemaStatements[len(schemaStatements)-1], continuitySQLiteSchemaVersion); err != nil {
		return fmt.Errorf("write sqlite schema version: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit sqlite initialization: %w", err)
	}
	return nil
}

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

func (store *continuitySQLiteStore) replaceBenchmarkProjectedNodes(seedNodes []BenchmarkProjectedNodeSeed) error {
	transaction, err := store.database.Begin()
	if err != nil {
		return fmt.Errorf("begin benchmark projected node sync: %w", err)
	}
	defer func() {
		_ = transaction.Rollback()
	}()

	if _, err := transaction.Exec(`DELETE FROM memory_nodes`); err != nil {
		return fmt.Errorf("clear benchmark projected nodes: %w", err)
	}
	for _, seedNode := range seedNodes {
		if err := store.insertBenchmarkProjectedNode(transaction, seedNode); err != nil {
			return err
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit benchmark projected node sync: %w", err)
	}
	return nil
}

func (store *continuitySQLiteStore) replaceBenchmarkFixtureProjectedNodes(seedNodes []BenchmarkProjectedNodeSeed) error {
	transaction, err := store.database.Begin()
	if err != nil {
		return fmt.Errorf("begin benchmark fixture projected node sync: %w", err)
	}
	defer func() {
		_ = transaction.Rollback()
	}()

	if _, err := transaction.Exec(`DELETE FROM memory_nodes WHERE node_kind = ?`, sqliteNodeKindBenchmarkFixtureStep); err != nil {
		return fmt.Errorf("clear benchmark fixture projected nodes: %w", err)
	}
	for _, seedNode := range seedNodes {
		if err := store.insertBenchmarkProjectedNode(transaction, seedNode); err != nil {
			return err
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit benchmark fixture projected node sync: %w", err)
	}
	return nil
}

func (store *continuitySQLiteStore) insertBenchmarkProjectedNode(transaction *sql.Tx, seedNode BenchmarkProjectedNodeSeed) error {
	nodeID := strings.TrimSpace(seedNode.NodeID)
	if nodeID == "" {
		return fmt.Errorf("benchmark projected node id is required")
	}
	createdAtUTC := strings.TrimSpace(seedNode.CreatedAtUTC)
	if createdAtUTC == "" {
		createdAtUTC = "2026-01-01T00:00:00Z"
	}
	scope := strings.TrimSpace(seedNode.Scope)
	if scope == "" {
		scope = memoryScopeGlobal
	}
	nodeKind := strings.TrimSpace(seedNode.NodeKind)
	if nodeKind == "" {
		nodeKind = sqliteNodeKindBenchmarkFixtureStep
	}
	state := strings.TrimSpace(seedNode.State)
	if state == "" {
		state = "active"
	}
	hintText := strings.TrimSpace(seedNode.HintText)
	hintID := "hint_" + nodeID
	tclCoreJSONBytes, err := json.Marshal(map[string]interface{}{
		"object":      "MEM",
		"source_kind": "memorybench_fixture",
		"node_kind":   nodeKind,
	})
	if err != nil {
		return fmt.Errorf("marshal benchmark projected node tcl core: %w", err)
	}
	if _, err := transaction.Exec(
		`INSERT INTO memory_nodes(
			node_id, created_at_utc, scope, node_kind, anchor_version, anchor_key, pattern_family_id,
			certainty_score, epistemic_flavor, state, current_hint_id, provenance_event_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nodeID,
		createdAtUTC,
		scope,
		nodeKind,
		nil,
		nil,
		nullIfEmpty(seedNode.FamilySignature),
		0,
		"benchmark_fixture",
		state,
		hintID,
		nullIfEmpty(seedNode.ProvenanceEvent),
	); err != nil {
		return fmt.Errorf("insert benchmark projected node %q: %w", nodeID, err)
	}
	if _, err := transaction.Exec(
		`INSERT INTO memory_hints(hint_id, node_id, hint_kind, hint_text, byte_count, created_at_utc)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		hintID,
		nodeID,
		sqliteHintKindExactValue,
		hintText,
		len([]byte(hintText)),
		createdAtUTC,
	); err != nil {
		return fmt.Errorf("insert benchmark projected node hint %q: %w", hintID, err)
	}
	if _, err := transaction.Exec(
		`INSERT INTO semantic_projections(
			node_id, tcl_core_json, exact_signature, family_signature, risk_motifs_json, confidence
		) VALUES (?, ?, ?, ?, ?, ?)`,
		nodeID,
		string(tclCoreJSONBytes),
		nullIfEmpty(seedNode.ExactSignature),
		nullIfEmpty(seedNode.FamilySignature),
		"[]",
		nil,
	); err != nil {
		return fmt.Errorf("insert benchmark projected node semantic projection %q: %w", nodeID, err)
	}
	return nil
}

func nullIfEmpty(rawValue string) interface{} {
	trimmedValue := strings.TrimSpace(rawValue)
	if trimmedValue == "" {
		return nil
	}
	return trimmedValue
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
