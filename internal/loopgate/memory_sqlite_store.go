package loopgate

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

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
