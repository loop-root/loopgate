package ui

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// Spinner renders an inline animated indicator while a blocking operation runs.
// It overwrites the same terminal line using \r and clears itself when stopped.
//
// Usage:
//
//	s := ui.NewSpinner("thinking")
//	s.Start()
//	result := doExpensiveThing()
//	s.Stop()
type Spinner struct {
	label    string
	interval time.Duration
	mu       sync.Mutex
	running  bool
	done     chan struct{}
}

// Morphing diamond frames — on-theme with the ◈ prompt glyph.
// The animation cycles through crystalline forms: empty → faceted → solid → faceted.
var spinnerFrames = []string{"◇", "◈", "◆", "◈"}

// NewSpinner creates a spinner with the given label text.
func NewSpinner(label string) *Spinner {
	return &Spinner{
		label:    label,
		interval: 180 * time.Millisecond,
	}
}

// Start begins the animation in a background goroutine.
// Safe to call multiple times — only the first call has effect.
func (s *Spinner) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return
	}
	s.running = true
	s.done = make(chan struct{})
	go s.loop()
}

// Stop halts the animation and clears the spinner line.
// Blocks until the animation goroutine exits.
func (s *Spinner) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.done)
	s.mu.Unlock()

	// Wait briefly for the goroutine to clear the line.
	// The goroutine writes the clear sequence on exit.
	time.Sleep(s.interval + 20*time.Millisecond)
}

func (s *Spinner) loop() {
	frame := 0
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			s.clearLine()
			return
		case <-ticker.C:
			s.render(frame)
			frame = (frame + 1) % len(spinnerFrames)
		}
	}
}

func (s *Spinner) render(frame int) {
	if !colorable {
		return
	}

	glyph := Pink(spinnerFrames[frame])
	label := Dim(s.label)

	// Build the trail — previous frames shown fading behind the active one.
	trail := ""
	for i := 1; i <= 2; i++ {
		prev := (frame - i + len(spinnerFrames)) % len(spinnerFrames)
		trail += Dim(spinnerFrames[prev])
	}

	line := fmt.Sprintf("  %s %s %s", glyph, label, trail)
	fmt.Fprintf(os.Stderr, "\r\x1b[K%s", line)
}

func (s *Spinner) clearLine() {
	if !colorable {
		return
	}
	fmt.Fprintf(os.Stderr, "\r\x1b[K")
}
