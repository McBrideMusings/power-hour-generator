package playback

import (
	"reflect"
	"testing"

	"powerhour/internal/config"
	"powerhour/internal/project"
	"powerhour/pkg/csvplan"
)

func rowsWithIDs(ids ...string) []csvplan.CollectionRow {
	rows := make([]csvplan.CollectionRow, len(ids))
	for i, id := range ids {
		rows[i] = csvplan.CollectionRow{Index: i + 1, RowID: id}
	}
	return rows
}

func TestMaterializeSlotKinds(t *testing.T) {
	cfg := config.Config{
		Timeline: config.TimelineConfig{
			Sequence: []config.SequenceEntry{
				{File: "Intro.mov"},
				{
					Collection: "songs",
					Interleave: &config.InterleaveConfig{
						Collection: "interstitials",
						Every:      1,
						Placement:  "after",
					},
				},
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
		"interstitials": {
			Name:   "interstitials",
			Config: config.CollectionConfig{Selection: "repeat"},
			Rows:   rowsWithIDs("i1", "i2"),
		},
	}

	order, err := Materialize(cfg, collections)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	if order.Version != 1 {
		t.Errorf("Version = %d, want 1", order.Version)
	}

	want := []Slot{
		{File: "Intro.mov"},
		{Collection: "songs", RowID: "s1"},
		{Collection: "interstitials", RowID: "i1"},
		{Collection: "songs", RowID: "s2"},
		{Collection: "interstitials", RowID: "i2"},
		{Collection: "songs", RowID: "s3"},
		{Collection: "interstitials", RowID: "i1"}, // repeat pool wraps
		{File: "Outro.mov"},
	}

	if !reflect.DeepEqual(order.Slots, want) {
		t.Fatalf("Slots = %+v, want %+v", order.Slots, want)
	}
}

func TestMaterializeFileSlotsHaveNoCollectionOrLock(t *testing.T) {
	cfg := config.Config{
		Timeline: config.TimelineConfig{
			Sequence: []config.SequenceEntry{
				{File: "Intro.mov"},
			},
		},
	}

	order, err := Materialize(cfg, nil)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if len(order.Slots) != 1 {
		t.Fatalf("expected 1 slot, got %d", len(order.Slots))
	}
	slot := order.Slots[0]
	if slot.Collection != "" || slot.RowID != "" || slot.Locked {
		t.Errorf("file slot = %+v, want no collection, no id, not locked", slot)
	}
	if slot.File != "Intro.mov" {
		t.Errorf("File = %q, want Intro.mov", slot.File)
	}
}
