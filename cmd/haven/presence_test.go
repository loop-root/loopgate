package main

import (
	"testing"
	"time"

	"morph/internal/loopgate"
)

func TestPresenceManager_NotifyToolStarted_AnchorsJournal(t *testing.T) {
	emitter := &recordingEmitter{}
	presenceManager := NewPresenceManager(emitter, "Morph")
	defer presenceManager.Stop()

	presenceManager.NotifyToolStarted("fs_write", map[string]string{
		"path": "scratch/journal/2026-03-18.md",
	})

	if presenceManager.state != PresenceCreating {
		t.Fatalf("expected creating state, got %s", presenceManager.state)
	}
	if presenceManager.anchor != PresenceAnchorJournal {
		t.Fatalf("expected journal anchor, got %s", presenceManager.anchor)
	}
	if presenceManager.detailText != "research/journal/2026-03-18.md" {
		t.Fatalf("unexpected detail text %q", presenceManager.detailText)
	}
}

func TestPresenceManager_NotifyAwaitingApproval_AnchorsLoopgate(t *testing.T) {
	emitter := &recordingEmitter{}
	presenceManager := NewPresenceManager(emitter, "Morph")
	defer presenceManager.Stop()

	presenceManager.NotifyAwaitingApproval("fs_write", map[string]string{
		"path": "workspace/notes/today.md",
	})

	if presenceManager.anchor != PresenceAnchorLoopgate {
		t.Fatalf("expected loopgate anchor, got %s", presenceManager.anchor)
	}
	if presenceManager.statusText != "Morph is waiting for you" {
		t.Fatalf("unexpected status text %q", presenceManager.statusText)
	}
}

func TestCompletionPresenceContext_PrefersPaintOutputs(t *testing.T) {
	statusText, detailText, anchor := completionPresenceContext("Morph", &completedWorkTracker{
		writtenSandboxPaths: []string{"outputs/paintings/20260318-evening-desk.svg"},
	})

	if anchor != PresenceAnchorPaint {
		t.Fatalf("expected paint anchor, got %s", anchor)
	}
	if statusText != "Morph finished a painting" {
		t.Fatalf("unexpected status text %q", statusText)
	}
	if detailText != "artifacts/paintings/20260318-evening-desk.svg" {
		t.Fatalf("unexpected detail text %q", detailText)
	}
}

func TestPresenceManager_NotifyContinuityLoaded_PrefersTodoAnchor(t *testing.T) {
	emitter := &recordingEmitter{}
	presenceManager := NewPresenceManager(emitter, "Morph")
	defer presenceManager.Stop()

	presenceManager.NotifyContinuityLoaded(loopgate.MemoryWakeStateResponse{
		UnresolvedItems: []loopgate.MemoryWakeStateOpenItem{
			{ID: "todo_report", Text: "Finish the Q1 report summary"},
		},
	})

	if presenceManager.anchor != PresenceAnchorTodo {
		t.Fatalf("expected todo anchor, got %s", presenceManager.anchor)
	}
	if presenceManager.statusText != "Morph is picking back up" {
		t.Fatalf("unexpected status text %q", presenceManager.statusText)
	}
}

func TestIdlePresenceSnapshot_EveningDoesNotDefaultToJournal(t *testing.T) {
	statusText, detailText, anchor := idlePresenceSnapshot("Morph", PresenceAnchorDesk, time.Date(2026, 3, 18, 19, 0, 0, 0, time.UTC))

	if anchor == PresenceAnchorJournal {
		t.Fatalf("expected non-journal anchor, got %s", anchor)
	}
	if statusText != "Morph is settling into the evening" {
		t.Fatalf("unexpected status text %q", statusText)
	}
	if detailText != "keeping the desk warm" {
		t.Fatalf("unexpected detail text %q", detailText)
	}
}
