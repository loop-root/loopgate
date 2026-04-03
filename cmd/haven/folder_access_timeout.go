package main

import (
	"context"
	"time"
)

const folderAccessRequestTimeout = 2 * time.Minute

func withFolderAccessTimeout(parentContext context.Context) (context.Context, context.CancelFunc) {
	if parentContext == nil {
		parentContext = context.Background()
	}
	if _, hasDeadline := parentContext.Deadline(); hasDeadline {
		return parentContext, func() {}
	}
	return context.WithTimeout(parentContext, folderAccessRequestTimeout)
}
