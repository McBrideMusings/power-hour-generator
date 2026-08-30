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
func AnnotateClips(o Order, collections map[string]project.Collection, clips []project.CollectionClip) []project.CollectionClip {
	if len(clips) == 0 {
		return clips
	}
	pos := NewPositionIndex(o, collections)

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
// The position is counted WITHIN the row's own collection, not across the
// whole order: the 12th song is 12 even when 11 interstitials and two file
// bookends play before it. A power hour numbers its drinks, and an
// interstitial is not one — counting every slot makes the number a slot
// index, which is not a fact about the song and is not what gets burned in.
//
// **A `repeat` collection's rows get no position at all.** Such a row plays
// in several places, so there is no single position that is true of it, and
// any number picked from one of its slots is really a fact about that slot.
// Handing one out anyway was a churn engine: render.EffectiveRow substitutes
// the position into the row's Index for the filename, so the row's segment
// name was pinned to whichever slot happened to be its first — and every
// reorder moved that, renaming the file and making an already-rendered clip
// read as unrendered. With no position the name falls back to the plan
// index, which reordering cannot touch. Position 0 also keeps the segment
// hash free of it (see render.SegmentInputHash), so nothing re-encodes.
func NewPositionIndex(o Order, collections map[string]project.Collection) PositionIndex {
	slots := make(map[[2]string]int, len(o.Slots))
	counts := make(map[string]int)
	for _, s := range o.Slots {
		if s.File != "" {
			continue
		}
		if coll, ok := collections[s.Collection]; ok && coll.Config.SelectionValue() == config.SelectionRepeat {
			continue
		}
		counts[s.Collection]++
		k := [2]string{s.Collection, s.RowID}
		if _, seen := slots[k]; !seen {
			slots[k] = counts[s.Collection]
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
	return AnnotateClips(order, collections, clips), nil
}
