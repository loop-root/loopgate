package signal

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// Handler manages graceful shutdown on SIGINT/SIGTERM.
type Handler struct {
	sigCh  chan os.Signal
	doneCh chan struct{}
}

// NewHandler creates a signal handler that listens for interrupt signals.
func NewHandler() *Handler {
	handler := &Handler{
		sigCh:  make(chan os.Signal, 1),
		doneCh: make(chan struct{}),
	}

	signal.Notify(handler.sigCh, syscall.SIGINT, syscall.SIGTERM)

	return handler
}

// Wait blocks until a shutdown signal is received.
// Returns the signal that triggered shutdown.
func (h *Handler) Wait() os.Signal {
	sig := <-h.sigCh
	return sig
}

// Context returns a context that is canceled when a shutdown signal is received.
func (h *Handler) Context() context.Context {
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		<-h.sigCh
		cancel()
	}()

	return ctx
}

// Done returns a channel that is closed when shutdown is requested.
func (h *Handler) Done() <-chan struct{} {
	go func() {
		<-h.sigCh
		close(h.doneCh)
	}()

	return h.doneCh
}

// Stop stops listening for signals.
func (h *Handler) Stop() {
	signal.Stop(h.sigCh)
	close(h.sigCh)
}

// ShutdownSequence represents the steps to perform during graceful shutdown.
type ShutdownSequence struct {
	steps []func() error
}

// NewShutdownSequence creates an empty shutdown sequence.
func NewShutdownSequence() *ShutdownSequence {
	return &ShutdownSequence{}
}

// Add adds a step to the shutdown sequence.
// Steps are executed in the order they are added.
func (s *ShutdownSequence) Add(step func() error) {
	s.steps = append(s.steps, step)
}

// Run executes all shutdown steps in order.
// Returns the first error encountered, but continues executing all steps.
func (s *ShutdownSequence) Run() error {
	var firstErr error
	for _, step := range s.steps {
		if err := step(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
