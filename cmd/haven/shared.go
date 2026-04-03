package main

import (
	"context"
	"fmt"

	"morph/internal/loopgate"
)

func (app *HavenApp) GetSharedFolderStatus() (loopgate.SharedFolderStatusResponse, error) {
	return app.loopgateClient.SharedFolderStatus(context.Background())
}

func (app *HavenApp) SyncSharedFolder() (loopgate.SharedFolderStatusResponse, error) {
	return app.syncSharedFolder(context.Background(), true)
}

func (app *HavenApp) syncSharedFolder(ctx context.Context, notify bool) (loopgate.SharedFolderStatusResponse, error) {
	folderAccessContext, cancelFolderAccess := withFolderAccessTimeout(ctx)
	defer cancelFolderAccess()

	statusResponse, err := app.loopgateClient.SyncSharedFolder(folderAccessContext)
	if err != nil {
		return loopgate.SharedFolderStatusResponse{}, err
	}

	havenPath := mapSandboxPathToHaven(statusResponse.SandboxRelativePath)
	if app.emitter != nil {
		app.emitter.Emit("haven:file_changed", map[string]interface{}{
			"action": "sync",
			"path":   havenPath,
		})
	}
	if notify {
		app.EmitToast("Shared space synced", fmt.Sprintf("Anything in %s now appears in Haven under %s.", statusResponse.Name, havenPath), "success")
	}

	return statusResponse, nil
}
