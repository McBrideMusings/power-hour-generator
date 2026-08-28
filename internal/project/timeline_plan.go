package project

import (
	"fmt"
	"sort"
	"strings"

	"powerhour/internal/config"
	"powerhour/pkg/csvplan"
)

// TimelinePlacement is a resolved clip position in timeline order.
type TimelinePlacement struct {
	SequenceEntryIndex int
	Collection         string
	RowIndex           int
	RowID              string
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
					RowID:              row.RowID,
				})
			}
			continue
		}

		secondary, err := requireCollection(collections, entry.Interleave.Collection)
		if err != nil {
			return nil, err
		}

		ilTotal := len(secondary.Rows)
		ilSelection := selectionOf(collections, entry.Interleave.Collection)
		ilStart := advance(cursor, entry.Interleave.Collection, ilSelection, ilTotal)

		every := entry.Interleave.Every
		if every <= 0 {
			every = 1
		}
		placement := ResolvePlacement(entry.Interleave.Placement)
		ilIdx := 0

		emitIL := func() {
			if ilTotal <= 0 {
				return
			}
			absIdx := ilStart + ilIdx
			if ilSelection == "repeat" {
				absIdx = absIdx % ilTotal
			} else if absIdx >= ilTotal {
				// "once" pool exhausted: stop silently rather than wrapping.
				return
			}
			ilRow := secondary.Rows[absIdx]
			placements = append(placements, TimelinePlacement{
				SequenceEntryIndex: entryIdx,
				Collection:         entry.Interleave.Collection,
				RowIndex:           ilRow.Index,
				RowID:              ilRow.RowID,
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
				RowID:              row.RowID,
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

		if ilTotal > 0 {
			if ilSelection == "repeat" {
				cursor[entry.Interleave.Collection] = (ilStart + ilIdx) % ilTotal
			} else {
				cursor[entry.Interleave.Collection] = ilStart + ilIdx
			}
		}
	}

	return placements, nil
}

// selectionOf returns the pool-consumption mode ("once" or "repeat")
// configured on the named collection, defaulting to "once" when the
// collection is unknown or its selection is unset/unrecognized. Selection is
// a property of the pool (CollectionConfig.Selection), never of the
// sequence/interleave entry that references it, per ADR 0002.
func selectionOf(collections map[string]Collection, name string) string {
	coll, ok := collections[name]
	if !ok {
		return "once"
	}
	selection := strings.ToLower(strings.TrimSpace(coll.Config.Selection))
	if selection != "once" && selection != "repeat" {
		return "once"
	}
	return selection
}

// advance returns the starting index into a pool of size total for the next
// interleave consumption, given the collection's current cursor and its
// selection mode. "repeat" cycles via modulo, so the returned start always
// lands inside [0, total). "once" returns the cursor unmodified so it can
// run past total — the caller detects exhaustion and stops instead of
// wrapping.
func advance(cursor map[string]int, name, selection string, total int) int {
	if selection == "repeat" {
		return cursor[name] % max(total, 1)
	}
	return cursor[name]
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
		// Set Config from the real project config so selectionOf resolves the
		// pool's actual selection here too — a zero-value Config would make
		// every interleave pool look like "once", silently changing which
		// rows receive sequence-entry fades.
		collections[name] = Collection{Name: name, Rows: rows, Config: cfg.Collections[name]}
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

// EffectiveCollectionFades resolves the per-row fade-in/fade-out seconds that
// the real render job would apply to each row of coll, including any
// timeline sequence-entry overrides layered on top of the collection's own
// fade config via ApplySequenceEntryFades. This is the single shared
// implementation of that resolution — used by both `status` and the TUI
// dashboard so the two surfaces agree on what a row's staleness hash should
// include.
func EffectiveCollectionFades(cfg config.Config, coll Collection) map[int][2]float64 {
	collCfg := cfg.Collections[coll.Name]
	baseIn, baseOut := config.ResolveFade(collCfg.Fade, collCfg.FadeIn, collCfg.FadeOut)

	clips := make([]CollectionClip, len(coll.Rows))
	for i, row := range coll.Rows {
		clip := Clip{
			ClipType:       ClipType(coll.Name),
			Row:            row.ToRow(),
			FadeInSeconds:  baseIn,
			FadeOutSeconds: baseOut,
		}
		clips[i] = CollectionClip{CollectionName: coll.Name, Clip: clip}
	}
	ApplySequenceEntryFades(cfg, clips)

	fades := make(map[int][2]float64, len(clips))
	for _, cc := range clips {
		fades[cc.Clip.Row.Index] = [2]float64{cc.Clip.FadeInSeconds, cc.Clip.FadeOutSeconds}
	}
	return fades
}
