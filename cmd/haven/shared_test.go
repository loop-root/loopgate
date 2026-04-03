package main

import (
	"context"
	"strings"
	"testing"

	"morph/internal/loopgate"
)

func TestSyncSharedFolderEmitsWorkspaceRefreshAndToast(t *testing.T) {
	client := &fakeLoopgateClient{
		syncSharedFolderResp: loopgate.SharedFolderStatusResponse{
			Name:                "Shared with Morph",
			HostPath:            "/Users/test/Shared with Morph",
			SandboxRelativePath: "imports/shared",
			SandboxAbsolutePath: "/morph/home/imports/shared",
			HostExists:          true,
			MirrorReady:         true,
			EntryCount:          2,
		},
	}
	app, emitter := testApp(t, client)

	response, err := app.SyncSharedFolder()
	if err != nil {
		t.Fatalf("sync shared folder: %v", err)
	}
	if response.SandboxRelativePath != "imports/shared" {
		t.Fatalf("unexpected sandbox path %q", response.SandboxRelativePath)
	}

	fileChangedEvents := emitter.eventsByName("haven:file_changed")
	if len(fileChangedEvents) != 1 {
		t.Fatalf("expected one file-changed event, got %d", len(fileChangedEvents))
	}
	fileChangedData := fileChangedEvents[0].Data.(map[string]interface{})
	if fileChangedData["path"] != "shared" {
		t.Fatalf("expected shared path refresh, got %#v", fileChangedData["path"])
	}

	toastEvents := emitter.eventsByName("haven:toast")
	if len(toastEvents) != 1 {
		t.Fatalf("expected one toast event, got %d", len(toastEvents))
	}
}

func TestSyncGrantedFolderAccessEmitsRefreshAndOfferForChangedFolder(t *testing.T) {
	client := &fakeLoopgateClient{
		syncFolderAccessResp: loopgate.FolderAccessSyncResponse{
			Folders: []loopgate.FolderAccessStatus{
				{
					ID:                  "downloads",
					Name:                "Downloads",
					Granted:             true,
					HostExists:          true,
					MirrorReady:         true,
					EntryCount:          9,
					SandboxRelativePath: "imports/downloads",
				},
			},
			ChangedIDs: []string{"downloads"},
		},
	}
	app, emitter := testApp(t, client)

	if _, err := app.syncGrantedFolderAccess(context.Background()); err != nil {
		t.Fatalf("sync granted folder access: %v", err)
	}

	fileChangedEvents := emitter.eventsByName("haven:file_changed")
	if len(fileChangedEvents) != 1 {
		t.Fatalf("expected one file-changed event, got %d", len(fileChangedEvents))
	}
	fileChangedData := fileChangedEvents[0].Data.(map[string]interface{})
	if fileChangedData["path"] != "imports/downloads" {
		t.Fatalf("expected downloads path refresh, got %#v", fileChangedData["path"])
	}

	deskNotes, err := app.ListDeskNotes()
	if err != nil {
		t.Fatalf("list desk notes: %v", err)
	}
	if len(deskNotes) != 1 {
		t.Fatalf("expected one folder offer note, got %d", len(deskNotes))
	}
	if deskNotes[0].Title != "Downloads changed" {
		t.Fatalf("unexpected desk note title: %q", deskNotes[0].Title)
	}
	if deskNotes[0].Action == nil {
		t.Fatal("expected downloads offer note action to be present")
	}
	if !strings.Contains(deskNotes[0].Action.Message, "host.organize.plan") {
		t.Fatalf("expected downloads offer to route through host organize planning, got %q", deskNotes[0].Action.Message)
	}
	if strings.Contains(deskNotes[0].Action.Message, "without touching the originals") {
		t.Fatalf("expected downloads offer to allow approval-based host changes, got %q", deskNotes[0].Action.Message)
	}
}
