package playback

import (
	"powerhour/internal/config"
	"powerhour/internal/project"
)

// Materialize resolves the timeline into a canonical Order: the slot kinds
// and counts the current config + plan-file rows demand, with no memory of
// any previously stored order. It is the generator side of Reconcile — used
// both to seed a project's first playback-order.yaml and as the "canonical"
// input Reconcile diffs a stored order against.
func Materialize(cfg config.Config, collections map[string]project.Collection) (Order, error) {
	placements, err := project.BuildTimelinePlacements(cfg.Timeline, collections)
	if err != nil {
		return Order{}, err
	}

	slots := make([]Slot, 0, len(placements))
	for _, p := range placements {
		if p.SourceFile != "" {
			// File slots have no collection and no pool — this is the
			// absence of a pool, not a lock, so they never carry Locked.
			slots = append(slots, Slot{File: p.SourceFile})
			continue
		}
		slots = append(slots, Slot{Collection: p.Collection, RowID: p.RowID})
	}

	return Order{Version: 1, Slots: slots}, nil
}
