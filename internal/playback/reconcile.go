package playback

import (
	"sort"
	"strings"

	"powerhour/internal/config"
	"powerhour/internal/project"
)

// ChangeKind categorizes a single mutation Reconcile made while producing an
// order from a stored one. Purely informational — Reconcile does no
// rendering, it only surfaces what happened for a caller to display.
type ChangeKind string

const (
	// ChangeDropped: a stored slot no longer resolves against what the
	// current timeline demands (its row or file was removed, or the
	// timeline no longer has room for it) and was removed.
	ChangeDropped ChangeKind = "dropped"
	// ChangeAdded: a pool row belonging to a "once" collection occupied no
	// slot in the reconciled order and was appended.
	ChangeAdded ChangeKind = "added"
	// ChangeFilled: the generator supplied a slot for a canonical position
	// the stored order had no surviving slot for.
	ChangeFilled ChangeKind = "filled"
)

// Change describes one slot-level mutation Reconcile made, for callers that
// want to display what changed between the stored order and the reconciled
// result.
type Change struct {
	Kind       ChangeKind
	Collection string
	RowID      string
	File       string
	Detail     string
}

// Reconcile merges a stored playback Order with what the current timeline
// config demands. The stored order always wins — nothing already placed
// ever moves — and the generator (Materialize) only fills gaps: positions a
// stored slot no longer covers, either because a row/file was dropped or
// because the canonical order now demands more slots of a kind than the
// stored order has surviving ones for.
//
// slice: on a SequenceEntry is a seeding instruction only — it decides the
// canonical slot allocation on the first Materialize, and describes nothing
// about the order thereafter, since a stored slot's position never depends
// on where BuildTimelinePlacements would put it today. A "once" collection
// row that a restrictive slice: excludes from the current canonical demand
// still gets a slot, via the leftover-append pass below.
func Reconcile(prev Order, cfg config.Config, collections map[string]project.Collection) (Order, []Change, error) {
	canonical, err := Materialize(cfg, collections)
	if err != nil {
		return Order{}, nil, err
	}

	canonicalByKind := make(map[string][]Slot)
	for _, cs := range canonical.Slots {
		k := kindOf(cs)
		canonicalByKind[k] = append(canonicalByKind[k], cs)
	}

	// A repeat collection's kind is constrained only by how many slots the
	// timeline demands, never by how many times a particular row appears:
	// Materialize cycles the pool to fill those slots, so the counts it
	// happens to produce are an artifact of cycling, not a rule. Holding a
	// stored slot to them makes any hand-picked occupant that is already "at
	// quota" fail to resolve — it is dropped and a leftover canonical row
	// takes the position, so the choice silently reverts on the next load.
	repeatKind := make(map[string]bool, len(canonicalByKind))
	poolOf := make(map[string]map[string]bool, len(collections))
	for name, coll := range collections {
		if coll.Config.SelectionValue() != config.SelectionRepeat {
			continue
		}
		repeatKind["collection:"+name] = true
		ids := make(map[string]bool, len(coll.Rows))
		for _, row := range coll.Rows {
			if row.RowID != "" {
				ids[row.RowID] = true
			}
		}
		poolOf[name] = ids
	}
	takenByKind := make(map[string]int, len(canonicalByKind))

	// remaining[kind][identity] tracks how many more canonical occupants of
	// that identity are still unclaimed — the multiset a stored slot must
	// draw against to "resolve". identity is the row id for a collection
	// slot and the file path for a file slot.
	remaining := make(map[string]map[string]int, len(canonicalByKind))
	for k, list := range canonicalByKind {
		m := make(map[string]int, len(list))
		for _, s := range list {
			m[identityOf(s)]++
		}
		remaining[k] = m
	}

	var changes []Change

	// Claim survivors against the canonical multiset, in the stored order's
	// own order, dropping anything the multiset has no room left for —
	// whether because the row/file is gone entirely or because the
	// timeline no longer demands as many of that kind.
	survivorsByKind := make(map[string][]Slot)
	for _, s := range prev.Slots {
		k := kindOf(s)
		id := identityOf(s)
		if repeatKind[k] {
			// Any pool row is a legal occupant of any slot of this kind; the
			// only limit is the number of slots the timeline demands.
			if poolOf[s.Collection][id] && takenByKind[k] < len(canonicalByKind[k]) {
				takenByKind[k]++
				survivorsByKind[k] = append(survivorsByKind[k], s)
				continue
			}
			changes = append(changes, Change{
				Kind: ChangeDropped, Collection: s.Collection, RowID: s.RowID, File: s.File,
				Detail: "no longer resolves against the current timeline/pools",
			})
			continue
		}
		if remaining[k][id] > 0 {
			remaining[k][id]--
			survivorsByKind[k] = append(survivorsByKind[k], s)
			continue
		}
		changes = append(changes, Change{
			Kind: ChangeDropped, Collection: s.Collection, RowID: s.RowID, File: s.File,
			Detail: "no longer resolves against the current timeline/pools",
		})
	}

	// Filler multisets: canonical occupants left over once survivors have
	// claimed theirs, in canonical order.
	fillerByKind := make(map[string][]Slot, len(canonicalByKind))
	for k, list := range canonicalByKind {
		if repeatKind[k] {
			// Positions the survivors do not cover take the canonical
			// occupant Materialize put there, which keeps the cycling
			// pattern for the tail of the run.
			fillerByKind[k] = list[min(takenByKind[k], len(list)):]
			continue
		}
		left := make(map[string]int, len(remaining[k]))
		for id, n := range remaining[k] {
			left[id] = n
		}
		var filler []Slot
		for _, s := range list {
			id := identityOf(s)
			if left[id] > 0 {
				filler = append(filler, s)
				left[id]--
			}
		}
		fillerByKind[k] = filler
	}

	// Walk canonical positions by kind: a surviving stored slot of that
	// kind, taken in the stored order's own order, wins the position;
	// otherwise a leftover canonical occupant fills the gap. Multiset
	// conservation (survivors ⊆ canonical, filler = canonical − survivors)
	// guarantees a slot is always available here.
	usedRowIDs := make(map[string]map[string]bool)
	markUsed := func(s Slot) {
		if s.Collection == "" {
			return
		}
		if usedRowIDs[s.Collection] == nil {
			usedRowIDs[s.Collection] = make(map[string]bool)
		}
		usedRowIDs[s.Collection][s.RowID] = true
	}

	result := make([]Slot, 0, len(canonical.Slots))
	for _, cs := range canonical.Slots {
		k := kindOf(cs)
		if q := survivorsByKind[k]; len(q) > 0 {
			chosen := q[0]
			survivorsByKind[k] = q[1:]
			result = append(result, chosen)
			markUsed(chosen)
			continue
		}
		q := fillerByKind[k]
		chosen := q[0]
		fillerByKind[k] = q[1:]
		result = append(result, chosen)
		markUsed(chosen)
		changes = append(changes, Change{
			Kind: ChangeFilled, Collection: chosen.Collection, RowID: chosen.RowID, File: chosen.File,
			Detail: "generator filled a position with no surviving stored slot",
		})
	}

	// For each "once" collection, append any pool row occupying no slot in
	// the reconciled order — a restrictive slice: on the sequence entry (or
	// a row added to the plan file since the order was last materialized)
	// can leave rows outside the current canonical demand entirely.
	names := make([]string, 0, len(collections))
	for name := range collections {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		coll := collections[name]
		selection := strings.ToLower(strings.TrimSpace(coll.Config.Selection))
		if selection == "" {
			selection = "once"
		}
		if selection != "once" {
			continue
		}
		for _, row := range coll.Rows {
			if row.RowID == "" || usedRowIDs[name][row.RowID] {
				continue
			}
			result = append(result, Slot{Collection: name, RowID: row.RowID})
			changes = append(changes, Change{
				Kind: ChangeAdded, Collection: name, RowID: row.RowID,
				Detail: "unplaced row in a once-selection collection appended",
			})
		}
	}

	return Order{Version: 1, Slots: result}, changes, nil
}

func kindOf(s Slot) string {
	if s.Collection != "" {
		return "collection:" + s.Collection
	}
	return "file:" + s.File
}

func identityOf(s Slot) string {
	if s.Collection != "" {
		return s.RowID
	}
	return s.File
}
