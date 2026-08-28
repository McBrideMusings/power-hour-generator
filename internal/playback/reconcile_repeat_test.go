package playback

import (
	"testing"

	"powerhour/internal/config"
	"powerhour/internal/project"
	"powerhour/pkg/csvplan"
)

// repeatFixture: a 1-row `once` collection interleaved with a `repeat` pool of
// three rows across six slots, so Materialize cycles the pool twice.
func repeatFixture() (config.Config, map[string]project.Collection) {
	ads := project.Collection{
		Name:   "ads",
		Config: config.CollectionConfig{Selection: "repeat"},
		Rows: []csvplan.CollectionRow{
			{Index: 1, RowID: "ad1"},
			{Index: 2, RowID: "ad2"},
			{Index: 3, RowID: "ad3"},
		},
	}
	cfg := config.Config{
		Collections: map[string]config.CollectionConfig{"ads": ads.Config},
		Timeline: config.TimelineConfig{
			Sequence: []config.SequenceEntry{{Collection: "ads"}},
		},
	}
	return cfg, map[string]project.Collection{"ads": ads}
}

// TestReconcileKeepsAHandPickedRepeatRow is the fix for a choice that silently
// reverted: Materialize cycles a repeat pool, so each row happens to appear a
// fixed number of times, but those counts are an artifact of cycling and not a
// rule. Setting several slots to the same pool row must survive a reload.
func TestReconcileKeepsAHandPickedRepeatRow(t *testing.T) {
	cfg, collections := repeatFixture()

	materialized, _, err := Reconcile(Order{}, cfg, collections)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	n := len(materialized.Slots)
	if n < 3 {
		t.Fatalf("fixture produced %d slots, want at least 3", n)
	}

	// Point every slot at one pool row — far past the quota cycling implies.
	picked := materialized
	picked.Slots = append([]Slot(nil), materialized.Slots...)
	for i := range picked.Slots {
		picked.Slots[i].RowID = "ad2"
	}

	got, changes, err := Reconcile(picked, cfg, collections)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(got.Slots) != n {
		t.Fatalf("slot count = %d, want %d unchanged", len(got.Slots), n)
	}
	for i, s := range got.Slots {
		if s.RowID != "ad2" {
			t.Fatalf("slot %d = %q, want ad2 — a repeat pool has no per-row quota", i, s.RowID)
		}
	}
	if len(changes) != 0 {
		t.Fatalf("changes = %v, want none: every slot resolves", changes)
	}
}

// TestReconcileDropsARepeatRowThatLeftThePool verifies the remaining rule for
// a repeat kind: an occupant must still exist in the pool.
func TestReconcileDropsARepeatRowThatLeftThePool(t *testing.T) {
	cfg, collections := repeatFixture()
	materialized, _, _ := Reconcile(Order{}, cfg, collections)

	stale := materialized
	stale.Slots = append([]Slot(nil), materialized.Slots...)
	stale.Slots[0].RowID = "deleted-row"

	got, changes, err := Reconcile(stale, cfg, collections)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	for i, s := range got.Slots {
		if s.RowID == "deleted-row" {
			t.Fatalf("slot %d still holds a row that is not in the pool", i)
		}
	}
	var dropped bool
	for _, c := range changes {
		if c.Kind == ChangeDropped && c.RowID == "deleted-row" {
			dropped = true
		}
	}
	if !dropped {
		t.Fatalf("changes = %v, want a drop for the missing row", changes)
	}
}

// TestReconcileStillQuotasAOnceCollection verifies the per-row multiset is
// kept where it is the actual rule: under `once` a row holds exactly one slot,
// so a duplicated occupant must not survive.
func TestReconcileStillQuotasAOnceCollection(t *testing.T) {
	songs := project.Collection{
		Name:   "songs",
		Config: config.CollectionConfig{Selection: "once"},
		Rows: []csvplan.CollectionRow{
			{Index: 1, RowID: "s1"},
			{Index: 2, RowID: "s2"},
			{Index: 3, RowID: "s3"},
		},
	}
	cfg := config.Config{
		Collections: map[string]config.CollectionConfig{"songs": songs.Config},
		Timeline:    config.TimelineConfig{Sequence: []config.SequenceEntry{{Collection: "songs"}}},
	}
	collections := map[string]project.Collection{"songs": songs}

	dup := Order{Version: 1, Slots: []Slot{
		{Collection: "songs", RowID: "s1"},
		{Collection: "songs", RowID: "s1"},
		{Collection: "songs", RowID: "s1"},
	}}

	got, _, err := Reconcile(dup, cfg, collections)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	seen := map[string]int{}
	for _, s := range got.Slots {
		seen[s.RowID]++
	}
	if seen["s1"] != 1 {
		t.Fatalf("s1 occupies %d slots, want 1 under `once`", seen["s1"])
	}
	for _, id := range []string{"s1", "s2", "s3"} {
		if seen[id] != 1 {
			t.Fatalf("%s occupies %d slots, want exactly 1", id, seen[id])
		}
	}
}
