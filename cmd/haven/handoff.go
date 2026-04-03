package main

import (
	"strconv"
	"strings"

	"morph/internal/loopgate"
)

// completedWorkTracker gathers user-visible outputs from a single Haven task run.
// It is execution-local state, not authoritative history.
type completedWorkTracker struct {
	writtenSandboxPaths []string
}

func (tracker *completedWorkTracker) recordCapabilityResult(capability string, arguments map[string]string, response loopgate.CapabilityResponse) {
	if tracker == nil || capability != "fs_write" || response.Status != loopgate.ResponseStatusSuccess {
		return
	}

	sandboxPath := strings.TrimSpace(arguments["path"])
	if sandboxPath == "" {
		return
	}
	for _, existingPath := range tracker.writtenSandboxPaths {
		if existingPath == sandboxPath {
			return
		}
	}
	tracker.writtenSandboxPaths = append(tracker.writtenSandboxPaths, sandboxPath)
}

func (tracker *completedWorkTracker) hasUserVisibleOutputs() bool {
	return tracker != nil && len(tracker.writtenSandboxPaths) > 0
}

func (app *HavenApp) createCompletionDeskNote(tracker *completedWorkTracker) error {
	if tracker == nil || !tracker.hasUserVisibleOutputs() {
		return nil
	}

	havenPaths := make([]string, 0, len(tracker.writtenSandboxPaths))
	for _, sandboxPath := range tracker.writtenSandboxPaths {
		havenPaths = append(havenPaths, mapSandboxPathToHaven(sandboxPath))
	}

	_, err := app.createDeskNote(DeskNoteDraft{
		Kind:  "update",
		Title: completionDeskNoteTitle(havenPaths),
		Body:  completionDeskNoteBody(havenPaths),
	})
	return err
}

func completionDeskNoteTitle(havenPaths []string) string {
	if len(havenPaths) == 1 {
		switch {
		case strings.HasPrefix(havenPaths[0], "artifacts/paintings/"):
			return "A new painting is ready"
		case strings.HasPrefix(havenPaths[0], "research/journal/"):
			return "A journal page is ready"
		case strings.HasPrefix(havenPaths[0], "artifacts/"):
			return "A new artifact is ready"
		}
	}
	return "Work is ready"
}

func completionDeskNoteBody(havenPaths []string) string {
	displayPaths := make([]string, 0, len(havenPaths))
	for _, havenPath := range havenPaths {
		displayPaths = append(displayPaths, compactHavenPath(havenPath))
	}

	switch len(displayPaths) {
	case 0:
		return "I finished working. Open Workspace when you want to take a look."
	case 1:
		return "I finished working and updated " + displayPaths[0] + ". Open Workspace when you want to take a look."
	case 2:
		return "I finished working and updated " + displayPaths[0] + " and " + displayPaths[1] + ". Open Workspace when you want to take a look."
	default:
		return "I finished working and updated " + displayPaths[0] + ", " + displayPaths[1] + ", and " + pluralizeRemainingFiles(len(displayPaths)-2) + ". Open Workspace when you want to take a look."
	}
}

func compactHavenPath(havenPath string) string {
	pathParts := strings.Split(strings.TrimSpace(havenPath), "/")
	if len(pathParts) <= 3 {
		return havenPath
	}
	return pathParts[0] + "/.../" + pathParts[len(pathParts)-1]
}

func pluralizeRemainingFiles(count int) string {
	if count == 1 {
		return "1 more file"
	}
	return strconv.Itoa(count) + " more files"
}
