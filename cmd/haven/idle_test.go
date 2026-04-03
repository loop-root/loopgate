package main

import (
	"testing"
	"time"

	"morph/internal/loopgate"
)

func TestIdleManagerNextBehaviorPrioritizesCarryForward(t *testing.T) {
	client := &fakeLoopgateClient{
		wakeStateResp: loopgate.MemoryWakeStateResponse{
			UnresolvedItems: []loopgate.MemoryWakeStateOpenItem{
				{ID: "todo_downloads", Text: "Organize the downloads pile"},
			},
		},
	}
	app, _ := testApp(t, client)
	app.idleManager = NewIdleManager(app)
	defer app.idleManager.Stop()

	nextBehavior, ok := app.idleManager.nextBehavior()
	if !ok {
		t.Fatal("expected a resident behavior")
	}
	if nextBehavior.Name != "carry_forward" {
		t.Fatalf("expected carry_forward behavior, got %q", nextBehavior.Name)
	}
}

func TestIdleCarryForwardCreatesPlanningDeskNote(t *testing.T) {
	app, _ := testApp(t, &fakeLoopgateClient{})
	app.setMemoryState(loopgate.MemoryWakeStateResponse{
		UnresolvedItems: []loopgate.MemoryWakeStateOpenItem{
			{ID: "todo_report", Text: "Draft the project update"},
		},
	}, loopgate.MemoryDiagnosticWakeResponse{})

	if err := idleCarryForward(nil, app); err != nil {
		t.Fatalf("idle carry forward: %v", err)
	}

	deskNotes, err := app.ListDeskNotes()
	if err != nil {
		t.Fatalf("list desk notes: %v", err)
	}
	if len(deskNotes) != 1 {
		t.Fatalf("expected one carry-forward note, got %d", len(deskNotes))
	}
	if deskNotes[0].Title != "Carry-over is waiting on your say-so" {
		t.Fatalf("unexpected carry-forward title: %q", deskNotes[0].Title)
	}
}

func TestIdleManagerNextBehavior_IgnoresFutureScheduledTasks(t *testing.T) {
	client := &fakeLoopgateClient{
		wakeStateResp: loopgate.MemoryWakeStateResponse{
			UnresolvedItems: []loopgate.MemoryWakeStateOpenItem{
				{
					ID:              "todo_future",
					Text:            "Review downloads cleanup",
					TaskKind:        "scheduled",
					ScheduledForUTC: time.Now().UTC().Add(6 * time.Hour).Format(time.RFC3339Nano),
				},
			},
		},
	}
	app, _ := testApp(t, client)
	app.idleManager = NewIdleManager(app)
	defer app.idleManager.Stop()
	app.idleManager.mu.Lock()
	app.idleManager.ambientEnabled = false
	app.idleManager.mu.Unlock()

	nextBehavior, ok := app.idleManager.nextBehavior()
	if ok {
		t.Fatalf("expected no resident behavior while only a future scheduled task exists, got %q", nextBehavior.Name)
	}
}
