package relationhints

import "strings"

// Candidate is the minimal shared shape both the benchmark harness and the
// runtime need for relation-hint reranking. Keeping it small avoids coupling
// the scorer to either package's richer retrieval structs.
type Candidate struct {
	StableID   string
	Text       string
	MatchCount int
}

const (
	defaultFinalEvidenceItems = 2
	minSearchPoolSize         = 12
	maxSearchPoolSize         = 24
)

var stopTokens = map[string]struct{}{
	"a": {}, "action": {}, "an": {}, "and": {}, "are": {}, "as": {}, "at": {}, "be": {}, "by": {}, "current": {},
	"do": {}, "does": {}, "during": {}, "find": {}, "follow": {}, "for": {}, "from": {}, "how": {}, "in": {},
	"instead": {}, "into": {}, "is": {}, "it": {}, "keep": {}, "next": {}, "note": {}, "of": {}, "on": {},
	"or": {}, "our": {}, "resume": {}, "so": {}, "state": {}, "step": {}, "stays": {}, "still": {}, "task": {},
	"that": {}, "the": {}, "their": {}, "them": {}, "then": {}, "this": {}, "through": {}, "to": {}, "up": {},
	"what": {}, "why": {}, "while": {}, "with": {}, "work": {},
}

// BuildLookupQuery carries only the already-selected current state into the
// evidence query. One continuity anchor should expand into one bounded lookup,
// not a graph flood.
func BuildLookupQuery(rawQuery string, relatedStateHints []string) string {
	trimmedQuery := strings.TrimSpace(rawQuery)
	trimmedRelatedStateHints := make([]string, 0, len(relatedStateHints))
	for _, relatedStateHint := range relatedStateHints {
		trimmedRelatedStateHint := strings.TrimSpace(relatedStateHint)
		if trimmedRelatedStateHint == "" {
			continue
		}
		trimmedRelatedStateHints = append(trimmedRelatedStateHints, trimmedRelatedStateHint)
	}
	if len(trimmedRelatedStateHints) == 0 {
		return trimmedQuery
	}
	return trimmedQuery + "\nRelated current state:\n" + strings.Join(trimmedRelatedStateHints, "\n")
}

// EvidenceSearchPoolSize widens the candidate pool just enough for reranking to
// see sibling rationale notes inside the same thread, while still keeping the
// retrieval envelope bounded.
func EvidenceSearchPoolSize(finalEvidenceItems int) int {
	if finalEvidenceItems <= 0 {
		finalEvidenceItems = defaultFinalEvidenceItems
	}
	searchPoolSize := finalEvidenceItems * 6
	if searchPoolSize < minSearchPoolSize {
		searchPoolSize = minSearchPoolSize
	}
	if searchPoolSize > maxSearchPoolSize {
		searchPoolSize = maxSearchPoolSize
	}
	return searchPoolSize
}

// RerankCandidates prefers evidence that stays inside the same local relation
// neighborhood and contributes a distinct supporting concept. Phrase coverage is
// scored before raw token overlap because token overlap alone kept selecting the
// wrong sibling note inside the right design thread.
func RerankCandidates(rawCandidates []Candidate, evidenceQuery string, relatedStateHints []string, maxItems int) []Candidate {
	if len(rawCandidates) == 0 {
		return nil
	}
	scoreTarget := buildScoreTarget(append([]string{evidenceQuery}, relatedStateHints...)...)
	remainingCandidates := append([]Candidate(nil), rawCandidates...)
	rerankedCandidates := make([]Candidate, 0, len(remainingCandidates))
	coveredPhrases := map[string]struct{}{}
	coveredTokens := map[string]struct{}{}
	for len(remainingCandidates) > 0 {
		bestIndex := 0
		bestScore := candidateScore{
			marginalPhraseCoverage: -1,
			totalPhraseOverlap:     -1,
			marginalTokenCoverage:  -1,
			totalTokenOverlap:      -1,
			matchCount:             -1,
			stableID:               "",
		}
		for currentIndex, currentCandidate := range remainingCandidates {
			currentScore := scoreCandidate(currentCandidate, scoreTarget, coveredPhrases, coveredTokens)
			if currentScore.betterThan(bestScore) {
				bestIndex = currentIndex
				bestScore = currentScore
			}
		}
		bestCandidate := remainingCandidates[bestIndex]
		rerankedCandidates = append(rerankedCandidates, bestCandidate)
		for coveredPhrase := range phraseSetFromText(bestCandidate.Text) {
			if _, relationPhrase := scoreTarget.phrases[coveredPhrase]; relationPhrase {
				coveredPhrases[coveredPhrase] = struct{}{}
			}
		}
		for coveredToken := range tokenSetFromText(bestCandidate.Text) {
			if _, relationToken := scoreTarget.tokens[coveredToken]; relationToken {
				coveredTokens[coveredToken] = struct{}{}
			}
		}
		remainingCandidates = append(remainingCandidates[:bestIndex], remainingCandidates[bestIndex+1:]...)
		if maxItems > 0 && len(rerankedCandidates) >= maxItems {
			break
		}
	}
	return rerankedCandidates
}

type scoreTarget struct {
	tokens  map[string]struct{}
	phrases map[string]struct{}
}

type candidateScore struct {
	marginalPhraseCoverage int
	totalPhraseOverlap     int
	marginalTokenCoverage  int
	totalTokenOverlap      int
	matchCount             int
	stableID               string
}

func buildScoreTarget(rawTexts ...string) scoreTarget {
	scoreTarget := scoreTarget{
		tokens:  map[string]struct{}{},
		phrases: map[string]struct{}{},
	}
	for _, rawText := range rawTexts {
		for relationToken := range tokenSetFromText(rawText) {
			scoreTarget.tokens[relationToken] = struct{}{}
		}
		for relationPhrase := range phraseSetFromText(rawText) {
			scoreTarget.phrases[relationPhrase] = struct{}{}
		}
	}
	return scoreTarget
}

func scoreCandidate(candidate Candidate, scoreTarget scoreTarget, coveredPhrases map[string]struct{}, coveredTokens map[string]struct{}) candidateScore {
	candidateTokens := tokenSetFromText(candidate.Text)
	candidatePhrases := phraseSetFromText(candidate.Text)
	return candidateScore{
		marginalPhraseCoverage: marginalOverlap(candidatePhrases, scoreTarget.phrases, coveredPhrases),
		totalPhraseOverlap:     overlapCount(candidatePhrases, scoreTarget.phrases),
		marginalTokenCoverage:  marginalOverlap(candidateTokens, scoreTarget.tokens, coveredTokens),
		totalTokenOverlap:      overlapCount(candidateTokens, scoreTarget.tokens),
		matchCount:             candidate.MatchCount,
		stableID:               strings.TrimSpace(candidate.StableID),
	}
}

func (score candidateScore) betterThan(other candidateScore) bool {
	switch {
	case score.marginalPhraseCoverage != other.marginalPhraseCoverage:
		return score.marginalPhraseCoverage > other.marginalPhraseCoverage
	case score.totalPhraseOverlap != other.totalPhraseOverlap:
		return score.totalPhraseOverlap > other.totalPhraseOverlap
	case score.marginalTokenCoverage != other.marginalTokenCoverage:
		return score.marginalTokenCoverage > other.marginalTokenCoverage
	case score.totalTokenOverlap != other.totalTokenOverlap:
		return score.totalTokenOverlap > other.totalTokenOverlap
	case score.matchCount != other.matchCount:
		return score.matchCount > other.matchCount
	default:
		return score.stableID < other.stableID
	}
}

func overlapCount(candidateSet map[string]struct{}, targetSet map[string]struct{}) int {
	if len(candidateSet) == 0 || len(targetSet) == 0 {
		return 0
	}
	overlapCount := 0
	for candidateValue := range candidateSet {
		if _, found := targetSet[candidateValue]; found {
			overlapCount++
		}
	}
	return overlapCount
}

func marginalOverlap(candidateSet map[string]struct{}, targetSet map[string]struct{}, coveredSet map[string]struct{}) int {
	if len(candidateSet) == 0 || len(targetSet) == 0 {
		return 0
	}
	overlapCount := 0
	for candidateValue := range candidateSet {
		if _, found := targetSet[candidateValue]; !found {
			continue
		}
		if _, alreadyCovered := coveredSet[candidateValue]; alreadyCovered {
			continue
		}
		overlapCount++
	}
	return overlapCount
}

func tokenSetFromText(rawText string) map[string]struct{} {
	normalizedTokens := tokenize(rawText)
	tokenSet := make(map[string]struct{}, len(normalizedTokens))
	for _, normalizedToken := range normalizedTokens {
		if _, ignoredToken := stopTokens[normalizedToken]; ignoredToken {
			continue
		}
		tokenSet[normalizedToken] = struct{}{}
	}
	return tokenSet
}

func phraseSetFromText(rawText string) map[string]struct{} {
	normalizedTokens := orderedTokens(rawText)
	filteredTokens := make([]string, 0, len(normalizedTokens))
	for _, normalizedToken := range normalizedTokens {
		if _, ignoredToken := stopTokens[normalizedToken]; ignoredToken {
			continue
		}
		filteredTokens = append(filteredTokens, normalizedToken)
	}
	phraseSet := map[string]struct{}{}
	for tokenIndex := 0; tokenIndex+1 < len(filteredTokens); tokenIndex++ {
		phraseSet[filteredTokens[tokenIndex]+" "+filteredTokens[tokenIndex+1]] = struct{}{}
	}
	return phraseSet
}

func tokenize(rawText string) []string {
	orderedTokens := orderedTokens(rawText)
	tokenSet := map[string]struct{}{}
	for _, orderedToken := range orderedTokens {
		tokenSet[orderedToken] = struct{}{}
	}
	normalizedTokens := make([]string, 0, len(tokenSet))
	for normalizedToken := range tokenSet {
		normalizedTokens = append(normalizedTokens, normalizedToken)
	}
	return normalizedTokens
}

func orderedTokens(rawText string) []string {
	normalizedText := strings.ToLower(strings.TrimSpace(rawText))
	if normalizedText == "" {
		return nil
	}
	currentToken := strings.Builder{}
	normalizedTokens := make([]string, 0, 8)
	flushToken := func() {
		tokenValue := currentToken.String()
		currentToken.Reset()
		if len(tokenValue) < 4 || len(tokenValue) > 32 {
			return
		}
		if isAllDigits(tokenValue) {
			return
		}
		normalizedTokens = append(normalizedTokens, tokenValue)
	}
	for _, currentRune := range normalizedText {
		switch {
		case currentRune >= 'a' && currentRune <= 'z':
			currentToken.WriteRune(currentRune)
		case currentRune >= '0' && currentRune <= '9':
			currentToken.WriteRune(currentRune)
		default:
			flushToken()
		}
	}
	flushToken()
	return normalizedTokens
}

func isAllDigits(rawText string) bool {
	if rawText == "" {
		return false
	}
	for _, currentRune := range rawText {
		if currentRune < '0' || currentRune > '9' {
			return false
		}
	}
	return true
}
