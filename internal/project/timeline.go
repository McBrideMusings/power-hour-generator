package project

import (
	"fmt"

	"powerhour/internal/config"
)

// TimelineEntry represents a single clip in the ordered timeline sequence.
type TimelineEntry struct {
	Collection  string
	Index       int    // 1-based index within the collection
	Sequence    int    // 1-based global sequence number across all entries
	SegmentPath string // empty at resolution time; populated by render service
	SourceFile  string // set for inline file entries (SequenceEntry.File); empty for collection entries
}

func ResolveTimeline(timeline config.TimelineConfig, collections map[string]Collection) ([]TimelineEntry, error) {
	placements, err := BuildTimelinePlacements(timeline, collections)
	if err != nil {
		return nil, err
	}

	entries := make([]TimelineEntry, 0, len(placements))
	for i, placement := range placements {
		entries = append(entries, TimelineEntry{
			Collection: placement.Collection,
			Index:      placement.RowIndex,
			Sequence:   i + 1,
			SourceFile: placement.SourceFile,
		})
	}
	return entries, nil
}

// ResolvePlacement returns the effective placement value, defaulting to "between".
func ResolvePlacement(p string) string {
	if p == "" {
		return "between"
	}
	return p
}

func requireCollection(collections map[string]Collection, name string) (Collection, error) {
	c, ok := collections[name]
	if !ok {
		return Collection{}, fmt.Errorf("timeline references unknown collection %q", name)
	}
	return c, nil
}
