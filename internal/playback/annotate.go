package playback

import (
	"powerhour/internal/config"
	"powerhour/internal/project"
)

// AnnotateClips stamps each clip with its 1-based position in the playback
// order.
//
// The collection resolver walks collections, not the timeline — it produces
// every row of every collection, in map order, and knows nothing about where
// any of them plays. The position therefore has to be joined in, keyed on
// the one thing both sides agree about: the pair (collection, row id).
//
// Per ADR 0003 this is the single domain implementation; both the CLI render
// path and the TUI dashboard call it rather than walking slots themselves.
//
// A clip whose row occupies no slot keeps position 0, and the render layer
// falls back to the plan row index for it. Clips are returned as a new slice;
// the input is not modified.
func AnnotateClips(o Order, clips []project.CollectionClip) []project.CollectionClip {
	if len(clips) == 0 {
		return clips
	}
	pos := NewPositionIndex(o)

	out := make([]project.CollectionClip, len(clips))
	copy(out, clips)
	for i := range out {
		out[i].Clip.PlaybackPosition = pos.Of(out[i].CollectionName, out[i].Clip.Row.RowID)
	}
	return out
}

// PositionIndex answers "where does this row play" for a resolved order. It
// exists so surfaces that build a project.Clip by hand — the dashboard's row
// states and VLC path resolution, the status command — reach the same
// position the render path does, without any of them walking slots.
type PositionIndex struct {
	slots map[[2]string]int
}

// NewPositionIndex builds the lookup for a resolved order.
//
// A row that occupies several slots (a repeat-selection pool) takes the
// first of them: one row produces one rendered file, so it can only burn in
// one number.
func NewPositionIndex(o Order) PositionIndex {
	slots := make(map[[2]string]int, len(o.Slots))
	for i, s := range o.Slots {
		if s.File != "" {
			continue
		}
		k := [2]string{s.Collection, s.RowID}
		if _, seen := slots[k]; !seen {
			slots[k] = i + 1
		}
	}
	return PositionIndex{slots: slots}
}

// Of returns the 1-based playback position of a row, or 0 when the row
// occupies no slot in the order.
func (p PositionIndex) Of(collection, rowID string) int {
	if p.slots == nil || rowID == "" {
		return 0
	}
	return p.slots[[2]string{collection, rowID}]
}

// AnnotateClipsFromProject resolves the project's playback order and
// annotates the clips with it in one call — the entry point both front ends
// use, so neither resolves the order for this purpose on its own.
func AnnotateClipsFromProject(projectRoot string, cfg config.Config, collections map[string]project.Collection, clips []project.CollectionClip) ([]project.CollectionClip, error) {
	order, _, err := ResolveOrder(projectRoot, cfg, collections)
	if err != nil {
		return nil, err
	}
	return AnnotateClips(order, clips), nil
}
