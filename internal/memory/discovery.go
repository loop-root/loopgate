package memory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

var ErrDiscoveryInvalidRequest = errors.New("invalid discovery request")

type DiscoveryRequest struct {
	Scope    string
	Query    string
	MaxItems int
}

type DiscoveryResponse struct {
	Scope string
	Query string
	Items []DiscoveryItem
}

type DiscoveryItem struct {
	KeyID        string
	SessionID    string
	Scope        string
	StartedAtUTC string
	EndedAtUTC   string
	TurnCount    int
	Tags         []string
	MatchCount   int
}

func DiscoverResonateKeys(paths RecallPaths, request DiscoveryRequest) (DiscoveryResponse, error) {
	validatedRequest, queryTags, err := validateDiscoveryRequest(request)
	if err != nil {
		return DiscoveryResponse{}, err
	}

	keyFilenames, err := os.ReadDir(paths.KeysPath)
	if err != nil {
		if os.IsNotExist(err) {
			return DiscoveryResponse{Scope: validatedRequest.Scope, Query: validatedRequest.Query}, nil
		}
		return DiscoveryResponse{}, err
	}

	discoveredItems := make([]DiscoveryItem, 0, len(keyFilenames))
	for _, keyFilename := range keyFilenames {
		if keyFilename.IsDir() {
			continue
		}
		keyPath := filepath.Join(paths.KeysPath, keyFilename.Name())
		keyDocument, err := loadResonateKeyDocument(keyPath)
		if err != nil {
			return DiscoveryResponse{}, fmt.Errorf("load resonate key %s: %w", keyFilename.Name(), err)
		}
		if keyDocument.Scope != validatedRequest.Scope {
			continue
		}
		matchCount := countDiscoveryMatches(queryTags, keyDocument)
		if matchCount == 0 {
			continue
		}
		discoveredItems = append(discoveredItems, DiscoveryItem{
			KeyID:        keyDocument.ID,
			SessionID:    keyDocument.SessionID,
			Scope:        keyDocument.Scope,
			StartedAtUTC: keyDocument.StartedAtUTC,
			EndedAtUTC:   keyDocument.EndedAtUTC,
			TurnCount:    keyDocument.TurnCount,
			Tags:         append([]string(nil), keyDocument.Tags...),
			MatchCount:   matchCount,
		})
	}

	sort.Slice(discoveredItems, func(leftIndex int, rightIndex int) bool {
		leftItem := discoveredItems[leftIndex]
		rightItem := discoveredItems[rightIndex]
		switch {
		case leftItem.MatchCount != rightItem.MatchCount:
			return leftItem.MatchCount > rightItem.MatchCount
		case leftItem.EndedAtUTC != rightItem.EndedAtUTC:
			return leftItem.EndedAtUTC > rightItem.EndedAtUTC
		default:
			return leftItem.KeyID < rightItem.KeyID
		}
	})
	if len(discoveredItems) > validatedRequest.MaxItems {
		discoveredItems = append([]DiscoveryItem(nil), discoveredItems[:validatedRequest.MaxItems]...)
	}
	return DiscoveryResponse{
		Scope: validatedRequest.Scope,
		Query: validatedRequest.Query,
		Items: discoveredItems,
	}, nil
}

func FormatDiscoveryResponse(discoveryResponse DiscoveryResponse) string {
	if len(discoveryResponse.Items) == 0 {
		return fmt.Sprintf("memory discovery: none\nscope: %s\nquery: %s", discoveryResponse.Scope, discoveryResponse.Query)
	}

	lines := []string{
		fmt.Sprintf("memory discovery results for: %s", discoveryResponse.Query),
		fmt.Sprintf("scope: %s", discoveryResponse.Scope),
	}
	for _, discoveryItem := range discoveryResponse.Items {
		lines = append(lines,
			fmt.Sprintf("key_id: %s", discoveryItem.KeyID),
			fmt.Sprintf("session_id: %s", discoveryItem.SessionID),
			fmt.Sprintf("ended_at_utc: %s", discoveryItem.EndedAtUTC),
			fmt.Sprintf("turns: %d", discoveryItem.TurnCount),
			fmt.Sprintf("match_count: %d", discoveryItem.MatchCount),
			fmt.Sprintf("tags: %s", formatMemoryList(discoveryItem.Tags, "none")),
			"",
		)
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

func validateDiscoveryRequest(rawRequest DiscoveryRequest) (DiscoveryRequest, []string, error) {
	validatedRequest := DiscoveryRequest{
		Scope: defaultMemoryScope(rawRequest.Scope),
		Query: strings.TrimSpace(rawRequest.Query),
	}
	if validatedRequest.Scope != MemoryScopeGlobal {
		return DiscoveryRequest{}, nil, fmt.Errorf("%w: unsupported discovery scope %q", ErrDiscoveryInvalidRequest, rawRequest.Scope)
	}
	if validatedRequest.Query == "" {
		return DiscoveryRequest{}, nil, fmt.Errorf("%w: query is required", ErrDiscoveryInvalidRequest)
	}
	validatedRequest.MaxItems = rawRequest.MaxItems
	if validatedRequest.MaxItems == 0 {
		validatedRequest.MaxItems = DefaultDiscoveryMaxItems
	}
	if validatedRequest.MaxItems < 1 || validatedRequest.MaxItems > MaxDiscoveryItems {
		return DiscoveryRequest{}, nil, fmt.Errorf("%w: max_items must be between 1 and %d", ErrDiscoveryInvalidRequest, MaxDiscoveryItems)
	}

	queryTags := tokenizeContinuityText(validatedRequest.Query)
	if len(queryTags) == 0 {
		return DiscoveryRequest{}, nil, fmt.Errorf("%w: query must contain at least one meaningful tag", ErrDiscoveryInvalidRequest)
	}
	return validatedRequest, queryTags, nil
}

func countDiscoveryMatches(queryTags []string, keyDocument resonateKeyDocument) int {
	documentTagSet := make(map[string]struct{}, len(keyDocument.Tags)+4)
	for _, keyTag := range keyDocument.Tags {
		documentTagSet[keyTag] = struct{}{}
	}
	for _, keyTag := range tokenizeContinuityText(keyDocument.ID + " " + keyDocument.SessionID) {
		documentTagSet[keyTag] = struct{}{}
	}

	matchCount := 0
	for _, queryTag := range queryTags {
		if _, found := documentTagSet[queryTag]; found {
			matchCount++
		}
	}
	return matchCount
}

func recordContinuityTags(discoveredTags map[string]struct{}, rawTexts ...string) {
	for _, rawText := range rawTexts {
		for _, continuityTag := range tokenizeContinuityText(rawText) {
			discoveredTags[continuityTag] = struct{}{}
		}
	}
}

func normalizeContinuityTags(rawTags []string) []string {
	tagSet := make(map[string]struct{}, len(rawTags))
	for _, rawTag := range rawTags {
		for _, normalizedTag := range tokenizeContinuityText(rawTag) {
			tagSet[normalizedTag] = struct{}{}
		}
	}

	normalizedTags := make([]string, 0, len(tagSet))
	for normalizedTag := range tagSet {
		normalizedTags = append(normalizedTags, normalizedTag)
	}
	sort.Strings(normalizedTags)
	return normalizedTags
}

func tokenizeContinuityText(rawText string) []string {
	normalizedText := strings.ToLower(strings.TrimSpace(rawText))
	if normalizedText == "" {
		return nil
	}

	tokenSet := map[string]struct{}{}
	tokenFields := strings.FieldsFunc(normalizedText, func(currentRune rune) bool {
		return !unicode.IsLetter(currentRune) && !unicode.IsNumber(currentRune)
	})
	for _, tokenField := range tokenFields {
		if len(tokenField) < 3 || len(tokenField) > 32 {
			continue
		}
		if isAllDigits(tokenField) {
			continue
		}
		tokenSet[tokenField] = struct{}{}
	}

	normalizedTokens := make([]string, 0, len(tokenSet))
	for normalizedToken := range tokenSet {
		normalizedTokens = append(normalizedTokens, normalizedToken)
	}
	sort.Strings(normalizedTokens)
	return normalizedTokens
}

func isAllDigits(rawText string) bool {
	if rawText == "" {
		return false
	}
	for _, currentRune := range rawText {
		if !unicode.IsDigit(currentRune) {
			return false
		}
	}
	return true
}
