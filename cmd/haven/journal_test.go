package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"morph/internal/loopgate"
)

type journalFakeClient struct {
	fakeLoopgateClient
	listResponse loopgate.SandboxListResponse
	listErr      error
}

func (client *journalFakeClient) SandboxList(_ context.Context, _ loopgate.SandboxListRequest) (loopgate.SandboxListResponse, error) {
	return client.listResponse, client.listErr
}

func TestListJournalEntries_SortsNewestFirstAndBuildsPreview(t *testing.T) {
	client := &journalFakeClient{
		listResponse: loopgate.SandboxListResponse{
			SandboxPath: journalSandboxDirectory,
			Entries: []loopgate.SandboxListEntry{
				{Name: "2026-03-17.md", EntryType: "file", ModTimeUTC: "2026-03-17T19:00:00Z"},
				{Name: "2026-03-18.md", EntryType: "file", ModTimeUTC: "2026-03-18T21:30:00Z"},
			},
		},
	}
	client.executeCapabilityFn = func(_ context.Context, request loopgate.CapabilityRequest) (loopgate.CapabilityResponse, error) {
		switch request.Arguments["path"] {
		case "scratch/journal/2026-03-17.md":
			return loopgate.CapabilityResponse{
				Status: loopgate.ResponseStatusSuccess,
				StructuredResult: map[string]interface{}{
					"content": "--- 9:10 AM ---\nI kept thinking about softer interfaces.\n",
				},
			}, nil
		case "scratch/journal/2026-03-18.md":
			return loopgate.CapabilityResponse{
				Status: loopgate.ResponseStatusSuccess,
				StructuredResult: map[string]interface{}{
					"content": "--- 8:15 AM ---\nI noticed the desk felt calmer today.\n\n--- 2:45 PM ---\nI want Haven to feel like a place someone actually lives in.\n",
				},
			}, nil
		default:
			return loopgate.CapabilityResponse{Status: loopgate.ResponseStatusDenied, DenialReason: "unexpected path"}, nil
		}
	}

	app, _ := testApp(t, &client.fakeLoopgateClient)
	app.loopgateClient = client
	app.sandboxHome = t.TempDir()
	if err := os.MkdirAll(filepath.Join(app.sandboxHome, "scratch", "journal"), 0o755); err != nil {
		t.Fatalf("create journal dir: %v", err)
	}

	entries, err := app.ListJournalEntries()
	if err != nil {
		t.Fatalf("list journal entries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 journal entries, got %d", len(entries))
	}
	if entries[0].Path != "research/journal/2026-03-18.md" {
		t.Fatalf("expected newest entry first, got %q", entries[0].Path)
	}
	if entries[0].Title != "March 18, 2026" {
		t.Fatalf("expected formatted title, got %q", entries[0].Title)
	}
	if entries[0].EntryCount != 2 {
		t.Fatalf("expected 2 journal sections, got %d", entries[0].EntryCount)
	}
	expectedPreview := "I want Haven to feel like a place someone actually lives in."
	if entries[0].Preview != expectedPreview {
		t.Fatalf("expected preview %q, got %q", expectedPreview, entries[0].Preview)
	}
}

func TestListJournalEntries_EmptyWhenJournalDirectoryMissing(t *testing.T) {
	app, _ := testApp(t, &fakeLoopgateClient{})
	app.sandboxHome = t.TempDir()

	entries, err := app.ListJournalEntries()
	if err != nil {
		t.Fatalf("expected missing journal directory to be non-fatal, got %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no entries, got %d", len(entries))
	}
}

func TestReadJournalEntry_ReadsViaLoopgate(t *testing.T) {
	client := &fakeLoopgateClient{}
	client.executeCapabilityFn = func(_ context.Context, request loopgate.CapabilityRequest) (loopgate.CapabilityResponse, error) {
		if request.Arguments["path"] != "scratch/journal/2026-03-18.md" {
			t.Fatalf("expected scratch journal path, got %q", request.Arguments["path"])
		}
		return loopgate.CapabilityResponse{
			Status: loopgate.ResponseStatusSuccess,
			StructuredResult: map[string]interface{}{
				"content": "--- 6:30 PM ---\nI am learning how to leave quieter traces behind.\n",
			},
		}, nil
	}

	app, _ := testApp(t, client)
	response := app.ReadJournalEntry("research/journal/2026-03-18.md")
	if response.Error != "" {
		t.Fatalf("expected read to succeed, got error: %s", response.Error)
	}
	if response.Title != "March 18, 2026" {
		t.Fatalf("expected formatted title, got %q", response.Title)
	}
	if response.EntryCount != 1 {
		t.Fatalf("expected single journal section, got %d", response.EntryCount)
	}
	if response.Content == "" {
		t.Fatal("expected journal content to be returned")
	}
}
