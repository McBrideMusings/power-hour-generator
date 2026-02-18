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
}

// ResolveTimeline produces an ordered list of timeline entries from the config
// sequence. It respects Count limits and Interleave rules, cycling through the
// interleave collection when it has fewer clips than insertion points.
func ResolveTimeline(timeline config.TimelineConfig, collections map[string]Collection) ([]TimelineEntry, error) {
	var entries []TimelineEntry
	seq := 0

	for _, entry := range timeline.Sequence {
		primary, err := requireCollection(collections, entry.Collection)
		if err != nil {
			return nil, err
		}

		rows := primary.Rows
		if entry.Count > 0 && entry.Count < len(rows) {
			rows = rows[:entry.Count]
		}

		if entry.Interleave == nil {
			for _, row := range rows {
				seq++
				entries = append(entries, TimelineEntry{
					Collection: entry.Collection,
					Index:      row.Index,
					Sequence:   seq,
				})
			}
			continue
		}

		secondary, err := requireCollection(collections, entry.Interleave.Collection)
		if err != nil {
			return nil, err
		}

		interleaveRows := secondary.Rows
		ilIdx := 0

		for i, row := range rows {
			seq++
			entries = append(entries, TimelineEntry{
				Collection: entry.Collection,
				Index:      row.Index,
				Sequence:   seq,
			})

			if len(interleaveRows) > 0 && (i+1)%entry.Interleave.Every == 0 {
				seq++
				ilRow := interleaveRows[ilIdx%len(interleaveRows)]
				entries = append(entries, TimelineEntry{
					Collection: entry.Interleave.Collection,
					Index:      ilRow.Index,
					Sequence:   seq,
				})
				ilIdx++
			}
		}
	}

	return entries, nil
}

func requireCollection(collections map[string]Collection, name string) (Collection, error) {
	c, ok := collections[name]
	if !ok {
		return Collection{}, fmt.Errorf("timeline references unknown collection %q", name)
	}
	return c, nil
}
