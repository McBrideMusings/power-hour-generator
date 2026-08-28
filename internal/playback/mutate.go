package playback

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	"powerhour/internal/config"
	"powerhour/internal/project"
)

// checkIndex bounds-checks a slot index against the order.
func checkIndex(o *Order, i int) error {
	if i < 0 || i >= len(o.Slots) {
		return fmt.Errorf("slot %d out of range (order has %d slots)", i, len(o.Slots))
	}
	return nil
}

// mutableSlot rejects a file slot. A file entry has no collection and no
// pool — that is the absence of a pool, not a lock — so it always holds its
// position and never participates in Swap, Set, or SetLock.
func mutableSlot(o *Order, i int) error {
	if s := o.Slots[i]; s.File != "" {
		return fmt.Errorf("slot %d is a file entry (%s): it has no collection or pool, so it holds its position", i, s.File)
	}
	return nil
}

// Swap exchanges the occupants of two slots (0-based indices). Swapping a
// locked slot is allowed — an explicit gesture beats an implicit hold; locks
// only exclude a slot from Shuffle. a == b is a no-op, not an error. Locking
// is a statement about a position, not an occupant (package doc), so only
// Collection/RowID/File move — each slot keeps its own Locked flag.
func Swap(o *Order, a, b int) error {
	if err := checkIndex(o, a); err != nil {
		return err
	}
	if err := checkIndex(o, b); err != nil {
		return err
	}
	if a == b {
		return nil
	}
	if err := mutableSlot(o, a); err != nil {
		return err
	}
	if err := mutableSlot(o, b); err != nil {
		return err
	}
	o.Slots[a].Collection, o.Slots[b].Collection = o.Slots[b].Collection, o.Slots[a].Collection
	o.Slots[a].RowID, o.Slots[b].RowID = o.Slots[b].RowID, o.Slots[a].RowID
	o.Slots[a].File, o.Slots[b].File = o.Slots[b].File, o.Slots[a].File
	return nil
}

// Set assigns a specific row id to a slot (0-based index), leaving
// Collection and Locked untouched. Whether rowID actually belongs to that
// collection's pool is the caller's concern — Set is pure index/field work.
func Set(o *Order, slot int, rowID string) error {
	if err := checkIndex(o, slot); err != nil {
		return err
	}
	if err := mutableSlot(o, slot); err != nil {
		return err
	}
	rowID = strings.TrimSpace(rowID)
	if rowID == "" {
		return fmt.Errorf("slot %d: row id must not be empty", slot)
	}
	o.Slots[slot].RowID = rowID
	return nil
}

// Cycle steps one slot's occupant to the next or previous row in pool order,
// wrapping at both ends. delta is normally +1 or -1; 0 is a no-op.
//
// The step honours the collection's selection the same way Shuffle does:
//
//   - config.SelectionRepeat: the slot is simply reassigned. Two slots may
//     end up holding the same row, which is what repeat means.
//   - config.SelectionOnce: if the incoming row already occupies another
//     slot, the two slots exchange occupants, so every row still holds
//     exactly one slot. A row that occupies no slot is assigned directly.
//
// The occupant's position in pool is where the walk starts. A slot holding a
// row that is not in pool starts before the first entry, so delta +1 lands on
// pool[0].
func Cycle(o *Order, slot int, sel config.Selection, pool []string, delta int) error {
	if err := checkIndex(o, slot); err != nil {
		return err
	}
	if err := mutableSlot(o, slot); err != nil {
		return err
	}
	if len(pool) == 0 {
		return fmt.Errorf("slot %d: cycle needs a non-empty pool", slot)
	}
	if delta == 0 {
		return nil
	}

	cur := -1
	for i, id := range pool {
		if id == o.Slots[slot].RowID {
			cur = i
			break
		}
	}

	n := len(pool)
	var next int
	if cur < 0 {
		// Not in the pool: step in from whichever end delta points at.
		if delta > 0 {
			next = (delta - 1) % n
		} else {
			next = ((n+delta)%n + n) % n
		}
	} else {
		next = ((cur+delta)%n + n) % n
	}
	target := pool[next]
	if target == o.Slots[slot].RowID {
		return nil
	}

	if config.ParseSelection(string(sel)) == config.SelectionOnce {
		for i := range o.Slots {
			if i != slot && o.Slots[i].Collection == o.Slots[slot].Collection && o.Slots[i].RowID == target {
				return Swap(o, slot, i)
			}
		}
	}
	return Set(o, slot, target)
}

// SetLock sets a slot's locked state (0-based index). A file slot has
// nothing to lock — it cannot move anyway — so it is rejected the same way
// Swap and Set reject it.
func SetLock(o *Order, slot int, locked bool) error {
	if err := checkIndex(o, slot); err != nil {
		return err
	}
	if err := mutableSlot(o, slot); err != nil {
		return err
	}
	o.Slots[slot].Locked = locked
	return nil
}

// Shuffle redraws or permutes the occupants of one collection's slots.
// Scope is GLOBAL: the group spans the whole order regardless of which
// sequence entry produced each slot, so a song can cross a `file:` bookend —
// it is never scoped to a sequence entry or a contiguous run. Locked slots
// are excluded from the group and never touched. Behavior comes only from
// sel — never from the collection's name (ADR 0002).
//
//   - config.SelectionOnce: the RowIDs currently occupying the group are
//     permuted (Fisher-Yates) — every row keeps exactly one slot.
//   - config.SelectionRepeat: each slot in the group is redrawn uniformly
//     from pool, independently.
//
// rng may be nil, in which case a time-seeded source is used — callers that
// need determinism (tests, and any caller that wants reproducibility) must
// pass their own.
func Shuffle(o *Order, collection string, sel config.Selection, pool []string, rng *rand.Rand) error {
	var group []int
	for i, s := range o.Slots {
		if s.Collection == collection && !s.Locked {
			group = append(group, i)
		}
	}
	if len(group) == 0 {
		return nil
	}

	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}

	switch config.ParseSelection(string(sel)) {
	case config.SelectionOnce:
		occupants := make([]string, len(group))
		for i, idx := range group {
			occupants[i] = o.Slots[idx].RowID
		}
		rng.Shuffle(len(occupants), func(i, j int) {
			occupants[i], occupants[j] = occupants[j], occupants[i]
		})
		for i, idx := range group {
			o.Slots[idx].RowID = occupants[i]
		}
	case config.SelectionRepeat:
		if len(pool) == 0 {
			return fmt.Errorf("collection %q: shuffle needs a non-empty pool", collection)
		}
		for _, idx := range group {
			o.Slots[idx].RowID = pool[rng.Intn(len(pool))]
		}
	default:
		return fmt.Errorf("collection %q: unknown selection %q", collection, sel)
	}

	return nil
}

// Pool returns the RowIDs of coll's rows, in plan order, skipping any row
// that has no id (a row-less collection, or one loaded before ids were
// assigned).
func Pool(coll project.Collection) []string {
	ids := make([]string, 0, len(coll.Rows))
	for _, row := range coll.Rows {
		if row.RowID == "" {
			continue
		}
		ids = append(ids, row.RowID)
	}
	return ids
}

// ShuffleAll shuffles every collection present in the order, each with its
// own configured selection and pool. Collection names are taken from the
// order's own slots (not cfg.Collections) and sorted for deterministic
// iteration, so repeated runs against the same rng seed behave the same way.
// It backs `order shuffle` with no --collection flag.
func ShuffleAll(o *Order, cfg config.Config, collections map[string]project.Collection, rng *rand.Rand) error {
	present := make(map[string]bool)
	for _, s := range o.Slots {
		if s.Collection != "" {
			present[s.Collection] = true
		}
	}

	names := make([]string, 0, len(present))
	for name := range present {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		coll, ok := collections[name]
		if !ok {
			return fmt.Errorf("collection %q has slots in the order but is not configured", name)
		}
		if err := Shuffle(o, name, coll.Config.SelectionValue(), Pool(coll), rng); err != nil {
			return err
		}
	}

	return nil
}
