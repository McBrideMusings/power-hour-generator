package project

import (
	"strings"
	"testing"

	"powerhour/internal/config"
	"powerhour/pkg/csvplan"
)

// makeCollectionWithRows builds a stub Collection with n synthetic rows (1-based Index).
func makeCollectionWithRows(name string, n int) Collection {
	rows := make([]csvplan.CollectionRow, n)
	for i := range rows {
		rows[i] = csvplan.CollectionRow{Index: i + 1}
	}
	return Collection{Name: name, Rows: rows}
}

func TestResolveTimeline_EmptySequence(t *testing.T) {
	timeline := config.TimelineConfig{}
	collections := map[string]Collection{
		"songs": makeCollectionWithRows("songs", 3),
	}

	entries, err := ResolveTimeline(timeline, collections)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestResolveTimeline_SingleCollection_NoInterleave(t *testing.T) {
	timeline := config.TimelineConfig{
		Sequence: []config.SequenceEntry{
			{Collection: "songs"},
		},
	}
	collections := map[string]Collection{
		"songs": makeCollectionWithRows("songs", 4),
	}

	entries, err := ResolveTimeline(timeline, collections)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}
	for i, e := range entries {
		if e.Collection != "songs" {
			t.Errorf("[%d] expected collection=songs, got %q", i, e.Collection)
		}
		if e.Index != i+1 {
			t.Errorf("[%d] expected Index=%d, got %d", i, i+1, e.Index)
		}
		if e.Sequence != i+1 {
			t.Errorf("[%d] expected Sequence=%d, got %d", i, i+1, e.Sequence)
		}
	}
}

func TestResolveTimeline_CountLimit(t *testing.T) {
	timeline := config.TimelineConfig{
		Sequence: []config.SequenceEntry{
			{Collection: "songs", Count: 2},
		},
	}
	collections := map[string]Collection{
		"songs": makeCollectionWithRows("songs", 5),
	}

	entries, err := ResolveTimeline(timeline, collections)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestResolveTimeline_CountExceedsAvailable(t *testing.T) {
	timeline := config.TimelineConfig{
		Sequence: []config.SequenceEntry{
			{Collection: "songs", Count: 100},
		},
	}
	collections := map[string]Collection{
		"songs": makeCollectionWithRows("songs", 3),
	}

	entries, err := ResolveTimeline(timeline, collections)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 entries (all available), got %d", len(entries))
	}
}

func TestResolveTimeline_Interleave_Even(t *testing.T) {
	// 4 songs, 4 interstitials, every=1
	// Expected: song1, inter1, song2, inter2, song3, inter3, song4, inter4
	timeline := config.TimelineConfig{
		Sequence: []config.SequenceEntry{
			{
				Collection: "songs",
				Interleave: &config.InterleaveConfig{
					Collection: "interstitials",
					Every:      1,
				},
			},
		},
	}
	collections := map[string]Collection{
		"songs":        makeCollectionWithRows("songs", 4),
		"interstitials": makeCollectionWithRows("interstitials", 4),
	}

	entries, err := ResolveTimeline(timeline, collections)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 8 {
		t.Fatalf("expected 8 entries, got %d", len(entries))
	}

	expected := []struct {
		coll string
		idx  int
	}{
		{"songs", 1}, {"interstitials", 1},
		{"songs", 2}, {"interstitials", 2},
		{"songs", 3}, {"interstitials", 3},
		{"songs", 4}, {"interstitials", 4},
	}
	for i, e := range entries {
		if e.Collection != expected[i].coll {
			t.Errorf("[%d] expected collection=%q, got %q", i, expected[i].coll, e.Collection)
		}
		if e.Index != expected[i].idx {
			t.Errorf("[%d] expected Index=%d, got %d", i, expected[i].idx, e.Index)
		}
		if e.Sequence != i+1 {
			t.Errorf("[%d] expected Sequence=%d, got %d", i, i+1, e.Sequence)
		}
	}
}

func TestResolveTimeline_Interleave_Cycling(t *testing.T) {
	// 3 songs, 2 interstitials, every=1
	// Expected: song1, inter1, song2, inter2, song3, inter1 (cycles)
	timeline := config.TimelineConfig{
		Sequence: []config.SequenceEntry{
			{
				Collection: "songs",
				Interleave: &config.InterleaveConfig{
					Collection: "interstitials",
					Every:      1,
				},
			},
		},
	}
	collections := map[string]Collection{
		"songs":        makeCollectionWithRows("songs", 3),
		"interstitials": makeCollectionWithRows("interstitials", 2),
	}

	entries, err := ResolveTimeline(timeline, collections)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 6 {
		t.Fatalf("expected 6 entries, got %d", len(entries))
	}

	expected := []struct {
		coll string
		idx  int
	}{
		{"songs", 1}, {"interstitials", 1},
		{"songs", 2}, {"interstitials", 2},
		{"songs", 3}, {"interstitials", 1}, // cycles back
	}
	for i, e := range entries {
		if e.Collection != expected[i].coll {
			t.Errorf("[%d] expected collection=%q, got %q", i, expected[i].coll, e.Collection)
		}
		if e.Index != expected[i].idx {
			t.Errorf("[%d] expected Index=%d, got %d", i, expected[i].idx, e.Index)
		}
	}
}

func TestResolveTimeline_Interleave_Every2(t *testing.T) {
	// 6 songs, 3 interstitials, every=2
	// Expected: song1, song2, inter1, song3, song4, inter2, song5, song6, inter3
	timeline := config.TimelineConfig{
		Sequence: []config.SequenceEntry{
			{
				Collection: "songs",
				Interleave: &config.InterleaveConfig{
					Collection: "interstitials",
					Every:      2,
				},
			},
		},
	}
	collections := map[string]Collection{
		"songs":        makeCollectionWithRows("songs", 6),
		"interstitials": makeCollectionWithRows("interstitials", 3),
	}

	entries, err := ResolveTimeline(timeline, collections)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 9 {
		t.Fatalf("expected 9 entries, got %d", len(entries))
	}

	expected := []struct {
		coll string
		idx  int
	}{
		{"songs", 1}, {"songs", 2}, {"interstitials", 1},
		{"songs", 3}, {"songs", 4}, {"interstitials", 2},
		{"songs", 5}, {"songs", 6}, {"interstitials", 3},
	}
	for i, e := range entries {
		if e.Collection != expected[i].coll {
			t.Errorf("[%d] expected collection=%q, got %q", i, expected[i].coll, e.Collection)
		}
		if e.Index != expected[i].idx {
			t.Errorf("[%d] expected Index=%d, got %d", i, expected[i].idx, e.Index)
		}
		if e.Sequence != i+1 {
			t.Errorf("[%d] expected Sequence=%d, got %d", i, i+1, e.Sequence)
		}
	}
}

func TestResolveTimeline_MultipleSequenceEntries(t *testing.T) {
	// intro (2) → songs+interleave (3 songs, every=1) → outro (1)
	// Expected: intro1, intro2, song1, inter1, song2, inter2, song3, inter3, outro1
	timeline := config.TimelineConfig{
		Sequence: []config.SequenceEntry{
			{Collection: "intro"},
			{
				Collection: "songs",
				Interleave: &config.InterleaveConfig{
					Collection: "interstitials",
					Every:      1,
				},
			},
			{Collection: "outro"},
		},
	}
	collections := map[string]Collection{
		"intro":        makeCollectionWithRows("intro", 2),
		"songs":        makeCollectionWithRows("songs", 3),
		"interstitials": makeCollectionWithRows("interstitials", 3),
		"outro":        makeCollectionWithRows("outro", 1),
	}

	entries, err := ResolveTimeline(timeline, collections)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 9 {
		t.Fatalf("expected 9 entries, got %d", len(entries))
	}

	expected := []struct {
		coll string
		idx  int
		seq  int
	}{
		{"intro", 1, 1},
		{"intro", 2, 2},
		{"songs", 1, 3},
		{"interstitials", 1, 4},
		{"songs", 2, 5},
		{"interstitials", 2, 6},
		{"songs", 3, 7},
		{"interstitials", 3, 8},
		{"outro", 1, 9},
	}
	for i, e := range entries {
		if e.Collection != expected[i].coll {
			t.Errorf("[%d] expected collection=%q, got %q", i, expected[i].coll, e.Collection)
		}
		if e.Index != expected[i].idx {
			t.Errorf("[%d] expected Index=%d, got %d", i, expected[i].idx, e.Index)
		}
		if e.Sequence != expected[i].seq {
			t.Errorf("[%d] expected Sequence=%d, got %d", i, expected[i].seq, e.Sequence)
		}
	}
}

func TestResolveTimeline_MissingPrimaryCollection(t *testing.T) {
	timeline := config.TimelineConfig{
		Sequence: []config.SequenceEntry{
			{Collection: "missing"},
		},
	}
	collections := map[string]Collection{}

	_, err := ResolveTimeline(timeline, collections)
	if err == nil {
		t.Fatal("expected error for missing primary collection, got nil")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("expected error to contain collection name %q, got: %v", "missing", err)
	}
}

func TestResolveTimeline_MissingInterleaveCollection(t *testing.T) {
	timeline := config.TimelineConfig{
		Sequence: []config.SequenceEntry{
			{
				Collection: "songs",
				Interleave: &config.InterleaveConfig{
					Collection: "ghost",
					Every:      1,
				},
			},
		},
	}
	collections := map[string]Collection{
		"songs": makeCollectionWithRows("songs", 3),
	}

	_, err := ResolveTimeline(timeline, collections)
	if err == nil {
		t.Fatal("expected error for missing interleave collection, got nil")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("expected error to contain collection name %q, got: %v", "ghost", err)
	}
}

func TestResolveTimeline_EmptyCollection(t *testing.T) {
	timeline := config.TimelineConfig{
		Sequence: []config.SequenceEntry{
			{Collection: "empty"},
			{Collection: "songs"},
		},
	}
	collections := map[string]Collection{
		"empty": makeCollectionWithRows("empty", 0),
		"songs": makeCollectionWithRows("songs", 2),
	}

	entries, err := ResolveTimeline(timeline, collections)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// empty collection contributes 0 entries; songs contributes 2
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Sequence != 1 || entries[1].Sequence != 2 {
		t.Errorf("unexpected sequence numbers: %v", entries)
	}
}
