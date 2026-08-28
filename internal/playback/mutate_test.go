package playback

import (
	"math/rand"
	"sort"
	"strings"
	"testing"

	"powerhour/internal/config"
	"powerhour/internal/project"
	"powerhour/pkg/csvplan"
)

func newTestOrder() Order {
	return Order{
		Version: 1,
		Slots: []Slot{
			{Collection: "songs", RowID: "aaaaaa"},               // 0
			{File: "Intro.mov"},                                  // 1
			{Collection: "songs", RowID: "bbbbbb"},               // 2
			{Collection: "songs", RowID: "cccccc"},               // 3
			{File: "Intermission.mov"},                           // 4
			{Collection: "songs", RowID: "dddddd"},               // 5
			{Collection: "songs", RowID: "eeeeee", Locked: true}, // 6
			{File: "Outro.mov"},                                  // 7
		},
	}
}

func rowIDMultiset(ids []string) map[string]int {
	m := make(map[string]int, len(ids))
	for _, id := range ids {
		m[id]++
	}
	return m
}

func collectionRowIDs(o Order, collection string) []string {
	var ids []string
	for _, s := range o.Slots {
		if s.Collection == collection {
			ids = append(ids, s.RowID)
		}
	}
	sort.Strings(ids)
	return ids
}

// TestShuffleOncePermutesGroup pins the "once" behavior: the multiset of
// RowIDs occupying the group is preserved — every row keeps exactly one
// slot, just possibly a different one.
func TestShuffleOncePermutesGroup(t *testing.T) {
	o := newTestOrder()
	before := collectionRowIDs(o, "songs")

	rng := rand.New(rand.NewSource(1))
	if err := Shuffle(&o, "songs", config.SelectionOnce, nil, rng); err != nil {
		t.Fatalf("Shuffle: %v", err)
	}

	after := collectionRowIDs(o, "songs")
	beforeSet := rowIDMultiset(before)
	afterSet := rowIDMultiset(after)
	if len(beforeSet) != len(afterSet) {
		t.Fatalf("multiset size changed: before=%v after=%v", beforeSet, afterSet)
	}
	for id, n := range beforeSet {
		if afterSet[id] != n {
			t.Fatalf("row %q count changed: before=%d after=%d", id, n, afterSet[id])
		}
	}
}

// TestShuffleRepeatRedrawsFromPool pins the "repeat" behavior: every
// occupant after the shuffle comes from the supplied pool (not necessarily
// preserving the pre-shuffle multiset, since draws are independent).
func TestShuffleRepeatRedrawsFromPool(t *testing.T) {
	o := newTestOrder()
	pool := []string{"pool-1", "pool-2", "pool-3"}
	poolSet := rowIDMultiset(pool)

	rng := rand.New(rand.NewSource(2))
	if err := Shuffle(&o, "songs", config.SelectionRepeat, pool, rng); err != nil {
		t.Fatalf("Shuffle: %v", err)
	}

	for _, s := range o.Slots {
		if s.Collection != "songs" || s.Locked {
			continue
		}
		if poolSet[s.RowID] == 0 {
			t.Fatalf("slot occupant %q not present in pool %v", s.RowID, pool)
		}
	}
}

// TestShuffleRepeatEmptyPoolErrors ensures a repeat-selection shuffle with no
// pool names the collection rather than panicking on an empty slice.
func TestShuffleRepeatEmptyPoolErrors(t *testing.T) {
	o := newTestOrder()
	rng := rand.New(rand.NewSource(3))
	err := Shuffle(&o, "songs", config.SelectionRepeat, nil, rng)
	if err == nil {
		t.Fatal("expected error for empty pool, got nil")
	}
}

// TestShuffleSkipsLockedSlots: locking rows leaves those exact slots
// untouched in both index and occupant.
func TestShuffleSkipsLockedSlots(t *testing.T) {
	o := newTestOrder()
	lockedIdx := 6
	lockedBefore := o.Slots[lockedIdx]

	rng := rand.New(rand.NewSource(4))
	if err := Shuffle(&o, "songs", config.SelectionOnce, nil, rng); err != nil {
		t.Fatalf("Shuffle: %v", err)
	}

	if o.Slots[lockedIdx] != lockedBefore {
		t.Fatalf("locked slot changed: before=%+v after=%+v", lockedBefore, o.Slots[lockedIdx])
	}
}

// TestShuffleScopeIsGlobal: the group spans the whole order, crossing file
// bookends, rather than being scoped to a contiguous run.
func TestShuffleScopeIsGlobal(t *testing.T) {
	// Songs slots straddle three file bookends (indices 1, 4, 7): 0 and 2-3
	// sit either side of Intro/Intermission, and 5 sits between
	// Intermission and Outro. A shuffle whose group only spanned one run
	// could never move a row from slot 0 into slot 5 or vice versa; run the
	// shuffle enough times with different seeds to observe it happening.
	moved := false
	for seed := int64(0); seed < 50; seed++ {
		o := newTestOrder()
		rng := rand.New(rand.NewSource(seed))
		if err := Shuffle(&o, "songs", config.SelectionOnce, nil, rng); err != nil {
			t.Fatalf("Shuffle: %v", err)
		}
		if o.Slots[0].RowID != "aaaaaa" && (o.Slots[0].RowID == "dddddd" || o.Slots[5].RowID == "aaaaaa") {
			moved = true
			break
		}
	}
	if !moved {
		t.Fatal("expected a row to cross a file bookend across repeated shuffles, none did — scope is not global")
	}

	// File slots themselves never move or get overwritten.
	o2 := newTestOrder()
	rng := rand.New(rand.NewSource(5))
	if err := Shuffle(&o2, "songs", config.SelectionOnce, nil, rng); err != nil {
		t.Fatalf("Shuffle: %v", err)
	}
	for _, i := range []int{1, 4, 7} {
		if o2.Slots[i].File == "" {
			t.Fatalf("slot %d lost its file entry after shuffle: %+v", i, o2.Slots[i])
		}
	}
}

func TestSwapRejectsFileSlot(t *testing.T) {
	o := newTestOrder()
	err := Swap(&o, 0, 1)
	if err == nil {
		t.Fatal("expected error swapping with a file slot")
	}
	if got := err.Error(); !strings.Contains(got, "file entry") {
		t.Fatalf("error %q does not name the missing pool", got)
	}
}

func TestSwapSameIndexIsNoop(t *testing.T) {
	o := newTestOrder()
	before := o.Slots[1]
	if err := Swap(&o, 1, 1); err != nil {
		t.Fatalf("Swap same index: %v", err)
	}
	if o.Slots[1] != before {
		t.Fatalf("slot mutated on a==b swap: before=%+v after=%+v", before, o.Slots[1])
	}
}

func TestSwapLockedSlotAllowed(t *testing.T) {
	o := newTestOrder()
	if err := Swap(&o, 0, 6); err != nil {
		t.Fatalf("Swap involving a locked slot should be allowed: %v", err)
	}
	if o.Slots[6].RowID != "aaaaaa" || !o.Slots[6].Locked {
		t.Fatalf("expected locked slot to carry its Locked flag with it, got %+v", o.Slots[6])
	}
}

func TestSetRejectsFileSlot(t *testing.T) {
	o := newTestOrder()
	err := Set(&o, 1, "aaaaaa")
	if err == nil {
		t.Fatal("expected error setting a file slot")
	}
	if got := err.Error(); !strings.Contains(got, "file entry") {
		t.Fatalf("error %q does not name the missing pool", got)
	}
}

func TestSetLockRejectsFileSlot(t *testing.T) {
	o := newTestOrder()
	if err := SetLock(&o, 1, true); err == nil {
		t.Fatal("expected error locking a file slot")
	}
}

func TestOutOfRangeErrors(t *testing.T) {
	o := newTestOrder()

	if err := Swap(&o, 0, 99); err == nil {
		t.Fatal("expected out-of-range error from Swap")
	}
	if err := Set(&o, 99, "x"); err == nil {
		t.Fatal("expected out-of-range error from Set")
	}
	if err := SetLock(&o, -1, true); err == nil {
		t.Fatal("expected out-of-range error from SetLock")
	}
}

func TestPoolSkipsEmptyRowIDs(t *testing.T) {
	coll := project.Collection{
		Rows: []csvplan.CollectionRow{
			{RowID: "aaaaaa"},
			{RowID: ""},
			{RowID: "bbbbbb"},
		},
	}
	got := Pool(coll)
	if len(got) != 2 || got[0] != "aaaaaa" || got[1] != "bbbbbb" {
		t.Fatalf("Pool = %v, want [aaaaaa bbbbbb]", got)
	}
}

func TestShuffleAllShufflesEveryPresentCollection(t *testing.T) {
	o := Order{Slots: []Slot{
		{Collection: "songs", RowID: "s1"},
		{Collection: "songs", RowID: "s2"},
		{Collection: "interstitials", RowID: "i1"},
		{File: "Intro.mov"},
	}}
	cfg := config.Config{
		Collections: map[string]config.CollectionConfig{
			"songs":         {Selection: "once"},
			"interstitials": {Selection: "repeat"},
		},
	}
	collections := map[string]project.Collection{
		"songs": {
			Config: cfg.Collections["songs"],
			Rows: []csvplan.CollectionRow{
				{RowID: "s1"}, {RowID: "s2"},
			},
		},
		"interstitials": {
			Config: cfg.Collections["interstitials"],
			Rows: []csvplan.CollectionRow{
				{RowID: "i1"}, {RowID: "i2"}, {RowID: "i3"},
			},
		},
	}

	rng := rand.New(rand.NewSource(7))
	if err := ShuffleAll(&o, cfg, collections, rng); err != nil {
		t.Fatalf("ShuffleAll: %v", err)
	}

	// File slot untouched.
	if o.Slots[3].File != "Intro.mov" {
		t.Fatalf("file slot mutated: %+v", o.Slots[3])
	}
	// interstitials slot must have redrawn from its 3-row pool (repeat).
	found := false
	for _, id := range []string{"i1", "i2", "i3"} {
		if o.Slots[2].RowID == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("interstitials slot got %q, not in pool", o.Slots[2].RowID)
	}
}

func TestShuffleAllErrorsOnUnconfiguredCollection(t *testing.T) {
	o := Order{Slots: []Slot{{Collection: "ghost", RowID: "x"}}}
	cfg := config.Config{}
	collections := map[string]project.Collection{}
	if err := ShuffleAll(&o, cfg, collections, rand.New(rand.NewSource(8))); err == nil {
		t.Fatal("expected error for a slot collection missing from collections map")
	}
}

// cycleOrder builds a 4-slot order over one collection for the Cycle tests.
func cycleOrder(ids ...string) *Order {
	o := &Order{}
	for _, id := range ids {
		o.Slots = append(o.Slots, Slot{Collection: "c", RowID: id})
	}
	return o
}

// TestCycleOnceSwapsWithTheIncumbent verifies a `once` step exchanges slots
// when the incoming row already holds one, so every row keeps exactly one
// slot — the invariant `once` exists to state.
func TestCycleOnceSwapsWithTheIncumbent(t *testing.T) {
	o := cycleOrder("a", "b", "c")
	pool := []string{"a", "b", "c"}

	if err := Cycle(o, 0, config.SelectionOnce, pool, +1); err != nil {
		t.Fatalf("Cycle: %v", err)
	}
	if o.Slots[0].RowID != "b" || o.Slots[1].RowID != "a" {
		t.Fatalf("slots = %q/%q, want b/a", o.Slots[0].RowID, o.Slots[1].RowID)
	}
	if o.Slots[2].RowID != "c" {
		t.Fatalf("slot 2 = %q, want c untouched", o.Slots[2].RowID)
	}
}

// TestCycleRepeatAssignsAndDuplicates verifies a `repeat` step assigns
// without swapping, so two slots may hold the same row.
func TestCycleRepeatAssignsAndDuplicates(t *testing.T) {
	o := cycleOrder("a", "b", "c")
	pool := []string{"a", "b", "c"}

	if err := Cycle(o, 0, config.SelectionRepeat, pool, +1); err != nil {
		t.Fatalf("Cycle: %v", err)
	}
	if o.Slots[0].RowID != "b" || o.Slots[1].RowID != "b" {
		t.Fatalf("slots = %q/%q, want b/b (repeat duplicates rather than swapping)", o.Slots[0].RowID, o.Slots[1].RowID)
	}
}

// TestCycleWrapsBothDirections verifies the walk wraps at both ends.
func TestCycleWrapsBothDirections(t *testing.T) {
	pool := []string{"a", "b", "c"}

	back := cycleOrder("a")
	if err := Cycle(back, 0, config.SelectionRepeat, pool, -1); err != nil {
		t.Fatalf("Cycle: %v", err)
	}
	if back.Slots[0].RowID != "c" {
		t.Fatalf("left from the first row = %q, want c", back.Slots[0].RowID)
	}

	fwd := cycleOrder("c")
	if err := Cycle(fwd, 0, config.SelectionRepeat, pool, +1); err != nil {
		t.Fatalf("Cycle: %v", err)
	}
	if fwd.Slots[0].RowID != "a" {
		t.Fatalf("right from the last row = %q, want a", fwd.Slots[0].RowID)
	}
}

// TestCycleFromOutsideThePoolStepsIn verifies a slot holding a row that is
// not in the pool lands on the first entry going forward, the last going back.
func TestCycleFromOutsideThePoolStepsIn(t *testing.T) {
	pool := []string{"a", "b", "c"}

	fwd := cycleOrder("gone")
	if err := Cycle(fwd, 0, config.SelectionRepeat, pool, +1); err != nil {
		t.Fatalf("Cycle: %v", err)
	}
	if fwd.Slots[0].RowID != "a" {
		t.Fatalf("right from an unknown row = %q, want a", fwd.Slots[0].RowID)
	}

	back := cycleOrder("gone")
	if err := Cycle(back, 0, config.SelectionRepeat, pool, -1); err != nil {
		t.Fatalf("Cycle: %v", err)
	}
	if back.Slots[0].RowID != "c" {
		t.Fatalf("left from an unknown row = %q, want c", back.Slots[0].RowID)
	}
}

// TestCycleRejectsFileSlotAndEmptyPool verifies the two refusals: a file slot
// has no pool at all, and an empty pool has nothing to step to.
func TestCycleRejectsFileSlotAndEmptyPool(t *testing.T) {
	o := &Order{Slots: []Slot{{File: "/media/bumper.mp4"}, {Collection: "c", RowID: "a"}}}

	if err := Cycle(o, 0, config.SelectionOnce, []string{"a"}, +1); err == nil {
		t.Fatal("Cycle on a file slot returned nil, want an error")
	}
	if err := Cycle(o, 1, config.SelectionOnce, nil, +1); err == nil {
		t.Fatal("Cycle with an empty pool returned nil, want an error")
	}
}
