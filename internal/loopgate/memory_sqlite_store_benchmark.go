package loopgate

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

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
