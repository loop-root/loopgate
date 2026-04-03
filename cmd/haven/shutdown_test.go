package main

import (
	"sync"
	"testing"
	"time"

	"morph/internal/config"
	"morph/internal/haven/threadstore"
	"morph/internal/tools"
)

type closableFakeLoopgateClient struct {
	*fakeLoopgateClient
	mu                 sync.Mutex
	closeIdleCallCount int
}

func (client *closableFakeLoopgateClient) CloseIdleConnections() {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.closeIdleCallCount++
}

func (client *closableFakeLoopgateClient) idleCloseCalls() int {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.closeIdleCallCount
}

func TestHavenApp_ShutdownCancelsRunningExecution(t *testing.T) {
	blockChannel := make(chan struct{})
	loopgateClient := &slowModelClient{
		fakeLoopgateClient: &fakeLoopgateClient{},
		blockCh:            blockChannel,
	}

	store, err := threadstore.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	emitter := &recordingEmitter{}
	app := NewHavenApp(loopgateClient, store, tools.NewRegistry(), config.Persona{}, config.Policy{}, nil, emitter, t.TempDir(), t.TempDir())
	app.presence = NewPresenceManager(emitter, "Morph")
	app.idleManager = NewIdleManager(app)

	thread, err := app.NewThread()
	if err != nil {
		t.Fatalf("new thread: %v", err)
	}

	response := app.SendMessage(thread.ThreadID, "Hello")
	if !response.Accepted {
		t.Fatalf("message rejected: %s", response.Reason)
	}

	waitForState(t, app, thread.ThreadID, threadstore.ExecutionRunning, time.Second)

	app.Shutdown()

	waitForDone(t, app, thread.ThreadID, 2*time.Second)

	if state := app.GetExecutionState(thread.ThreadID); state != threadstore.ExecutionCancelled {
		t.Fatalf("expected cancelled state after shutdown, got %s", state)
	}
}

func TestHavenApp_ShutdownIsIdempotentAndClosesIdleConnections(t *testing.T) {
	loopgateClient := &closableFakeLoopgateClient{
		fakeLoopgateClient: &fakeLoopgateClient{},
	}

	app, emitter := testApp(t, loopgateClient.fakeLoopgateClient)
	app.loopgateClient = loopgateClient
	app.presence = NewPresenceManager(emitter, "Morph")
	app.idleManager = NewIdleManager(app)

	app.Shutdown()
	app.Shutdown()

	if calls := loopgateClient.idleCloseCalls(); calls != 1 {
		t.Fatalf("expected CloseIdleConnections once, got %d", calls)
	}
}
