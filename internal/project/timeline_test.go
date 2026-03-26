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

func TestResolveTimeline(t *testing.T) {
	type entry struct {
		coll       string
		idx        int
		seq        int    // 0 = don't check
		sourceFile string // non-empty = expect SourceFile set to this value
	}
	tests := []struct {
		name        string
		timeline    config.TimelineConfig
		collections map[string]Collection
		want        []entry
		wantErr     string // non-empty = expect error containing this substring
	}{
		{
			name:     "empty sequence",
			timeline: config.TimelineConfig{},
			collections: map[string]Collection{
				"songs": makeCollectionWithRows("songs", 3),
			},
			want: []entry{},
		},
		{
			name: "single collection no interleave",
			timeline: config.TimelineConfig{
				Sequence: []config.SequenceEntry{
					{Collection: "songs"},
				},
			},
			collections: map[string]Collection{
				"songs": makeCollectionWithRows("songs", 4),
			},
			want: []entry{
				{coll: "songs", idx: 1, seq: 1},
				{coll: "songs", idx: 2, seq: 2},
				{coll: "songs", idx: 3, seq: 3},
				{coll: "songs", idx: 4, seq: 4},
			},
		},
		{
			name: "count limit",
			timeline: config.TimelineConfig{
				Sequence: []config.SequenceEntry{
					{Collection: "songs", Count: 2},
				},
			},
			collections: map[string]Collection{
				"songs": makeCollectionWithRows("songs", 5),
			},
			want: []entry{
				{coll: "songs", idx: 1},
				{coll: "songs", idx: 2},
			},
		},
		{
			name: "count exceeds available",
			timeline: config.TimelineConfig{
				Sequence: []config.SequenceEntry{
					{Collection: "songs", Count: 100},
				},
			},
			collections: map[string]Collection{
				"songs": makeCollectionWithRows("songs", 3),
			},
			want: []entry{
				{coll: "songs", idx: 1},
				{coll: "songs", idx: 2},
				{coll: "songs", idx: 3},
			},
		},
		{
			name: "interleave every 1 equal count",
			// 4 songs, 4 interstitials, every=1
			// Expected: song1, inter1, song2, inter2, song3, inter3, song4, inter4
			timeline: config.TimelineConfig{
				Sequence: []config.SequenceEntry{
					{
						Collection: "songs",
						Interleave: &config.InterleaveConfig{
							Collection: "interstitials",
							Every:      1,
						},
					},
				},
			},
			collections: map[string]Collection{
				"songs":         makeCollectionWithRows("songs", 4),
				"interstitials": makeCollectionWithRows("interstitials", 4),
			},
			want: []entry{
				{coll: "songs", idx: 1, seq: 1}, {coll: "interstitials", idx: 1, seq: 2},
				{coll: "songs", idx: 2, seq: 3}, {coll: "interstitials", idx: 2, seq: 4},
				{coll: "songs", idx: 3, seq: 5}, {coll: "interstitials", idx: 3, seq: 6},
				{coll: "songs", idx: 4, seq: 7}, {coll: "interstitials", idx: 4, seq: 8},
			},
		},
		{
			name: "interleave cycling fewer interstitials than insertion points",
			// 3 songs, 2 interstitials, every=1
			// Expected: song1, inter1, song2, inter2, song3, inter1 (cycles)
			timeline: config.TimelineConfig{
				Sequence: []config.SequenceEntry{
					{
						Collection: "songs",
						Interleave: &config.InterleaveConfig{
							Collection: "interstitials",
							Every:      1,
						},
					},
				},
			},
			collections: map[string]Collection{
				"songs":         makeCollectionWithRows("songs", 3),
				"interstitials": makeCollectionWithRows("interstitials", 2),
			},
			want: []entry{
				{coll: "songs", idx: 1}, {coll: "interstitials", idx: 1},
				{coll: "songs", idx: 2}, {coll: "interstitials", idx: 2},
				{coll: "songs", idx: 3}, {coll: "interstitials", idx: 1},
			},
		},
		{
			name: "interleave every 2",
			// 6 songs, 3 interstitials, every=2
			// Expected: song1, song2, inter1, song3, song4, inter2, song5, song6, inter3
			timeline: config.TimelineConfig{
				Sequence: []config.SequenceEntry{
					{
						Collection: "songs",
						Interleave: &config.InterleaveConfig{
							Collection: "interstitials",
							Every:      2,
						},
					},
				},
			},
			collections: map[string]Collection{
				"songs":         makeCollectionWithRows("songs", 6),
				"interstitials": makeCollectionWithRows("interstitials", 3),
			},
			want: []entry{
				{coll: "songs", idx: 1, seq: 1}, {coll: "songs", idx: 2, seq: 2}, {coll: "interstitials", idx: 1, seq: 3},
				{coll: "songs", idx: 3, seq: 4}, {coll: "songs", idx: 4, seq: 5}, {coll: "interstitials", idx: 2, seq: 6},
				{coll: "songs", idx: 5, seq: 7}, {coll: "songs", idx: 6, seq: 8}, {coll: "interstitials", idx: 3, seq: 9},
			},
		},
		{
			name: "multiple sequence entries intro songs interleave outro",
			// intro (2) → songs+interleave (3 songs, every=1) → outro (1)
			// Expected: intro1, intro2, song1, inter1, song2, inter2, song3, inter3, outro1
			timeline: config.TimelineConfig{
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
			},
			collections: map[string]Collection{
				"intro":         makeCollectionWithRows("intro", 2),
				"songs":         makeCollectionWithRows("songs", 3),
				"interstitials": makeCollectionWithRows("interstitials", 3),
				"outro":         makeCollectionWithRows("outro", 1),
			},
			want: []entry{
				{coll: "intro", idx: 1, seq: 1},
				{coll: "intro", idx: 2, seq: 2},
				{coll: "songs", idx: 1, seq: 3},
				{coll: "interstitials", idx: 1, seq: 4},
				{coll: "songs", idx: 2, seq: 5},
				{coll: "interstitials", idx: 2, seq: 6},
				{coll: "songs", idx: 3, seq: 7},
				{coll: "interstitials", idx: 3, seq: 8},
				{coll: "outro", idx: 1, seq: 9},
			},
		},
		{
			name: "intro songs outro no interleave",
			// intro (1) → songs (2) → outro (1), no interleave
			// Expected: intro1, song1, song2, outro1 with seq 1-4
			timeline: config.TimelineConfig{
				Sequence: []config.SequenceEntry{
					{Collection: "intro"},
					{Collection: "songs"},
					{Collection: "outro"},
				},
			},
			collections: map[string]Collection{
				"intro": makeCollectionWithRows("intro", 1),
				"songs": makeCollectionWithRows("songs", 2),
				"outro": makeCollectionWithRows("outro", 1),
			},
			want: []entry{
				{coll: "intro", idx: 1, seq: 1},
				{coll: "songs", idx: 1, seq: 2},
				{coll: "songs", idx: 2, seq: 3},
				{coll: "outro", idx: 1, seq: 4},
			},
		},
		{
			name: "empty collection in sequence",
			timeline: config.TimelineConfig{
				Sequence: []config.SequenceEntry{
					{Collection: "empty"},
					{Collection: "songs"},
				},
			},
			collections: map[string]Collection{
				"empty": makeCollectionWithRows("empty", 0),
				"songs": makeCollectionWithRows("songs", 2),
			},
			want: []entry{
				{coll: "songs", idx: 1, seq: 1},
				{coll: "songs", idx: 2, seq: 2},
			},
		},
		{
			name: "missing primary collection returns error",
			timeline: config.TimelineConfig{
				Sequence: []config.SequenceEntry{
					{Collection: "missing"},
				},
			},
			collections: map[string]Collection{},
			wantErr:     "missing",
		},
		{
			name: "missing interleave collection returns error",
			timeline: config.TimelineConfig{
				Sequence: []config.SequenceEntry{
					{
						Collection: "songs",
						Interleave: &config.InterleaveConfig{
							Collection: "ghost",
							Every:      1,
						},
					},
				},
			},
			collections: map[string]Collection{
				"songs": makeCollectionWithRows("songs", 3),
			},
			wantErr: "ghost",
		},
		{
			name: "stateful cursor two halves",
			// songs appears twice: first count:2, then no count on 5-row collection
			// Expected: rows 1-2, then rows 3-5 (cursor picks up at 2)
			timeline: config.TimelineConfig{
				Sequence: []config.SequenceEntry{
					{Collection: "songs", Count: 2},
					{Collection: "songs"},
				},
			},
			collections: map[string]Collection{
				"songs": makeCollectionWithRows("songs", 5),
			},
			want: []entry{
				{coll: "songs", idx: 1, seq: 1},
				{coll: "songs", idx: 2, seq: 2},
				{coll: "songs", idx: 3, seq: 3},
				{coll: "songs", idx: 4, seq: 4},
				{coll: "songs", idx: 5, seq: 5},
			},
		},
		{
			name: "stateful cursor both halves with count",
			// songs appears twice, each count:2 on 4-row collection
			// Expected: rows 1-2, then rows 3-4
			timeline: config.TimelineConfig{
				Sequence: []config.SequenceEntry{
					{Collection: "songs", Count: 2},
					{Collection: "songs", Count: 2},
				},
			},
			collections: map[string]Collection{
				"songs": makeCollectionWithRows("songs", 4),
			},
			want: []entry{
				{coll: "songs", idx: 1, seq: 1},
				{coll: "songs", idx: 2, seq: 2},
				{coll: "songs", idx: 3, seq: 3},
				{coll: "songs", idx: 4, seq: 4},
			},
		},
		{
			name: "stateful cursor collection exhausted skips silently",
			// songs count:3 on 3-row collection, then songs again (exhausted) → only first 3 entries
			timeline: config.TimelineConfig{
				Sequence: []config.SequenceEntry{
					{Collection: "songs", Count: 3},
					{Collection: "songs"},
				},
			},
			collections: map[string]Collection{
				"songs": makeCollectionWithRows("songs", 3),
			},
			want: []entry{
				{coll: "songs", idx: 1, seq: 1},
				{coll: "songs", idx: 2, seq: 2},
				{coll: "songs", idx: 3, seq: 3},
			},
		},
		{
			name: "inline file entry",
			timeline: config.TimelineConfig{
				Sequence: []config.SequenceEntry{
					{File: "videos/intro.mp4"},
				},
			},
			collections: map[string]Collection{},
			want: []entry{
				{sourceFile: "videos/intro.mp4", seq: 1},
			},
		},
		{
			name: "inline file between collection entries",
			// intro file, songs (2 rows), outro file
			timeline: config.TimelineConfig{
				Sequence: []config.SequenceEntry{
					{File: "intro.mp4"},
					{Collection: "songs"},
					{File: "outro.mp4"},
				},
			},
			collections: map[string]Collection{
				"songs": makeCollectionWithRows("songs", 2),
			},
			want: []entry{
				{sourceFile: "intro.mp4", seq: 1},
				{coll: "songs", idx: 1, seq: 2},
				{coll: "songs", idx: 2, seq: 3},
				{sourceFile: "outro.mp4", seq: 4},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveTimeline(tc.timeline, tc.collections)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("len=%d, want %d; got %v", len(got), len(tc.want), got)
			}
			for i, w := range tc.want {
				if w.sourceFile != "" {
					if got[i].SourceFile != w.sourceFile {
						t.Errorf("[%d] SourceFile=%q, want %q", i, got[i].SourceFile, w.sourceFile)
					}
				} else {
					if got[i].Collection != w.coll {
						t.Errorf("[%d] collection=%q, want %q", i, got[i].Collection, w.coll)
					}
					if got[i].Index != w.idx {
						t.Errorf("[%d] Index=%d, want %d", i, got[i].Index, w.idx)
					}
				}
				if w.seq != 0 && got[i].Sequence != w.seq {
					t.Errorf("[%d] Sequence=%d, want %d", i, got[i].Sequence, w.seq)
				}
			}
		})
	}
}
