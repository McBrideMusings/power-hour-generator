package playback

import (
	"reflect"
	"testing"

	"powerhour/internal/config"
	"powerhour/internal/project"
)

func songsCollection(selection string, ids ...string) map[string]project.Collection {
	return map[string]project.Collection{
		"songs": {
			Name:   "songs",
			Config: config.CollectionConfig{Selection: selection},
			Rows:   rowsWithIDs(ids...),
		},
	}
}

func hasChange(changes []Change, kind ChangeKind, rowID string) bool {
	for _, c := range changes {
		if c.Kind == kind && c.RowID == rowID {
			return true
		}
	}
	return false
}

// TestReconcileStoredOrderWins: a hand-shuffled stored order must survive
// Reconcile unchanged even though the generator's own (canonical) order
// would place the rows differently — nothing already placed ever moves.
func TestReconcileStoredOrderWins(t *testing.T) {
	cfg := config.Config{
		Timeline: config.TimelineConfig{
			Sequence: []config.SequenceEntry{{Collection: "songs"}},
		},
	}
	collections := songsCollection("once", "s1", "s2", "s3")

	prev := Order{Version: 1, Slots: []Slot{
		{Collection: "songs", RowID: "s2"},
		{Collection: "songs", RowID: "s1"},
		{Collection: "songs", RowID: "s3"},
	}}

	got, changes, err := Reconcile(prev, cfg, collections)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if !reflect.DeepEqual(got.Slots, prev.Slots) {
		t.Fatalf("Slots = %+v, want unchanged stored order %+v", got.Slots, prev.Slots)
	}
	if len(changes) != 0 {
		t.Fatalf("changes = %+v, want none — every stored slot still resolves", changes)
	}
}

// TestReconcileDropsUnresolvableSlot: a stored slot naming a row that no
// longer exists is dropped, and the generator fills the resulting gap with
// whatever current row nothing else claimed — without ever duplicating a
// row that a surviving stored slot already claims.
func TestReconcileDropsUnresolvableSlot(t *testing.T) {
	cfg := config.Config{
		Timeline: config.TimelineConfig{
			Sequence: []config.SequenceEntry{{Collection: "songs"}},
		},
	}
	collections := songsCollection("once", "s1", "s2", "s3")

	prev := Order{Version: 1, Slots: []Slot{
		{Collection: "songs", RowID: "s1"},
		{Collection: "songs", RowID: "s99"}, // deleted row
		{Collection: "songs", RowID: "s3"},
	}}

	got, changes, err := Reconcile(prev, cfg, collections)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	want := []Slot{
		{Collection: "songs", RowID: "s1"},
		{Collection: "songs", RowID: "s3"},
		{Collection: "songs", RowID: "s2"},
	}
	if !reflect.DeepEqual(got.Slots, want) {
		t.Fatalf("Slots = %+v, want %+v", got.Slots, want)
	}

	if !hasChange(changes, ChangeDropped, "s99") {
		t.Errorf("changes = %+v, want a dropped change for s99", changes)
	}
	if !hasChange(changes, ChangeFilled, "s2") {
		t.Errorf("changes = %+v, want a filled change for s2", changes)
	}

	// No row may appear twice, and none may be lost.
	seen := make(map[string]int)
	for _, s := range got.Slots {
		seen[s.RowID]++
	}
	for _, id := range []string{"s1", "s2", "s3"} {
		if seen[id] != 1 {
			t.Errorf("row %q appears %d times, want exactly 1", id, seen[id])
		}
	}
}

// TestReconcileAppendsOnceLeftovers: a restrictive slice: on the sequence
// entry means the canonical order under-demands a "once" collection's rows
// relative to its full pool; Reconcile still gives every row a slot by
// appending the ones no position claimed.
func TestReconcileAppendsOnceLeftovers(t *testing.T) {
	cfg := config.Config{
		Timeline: config.TimelineConfig{
			Sequence: []config.SequenceEntry{{Collection: "songs", Slice: "start:2"}},
		},
	}
	collections := songsCollection("once", "s1", "s2", "s3")

	got, changes, err := Reconcile(Order{}, cfg, collections)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	want := []Slot{
		{Collection: "songs", RowID: "s1"},
		{Collection: "songs", RowID: "s2"},
		{Collection: "songs", RowID: "s3"},
	}
	if !reflect.DeepEqual(got.Slots, want) {
		t.Fatalf("Slots = %+v, want %+v", got.Slots, want)
	}
	if !hasChange(changes, ChangeAdded, "s3") {
		t.Errorf("changes = %+v, want an added change for the leftover row s3", changes)
	}
}

// TestReconcileRepeatCollectionGetsNoLeftoverAppend: a "repeat" pool cycles
// rather than running out, so it has no concept of an "unplaced" row — the
// once-leftover append pass must skip it entirely.
func TestReconcileRepeatCollectionGetsNoLeftoverAppend(t *testing.T) {
	cfg := config.Config{
		Timeline: config.TimelineConfig{
			Sequence: []config.SequenceEntry{{Collection: "songs", Interleave: &config.InterleaveConfig{
				Collection: "interstitials", Every: 1,
			}}},
		},
	}
	collections := map[string]project.Collection{
		"songs":         {Name: "songs", Config: config.CollectionConfig{Selection: "once"}, Rows: rowsWithIDs("s1")},
		"interstitials": {Name: "interstitials", Config: config.CollectionConfig{Selection: "repeat"}, Rows: rowsWithIDs("i1", "i2", "i3")},
	}

	got, changes, err := Reconcile(Order{}, cfg, collections)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	for _, c := range changes {
		if c.Kind == ChangeAdded && c.Collection == "interstitials" {
			t.Fatalf("unexpected leftover-append change for repeat collection: %+v", c)
		}
	}
	// Only one interstitial slot was demanded (one song, every=1, default
	// placement=between never fires for a single song) — i2 and i3 must
	// not be force-appended.
	count := 0
	for _, s := range got.Slots {
		if s.Collection == "interstitials" {
			count++
		}
	}
	if count != 0 {
		t.Fatalf("interstitial slots = %d, want 0 (a single song has no 'between' insertion point)", count)
	}
}
