package render

import (
	"strings"
	"testing"

	"powerhour/internal/config"
	"powerhour/internal/project"
	"powerhour/pkg/csvplan"
)

func indexTestRow() csvplan.Row {
	return csvplan.Row{
		Index:           4,
		RowID:           "aaa111",
		Title:           "Test Song",
		Artist:          "Test Artist",
		StartRaw:        "0:30",
		DurationSeconds: 60,
		Link:            "https://example.com/v",
	}
}

func TestOverlaysDependOnIndex(t *testing.T) {
	cases := []struct {
		name     string
		overlays []config.OverlayEntry
		want     bool
	}{
		{"no overlays", nil, false},
		{"song-info burns in a number badge", []config.OverlayEntry{{Type: "song-info"}}, true},
		{"drink renders no number", []config.OverlayEntry{{Type: "drink"}}, false},
		{
			"song-info with the badge turned off",
			[]config.OverlayEntry{{Type: "song-info", Options: map[string]string{"show_number": "false"}}},
			false,
		},
		{
			"a custom filter that interpolates the token",
			[]config.OverlayEntry{{Type: "custom", Filters: []string{"drawtext=text='{index}'"}}},
			true,
		},
		{
			"a custom filter that does not",
			[]config.OverlayEntry{{Type: "custom", Filters: []string{"drawtext=text='{title}'"}}},
			false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := OverlaysDependOnIndex(tc.overlays, indexTestRow(), 60); got != tc.want {
				t.Errorf("OverlaysDependOnIndex = %v, want %v", got, tc.want)
			}
		})
	}
}

// The whole point of the conditional input: a clip whose overlays render the
// index hashes differently at a different position; a clip whose overlays do
// not must hash identically, or nothing is ever renamed.
func TestSegmentInputHashDependsOnPositionOnlyWhenOverlaysDo(t *testing.T) {
	build := func(position int, overlays []config.OverlayEntry) Segment {
		return Segment{
			Clip: project.Clip{
				Sequence:         position,
				PlaybackPosition: position,
				ClipType:         "songs",
				TypeIndex:        position,
				DurationSeconds:  60,
				Row:              indexTestRow(),
			},
			Overlays: overlays,
		}
	}

	song := []config.OverlayEntry{{Type: "song-info"}}
	if SegmentInputHash(build(1, song), "$INDEX") == SegmentInputHash(build(2, song), "$INDEX") {
		t.Error("a song's hash did not change when its playback position did; its badge would go stale")
	}

	drink := []config.OverlayEntry{{Type: "drink"}}
	if SegmentInputHash(build(1, drink), "$INDEX") != SegmentInputHash(build(2, drink), "$INDEX") {
		t.Error("an interstitial's hash changed with its position; it would be re-encoded for no visible reason")
	}
}

// EffectiveRow is what makes $INDEX and {index} mean playback position.
func TestEffectiveRowPrefersPlaybackPosition(t *testing.T) {
	clip := project.Clip{Row: indexTestRow(), PlaybackPosition: 17}
	if got := EffectiveRow(clip).Index; got != 17 {
		t.Errorf("Index = %d, want the playback position 17", got)
	}

	clip.PlaybackPosition = 0
	if got := EffectiveRow(clip).Index; got != 4 {
		t.Errorf("Index = %d, want the plan row index 4 when no slot annotated the clip", got)
	}
}

func TestSegmentBaseNameUsesPlaybackPosition(t *testing.T) {
	seg := Segment{Clip: project.Clip{
		Row:              indexTestRow(),
		PlaybackPosition: 7,
		ClipType:         "songs",
		TypeIndex:        4,
		DurationSeconds:  60,
	}}
	got := SegmentBaseName("$INDEX_PAD3_$SAFE_TITLE", seg)
	if !strings.HasPrefix(got, "007_") {
		t.Errorf("SegmentBaseName = %q, want it to lead with the playback position 007", got)
	}
}
