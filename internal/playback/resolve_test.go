package playback

import (
	"testing"

	"powerhour/internal/config"
	"powerhour/internal/project"
)

func testConfigAndCollections() (config.Config, map[string]project.Collection) {
	cfg := config.Config{
		Timeline: config.TimelineConfig{
			Sequence: []config.SequenceEntry{
				{File: "Intro.mov"},
				{Collection: "songs"},
				{File: "Outro.mov"},
			},
		},
	}
	collections := map[string]project.Collection{
		"songs": {
			Name:   "songs",
			Config: config.CollectionConfig{Selection: "once"},
			Rows:   rowsWithIDs("s1", "s2", "s3"),
		},
	}
	return cfg, collections
}

func TestResolveOrderMaterializesWhenAbsent(t *testing.T) {
	cfg, collections := testConfigAndCollections()
	root := t.TempDir()

	order, changes, err := ResolveOrder(root, cfg, collections)
	if err != nil {
		t.Fatalf("ResolveOrder: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("changes = %+v, want none on first materialize", changes)
	}
	if len(order.Slots) != 5 {
		t.Fatalf("Slots = %d, want 5 (intro, s1, s2, s3, outro)", len(order.Slots))
	}

	// Read-only: nothing should have been written to disk.
	if _, found, err := Load(root); err != nil {
		t.Fatalf("Load: %v", err)
	} else if found {
		t.Error("ResolveOrder wrote playback-order.yaml; it must stay read-only")
	}
}

func TestResolveOrderReconcilesWhenPresent(t *testing.T) {
	cfg, collections := testConfigAndCollections()
	root := t.TempDir()

	materialized, err := Materialize(cfg, collections)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	// Swap two collection slots so the saved order disagrees with a fresh
	// config-order materialization; ResolveOrder must preserve that swap
	// (reconcile, not re-materialize).
	if err := Swap(&materialized, 1, 3); err != nil {
		t.Fatalf("Swap: %v", err)
	}
	if err := Save(root, materialized); err != nil {
		t.Fatalf("Save: %v", err)
	}

	order, _, err := ResolveOrder(root, cfg, collections)
	if err != nil {
		t.Fatalf("ResolveOrder: %v", err)
	}
	if order.Slots[1].RowID != "s3" || order.Slots[3].RowID != "s1" {
		t.Fatalf("Slots = %+v, want the saved swap preserved", order.Slots)
	}
}

func TestPlacementsCollectionSlot(t *testing.T) {
	cfg, collections := testConfigAndCollections()
	order, err := Materialize(cfg, collections)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if err := Swap(&order, 1, 3); err != nil {
		t.Fatalf("Swap: %v", err)
	}

	placements, err := Placements(order, cfg, collections)
	if err != nil {
		t.Fatalf("Placements: %v", err)
	}
	if len(placements) != 5 {
		t.Fatalf("placements = %d, want 5", len(placements))
	}

	// Slot 1 (index 1) now holds s3, slot 3 (index 3) now holds s1.
	if placements[1].RowID != "s3" || placements[1].RowIndex != 3 {
		t.Errorf("placements[1] = %+v, want RowID s3 RowIndex 3", placements[1])
	}
	if placements[3].RowID != "s1" || placements[3].RowIndex != 1 {
		t.Errorf("placements[3] = %+v, want RowID s1 RowIndex 1", placements[3])
	}
}

func TestPlacementsFileSlotRecoversSequenceIndex(t *testing.T) {
	cfg := config.Config{
		Timeline: config.TimelineConfig{
			Sequence: []config.SequenceEntry{
				{File: "Bookend.mov"},
				{Collection: "songs"},
				{File: "Bookend.mov"}, // same file used twice — must map positionally
			},
		},
	}
	collections := map[string]project.Collection{
		"songs": {
			Name:   "songs",
			Config: config.CollectionConfig{Selection: "once"},
			Rows:   rowsWithIDs("s1"),
		},
	}

	order, err := Materialize(cfg, collections)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	placements, err := Placements(order, cfg, collections)
	if err != nil {
		t.Fatalf("Placements: %v", err)
	}
	if len(placements) != 3 {
		t.Fatalf("placements = %d, want 3", len(placements))
	}
	if placements[0].SourceFile != "Bookend.mov" || placements[0].SequenceEntryIndex != 0 {
		t.Errorf("placements[0] = %+v, want SourceFile Bookend.mov SequenceEntryIndex 0", placements[0])
	}
	if placements[2].SourceFile != "Bookend.mov" || placements[2].SequenceEntryIndex != 2 {
		t.Errorf("placements[2] = %+v, want SourceFile Bookend.mov SequenceEntryIndex 2", placements[2])
	}
}

func TestOrderedPlacementsRegressionSwapChangesOutputOrder(t *testing.T) {
	// This is the issue's own regression test, at the domain layer: before
	// the seam swap, an order.go mutation had no effect on what
	// render.ResolveTimelineSegments produced.
	cfg, collections := testConfigAndCollections()
	root := t.TempDir()

	before, err := OrderedPlacements(root, cfg, collections)
	if err != nil {
		t.Fatalf("OrderedPlacements (before): %v", err)
	}

	order, _, err := ResolveOrder(root, cfg, collections)
	if err != nil {
		t.Fatalf("ResolveOrder: %v", err)
	}
	if err := Swap(&order, 1, 3); err != nil {
		t.Fatalf("Swap: %v", err)
	}
	if err := Save(root, order); err != nil {
		t.Fatalf("Save: %v", err)
	}

	after, err := OrderedPlacements(root, cfg, collections)
	if err != nil {
		t.Fatalf("OrderedPlacements (after): %v", err)
	}

	if before[1].RowID == after[1].RowID {
		t.Fatalf("swap did not change resolved order: before[1]=%q after[1]=%q", before[1].RowID, after[1].RowID)
	}
}
