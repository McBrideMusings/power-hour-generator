package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"powerhour/internal/render"
)

// RenderReporter adapts bubbletea message sending to the render.ProgressReporter
// interface. It uses caller-supplied functions to extract keys and fields so the
// tui package doesn't need to know about specific column layouts.
type RenderReporter struct {
	send      func(tea.Msg)
	keyFromSeg func(render.Segment) string
	keyFromRes func(render.Result) string
	startFields func(render.Segment) map[string]string
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

// Complete implements render.ProgressReporter.
func (r *RenderReporter) Complete(res render.Result) {
	r.send(RowUpdateMsg{
		Key:    r.keyFromRes(res),
		Fields: r.completeFields(res),
	})
}
