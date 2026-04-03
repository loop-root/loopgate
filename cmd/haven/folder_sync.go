package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"morph/internal/loopgate"
)

const defaultFolderSyncInterval = 30 * time.Second

type FolderSyncManager struct {
	app      *HavenApp
	stopCh   chan struct{}
	stopOnce sync.Once
}

func NewFolderSyncManager(app *HavenApp) *FolderSyncManager {
	folderSyncManager := &FolderSyncManager{
		app:    app,
		stopCh: make(chan struct{}),
	}
	go folderSyncManager.watch()
	return folderSyncManager
}

func (folderSyncManager *FolderSyncManager) Stop() {
	folderSyncManager.stopOnce.Do(func() {
		close(folderSyncManager.stopCh)
	})
}

func (folderSyncManager *FolderSyncManager) watch() {
	ticker := time.NewTicker(defaultFolderSyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-folderSyncManager.stopCh:
			return
		case <-ticker.C:
			if _, err := folderSyncManager.app.syncGrantedFolderAccess(context.Background()); err != nil {
				fmt.Fprintf(os.Stderr, "haven: folder sync unavailable: %v\n", err)
			}
		}
	}
}

func (app *HavenApp) currentFolderAccessStatus() loopgate.FolderAccessStatusResponse {
	app.folderAccessMu.RLock()
	defer app.folderAccessMu.RUnlock()

	return loopgate.FolderAccessStatusResponse{
		Folders: append([]loopgate.FolderAccessStatus(nil), app.folderAccess.Folders...),
	}
}

func (app *HavenApp) setFolderAccessStatus(statusResponse loopgate.FolderAccessStatusResponse) loopgate.FolderAccessStatusResponse {
	app.folderAccessMu.Lock()
	defer app.folderAccessMu.Unlock()

	previousStatus := loopgate.FolderAccessStatusResponse{
		Folders: append([]loopgate.FolderAccessStatus(nil), app.folderAccess.Folders...),
	}
	app.folderAccess = loopgate.FolderAccessStatusResponse{
		Folders: append([]loopgate.FolderAccessStatus(nil), statusResponse.Folders...),
	}
	return previousStatus
}

func (app *HavenApp) syncGrantedFolderAccess(ctx context.Context) (loopgate.FolderAccessSyncResponse, error) {
	folderAccessContext, cancelFolderAccess := withFolderAccessTimeout(ctx)
	defer cancelFolderAccess()

	syncResponse, err := app.loopgateClient.SyncFolderAccess(folderAccessContext)
	if err != nil {
		return loopgate.FolderAccessSyncResponse{}, err
	}

	previousStatus := app.setFolderAccessStatus(loopgate.FolderAccessStatusResponse{Folders: syncResponse.Folders})
	previousByID := make(map[string]loopgate.FolderAccessStatus, len(previousStatus.Folders))
	for _, folderStatus := range previousStatus.Folders {
		previousByID[folderStatus.ID] = folderStatus
	}

	for _, changedID := range syncResponse.ChangedIDs {
		folderStatus, found := folderAccessStatusByID(syncResponse.Folders, changedID)
		if !found {
			continue
		}
		if app.emitter != nil {
			app.emitter.Emit("haven:file_changed", map[string]interface{}{
				"action": "sync",
				"path":   mapSandboxPathToHaven(folderStatus.SandboxRelativePath),
			})
		}
		app.maybeCreateFolderOfferNote(folderStatus, previousByID[changedID])
	}

	return syncResponse, nil
}

func (app *HavenApp) maybeCreateFolderOfferNote(folderStatus loopgate.FolderAccessStatus, previousStatus loopgate.FolderAccessStatus) {
	if !folderStatus.Granted || !folderStatus.HostExists || folderStatus.EntryCount <= 0 {
		return
	}
	if previousStatus.EntryCount == folderStatus.EntryCount && previousStatus.MirrorReady == folderStatus.MirrorReady {
		return
	}

	var draft *DeskNoteDraft
	switch folderStatus.ID {
	case "shared":
		draft = &DeskNoteDraft{
			Kind:  "update",
			Title: "Something new arrived in shared",
			Body:  fmt.Sprintf("I noticed %d item%s in shared. I can take a quick first pass from here in Haven whenever you want.", folderStatus.EntryCount, pluralSuffix(folderStatus.EntryCount)),
			Action: &DeskNoteAction{
				Kind:    "send_message",
				Label:   "Yes, do it",
				Message: "Please take a first pass through the mirrored shared folder in Haven. Survey what arrived, leave a short note about what seems important, and ask me one short question if anything is unclear.",
			},
		}
	case "downloads":
		draft = &DeskNoteDraft{
			Kind:  "reminder",
			Title: "Downloads changed",
			Body:  fmt.Sprintf("Downloads now has %d item%s. If you want, I can review the real folder, draft an organization plan, and ask before making changes.", folderStatus.EntryCount, pluralSuffix(folderStatus.EntryCount)),
			Action: &DeskNoteAction{
				Kind:    "send_message",
				Label:   "Yes, do it",
				Message: "Please look through my Downloads folder using host.folder.list. Categorize what you find and create an organization plan using host.organize.plan. Show me the plan before applying anything.",
			},
		}
	case "desktop":
		draft = &DeskNoteDraft{
			Kind:  "reminder",
			Title: "Desktop changed",
			Body:  fmt.Sprintf("I noticed %d item%s on your Desktop. I can review the real folder, draft a cleanup plan, and ask before making changes.", folderStatus.EntryCount, pluralSuffix(folderStatus.EntryCount)),
			Action: &DeskNoteAction{
				Kind:    "send_message",
				Label:   "Yes, do it",
				Message: "Please review my Desktop folder using host.folder.list. Note what looks cluttered or out of place, create a cleanup plan using host.organize.plan, and show me the plan before applying anything.",
			},
		}
	case "documents":
		draft = &DeskNoteDraft{
			Kind:  "reminder",
			Title: "Documents changed",
			Body:  fmt.Sprintf("Documents now has %d item%s mirrored into Haven. If you want, I can help surface a starting point without touching the originals.", folderStatus.EntryCount, pluralSuffix(folderStatus.EntryCount)),
			Action: &DeskNoteAction{
				Kind:    "send_message",
				Label:   "Yes, do it",
				Message: "Please take a first pass through the mirrored Documents folder in Haven. Surface likely starting points, summarize what stands out, and ask me one short question if you need direction.",
			},
		}
	}
	if draft == nil || app.hasActiveDeskNoteTitle(draft.Title) {
		return
	}
	if _, err := app.createDeskNote(*draft); err != nil {
		fmt.Fprintf(os.Stderr, "haven: folder offer note failed for %s: %v\n", folderStatus.ID, err)
	}
}

func folderAccessStatusByID(folderStatuses []loopgate.FolderAccessStatus, folderID string) (loopgate.FolderAccessStatus, bool) {
	for _, folderStatus := range folderStatuses {
		if folderStatus.ID == folderID {
			return folderStatus, true
		}
	}
	return loopgate.FolderAccessStatus{}, false
}
