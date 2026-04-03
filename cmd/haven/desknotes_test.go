package main

import (
	"testing"
	"time"

	modelpkg "morph/internal/model"
)

func TestDeskNotesCreateListAndDismiss(t *testing.T) {
	app, emitter := testApp(t, &fakeLoopgateClient{})

	createdNote, err := app.createDeskNote(DeskNoteDraft{
		Kind:  "update",
		Title: "While you were away",
		Body:  "I sorted through a few things.",
	})
	if err != nil {
		t.Fatalf("create desk note: %v", err)
	}

	deskNotes, err := app.ListDeskNotes()
	if err != nil {
		t.Fatalf("list desk notes: %v", err)
	}
	if len(deskNotes) != 1 {
		t.Fatalf("expected 1 active desk note, got %d", len(deskNotes))
	}
	if deskNotes[0].ID != createdNote.ID {
		t.Fatalf("unexpected desk note id: got %q want %q", deskNotes[0].ID, createdNote.ID)
	}

	dismissResponse := app.DismissDeskNote(createdNote.ID)
	if !dismissResponse.Success {
		t.Fatalf("dismiss desk note failed: %s", dismissResponse.Error)
	}

	deskNotes, err = app.ListDeskNotes()
	if err != nil {
		t.Fatalf("list desk notes after dismiss: %v", err)
	}
	if len(deskNotes) != 0 {
		t.Fatalf("expected no active desk notes after dismiss, got %d", len(deskNotes))
	}

	if len(emitter.eventsByName("haven:desk_notes_changed")) < 2 {
		t.Fatalf("expected desk note change events for create and dismiss")
	}
}

func TestDeskNotesArchiveOldestWhenActiveLimitExceeded(t *testing.T) {
	app, _ := testApp(t, &fakeLoopgateClient{})

	for index := 0; index < maxActiveDeskNotes+2; index++ {
		_, err := app.createDeskNote(DeskNoteDraft{
			Title: "While you were away",
			Body:  "I left a note behind.",
		})
		if err != nil {
			t.Fatalf("create desk note %d: %v", index, err)
		}
	}

	deskNotes, err := app.ListDeskNotes()
	if err != nil {
		t.Fatalf("list desk notes: %v", err)
	}
	if len(deskNotes) != maxActiveDeskNotes {
		t.Fatalf("expected %d active desk notes, got %d", maxActiveDeskNotes, len(deskNotes))
	}
}

func TestDeskNoteDraftRejectsEmptyBody(t *testing.T) {
	app, _ := testApp(t, &fakeLoopgateClient{})

	if _, err := app.createDeskNote(DeskNoteDraft{Title: "While you were away"}); err == nil {
		t.Fatal("expected empty desk note body to be rejected")
	}
}

func TestExecuteDeskNoteAction_StartsMorphThreadAndMarksNoteExecuted(t *testing.T) {
	client := &fakeLoopgateClient{
		modelResponses: []modelpkg.Response{
			{AssistantText: "I'll take a first pass from here."},
		},
	}
	app, _ := testApp(t, client)

	createdNote, err := app.createDeskNote(DeskNoteDraft{
		Kind:  "reminder",
		Title: "Downloads changed",
		Body:  "I can take a look through the mirrored copy.",
		Action: &DeskNoteAction{
			Kind:    "send_message",
			Label:   "Yes, do it",
			Message: "Please take a first pass through the mirrored Downloads folder in Haven.",
		},
	})
	if err != nil {
		t.Fatalf("create actionable desk note: %v", err)
	}

	response := app.ExecuteDeskNoteAction(createdNote.ID)
	if !response.Success {
		t.Fatalf("execute desk note action failed: %s", response.Error)
	}
	if response.ThreadID == "" {
		t.Fatal("expected execute desk note action to return a thread id")
	}

	waitForDone(t, app, response.ThreadID, 2*time.Second)

	deskNotes, err := app.ListDeskNotes()
	if err != nil {
		t.Fatalf("list desk notes: %v", err)
	}
	if len(deskNotes) != 1 {
		t.Fatalf("expected one active desk note, got %d", len(deskNotes))
	}
	if deskNotes[0].ActionExecutedAtUTC == "" {
		t.Fatal("expected desk note action to be marked as executed")
	}
	if deskNotes[0].ActionThreadID != response.ThreadID {
		t.Fatalf("unexpected action thread id %q", deskNotes[0].ActionThreadID)
	}

	events, err := app.LoadThread(response.ThreadID)
	if err != nil {
		t.Fatalf("load thread: %v", err)
	}
	if len(events) == 0 || events[0].Type != "user_message" {
		t.Fatalf("expected started thread to include the triggering user message, got %#v", events)
	}
}
