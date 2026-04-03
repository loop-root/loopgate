package shell

import (
	"strings"
	"testing"

	"morph/internal/config"
)

type fakeCompletionOperation struct {
	candidates [][]rune
	exited     bool
	inMode     bool
}

func (operation *fakeCompletionOperation) EnterCompleteMode(offset int, candidate [][]rune) {
	operation.candidates = candidate
	operation.inMode = true
}

func (operation *fakeCompletionOperation) ExitCompleteMode(revent bool) {
	operation.exited = true
	operation.inMode = false
}

func (operation *fakeCompletionOperation) IsInCompleteMode() bool {
	return operation.inMode
}

func TestMaybeTriggerSlashSuggestions_ShowsCommandCandidates(t *testing.T) {
	completer := NewCompleter(t.TempDir(), config.Policy{})
	operation := &fakeCompletionOperation{}

	MaybeTriggerSlashSuggestions(operation, completer, []rune(""), 0, '/')

	if len(operation.candidates) == 0 {
		t.Fatal("expected slash suggestions to enter completion mode")
	}

	foundSetup := false
	for _, candidate := range operation.candidates {
		if string(candidate) == "setup" {
			foundSetup = true
			break
		}
	}
	if !foundSetup {
		t.Fatalf("expected /setup suggestion, got %#v", operation.candidates)
	}
}

func TestMaybeTriggerSlashSuggestions_UpdatesCommandCandidatesAsYouType(t *testing.T) {
	completer := NewCompleter(t.TempDir(), config.Policy{})
	operation := &fakeCompletionOperation{}

	MaybeTriggerSlashSuggestions(operation, completer, []rune("/"), 1, 'w')

	if len(operation.candidates) == 0 {
		t.Fatal("expected narrowed slash suggestions")
	}

	candidateStrings := make([]string, 0, len(operation.candidates))
	for _, candidate := range operation.candidates {
		candidateStrings = append(candidateStrings, string(candidate))
	}
	if !strings.Contains(strings.Join(candidateStrings, ","), "rite") {
		t.Fatalf("expected /write suffix suggestion, got %v", candidateStrings)
	}
}

func TestMaybeTriggerSlashSuggestions_ExitsAfterCommandTokenEnds(t *testing.T) {
	completer := NewCompleter(t.TempDir(), config.Policy{})
	operation := &fakeCompletionOperation{inMode: true}

	MaybeTriggerSlashSuggestions(operation, completer, []rune("/help"), 5, ' ')

	if !operation.exited {
		t.Fatal("expected completion mode to exit after command token was completed")
	}
}
