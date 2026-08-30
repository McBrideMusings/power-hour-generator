package playback

import (
	"fmt"
	"powerhour/internal/config"
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
	pos := NewPositionIndex(annotateOrder(), annotateCollections())

	cases := []struct {
		collection string
		rowID      string
		want       int
	}{
		// The count is per collection: file slots and the other pool's
		// slots do not shift a song's number.
		{"songs", "bbb222", 1},
		{"interstitials", "ccc333", 1},
		{"songs", "aaa111", 2},
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
	if got := NewPositionIndex(o, annotateCollections()).Of("interstitials", "ccc333"); got != 1 {
		t.Errorf("Of = %d, want 1", got)
	}
}

func TestAnnotateClipsStampsPositions(t *testing.T) {
	clips := []project.CollectionClip{
		annotateClip("songs", "aaa111"),
		annotateClip("songs", "bbb222"),
		annotateClip("songs", "zzz999"),
	}

	out := AnnotateClips(annotateOrder(), annotateCollections(), clips)

	want := []int{2, 1, 0}
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
	// Two songs come first so the shared row id lands on a different number
	// in each pool — otherwise both would be 1 and the test could not tell a
	// collection-keyed lookup from a row-id-only one.
	o := Order{Slots: []Slot{
		{Collection: "songs", RowID: "first"},
		{Collection: "interstitials", RowID: "dup"},
		{Collection: "songs", RowID: "dup"},
	}}
	out := AnnotateClips(o, annotateCollections(), []project.CollectionClip{
		annotateClip("interstitials", "dup"),
		annotateClip("songs", "dup"),
	})

	if out[0].Clip.PlaybackPosition != 1 {
		t.Errorf("interstitial position = %d, want 1", out[0].Clip.PlaybackPosition)
	}
	if out[1].Clip.PlaybackPosition != 2 {
		t.Errorf("song position = %d, want 2", out[1].Clip.PlaybackPosition)
	}
}

// TestPositionIndexCountsOnlyItsOwnCollection is the number a power hour
// actually burns in: the Nth song is N, however many interstitials and file
// bookends play between them. Counting every slot instead makes the number a
// slot index, which is not a fact about the song.
func TestPositionIndexCountsOnlyItsOwnCollection(t *testing.T) {
	var slots []Slot
	slots = append(slots, Slot{File: "intro.mov"})
	for i := 0; i < 5; i++ {
		slots = append(slots,
			Slot{Collection: "songs", RowID: fmt.Sprintf("song%d", i)},
			Slot{Collection: "interstitials", RowID: fmt.Sprintf("ad%d", i)},
		)
	}
	slots = append(slots, Slot{File: "outro.mov"})

	pos := NewPositionIndex(Order{Slots: slots}, annotateCollections())
	for i := 0; i < 5; i++ {
		if got := pos.Of("songs", fmt.Sprintf("song%d", i)); got != i+1 {
			t.Errorf("song%d position = %d, want %d", i, got, i+1)
		}
		if got := pos.Of("interstitials", fmt.Sprintf("ad%d", i)); got != i+1 {
			t.Errorf("ad%d position = %d, want %d", i, got, i+1)
		}
	}
}

// annotateCollections describes the two pools the annotate tests use. Both are
// `once`, so every row gets a position; a repeat pool's rows deliberately get
// none (see NewPositionIndex).
func annotateCollections() map[string]project.Collection {
	return map[string]project.Collection{
		"songs":         {Name: "songs", Config: config.CollectionConfig{Selection: "once"}},
		"interstitials": {Name: "interstitials", Config: config.CollectionConfig{Selection: "once"}},
	}
}

// TestPositionIndexGivesRepeatRowsNoPosition pins the churn fix: a repeat
// row plays in several slots, so no single position is true of it, and
// handing one out pinned its segment filename to whichever slot happened to
// be first — every reorder then renamed the file and the clip read as
// unrendered.
func TestPositionIndexGivesRepeatRowsNoPosition(t *testing.T) {
	o := Order{Slots: []Slot{
		{Collection: "songs", RowID: "s1"},
		{Collection: "ads", RowID: "ad1"},
		{Collection: "songs", RowID: "s2"},
		{Collection: "ads", RowID: "ad2"},
		{Collection: "ads", RowID: "ad1"},
	}}
	colls := map[string]project.Collection{
		"songs": {Name: "songs", Config: config.CollectionConfig{Selection: "once"}},
		"ads":   {Name: "ads", Config: config.CollectionConfig{Selection: "repeat"}},
	}

	pos := NewPositionIndex(o, colls)
	if got := pos.Of("songs", "s1"); got != 1 {
		t.Errorf("s1 = %d, want 1", got)
	}
	if got := pos.Of("songs", "s2"); got != 2 {
		t.Errorf("s2 = %d, want 2 — a repeat pool between them must not shift it", got)
	}
	for _, id := range []string{"ad1", "ad2"} {
		if got := pos.Of("ads", id); got != 0 {
			t.Errorf("%s = %d, want 0 — a repeat row has no single position", id, got)
		}
	}
}
