package tui

import (
	"fmt"
	"io"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// RunWithWork creates a bubbletea program, launches workFn in a goroutine,
// and blocks until the program exits. workFn receives a send callback that
// wraps tea.Program.Send with a small yield to give the renderer time to
// draw between updates.
func RunWithWork(out io.Writer, model ProgressModel, workFn func(send func(tea.Msg))) error {
	p := tea.NewProgram(model, tea.WithOutput(out), tea.WithAltScreen())

	go func() {
		// Let bubbletea start its event loop and render the initial frame.
		time.Sleep(50 * time.Millisecond)

		workFn(func(msg tea.Msg) {
			p.Send(msg)
			// Small yield between sends so the renderer can draw frames.
			// For 60 cached rows (~120 messages) this adds ~600ms total,
			// which gives a nice visual sweep. For actual downloads the
			// delay is negligible compared to I/O time.
			time.Sleep(5 * time.Millisecond)
		})

		p.Send(WorkDoneMsg{})
	}()

	finalModel, err := p.Run()
	if err != nil {
		return err
	}
	if m, ok := finalModel.(ProgressModel); ok {
		// Print the full static table so all rows appear in terminal scrollback.
		fmt.Fprint(out, m.RenderFinalTable())
		if m.Err() != nil {
			return m.Err()
		}
	}
	return nil
}
