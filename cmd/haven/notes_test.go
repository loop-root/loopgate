package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"morph/internal/loopgate"
)

type notesFakeClient struct {
	fakeLoopgateClient
	listResponse loopgate.SandboxListResponse
	listErr      error
}

func (client *notesFakeClient) SandboxList(_ context.Context, _ loopgate.SandboxListRequest) (loopgate.SandboxListResponse, error) {
	return client.listResponse, client.listErr
}

func TestListWorkingNotes_SortsNewestFirstAndBuildsPreview(t *testing.T) {
	client := &notesFakeClient{
		listResponse: loopgate.SandboxListResponse{
			SandboxPath: workingNotesSandboxDirectory,
			Entries: []loopgate.SandboxListEntry{
				{Name: "inbox.md", EntryType: "file", ModTimeUTC: "2026-03-19T20:00:00Z"},
				{Name: "downloads-cleanup.md", EntryType: "file", ModTimeUTC: "2026-03-19T22:30:00Z"},
			},
		},
	}
	client.executeCapabilityFn = func(_ context.Context, request loopgate.CapabilityRequest) (loopgate.CapabilityResponse, error) {
		switch request.Arguments["path"] {
		case "scratch/notes/inbox.md":
			return loopgate.CapabilityResponse{
				Status: loopgate.ResponseStatusSuccess,
				StructuredResult: map[string]interface{}{
					"content": "# Inbox\nAsk whether the user wants receipts grouped separately.\n",
				},
			}, nil
		case "scratch/notes/downloads-cleanup.md":
			return loopgate.CapabilityResponse{
				Status: loopgate.ResponseStatusSuccess,
				StructuredResult: map[string]interface{}{
					"content": "# Downloads Cleanup\nGroup screenshots.\nThen sort invoices.\n",
				},
			}, nil
		default:
			return loopgate.CapabilityResponse{Status: loopgate.ResponseStatusDenied, DenialReason: "unexpected path"}, nil
		}
	}

	app, _ := testApp(t, &client.fakeLoopgateClient)
	app.loopgateClient = client
	app.sandboxHome = t.TempDir()
	if err := os.MkdirAll(filepath.Join(app.sandboxHome, "scratch", "notes"), 0o755); err != nil {
		t.Fatalf("create notes dir: %v", err)
	}

	notes, err := app.ListWorkingNotes()
	if err != nil {
		t.Fatalf("list working notes: %v", err)
	}
	if len(notes) != 2 {
		t.Fatalf("expected 2 working notes, got %d", len(notes))
	}
	if notes[0].Path != "research/notes/downloads-cleanup.md" {
		t.Fatalf("expected newest note first, got %q", notes[0].Path)
	}
	if notes[0].Title != "Downloads Cleanup" {
		t.Fatalf("expected heading-derived title, got %q", notes[0].Title)
	}
	if notes[0].Preview == "" {
		t.Fatal("expected preview text")
	}
}

func TestReadWorkingNote_ReadsViaLoopgate(t *testing.T) {
	client := &fakeLoopgateClient{}
	client.executeCapabilityFn = func(_ context.Context, request loopgate.CapabilityRequest) (loopgate.CapabilityResponse, error) {
		if request.Capability != "notes.read" {
			t.Fatalf("expected notes.read capability, got %q", request.Capability)
		}
		if request.Arguments["path"] != "scratch/notes/inbox.md" {
			t.Fatalf("expected scratch notes path, got %q", request.Arguments["path"])
		}
		return loopgate.CapabilityResponse{
			Status: loopgate.ResponseStatusSuccess,
			StructuredResult: map[string]interface{}{
				"content": "# Inbox\nRemember to ask about folders.\n",
			},
		}, nil
	}

	app, _ := testApp(t, client)
	response := app.ReadWorkingNote("research/notes/inbox.md")
	if response.Error != "" {
		t.Fatalf("expected note read to succeed, got error: %s", response.Error)
	}
	if response.Title != "Inbox" {
		t.Fatalf("expected note title, got %q", response.Title)
	}
	if response.Content == "" {
		t.Fatal("expected note content to be returned")
	}
}

func TestSaveWorkingNote_WritesViaLoopgate(t *testing.T) {
	client := &fakeLoopgateClient{}
	client.executeCapabilityFn = func(_ context.Context, request loopgate.CapabilityRequest) (loopgate.CapabilityResponse, error) {
		if request.Capability != "notes.write" {
			t.Fatalf("expected notes.write capability, got %q", request.Capability)
		}
		if request.Arguments["title"] != "Downloads Cleanup" {
			t.Fatalf("expected title to be forwarded, got %#v", request.Arguments)
		}
		if request.Arguments["body"] != "Group screenshots, then sort invoices." {
			t.Fatalf("expected note body to be forwarded, got %#v", request.Arguments)
		}
		return loopgate.CapabilityResponse{
			Status: loopgate.ResponseStatusSuccess,
			StructuredResult: map[string]interface{}{
				"content": "Working note saved to research/notes/downloads-cleanup.md",
			},
		}, nil
	}

	app, _ := testApp(t, client)
	response := app.SaveWorkingNote(WorkingNoteSaveRequest{
		Title:   "Downloads Cleanup",
		Content: "Group screenshots, then sort invoices.",
	})
	if response.Error != "" {
		t.Fatalf("expected save note to succeed, got %s", response.Error)
	}
	if !response.Saved || response.Path != "research/notes/downloads-cleanup.md" {
		t.Fatalf("unexpected save note response %#v", response)
	}
}
