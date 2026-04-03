package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"morph/internal/loopgate"
)

type paintFakeClient struct {
	fakeLoopgateClient
	listResponse loopgate.SandboxListResponse
	listErr      error
}

func (client *paintFakeClient) SandboxList(_ context.Context, _ loopgate.SandboxListRequest) (loopgate.SandboxListResponse, error) {
	return client.listResponse, client.listErr
}

func TestPaintSaveArtwork_WritesValidatedSVGViaLoopgate(t *testing.T) {
	client := &fakeLoopgateClient{}
	app, emitter := testApp(t, client)
	app.sandboxHome = t.TempDir()

	response := app.PaintSaveArtwork(PaintSaveRequest{
		Title:      `Study <script>alert("x")</script>`,
		Width:      960,
		Height:     540,
		Background: "#F6F1E7",
		Strokes: []PaintStroke{
			{
				Color: "#8E6C4B",
				Width: 6,
				Points: []PaintPoint{
					{X: 120, Y: 160},
					{X: 320, Y: 190},
					{X: 480, Y: 120},
				},
			},
		},
	})
	if response.Error != "" {
		t.Fatalf("expected save to succeed, got error: %s", response.Error)
	}
	if !response.Saved {
		t.Fatal("expected save response to be marked saved")
	}
	if !strings.HasPrefix(response.Path, "artifacts/paintings/") {
		t.Fatalf("expected artifacts paint path, got %q", response.Path)
	}

	recordedRequests := client.recordedCapabilityRequests()
	if len(recordedRequests) != 1 {
		t.Fatalf("expected exactly one capability request, got %d", len(recordedRequests))
	}
	recordedRequest := recordedRequests[0]
	if recordedRequest.Capability != "fs_write" {
		t.Fatalf("expected fs_write capability, got %q", recordedRequest.Capability)
	}
	if !strings.HasPrefix(recordedRequest.Arguments["path"], paintGallerySandboxDirectory+"/") {
		t.Fatalf("expected paint gallery path, got %q", recordedRequest.Arguments["path"])
	}
	if strings.Contains(recordedRequest.Arguments["path"], "<script>") {
		t.Fatalf("expected safe filename, got %q", recordedRequest.Arguments["path"])
	}
	if !strings.Contains(recordedRequest.Arguments["content"], "<svg") {
		t.Fatal("expected saved SVG content")
	}
	if strings.Contains(recordedRequest.Arguments["content"], `<script>alert("x")</script>`) {
		t.Fatal("expected title to be escaped inside SVG content")
	}
	if !strings.Contains(recordedRequest.Arguments["content"], "&lt;script&gt;") {
		t.Fatal("expected escaped title markup inside SVG content")
	}

	fileChangedEvents := emitter.eventsByName("haven:file_changed")
	if len(fileChangedEvents) != 1 {
		t.Fatalf("expected one file change event, got %d", len(fileChangedEvents))
	}
}

func TestPaintSaveArtwork_RejectsInvalidStrokePayload(t *testing.T) {
	client := &fakeLoopgateClient{}
	app, _ := testApp(t, client)
	app.sandboxHome = t.TempDir()

	response := app.PaintSaveArtwork(PaintSaveRequest{
		Width:  960,
		Height: 540,
		Strokes: []PaintStroke{
			{
				Color: "#8E6C4B",
				Width: 4,
				Points: []PaintPoint{
					{X: -10, Y: 20},
				},
			},
		},
	})
	if response.Error == "" {
		t.Fatal("expected invalid stroke to be rejected")
	}
	if len(client.recordedCapabilityRequests()) != 0 {
		t.Fatal("expected invalid request to avoid capability execution")
	}
}

func TestListPaintings_SortsNewestFirstAndLoadsPreview(t *testing.T) {
	client := &paintFakeClient{
		listResponse: loopgate.SandboxListResponse{
			SandboxPath: paintGallerySandboxDirectory,
			Entries: []loopgate.SandboxListEntry{
				{Name: "20260318-081500-0001-soft-study.svg", EntryType: "file", ModTimeUTC: "2026-03-18T08:15:00Z"},
				{Name: "20260318-211000-0002-evening-desk.svg", EntryType: "file", ModTimeUTC: "2026-03-18T21:10:00Z"},
			},
		},
	}
	client.executeCapabilityFn = func(_ context.Context, request loopgate.CapabilityRequest) (loopgate.CapabilityResponse, error) {
		switch request.Arguments["path"] {
		case "outputs/paintings/20260318-081500-0001-soft-study.svg":
			return loopgate.CapabilityResponse{
				Status: loopgate.ResponseStatusSuccess,
				StructuredResult: map[string]interface{}{
					"content": `<svg><title>Soft Study</title><rect width="100%" height="100%" fill="#F6F1E7"/></svg>`,
				},
			}, nil
		case "outputs/paintings/20260318-211000-0002-evening-desk.svg":
			return loopgate.CapabilityResponse{
				Status: loopgate.ResponseStatusSuccess,
				StructuredResult: map[string]interface{}{
					"content": `<svg><title>Evening Desk</title><path d="M 1 1 L 2 2" /></svg>`,
				},
			}, nil
		default:
			return loopgate.CapabilityResponse{Status: loopgate.ResponseStatusDenied, DenialReason: "unexpected path"}, nil
		}
	}

	app, _ := testApp(t, &client.fakeLoopgateClient)
	app.loopgateClient = client
	app.sandboxHome = t.TempDir()
	if err := os.MkdirAll(filepath.Join(app.sandboxHome, "outputs", "paintings"), 0o755); err != nil {
		t.Fatalf("create paint dir: %v", err)
	}

	gallery, err := app.ListPaintings()
	if err != nil {
		t.Fatalf("list paintings: %v", err)
	}
	if len(gallery) != 2 {
		t.Fatalf("expected 2 paintings, got %d", len(gallery))
	}
	if gallery[0].Title != "Evening Desk" {
		t.Fatalf("expected newest painting first, got %q", gallery[0].Title)
	}
	if gallery[0].Path != "artifacts/paintings/20260318-211000-0002-evening-desk.svg" {
		t.Fatalf("unexpected haven path %q", gallery[0].Path)
	}
	if !strings.Contains(gallery[0].PreviewSVG, "<svg") {
		t.Fatal("expected preview svg content to be returned")
	}
}
