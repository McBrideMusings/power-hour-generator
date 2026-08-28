package playback

import (
	"fmt"

	"powerhour/internal/config"
	"powerhour/internal/project"
)

// ResolveOrder loads (or materializes) and reconciles the project's playback
// order, mirroring the load/materialize/reconcile sequence
// internal/cli/order.go's loadOrderForMutation already runs. It is
// deliberately read-only — it never calls Save. Only the `order` command
// persists the order file; finalize and the TUI panel both resolve against
// whatever is on disk (or, when nothing is on disk yet, a fresh in-memory
// materialization) without writing it back.
func ResolveOrder(projectRoot string, cfg config.Config, collections map[string]project.Collection) (Order, []Change, error) {
	prev, found, err := Load(projectRoot)
	if err != nil {
		return Order{}, nil, err
	}
	if !found {
		prev, err = Materialize(cfg, collections)
		if err != nil {
			return Order{}, nil, err
		}
	}

	return Reconcile(prev, cfg, collections)
}

// Placements maps an already-resolved Order onto the same
// project.TimelinePlacement shape project.BuildTimelinePlacements produces,
// so every existing consumer of that type (render.ResolveTimelineSegments,
// the TUI panel, VLC playback) is unaffected by where the order came from.
//
// A collection slot resolves through its RowID to that collection's row and
// so to the row's index. A file slot resolves to its source file — but the
// sequence-entry index it carries has to be recovered from the canonical
// (config-order) materialization, because render.InlineSegmentPath keys the
// __inline__ segment filename on that 0-based cfg.Timeline.Sequence index,
// not on anything the order itself stores. The recovery walks the canonical
// placements once, building a per-file queue of sequence indices consumed in
// order, so a file path used twice as a bookend still maps to two distinct
// indices.
func Placements(o Order, cfg config.Config, collections map[string]project.Collection) ([]project.TimelinePlacement, error) {
	canonical, err := project.BuildTimelinePlacements(cfg.Timeline, collections)
	if err != nil {
		return nil, err
	}

	fileSeqIndex := make(map[string][]int)
	for _, p := range canonical {
		if p.SourceFile != "" {
			fileSeqIndex[p.SourceFile] = append(fileSeqIndex[p.SourceFile], p.SequenceEntryIndex)
		}
	}

	result := make([]project.TimelinePlacement, 0, len(o.Slots))
	for i, s := range o.Slots {
		if s.File != "" {
			queue := fileSeqIndex[s.File]
			if len(queue) == 0 {
				return nil, fmt.Errorf("playback order slot %d: file %q has no matching timeline sequence entry", i+1, s.File)
			}
			seqIdx := queue[0]
			fileSeqIndex[s.File] = queue[1:]
			result = append(result, project.TimelinePlacement{
				SequenceEntryIndex: seqIdx,
				SourceFile:         s.File,
			})
			continue
		}

		coll, ok := collections[s.Collection]
		if !ok {
			return nil, fmt.Errorf("playback order slot %d: collection %q is not configured", i+1, s.Collection)
		}

		rowIndex, found := -1, false
		for _, row := range coll.Rows {
			if row.RowID == s.RowID {
				rowIndex = row.Index
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("playback order slot %d: row id %q not found in collection %q", i+1, s.RowID, s.Collection)
		}

		result = append(result, project.TimelinePlacement{
			Collection: s.Collection,
			RowIndex:   rowIndex,
			RowID:      s.RowID,
		})
	}

	return result, nil
}

// OrderedPlacements resolves the project's playback order and maps it to
// timeline placements in one call — the single function both `finalize` and
// the TUI's PLAYBACK ORDER panel use, per ADR 0003. Neither front end
// resolves order or reconciles it independently; they both call this.
func OrderedPlacements(projectRoot string, cfg config.Config, collections map[string]project.Collection) ([]project.TimelinePlacement, error) {
	order, _, err := ResolveOrder(projectRoot, cfg, collections)
	if err != nil {
		return nil, err
	}
	return Placements(order, cfg, collections)
}
