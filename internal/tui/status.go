package tui

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// StatusWriter prints a spinning status line to a writer. It runs in
// the background and updates the current phase text in-place. Use this
// to show progress during setup phases before the main TUI starts.
type StatusWriter struct {
	w          io.Writer
	mu         sync.Mutex
	message    string
	phaseStart time.Time
	done       chan struct{}
	stopped    bool
}

// NewStatusWriter starts a background spinner that renders the current
// status message to w every 100ms.
func NewStatusWriter(w io.Writer) *StatusWriter {
	sw := &StatusWriter{
		w:          w,
		phaseStart: time.Now(),
		done:       make(chan struct{}),
	}
	go sw.loop()
	return sw
}

// Update changes the status message shown next to the spinner and resets
// the phase timer so elapsed time restarts from zero.
func (sw *StatusWriter) Update(msg string) {
	sw.mu.Lock()
	sw.message = msg
	sw.phaseStart = time.Now()
	sw.mu.Unlock()
}

// Stop clears the status line and stops the spinner.
func (sw *StatusWriter) Stop() {
	sw.mu.Lock()
	if sw.stopped {
		sw.mu.Unlock()
		return
	}
	sw.stopped = true
	sw.mu.Unlock()
	close(sw.done)
	// Clear the status line.
	fmt.Fprintf(sw.w, "\r\033[K")
}

func (sw *StatusWriter) loop() {
	tick := 0
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-sw.done:
			return
		case <-ticker.C:
			sw.mu.Lock()
			msg := sw.message
			start := sw.phaseStart
			sw.mu.Unlock()

			spinner := spinnerFrames[tick%len(spinnerFrames)]
			tick++
			elapsed := time.Since(start)
			fmt.Fprintf(sw.w, "\r\033[K%s %s (%s)", spinner, msg, formatElapsed(elapsed))
		}
	}
}

// formatElapsed formats a duration for display in the status line.
func formatElapsed(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < 10*time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}
