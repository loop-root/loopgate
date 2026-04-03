package memory

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var (
	ErrRecallInvalidRequest = errors.New("invalid recall request")
	ErrRecallKeyNotFound    = errors.New("resonate key not found")
)

type RecallPaths struct {
	KeysPath string
}

type RecallRequest struct {
	Scope         string
	Reason        string
	MaxItems      int
	MaxTokens     int
	RequestedKeys []string
}

type RecallResponse struct {
	Scope            string
	MaxItems         int
	MaxTokens        int
	ApproxTokenCount int
	Items            []RecallItem
}

type RecallItem struct {
	KeyID           string
	SessionID       string
	Scope           string
	StartedAtUTC    string
	EndedAtUTC      string
	TurnCount       int
	Tags            []string
	EpistemicFlavor string
}

type resonateKeyDocument struct {
	ID           string   `json:"id"`
	SessionID    string   `json:"session_id"`
	Scope        string   `json:"scope,omitempty"`
	StartedAtUTC string   `json:"started_at_utc"`
	EndedAtUTC   string   `json:"ended_at_utc"`
	TurnCount    int      `json:"turns"`
	Tags         []string `json:"tags,omitempty"`
}

func RecallByKeys(paths RecallPaths, request RecallRequest) (RecallResponse, error) {
	validatedRequest, err := validateRecallRequest(request)
	if err != nil {
		return RecallResponse{}, err
	}

	keyFilenames, err := os.ReadDir(paths.KeysPath)
	if err != nil {
		if os.IsNotExist(err) {
			return RecallResponse{}, fmt.Errorf("%w: %s", ErrRecallKeyNotFound, strings.Join(validatedRequest.RequestedKeys, ", "))
		}
		return RecallResponse{}, err
	}

	requestedKeySet := make(map[string]struct{}, len(validatedRequest.RequestedKeys))
	for _, requestedKeyID := range validatedRequest.RequestedKeys {
		requestedKeySet[requestedKeyID] = struct{}{}
	}

	recalledItemsByKey := make(map[string]RecallItem, len(validatedRequest.RequestedKeys))
	for _, keyFilename := range keyFilenames {
		if keyFilename.IsDir() {
			continue
		}
		keyPath := filepath.Join(paths.KeysPath, keyFilename.Name())
		loadedKeyDocument, err := loadResonateKeyDocument(keyPath)
		if err != nil {
			return RecallResponse{}, fmt.Errorf("load resonate key %s: %w", keyFilename.Name(), err)
		}
		if loadedKeyDocument.Scope != validatedRequest.Scope {
			continue
		}
		if _, requested := requestedKeySet[loadedKeyDocument.ID]; !requested {
			continue
		}
		recalledItemsByKey[loadedKeyDocument.ID] = RecallItem{
			KeyID:           loadedKeyDocument.ID,
			SessionID:       loadedKeyDocument.SessionID,
			Scope:           loadedKeyDocument.Scope,
			StartedAtUTC:    loadedKeyDocument.StartedAtUTC,
			EndedAtUTC:      loadedKeyDocument.EndedAtUTC,
			TurnCount:       loadedKeyDocument.TurnCount,
			Tags:            append([]string(nil), loadedKeyDocument.Tags...),
			EpistemicFlavor: EpistemicFlavorRemembered,
		}
	}

	recalledItems := make([]RecallItem, 0, len(validatedRequest.RequestedKeys))
	approxTokenCount := 0
	for _, requestedKeyID := range validatedRequest.RequestedKeys {
		recalledItem, found := recalledItemsByKey[requestedKeyID]
		if !found {
			return RecallResponse{}, fmt.Errorf("%w: %s", ErrRecallKeyNotFound, requestedKeyID)
		}
		approxTokenCount += approximateRecallItemTokens(recalledItem)
		recalledItems = append(recalledItems, recalledItem)
	}
	if approxTokenCount > validatedRequest.MaxTokens {
		return RecallResponse{}, fmt.Errorf("%w: requested keys exceed max_tokens", ErrRecallInvalidRequest)
	}

	return RecallResponse{
		Scope:            validatedRequest.Scope,
		MaxItems:         validatedRequest.MaxItems,
		MaxTokens:        validatedRequest.MaxTokens,
		ApproxTokenCount: approxTokenCount,
		Items:            recalledItems,
	}, nil
}

func FormatRecallResponse(recallResponse RecallResponse) string {
	if len(recallResponse.Items) == 0 {
		return "remembered continuity: none"
	}

	lines := []string{
		"remembered continuity follows. these items are historical memory, not freshly checked state.",
		fmt.Sprintf("scope: %s", recallResponse.Scope),
		fmt.Sprintf("approx_token_count: %d", recallResponse.ApproxTokenCount),
		fmt.Sprintf("max_tokens: %d", recallResponse.MaxTokens),
	}
	for _, recalledItem := range recallResponse.Items {
		lines = append(lines,
			fmt.Sprintf("key_id: %s", recalledItem.KeyID),
			fmt.Sprintf("session_id: %s", recalledItem.SessionID),
			fmt.Sprintf("scope: %s", recalledItem.Scope),
			fmt.Sprintf("started_at_utc: %s", recalledItem.StartedAtUTC),
			fmt.Sprintf("ended_at_utc: %s", recalledItem.EndedAtUTC),
			fmt.Sprintf("turns: %d", recalledItem.TurnCount),
			fmt.Sprintf("tags: %s", formatMemoryList(recalledItem.Tags, "none")),
			fmt.Sprintf("epistemic_flavor: %s", recalledItem.EpistemicFlavor),
			"",
		)
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

func validateRecallRequest(rawRequest RecallRequest) (RecallRequest, error) {
	validatedRequest := RecallRequest{
		Scope: defaultMemoryScope(rawRequest.Scope),
	}
	if validatedRequest.Scope != MemoryScopeGlobal {
		return RecallRequest{}, fmt.Errorf("%w: unsupported recall scope %q", ErrRecallInvalidRequest, rawRequest.Scope)
	}

	validatedRequest.Reason = strings.TrimSpace(rawRequest.Reason)
	validatedRequest.MaxItems = rawRequest.MaxItems
	if validatedRequest.MaxItems == 0 {
		validatedRequest.MaxItems = DefaultRecallMaxItems
	}
	if validatedRequest.MaxItems < 1 || validatedRequest.MaxItems > MaxRecallItems {
		return RecallRequest{}, fmt.Errorf("%w: max_items must be between 1 and %d", ErrRecallInvalidRequest, MaxRecallItems)
	}
	validatedRequest.MaxTokens = rawRequest.MaxTokens
	if validatedRequest.MaxTokens == 0 {
		validatedRequest.MaxTokens = DefaultRecallMaxTokens
	}
	if validatedRequest.MaxTokens < 1 || validatedRequest.MaxTokens > MaxRecallMaxTokens {
		return RecallRequest{}, fmt.Errorf("%w: max_tokens must be between 1 and %d", ErrRecallInvalidRequest, MaxRecallMaxTokens)
	}

	requestedKeySet := map[string]struct{}{}
	for _, rawKeyID := range rawRequest.RequestedKeys {
		validatedKeyID := strings.TrimSpace(rawKeyID)
		if validatedKeyID == "" {
			continue
		}
		if _, duplicate := requestedKeySet[validatedKeyID]; duplicate {
			return RecallRequest{}, fmt.Errorf("%w: duplicate requested key %q", ErrRecallInvalidRequest, validatedKeyID)
		}
		requestedKeySet[validatedKeyID] = struct{}{}
		validatedRequest.RequestedKeys = append(validatedRequest.RequestedKeys, validatedKeyID)
	}
	if len(validatedRequest.RequestedKeys) == 0 {
		return RecallRequest{}, fmt.Errorf("%w: at least one key is required", ErrRecallInvalidRequest)
	}
	if len(validatedRequest.RequestedKeys) > validatedRequest.MaxItems {
		return RecallRequest{}, fmt.Errorf("%w: requested keys exceed max_items", ErrRecallInvalidRequest)
	}
	sort.Strings(validatedRequest.RequestedKeys)
	return validatedRequest, nil
}

func loadResonateKeyDocument(keyPath string) (resonateKeyDocument, error) {
	rawKeyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return resonateKeyDocument{}, err
	}

	var parsedKeyDocument resonateKeyDocument
	keyDecoder := json.NewDecoder(bytes.NewReader(rawKeyBytes))
	keyDecoder.DisallowUnknownFields()
	if err := keyDecoder.Decode(&parsedKeyDocument); err != nil {
		return resonateKeyDocument{}, err
	}
	if strings.TrimSpace(parsedKeyDocument.ID) == "" {
		return resonateKeyDocument{}, fmt.Errorf("missing resonate key id")
	}
	if strings.TrimSpace(parsedKeyDocument.SessionID) == "" {
		return resonateKeyDocument{}, fmt.Errorf("missing session_id")
	}
	parsedKeyDocument.Scope = defaultMemoryScope(parsedKeyDocument.Scope)
	parsedKeyDocument.Tags = normalizeContinuityTags(parsedKeyDocument.Tags)
	return parsedKeyDocument, nil
}

func defaultMemoryScope(rawScope string) string {
	normalizedScope := strings.TrimSpace(rawScope)
	if normalizedScope == "" {
		return MemoryScopeGlobal
	}
	return normalizedScope
}

func formatMemoryList(values []string, emptyValue string) string {
	if len(values) == 0 {
		return emptyValue
	}
	return strings.Join(values, ", ")
}
