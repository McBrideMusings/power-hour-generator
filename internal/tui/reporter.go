package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"powerhour/internal/project"
	"powerhour/internal/render"
)

// RenderReporter adapts bubbletea message sending to the render.ProgressReporter
// interface. It uses caller-supplied functions to extract keys and fields so the
// tui package doesn't need to know about specific column layouts.
type RenderReporter struct {
	send             func(tea.Msg)
	keyFromSeg       func(render.Segment) string
	keyFromRes       func(render.Result) string
	keyFromClip      func(project.CollectionClip) string
	startFields      func(render.Segment) map[string]string
	completeFields   func(render.Result) map[string]string
	fetchingFields   func(project.CollectionClip) map[string]string
	fetchedFields    func(project.CollectionClip, render.Segment) map[string]string
	fetchErrorFields func(project.CollectionClip, error) map[string]string
}

// NewRenderReporter constructs a reporter with the given mapping functions.
// The fetch-phase functions (keyFromClip, fetchingFields, fetchedFields,
// fetchErrorFields) may be nil if the caller never reports fetch events.
func NewRenderReporter(
	send func(tea.Msg),
	keyFromSeg func(render.Segment) string,
	keyFromRes func(render.Result) string,
	keyFromClip func(project.CollectionClip) string,
	startFields func(render.Segment) map[string]string,
	completeFields func(render.Result) map[string]string,
	fetchingFields func(project.CollectionClip) map[string]string,
	fetchedFields func(project.CollectionClip, render.Segment) map[string]string,
	fetchErrorFields func(project.CollectionClip, error) map[string]string,
) *RenderReporter {
	return &RenderReporter{
		send:             send,
		keyFromSeg:       keyFromSeg,
		keyFromRes:       keyFromRes,
		keyFromClip:      keyFromClip,
		startFields:      startFields,
		completeFields:   completeFields,
		fetchingFields:   fetchingFields,
		fetchedFields:    fetchedFields,
		fetchErrorFields: fetchErrorFields,
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

// Fetching implements render.ProgressReporter.
func (r *RenderReporter) Fetching(clip project.CollectionClip) {
	if r.keyFromClip == nil || r.fetchingFields == nil {
		return
	}
	r.send(RowUpdateMsg{
		Key:    r.keyFromClip(clip),
		Fields: r.fetchingFields(clip),
	})
}

// Fetched implements render.ProgressReporter.
func (r *RenderReporter) Fetched(clip project.CollectionClip, seg render.Segment) {
	if r.keyFromClip == nil || r.fetchedFields == nil {
		return
	}
	r.send(RowUpdateMsg{
		Key:    r.keyFromClip(clip),
		Fields: r.fetchedFields(clip, seg),
	})
}

// FetchError implements render.ProgressReporter.
func (r *RenderReporter) FetchError(clip project.CollectionClip, err error) {
	if r.keyFromClip == nil || r.fetchErrorFields == nil {
		return
	}
	r.send(RowUpdateMsg{
		Key:    r.keyFromClip(clip),
		Fields: r.fetchErrorFields(clip, err),
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
