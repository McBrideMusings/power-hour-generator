package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"powerhour/internal/render"
)

// RenderReporter adapts bubbletea message sending to the render.ProgressReporter
// interface. It uses caller-supplied functions to extract keys and fields so the
// tui package doesn't need to know about specific column layouts.
type RenderReporter struct {
	send           func(tea.Msg)
	keyFromSeg     func(render.Segment) string
	keyFromRes     func(render.Result) string
	startFields    func(render.Segment) map[string]string
	completeFields func(render.Result) map[string]string
}

// NewRenderReporter constructs a reporter with the given mapping functions.
func NewRenderReporter(
	send func(tea.Msg),
	keyFromSeg func(render.Segment) string,
	keyFromRes func(render.Result) string,
	startFields func(render.Segment) map[string]string,
	completeFields func(render.Result) map[string]string,
) *RenderReporter {
	return &RenderReporter{
		send:           send,
		keyFromSeg:     keyFromSeg,
		keyFromRes:     keyFromRes,
		startFields:    startFields,
		completeFields: completeFields,
	}
}

// Start implements render.ProgressReporter.
func (r *RenderReporter) Start(seg render.Segment) {
	r.send(RowUpdateMsg{
		Key:    r.keyFromSeg(seg),
		Fields: r.startFields(seg),
	})
}

// Progress implements render.ProgressReporter.
func (r *RenderReporter) Progress(seg render.Segment, pct float64) {
	r.send(RowUpdateMsg{
		Key:    r.keyFromSeg(seg),
		Fields: map[string]string{"STATUS": FormatProgressBar(pct)},
	})
}

// Complete implements render.ProgressReporter.
func (r *RenderReporter) Complete(res render.Result) {
	r.send(RowUpdateMsg{
		Key:    r.keyFromRes(res),
		Fields: r.completeFields(res),
	})
}

// FormatProgressBar renders a compact progress bar for the STATUS column.
// Uses ASCII characters to avoid multi-byte UTF-8 truncation issues.
// Output fits within 10 characters: "===-- 100%"
func FormatProgressBar(pct float64) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	const barWidth = 5
	filled := int(pct * barWidth)
	if filled > barWidth {
		filled = barWidth
	}
	bar := make([]byte, barWidth)
	for i := range bar {
		if i < filled {
			bar[i] = '='
		} else {
			bar[i] = '-'
		}
	}
	return fmt.Sprintf("%s %3d%%", string(bar), int(pct*100))
}
