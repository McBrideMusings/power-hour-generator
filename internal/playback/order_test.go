package playback

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()

	order := Order{
		Version: 1,
		Slots: []Slot{
			{File: "Intro.mov"},
			{Collection: "songs", RowID: "a3f9c1", Locked: true},
			{Collection: "interstitials", RowID: "7b2e04"},
			{File: "Intermission.mov"},
		},
	}

	if err := Save(dir, order); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// The file must land at the project root, not under .powerhour/.
	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Fatalf("expected %s at project root: %v", FileName, err)
	}

	got, found, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatalf("Load: found = false, want true")
	}

	if got.Version != order.Version {
		t.Errorf("Version = %d, want %d", got.Version, order.Version)
	}
	if len(got.Slots) != len(order.Slots) {
		t.Fatalf("len(Slots) = %d, want %d", len(got.Slots), len(order.Slots))
	}
	for i, want := range order.Slots {
		if got.Slots[i] != want {
			t.Errorf("Slots[%d] = %+v, want %+v", i, got.Slots[i], want)
		}
	}
}

func TestLoadMissingFileReturnsNotFound(t *testing.T) {
	dir := t.TempDir()

	got, found, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	if found {
		t.Fatalf("Load: found = true, want false for a project with no order file yet")
	}
	if len(got.Slots) != 0 {
		t.Fatalf("Load: Slots = %v, want empty", got.Slots)
	}
}

func TestFileSlotHasNoCollection(t *testing.T) {
	slot := Slot{File: "Intro.mov"}
	if slot.Collection != "" {
		t.Errorf("Collection = %q, want empty for a file slot", slot.Collection)
	}
	if slot.RowID != "" {
		t.Errorf("RowID = %q, want empty for a file slot", slot.RowID)
	}
	if slot.Locked {
		t.Errorf("Locked = true, want false — a file slot has no pool, so it is never locked (absence of a pool, not a lock)")
	}
}

func TestSaveIsAtomic(t *testing.T) {
	dir := t.TempDir()

	if err := Save(dir, Order{Version: 1, Slots: []Slot{{File: "a.mp4"}}}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != FileName {
			t.Errorf("unexpected leftover file in project root: %q (temp file not cleaned up)", e.Name())
		}
	}
}
