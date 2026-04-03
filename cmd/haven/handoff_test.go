package main

import (
	"testing"
	"time"

	modelpkg "morph/internal/model"
)

func TestSendMessage_CreatesCompletionDeskNoteAfterWrite(t *testing.T) {
	client := &fakeLoopgateClient{
		modelResponses: []modelpkg.Response{
			{AssistantText: `<tool_call>{"name":"fs_write","args":{"path":"workspace/notes/today.md","content":"hello world"}}</tool_call>`},
			{AssistantText: "All set. I saved the note."},
		},
	}
	app, _ := testApp(t, client)

	thread, err := app.NewThread()
	if err != nil {
		t.Fatalf("new thread: %v", err)
	}

	response := app.SendMessage(thread.ThreadID, "Write down a note")
	if !response.Accepted {
		t.Fatalf("expected message to be accepted, got %q", response.Reason)
	}

	waitForDone(t, app, thread.ThreadID, 2*time.Second)

	deskNotes, err := app.ListDeskNotes()
	if err != nil {
		t.Fatalf("list desk notes: %v", err)
	}
	if len(deskNotes) != 1 {
		t.Fatalf("expected one completion desk note, got %d", len(deskNotes))
	}
	if deskNotes[0].Title != "Work is ready" {
		t.Fatalf("unexpected note title %q", deskNotes[0].Title)
	}
	expectedBody := "I finished working and updated projects/notes/today.md. Open Workspace when you want to take a look."
	if deskNotes[0].Body != expectedBody {
		t.Fatalf("unexpected note body %q", deskNotes[0].Body)
	}
}

func TestSendMessage_DoesNotCreateCompletionDeskNoteWithoutWrite(t *testing.T) {
	client := &fakeLoopgateClient{
		modelResponses: []modelpkg.Response{
			{AssistantText: "Nothing needed changing."},
		},
	}
	app, _ := testApp(t, client)

	thread, err := app.NewThread()
	if err != nil {
		t.Fatalf("new thread: %v", err)
	}

	response := app.SendMessage(thread.ThreadID, "How are things looking?")
	if !response.Accepted {
		t.Fatalf("expected message to be accepted, got %q", response.Reason)
	}

	waitForDone(t, app, thread.ThreadID, 2*time.Second)

	deskNotes, err := app.ListDeskNotes()
	if err != nil {
		t.Fatalf("list desk notes: %v", err)
	}
	if len(deskNotes) != 0 {
		t.Fatalf("expected no completion desk note, got %d", len(deskNotes))
	}
}
