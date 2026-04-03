package shell

import (
	"strings"
	"testing"
)

func TestIsHelpRequest(t *testing.T) {
	for _, arg := range []string{"help", "-help", "--help", "-h"} {
		if !isHelpRequest(arg) {
			t.Errorf("expected %q to be a help request", arg)
		}
	}
	for _, arg := range []string{"spawn", "status", "add", "", "helpp"} {
		if isHelpRequest(arg) {
			t.Errorf("expected %q to NOT be a help request", arg)
		}
	}
}

func TestLookupManPage_KnownCommands(t *testing.T) {
	knownCommands := []string{
		"/morphling", "/goal", "/todo", "/memory", "/sandbox",
		"/site", "/connections", "/model", "/quarantine", "/debug",
		"/write", "/ls", "/cat",
		"/help", "/man", "/exit", "/reset", "/pwd", "/setup",
		"/agent", "/persona", "/settings", "/network", "/config",
		"/tools", "/policy",
	}
	for _, cmd := range knownCommands {
		page, found := LookupManPage(cmd)
		if !found {
			t.Errorf("expected man page for %s", cmd)
			continue
		}
		if page == "" {
			t.Errorf("man page for %s is empty", cmd)
		}
	}
}

func TestLookupManPage_UnknownCommand(t *testing.T) {
	_, found := LookupManPage("/nonexistent")
	if found {
		t.Error("expected no man page for /nonexistent")
	}
}

func TestRenderManPage_MorphlingContainsExpectedSections(t *testing.T) {
	page, found := LookupManPage("/morphling")
	if !found {
		t.Fatal("no man page for /morphling")
	}

	for _, expected := range []string{
		"MORPHLING",
		"spawn",
		"status",
		"review",
		"terminate",
		"/morphling spawn editor",
		"Subcommands:",
		"Examples:",
	} {
		if !strings.Contains(page, expected) {
			t.Errorf("morphling man page missing %q", expected)
		}
	}
}

func TestRenderManPage_GoalContainsSubcommands(t *testing.T) {
	page, found := LookupManPage("/goal")
	if !found {
		t.Fatal("no man page for /goal")
	}

	for _, expected := range []string{"add", "close", "list"} {
		if !strings.Contains(page, expected) {
			t.Errorf("goal man page missing subcommand %q", expected)
		}
	}
}

func TestRenderManPage_HasBorders(t *testing.T) {
	page, found := LookupManPage("/morphling")
	if !found {
		t.Fatal("no man page for /morphling")
	}

	// Single border chars from ui.SingleBorder()
	if !strings.Contains(page, "┌") || !strings.Contains(page, "┘") {
		t.Error("man page missing border characters")
	}
}
