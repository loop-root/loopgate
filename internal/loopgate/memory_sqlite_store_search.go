package loopgate

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type continuitySQLiteSearchDebugNode struct {
	NodeID                     string
	CreatedAtUTC               string
	Scope                      string
	NodeKind                   string
	SourceKind                 string
	CanonicalKey               string
	AnchorTupleKey             string
	State                      string
	HintText                   string
	ExactSignature             string
	FamilySignature            string
	SearchOnlyText             string
	ProvenanceEvent            string
	SearchableTokens           []string
	MatchCount                 int
	RankBeforeSlotPreference   int
	RankBeforeTruncation       int
	FinalKeptRank              int
	SlotPreferenceTargetAnchor string
	SlotPreferenceApplied      bool
	AdmissionResult            string
	Returned                   bool
}

type continuitySQLiteSearchDebugReport struct {
	Scope                      string
	Query                      string
	QueryTokens                []string
	SlotPreferenceTargetAnchor string
	SlotPreferenceApplied      bool
	Nodes                      []continuitySQLiteSearchDebugNode
}

func (store *continuitySQLiteStore) listProjectedNodes(scope string) ([]continuitySQLiteProjectedNode, error) {
	queryRows, err := store.database.Query(`
		SELECT
			memory_nodes.node_id,
			memory_nodes.created_at_utc,
			memory_nodes.scope,
			memory_nodes.node_kind,
			COALESCE(memory_nodes.anchor_version, ''),
			COALESCE(memory_nodes.anchor_key, ''),
			memory_nodes.state,
			COALESCE(memory_hints.hint_text, ''),
			COALESCE(semantic_projections.exact_signature, ''),
			COALESCE(semantic_projections.family_signature, ''),
			COALESCE(semantic_projections.tcl_core_json, ''),
			memory_nodes.provenance_event_id
		FROM memory_nodes
		LEFT JOIN memory_hints ON memory_hints.hint_id = memory_nodes.current_hint_id
		LEFT JOIN semantic_projections ON semantic_projections.node_id = memory_nodes.node_id
		WHERE memory_nodes.scope = ?
		ORDER BY memory_nodes.created_at_utc ASC, memory_nodes.node_id ASC
	`, scope)
	if err != nil {
		return nil, fmt.Errorf("query projected nodes: %w", err)
	}
	defer queryRows.Close()

	projectedNodes := make([]continuitySQLiteProjectedNode, 0)
	for queryRows.Next() {
		var projectedNode continuitySQLiteProjectedNode
		if err := queryRows.Scan(
			&projectedNode.NodeID,
			&projectedNode.CreatedAtUTC,
			&projectedNode.Scope,
			&projectedNode.NodeKind,
			&projectedNode.AnchorVersion,
			&projectedNode.AnchorKey,
			&projectedNode.State,
			&projectedNode.HintText,
			&projectedNode.ExactSignature,
			&projectedNode.FamilySignature,
			&projectedNode.TCLCoreJSON,
			&projectedNode.ProvenanceEvent,
		); err != nil {
			return nil, fmt.Errorf("scan projected node: %w", err)
		}
		projectedNodes = append(projectedNodes, projectedNode)
	}
	if err := queryRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate projected nodes: %w", err)
	}
	return projectedNodes, nil
}

func (store *continuitySQLiteStore) searchProjectedNodes(scope string, query string, maxItems int) ([]continuitySQLiteProjectedNode, error) {
	matchedNodes, _, err := store.searchProjectedNodesWithDebug(scope, query, maxItems)
	if err != nil {
		return nil, err
	}
	return matchedNodes, nil
}

func (store *continuitySQLiteStore) debugSearchProjectedNodes(scope string, query string, maxItems int) (continuitySQLiteSearchDebugReport, error) {
	_, debugReport, err := store.searchProjectedNodesWithDebug(scope, query, maxItems)
	if err != nil {
		return continuitySQLiteSearchDebugReport{}, err
	}
	return debugReport, nil
}

func (store *continuitySQLiteStore) searchProjectedNodesWithDebug(scope string, query string, maxItems int) ([]continuitySQLiteProjectedNode, continuitySQLiteSearchDebugReport, error) {
	if maxItems <= 0 {
		maxItems = 10
	}
	projectedNodes, err := store.listProjectedNodes(scope)
	if err != nil {
		return nil, continuitySQLiteSearchDebugReport{}, err
	}
	queryTokens := tokenizeLoopgateMemoryText(query)
	debugReport := continuitySQLiteSearchDebugReport{
		Scope:       scope,
		Query:       query,
		QueryTokens: append([]string(nil), queryTokens...),
		Nodes:       make([]continuitySQLiteSearchDebugNode, 0, len(projectedNodes)),
	}
	if len(queryTokens) == 0 {
		return []continuitySQLiteProjectedNode{}, debugReport, nil
	}

	matchedNodes := make([]continuitySQLiteProjectedNode, 0, len(projectedNodes))
	for _, projectedNode := range projectedNodes {
		projectedNodeMetadata := projectedNodeSearchMetadata(projectedNode)
		searchOnlyAdmissionText := projectedNodeSearchOnlyAdmissionText(projectedNode)
		searchableTokens := tokenizeLoopgateMemoryText(strings.Join([]string{
			projectedNode.HintText,
			projectedNode.NodeKind,
			projectedNode.ExactSignature,
			projectedNode.FamilySignature,
			searchOnlyAdmissionText,
		}, " "))
		debugNode := continuitySQLiteSearchDebugNode{
			NodeID:           projectedNode.NodeID,
			CreatedAtUTC:     projectedNode.CreatedAtUTC,
			Scope:            projectedNode.Scope,
			NodeKind:         projectedNode.NodeKind,
			SourceKind:       projectedNodeMetadata.SourceKind,
			CanonicalKey:     projectedNodeMetadata.FactKey,
			AnchorTupleKey:   anchorTupleKey(projectedNode.AnchorVersion, projectedNode.AnchorKey),
			State:            projectedNode.State,
			HintText:         projectedNode.HintText,
			ExactSignature:   projectedNode.ExactSignature,
			FamilySignature:  projectedNode.FamilySignature,
			SearchOnlyText:   searchOnlyAdmissionText,
			ProvenanceEvent:  projectedNode.ProvenanceEvent,
			SearchableTokens: append([]string(nil), searchableTokens...),
		}
		if projectedNode.State != "active" {
			debugNode.AdmissionResult = "filtered_non_active_state"
			debugReport.Nodes = append(debugReport.Nodes, debugNode)
			continue
		}
		matchCount := 0
		for _, queryToken := range queryTokens {
			for _, searchableToken := range searchableTokens {
				if searchableToken == queryToken {
					matchCount++
					break
				}
			}
		}
		if matchCount == 0 {
			debugNode.AdmissionResult = "filtered_no_query_overlap"
			debugReport.Nodes = append(debugReport.Nodes, debugNode)
			continue
		}
		projectedNode.MatchCount = matchCount
		debugNode.MatchCount = matchCount
		debugNode.AdmissionResult = "matched_query_overlap"
		debugReport.Nodes = append(debugReport.Nodes, debugNode)
		matchedNodes = append(matchedNodes, projectedNode)
	}

	sort.Slice(matchedNodes, func(leftIndex int, rightIndex int) bool {
		leftNode := matchedNodes[leftIndex]
		rightNode := matchedNodes[rightIndex]
		switch {
		case leftNode.MatchCount != rightNode.MatchCount:
			return leftNode.MatchCount > rightNode.MatchCount
		case leftNode.CreatedAtUTC != rightNode.CreatedAtUTC:
			return leftNode.CreatedAtUTC > rightNode.CreatedAtUTC
		default:
			return leftNode.NodeID < rightNode.NodeID
		}
	})
	preferenceTargetAnchorTupleKey := detectProjectedNodeSlotPreferenceTargetAnchor(query)
	debugReport.SlotPreferenceTargetAnchor = preferenceTargetAnchorTupleKey
	preferenceRankByNodeID := make(map[string]int, len(matchedNodes))
	for matchedNodeIndex, matchedNode := range matchedNodes {
		preferenceRankByNodeID[matchedNode.NodeID] = matchedNodeIndex + 1
	}
	debugReport.SlotPreferenceApplied = applyProjectedNodeSlotPreference(matchedNodes, preferenceTargetAnchorTupleKey)
	preTruncationMatchedNodes := append([]continuitySQLiteProjectedNode(nil), matchedNodes...)
	if len(matchedNodes) > maxItems {
		matchedNodes = append([]continuitySQLiteProjectedNode(nil), matchedNodes[:maxItems]...)
	}
	returnedNodeIDs := make(map[string]struct{}, len(matchedNodes))
	preTruncationRankByNodeID := make(map[string]int, len(preTruncationMatchedNodes))
	for matchedNodeIndex, matchedNode := range preTruncationMatchedNodes {
		preTruncationRankByNodeID[matchedNode.NodeID] = matchedNodeIndex + 1
	}
	for _, matchedNode := range matchedNodes {
		returnedNodeIDs[matchedNode.NodeID] = struct{}{}
	}
	for debugNodeIndex := range debugReport.Nodes {
		if prePreferenceRank, found := preferenceRankByNodeID[debugReport.Nodes[debugNodeIndex].NodeID]; found {
			debugReport.Nodes[debugNodeIndex].RankBeforeSlotPreference = prePreferenceRank
		}
		if preTruncationRank, found := preTruncationRankByNodeID[debugReport.Nodes[debugNodeIndex].NodeID]; found {
			debugReport.Nodes[debugNodeIndex].RankBeforeTruncation = preTruncationRank
		}
		debugReport.Nodes[debugNodeIndex].SlotPreferenceTargetAnchor = preferenceTargetAnchorTupleKey
		debugReport.Nodes[debugNodeIndex].SlotPreferenceApplied = debugReport.SlotPreferenceApplied
		if _, found := returnedNodeIDs[debugReport.Nodes[debugNodeIndex].NodeID]; found {
			debugReport.Nodes[debugNodeIndex].Returned = true
		}
	}
	for matchedNodeIndex, matchedNode := range matchedNodes {
		for debugNodeIndex := range debugReport.Nodes {
			if debugReport.Nodes[debugNodeIndex].NodeID == matchedNode.NodeID {
				debugReport.Nodes[debugNodeIndex].FinalKeptRank = matchedNodeIndex + 1
				break
			}
		}
	}
	return matchedNodes, debugReport, nil
}

func applyProjectedNodeSlotPreference(matchedNodes []continuitySQLiteProjectedNode, preferenceTargetAnchorTupleKey string) bool {
	if len(matchedNodes) < 2 || strings.TrimSpace(preferenceTargetAnchorTupleKey) == "" {
		return false
	}
	preferredNodeIndex := -1
	for matchedNodeIndex, matchedNode := range matchedNodes {
		if !isProjectedNodeSlotPreferenceCandidate(matchedNode, preferenceTargetAnchorTupleKey) {
			continue
		}
		preferredNodeIndex = matchedNodeIndex
		break
	}
	if preferredNodeIndex <= 0 {
		return false
	}
	preferredNode := matchedNodes[preferredNodeIndex]
	copy(matchedNodes[1:preferredNodeIndex+1], matchedNodes[0:preferredNodeIndex])
	matchedNodes[0] = preferredNode
	return true
}

func isProjectedNodeSlotPreferenceCandidate(projectedNode continuitySQLiteProjectedNode, preferenceTargetAnchorTupleKey string) bool {
	projectedNodeMetadata := projectedNodeSearchMetadata(projectedNode)
	if strings.TrimSpace(projectedNode.NodeKind) != sqliteNodeKindExplicitRememberedFact {
		return false
	}
	if strings.TrimSpace(projectedNodeMetadata.SourceKind) != explicitProfileFactSourceKind {
		return false
	}
	return anchorTupleKey(projectedNode.AnchorVersion, projectedNode.AnchorKey) == strings.TrimSpace(preferenceTargetAnchorTupleKey)
}

func detectProjectedNodeSlotPreferenceTargetAnchor(rawQuery string) string {
	queryTags := tokenizeLoopgateMemoryText(rawQuery)
	if len(queryTags) == 0 {
		return ""
	}
	queryTagSet := make(map[string]struct{}, len(queryTags))
	for _, queryTag := range queryTags {
		queryTagSet[queryTag] = struct{}{}
	}
	if _, hasSlotTag := queryTagSet["slot"]; !hasSlotTag {
		return ""
	}
	if isProjectedNodePreviewLabelTargetQuery(queryTagSet) {
		return ""
	}
	discoverPreferenceTarget := detectDiscoverSlotPreference(rawQuery)
	switch discoverPreferenceTarget {
	case "v1:usr_profile:settings:fact:timezone", "v1:usr_profile:settings:fact:locale":
		return discoverPreferenceTarget
	default:
		return ""
	}
}

func isProjectedNodePreviewLabelTargetQuery(queryTagSet map[string]struct{}) bool {
	if !containsAnyLoopgateMemoryTag(queryTagSet, "preview", "label", "card", "chip", "display") {
		return false
	}
	if containsAnyLoopgateMemoryTag(queryTagSet, "not", "ignore", "exclude", "without") {
		return false
	}
	return true
}

func projectedNodeSearchOnlyAdmissionText(projectedNode continuitySQLiteProjectedNode) string {
	if strings.TrimSpace(projectedNode.NodeKind) != sqliteNodeKindExplicitRememberedFact {
		return ""
	}
	return strings.TrimSpace(projectedNodeSearchMetadata(projectedNode).FactKey)
}

type continuitySQLiteProjectedNodeSearchMetadata struct {
	SourceKind string `json:"source_kind"`
	FactKey    string `json:"fact_key"`
}

func projectedNodeSearchMetadata(projectedNode continuitySQLiteProjectedNode) continuitySQLiteProjectedNodeSearchMetadata {
	if strings.TrimSpace(projectedNode.TCLCoreJSON) == "" {
		return continuitySQLiteProjectedNodeSearchMetadata{}
	}
	var projectedNodeMetadata continuitySQLiteProjectedNodeSearchMetadata
	if err := json.Unmarshal([]byte(projectedNode.TCLCoreJSON), &projectedNodeMetadata); err != nil {
		return continuitySQLiteProjectedNodeSearchMetadata{}
	}
	projectedNodeMetadata.SourceKind = strings.TrimSpace(projectedNodeMetadata.SourceKind)
	projectedNodeMetadata.FactKey = strings.TrimSpace(projectedNodeMetadata.FactKey)
	return projectedNodeMetadata
}
