package playback

import (
	"testing"

	"powerhour/internal/project"
	"powerhour/pkg/csvplan"
)

func annotateOrder() Order {
	return Order{Slots: []Slot{
		{File: "intro.mp4"},
		{Collection: "songs", RowID: "bbb222"},
		{Collection: "interstitials", RowID: "ccc333"},
		{Collection: "songs", RowID: "aaa111"},
		{File: "outro.mp4"},
	}}
}

func annotateClip(collection, rowID string) project.CollectionClip {
	return project.CollectionClip{
		CollectionName: collection,
		Clip:           project.Clip{Row: csvplan.Row{RowID: rowID}},
	}
}

func TestPositionIndexOf(t *testing.T) {
	pos := NewPositionIndex(annotateOrder())

	cases := []struct {
		collection string
		rowID      string
		want       int
	}{
		// File slots occupy positions, so they shift the collection rows
		// that follow them.
		{"songs", "bbb222", 2},
		{"interstitials", "ccc333", 3},
		{"songs", "aaa111", 4},
		// A row in no slot, and a row id that belongs to another pool.
		{"songs", "zzz999", 0},
		{"interstitials", "aaa111", 0},
		{"songs", "", 0},
	}

	for _, tc := range cases {
		if got := pos.Of(tc.collection, tc.rowID); got != tc.want {
			t.Errorf("Of(%q, %q) = %d, want %d", tc.collection, tc.rowID, got, tc.want)
		}
	}
}

// A row a repeat-selection pool placed in several slots renders one file, so
// it can only carry one number: the first slot it holds.
func TestPositionIndexTakesFirstSlotOfARepeatedRow(t *testing.T) {
	o := Order{Slots: []Slot{
		{Collection: "interstitials", RowID: "ccc333"},
		{Collection: "songs", RowID: "aaa111"},
		{Collection: "interstitials", RowID: "ccc333"},
	}}
	if got := NewPositionIndex(o).Of("interstitials", "ccc333"); got != 1 {
		t.Errorf("Of = %d, want 1", got)
	}
}

func TestAnnotateClipsStampsPositions(t *testing.T) {
	clips := []project.CollectionClip{
		annotateClip("songs", "aaa111"),
		annotateClip("songs", "bbb222"),
		annotateClip("songs", "zzz999"),
	}

	out := AnnotateClips(annotateOrder(), clips)

	want := []int{4, 2, 0}
	for i, w := range want {
		if got := out[i].Clip.PlaybackPosition; got != w {
			t.Errorf("clip %d position = %d, want %d", i, got, w)
		}
	}

	for i, c := range clips {
		if c.Clip.PlaybackPosition != 0 {
			t.Errorf("input clip %d was mutated (position %d); AnnotateClips must return a new slice", i, c.Clip.PlaybackPosition)
		}
	}
}

// The join is on (collection, row id) — a row id shared across two pools
// must not pick up the other pool's position.
func TestAnnotateClipsKeysOnCollectionAndRowID(t *testing.T) {
	o := Order{Slots: []Slot{
		{Collection: "songs", RowID: "dup"},
		{Collection: "interstitials", RowID: "dup"},
	}}
	out := AnnotateClips(o, []project.CollectionClip{
		annotateClip("interstitials", "dup"),
		annotateClip("songs", "dup"),
	})

	if out[0].Clip.PlaybackPosition != 2 {
		t.Errorf("interstitial position = %d, want 2", out[0].Clip.PlaybackPosition)
	}
	if out[1].Clip.PlaybackPosition != 1 {
		t.Errorf("song position = %d, want 1", out[1].Clip.PlaybackPosition)
	}
}
