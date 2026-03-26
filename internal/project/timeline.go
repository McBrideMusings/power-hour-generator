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

// ResolveTimeline produces an ordered list of timeline entries from the config
// sequence. It respects Count limits and Interleave rules, cycling through the
// interleave collection when it has fewer clips than insertion points.
//
// Collection references are stateful: each time a collection appears in the
// sequence, it resumes from where the previous reference left off. This allows
// splitting a collection across multiple sequence entries (e.g. two halves of a
// song list separated by an intermission) without specifying an explicit offset.
//
// Inline file entries (SequenceEntry.File != "") produce a single TimelineEntry
// with SourceFile set and no Collection/Index.
func ResolveTimeline(timeline config.TimelineConfig, collections map[string]Collection) ([]TimelineEntry, error) {
	var entries []TimelineEntry
	seq := 0
	cursor := make(map[string]int) // consumed row count per collection

	for _, entry := range timeline.Sequence {
		// Inline file entry.
		if entry.File != "" {
			seq++
			entries = append(entries, TimelineEntry{
				SourceFile: entry.File,
				Sequence:   seq,
			})
			continue
		}

		primary, err := requireCollection(collections, entry.Collection)
		if err != nil {
			return nil, err
		}

		start := cursor[entry.Collection]
		rows := primary.Rows
		if start >= len(rows) {
			// Collection exhausted; skip this entry silently.
			continue
		}
		rows = rows[start:]

		if entry.Count > 0 && entry.Count < len(rows) {
			rows = rows[:entry.Count]
		}

		cursor[entry.Collection] = start + len(rows)

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
		ilStart := cursor[entry.Interleave.Collection]
		ilAvail := len(interleaveRows) - ilStart
		if ilAvail <= 0 {
			// Cycle from the beginning.
			ilStart = 0
			ilAvail = len(interleaveRows)
		}

		ilIdx := 0

		for i, row := range rows {
			seq++
			entries = append(entries, TimelineEntry{
				Collection: entry.Collection,
				Index:      row.Index,
				Sequence:   seq,
			})

			if ilAvail > 0 && (i+1)%entry.Interleave.Every == 0 {
				seq++
				absIdx := ilStart + (ilIdx % ilAvail)
				ilRow := interleaveRows[absIdx]
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
