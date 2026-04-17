package project

import (
	"fmt"
	"sort"

	"powerhour/internal/config"
	"powerhour/pkg/csvplan"
)

// TimelinePlacement is a resolved clip position in timeline order.
type TimelinePlacement struct {
	SequenceEntryIndex int
	Collection         string
	RowIndex           int
	SourceFile         string
	Interleaved        bool
}

// BuildTimelinePlacements resolves the timeline into ordered placements.
func BuildTimelinePlacements(timeline config.TimelineConfig, collections map[string]Collection) ([]TimelinePlacement, error) {
	var placements []TimelinePlacement
	cursor := make(map[string]int)

	for entryIdx, entry := range timeline.Sequence {
		if entry.File != "" {
			placements = append(placements, TimelinePlacement{
				SequenceEntryIndex: entryIdx,
				SourceFile:         entry.File,
			})
			continue
		}

		primary, err := requireCollection(collections, entry.Collection)
		if err != nil {
			return nil, err
		}

		selected, err := selectCollectionRows(primary.Rows, cursor[entry.Collection], entry.Slice)
		if err != nil {
			return nil, fmt.Errorf("timeline sequence[%d] (%q): %w", entryIdx, entry.Collection, err)
		}
		cursor[entry.Collection] = selected.nextCursor

		if len(selected.rows) == 0 {
			continue
		}

		if entry.Interleave == nil {
			for _, row := range selected.rows {
				placements = append(placements, TimelinePlacement{
					SequenceEntryIndex: entryIdx,
					Collection:         entry.Collection,
					RowIndex:           row.Index,
				})
			}
			continue
		}

		secondary, err := requireCollection(collections, entry.Interleave.Collection)
		if err != nil {
			return nil, err
		}

		ilStart := cursor[entry.Interleave.Collection]
		ilAvail := len(secondary.Rows) - ilStart
		if ilAvail <= 0 {
			ilStart = 0
			ilAvail = len(secondary.Rows)
		}

		every := entry.Interleave.Every
		if every <= 0 {
			every = 1
		}
		placement := ResolvePlacement(entry.Interleave.Placement)
		ilIdx := 0

		emitIL := func() {
			if ilAvail <= 0 {
				return
			}
			absIdx := ilStart + (ilIdx % ilAvail)
			ilRow := secondary.Rows[absIdx]
			placements = append(placements, TimelinePlacement{
				SequenceEntryIndex: entryIdx,
				Collection:         entry.Interleave.Collection,
				RowIndex:           ilRow.Index,
				Interleaved:        true,
			})
			ilIdx++
		}

		for i, row := range selected.rows {
			isLast := i == len(selected.rows)-1

			if placement == "before" || placement == "around" {
				if i%every == 0 {
					emitIL()
				}
			}

			placements = append(placements, TimelinePlacement{
				SequenceEntryIndex: entryIdx,
				Collection:         entry.Collection,
				RowIndex:           row.Index,
			})

			switch placement {
			case "after":
				if (i+1)%every == 0 {
					emitIL()
				}
			case "between":
				if (i+1)%every == 0 && !isLast {
					emitIL()
				}
			case "around":
				if isLast {
					emitIL()
				}
			}
		}

		if ilAvail > 0 {
			cursor[entry.Interleave.Collection] = ilStart + (ilIdx % ilAvail)
		}
	}

	return placements, nil
}

type selectedCollectionRows struct {
	rows       []csvplan.CollectionRow
	nextCursor int
}

func selectCollectionRows(rows []csvplan.CollectionRow, cursor int, slice string) (selectedCollectionRows, error) {
	if cursor >= len(rows) {
		return selectedCollectionRows{nextCursor: len(rows)}, nil
	}
	start, end, err := config.ResolveTimelineSlice(slice, len(rows)-cursor)
	if err != nil {
		return selectedCollectionRows{}, err
	}
	return selectedCollectionRows{
		rows:       rows[cursor+start : cursor+end],
		nextCursor: cursor + end,
	}, nil
}

// ApplySequenceEntryFades applies per-entry fade overrides to primary clips.
func ApplySequenceEntryFades(cfg config.Config, clips []CollectionClip) {
	byCollection := make(map[string]map[int]int)
	for i, cc := range clips {
		if byCollection[cc.CollectionName] == nil {
			byCollection[cc.CollectionName] = make(map[int]int)
		}
		byCollection[cc.CollectionName][cc.Clip.Row.Index] = i
	}

	collections := make(map[string]Collection, len(byCollection))
	for name, indices := range byCollection {
		rows := make([]csvplan.CollectionRow, 0, len(indices))
		for rowIndex := range indices {
			rows = append(rows, csvplan.CollectionRow{Index: rowIndex})
		}
		sort.Slice(rows, func(i, j int) bool {
			return rows[i].Index < rows[j].Index
		})
		collections[name] = Collection{Name: name, Rows: rows}
	}

	placements, err := BuildTimelinePlacements(cfg.Timeline, collections)
	if err != nil {
		return
	}

	for _, placement := range placements {
		if placement.SourceFile != "" || placement.Interleaved {
			continue
		}
		if placement.SequenceEntryIndex < 0 || placement.SequenceEntryIndex >= len(cfg.Timeline.Sequence) {
			continue
		}
		entry := cfg.Timeline.Sequence[placement.SequenceEntryIndex]
		if entry.Fade == 0 && entry.FadeIn == 0 && entry.FadeOut == 0 {
			continue
		}
		indices := byCollection[placement.Collection]
		if indices == nil {
			continue
		}
		idx, ok := indices[placement.RowIndex]
		if !ok {
			continue
		}
		fadeIn, fadeOut := config.ResolveFade(entry.Fade, entry.FadeIn, entry.FadeOut)
		clips[idx].Clip.FadeInSeconds = fadeIn
		clips[idx].Clip.FadeOutSeconds = fadeOut
	}
}
