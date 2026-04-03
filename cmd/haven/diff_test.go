package main

import (
	"testing"

	"morph/internal/loopgate"
)

func TestWorkspaceRestoreOriginal_RestoresImportedContent(t *testing.T) {
	client := &fakeLoopgateClient{
		capabilityResponses: map[string]loopgate.CapabilityResponse{
			"fs_write": {
				Status: loopgate.ResponseStatusSuccess,
			},
		},
	}
	app, emitter := testApp(t, client)

	const havenPath = "projects/notes.txt"
	const originalContent = "hello from the original file\n"

	if err := app.StoreOriginal(havenPath, originalContent); err != nil {
		t.Fatalf("store original: %v", err)
	}

	response := app.WorkspaceRestoreOriginal(havenPath)
	if !response.Restored {
		t.Fatalf("expected restore to succeed, got error: %s", response.Error)
	}

	recordedRequests := client.recordedCapabilityRequests()
	if len(recordedRequests) != 1 {
		t.Fatalf("expected 1 capability request, got %d", len(recordedRequests))
	}

	recordedRequest := recordedRequests[0]
	if recordedRequest.Capability != "fs_write" {
		t.Fatalf("expected fs_write capability, got %q", recordedRequest.Capability)
	}
	if got := recordedRequest.Arguments["path"]; got != "workspace/notes.txt" {
		t.Fatalf("expected restore path workspace/notes.txt, got %q", got)
	}
	if got := recordedRequest.Arguments["content"]; got != originalContent {
		t.Fatalf("expected original content to be restored, got %q", got)
	}

	events := emitter.eventsByName("haven:file_changed")
	if len(events) != 1 {
		t.Fatalf("expected 1 file_changed event, got %d", len(events))
	}
	data, ok := events[0].Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected event payload map, got %T", events[0].Data)
	}
	if got := data["path"]; got != havenPath {
		t.Fatalf("expected event path %q, got %#v", havenPath, got)
	}
	if got := data["action"]; got != "restore" {
		t.Fatalf("expected restore action, got %#v", got)
	}
}

func TestWorkspaceRestoreOriginal_RejectsMissingOriginal(t *testing.T) {
	app, _ := testApp(t, &fakeLoopgateClient{})

	response := app.WorkspaceRestoreOriginal("projects/missing.txt")
	if response.Restored {
		t.Fatal("expected restore to fail when no original exists")
	}
	if response.Error == "" {
		t.Fatal("expected missing original to return an error")
	}
}
