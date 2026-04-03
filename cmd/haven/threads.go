package main

import "morph/internal/haven/threadstore"

// NewThread creates a new empty thread and returns its summary.
func (app *HavenApp) NewThread() (threadstore.ThreadSummary, error) {
	return app.threadStore.NewThread()
}

// ListThreads returns all thread summaries, most recently updated first.
func (app *HavenApp) ListThreads() []threadstore.ThreadSummary {
	return app.threadStore.ListThreads()
}

// ListFolders returns distinct folder names across all threads.
func (app *HavenApp) ListFolders() []string {
	return app.threadStore.ListFolders()
}

// SetThreadFolder moves a thread into the given folder.
func (app *HavenApp) SetThreadFolder(threadID string, folder string) error {
	return app.threadStore.SetThreadFolder(threadID, folder)
}

// RenameThread sets the title of a thread.
func (app *HavenApp) RenameThread(threadID string, title string) error {
	return app.threadStore.RenameThread(threadID, title)
}

// LoadThread reads all user-visible conversation events from a thread.
// Orchestration events are filtered out — only user_message and
// assistant_message events are returned.
func (app *HavenApp) LoadThread(threadID string) ([]threadstore.ConversationEvent, error) {
	events, err := app.threadStore.LoadThread(threadID)
	if err != nil {
		return nil, err
	}

	visible := make([]threadstore.ConversationEvent, 0, len(events))
	for _, event := range events {
		if threadstore.IsUserVisible(event.Type) {
			visible = append(visible, event)
		}
	}
	return visible, nil
}
